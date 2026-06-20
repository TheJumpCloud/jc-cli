package apple_mdm

import (
	"encoding/base64"
	"strings"
	"testing"
)

// firewallPlist is the unsigned plist a "Firewall — enforce" policy
// would carry. Mirrors the wire shape JumpCloud's Admin Portal
// produces and the emitter generates in plist.go.
const firewallPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>PayloadType</key><string>Configuration</string>
  <key>PayloadVersion</key><integer>1</integer>
  <key>PayloadUUID</key><string>00000000-0000-4000-8000-000000000001</string>
  <key>PayloadIdentifier</key><string>jc.00000000-0000-4000-8000-000000000002</string>
  <key>PayloadDisplayName</key><string>Firewall — enforce</string>
  <key>PayloadRemovalDisallowed</key><true/>
  <key>PayloadContent</key>
  <array>
    <dict>
      <key>PayloadType</key><string>com.apple.security.firewall</string>
      <key>PayloadVersion</key><integer>1</integer>
      <key>PayloadUUID</key><string>00000000-0000-4000-8000-000000000003</string>
      <key>PayloadIdentifier</key><string>jc.apple.security.firewall.00000000-0000-4000-8000-000000000004</string>
      <key>PayloadDisplayName</key><string>Firewall</string>
      <key>EnableFirewall</key><true/>
      <key>EnableStealthMode</key><true/>
      <key>BlockAllIncoming</key><false/>
      <key>EnableLogging</key><true/>
      <key>LoggingOption</key><string>detail</string>
    </dict>
  </array>
