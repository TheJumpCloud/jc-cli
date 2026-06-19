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