</dict>
</plist>`

func TestDecodeCustomMDMPolicy_SinglePayload(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(firewallPlist))
	rawJSON := `{
		"id": "abc1234567890",
		"name": "test-jc-firewall-DELETE-ME",
		"values": [
			{"configFieldID":"pid","configFieldName":"payload","value":"` + encoded + `"},
			{"configFieldID":"rdid","configFieldName":"redispatchPolicy","value":true}
		]
	}`
	d, err := DecodeCustomMDMPolicy([]byte(rawJSON))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if d.PolicyID != "abc1234567890" {
		t.Errorf("PolicyID = %q", d.PolicyID)
	}
	if d.PolicyName != "test-jc-firewall-DELETE-ME" {
		t.Errorf("PolicyName = %q", d.PolicyName)
	}
	if d.IsMulti {
		t.Error("single-payload policy should not be marked IsMulti")
	}
	if !d.Redispatch {
		t.Error("redispatch should be true")
	}
	if !d.RemovalDisallowed {
		t.Error("removalDisallowed should be true (from envelope)")
	}
	if d.PayloadType != "com.apple.security.firewall" {
		t.Errorf("PayloadType = %q", d.PayloadType)
	}
	if d.Schema.Type != "com.apple.security.firewall" {
		t.Errorf("Schema not matched against catalog (got Type=%q)", d.Schema.Type)
	}

	// Values must NOT contain the reserved Payload* keys — those
	// belong to the emitter and shouldn't surface in the form.
	for _, k := range []string{"PayloadType", "PayloadUUID", "PayloadVersion",
		"PayloadIdentifier", "PayloadDisplayName"} {
		if _, ok := d.Values[k]; ok {
			t.Errorf("Values still contains reserved key %q", k)
		}
	}
	// Values must contain the inner schema keys.
	for k, want := range map[string]any{
		"EnableFirewall":    true,
		"EnableStealthMode": true,
		"BlockAllIncoming":  false,
		"EnableLogging":     true,
		"LoggingOption":     "detail",
	} {
		got := d.Values[k]
		// plist library decodes booleans as bool; integers as uint64;
		// strings as string. Compare loosely on bool/string and
		// allow uint64 for integer types.
		switch w := want.(type) {
		case bool:
			if got != w {
				t.Errorf("Values[%q] = %v, want %v", k, got, w)
			}
		case string:
			if got != w {
				t.Errorf("Values[%q] = %v, want %v", k, got, w)
			}
		}
	}
}

func TestOSFamilyFromTemplateName(t *testing.T) {
	// Bugbot PR #54 review: silently mapping every edit to darwin
	// reassigned iOS-family policies on save. The mapping must lift
	// the family suffix verbatim and reject malformed names.
	tests := []struct {
		in, want string
	}{
		{"custom_mdm_profile_darwin", "darwin"},
		{"custom_mdm_profile_iphone", "iphone"},
		{"custom_mdm_profile_tvos", "tvos"},
		{"some_other_template", ""},
		{"custom_mdm_profile_", ""}, // empty suffix → empty
		{"", ""},
	}
	for _, tc := range tests {
		if got := OSFamilyFromTemplateName(tc.in); got != tc.want {
			t.Errorf("OSFamilyFromTemplateName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDecodeCustomMDMPolicy_CapturesTemplateName(t *testing.T) {
	// Bugbot PR #54 finding #3: edit was always darwin because the
	// decoded policy didn't carry the template family forward.
	// Verify the decoder now does.
	plistB64 := base64.StdEncoding.EncodeToString([]byte(firewallPlist))
	rawJSON := `{
		"id": "abc",
		"name": "iOS test",
		"template": {"id": "tid", "name": "custom_mdm_profile_iphone"},
		"values": [
			{"configFieldID":"pid","configFieldName":"payload","value":"` + plistB64 + `"}
		]
	}`
	d, err := DecodeCustomMDMPolicy([]byte(rawJSON))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.TemplateName != "custom_mdm_profile_iphone" {
		t.Errorf("TemplateName = %q", d.TemplateName)
	}
	if OSFamilyFromTemplateName(d.TemplateName) != "iphone" {
		t.Errorf("OS family derivation broken: got %q", OSFamilyFromTemplateName(d.TemplateName))
	}
}

func TestDecodeCustomMDMPolicy_MultiPayloadDetected(t *testing.T) {
	multi := strings.Replace(firewallPlist,
		"  <key>PayloadContent</key>\n  <array>\n    <dict>",
		"  <key>PayloadContent</key>\n  <array>\n    <dict><key>PayloadType</key><string>com.apple.AdLib</string></dict>\n    <dict>",
		1)
	encoded := base64.StdEncoding.EncodeToString([]byte(multi))
	rawJSON := `{
		"id": "xxx",
		"name": "CIS-style multi-payload",
		"values": [
			{"configFieldID":"pid","configFieldName":"payload","value":"` + encoded + `"}
		]
	}`
	d, err := DecodeCustomMDMPolicy([]byte(rawJSON))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !d.IsMulti {
		t.Error("multi-payload policy should be marked IsMulti")
	}
	if d.Schema.Type != "" {
		t.Errorf("multi-payload should leave Schema empty, got %q", d.Schema.Type)
	}
}

func TestDecodeCustomMDMPolicy_MissingPayloadValuesEntry(t *testing.T) {
	rawJSON := `{
		"id": "xxx",
		"name": "broken",
		"values": [
			{"configFieldID":"rdid","configFieldName":"redispatchPolicy","value":true}
		]
	}`
	_, err := DecodeCustomMDMPolicy([]byte(rawJSON))
	if err == nil {
		t.Error("expected error when payload value is missing")
	}
}

func TestPickBestSchemaVariant_PrefersKeyOverlap(t *testing.T) {
	// Bugbot PR #54 re-review: com.apple.MCX has 6 catalog variants
	// (EnergySaver, FileVault2, TimeServer, WiFi, Accounts,
	// Mobility) all sharing one PayloadType. Pre-fix the decoder
	// used ByType (first-wins); a policy that targeted MCX(WiFi)
	// could decode against the EnergySaver variant's schema, leading
	// to wrong fields and dropped keys on save. The picker now uses
	// inner-values' keys to disambiguate.
	wifiVariant := Payload{
		ID:   "com.apple.MCX(WiFi)",
		Type: "com.apple.MCX",
		Keys: []Key{
			{Name: "WiFiPreferences", Type: "dictionary"},
		},
	}
	energyVariant := Payload{
		ID:   "com.apple.MCX(EnergySaver)",
		Type: "com.apple.MCX",
		Keys: []Key{
			{Name: "com.apple.EnergySaver.desktop.ACPower", Type: "dictionary"},
		},
	}
	variants := []Payload{energyVariant, wifiVariant}

	// Inner values that look like a WiFi policy.
	wifiValues := map[string]any{
		"WiFiPreferences": map[string]any{"SSID": "Corp"},
	}
	got := pickBestSchemaVariant(variants, wifiValues)
	if got.ID != "com.apple.MCX(WiFi)" {
		t.Errorf("got %q, want com.apple.MCX(WiFi) (key-overlap should pick wifi variant)", got.ID)
	}

	// Inner values that look like an EnergySaver policy.
	energyValues := map[string]any{
		"com.apple.EnergySaver.desktop.ACPower": map[string]any{},
	}
	got = pickBestSchemaVariant(variants, energyValues)
	if got.ID != "com.apple.MCX(EnergySaver)" {
		t.Errorf("got %q, want com.apple.MCX(EnergySaver)", got.ID)
	}
}

func TestPickBestSchemaVariant_HandlesEmptyAndSingle(t *testing.T) {
	if got := pickBestSchemaVariant(nil, nil); got.Type != "" {
		t.Error("empty variants should return zero Payload")
	}
	single := Payload{ID: "only", Type: "x"}
	if got := pickBestSchemaVariant([]Payload{single}, nil); got.ID != "only" {
		t.Error("single-variant slice should return the only entry")
	}
}

func TestDecodeCustomMDMPolicy_HandlesOlderTemplateWithoutRedispatch(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(firewallPlist))
	rawJSON := `{
		"id": "abc",
		"name": "older policy",
		"values": [
			{"configFieldID":"pid","configFieldName":"payload","value":"` + encoded + `"}
		]
	}`
	d, err := DecodeCustomMDMPolicy([]byte(rawJSON))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Redispatch {
		t.Error("redispatch should default to false when the values entry is absent")
	}
	// The decode still produces a usable shape.
	if d.PayloadType != "com.apple.security.firewall" {
		t.Errorf("PayloadType = %q", d.PayloadType)
	}
}
