package apple_mdm

import (
	"encoding/json"
	"strings"
	"testing"
)

// Fixtures are live: the IOS setting and the discovery-URL answer as observed
// on 2026-10-28.
const liveIOSSetting = `{"objectId":"690bf92bc4816d00015b8c41","adeTokenObjectId":"6a7e1bb88cf0be0001a4e3cd",
"deviceManagerObjectId":"690b6c939c0bf5000100039a","osFamily":"IOS",
"ade":{"welcomeScreen":{"title":"Welcome","paragraph":"In just a few steps, you will be working securely from your Apple device.","button":"continue"},
"setupOptions":[],"enableZeroTouchEnrollment":false,"defaultDeviceGroupObjectIds":[]},
"organizationObjectId":"5ec71e8e96bfda0611fc6c5b"}`

func ptr[T any](v T) *T { return &v }

func maskOf(t *testing.T, c ADESettingsChange) ([]string, map[string]any) {
	t.Helper()
	b, err := c.Body()
	if err != nil {
		t.Fatalf("Body: %v", err)
	}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return b.PatchMask.Fields, m
}

// The central guard. A masked field with no value is RESET by the service
// (MaskedButAbsentIsReset), so a field the operator did not name must be
// absent from the mask — not just from the body.
func TestADESettingsChange_MasksOnlyWhatWasNamed(t *testing.T) {
	mask, body := maskOf(t, ADESettingsChange{WelcomeButton: ptr("Go")})
	if len(mask) != 1 || mask[0] != MaskWelcomeButton {
		t.Fatalf("mask = %v, want only %s", mask, MaskWelcomeButton)
	}
	for _, k := range []string{"enableZeroTouchEnrollment", "setupOptions", "defaultDeviceGroupObjectIds"} {
		if _, ok := body[k]; ok {
			t.Errorf("%s is in the body although nobody asked to change it: %v", k, body)
		}
	}
	ws := body["welcomeScreen"].(map[string]any)
	if len(ws) != 1 || ws["button"] != "Go" {
		t.Errorf("welcomeScreen = %v, want only the button", ws)
	}
}

// Every masked field carries its value, and every value is masked — checked
// across all fields at once so a new field cannot be wired to only one side.
func TestADESettingsChange_MaskAndBodyAgree(t *testing.T) {
	mask, body := maskOf(t, ADESettingsChange{
		ZeroTouch:       ptr(false),
		WelcomeTitle:    ptr("Hi"),
		WelcomeText:     ptr("Set up"),
		WelcomeButton:   ptr("Go"),
		SetupOptions:    ptr([]string{"siri_setup"}),
		DefaultGroupIDs: ptr([]string{"609384d91f24757289141557"}),
	})
	want := []string{MaskZeroTouch, MaskWelcomeTitle, MaskWelcomeText, MaskWelcomeButton, MaskSetupOptions, MaskDefaultGroupIDs}
	if strings.Join(mask, ",") != strings.Join(want, ",") {
		t.Errorf("mask = %v, want %v", mask, want)
	}
	// false must be SENT, not omitted: omitempty on a plain bool would drop it.
	if v, ok := body["enableZeroTouchEnrollment"]; !ok || v != false {
		t.Errorf("zero-touch false must be on the wire, got %v (present=%v)", v, ok)
	}
	if got := body["setupOptions"].([]any); len(got) != 1 || got[0] != "SIRI_SETUP" {
		t.Errorf("setup options not normalised: %v", got)
	}
}

func TestADESettingsChange_ResetMasksWithoutAValue(t *testing.T) {
	mask, body := maskOf(t, ADESettingsChange{ResetTitle: true})
	if len(mask) != 1 || mask[0] != MaskWelcomeTitle {
		t.Fatalf("mask = %v", mask)
	}
	if _, ok := body["welcomeScreen"]; ok {
		t.Errorf("a reset sends no welcome value, got %v", body)
	}
}

func TestADESettingsChange_ClearingAListMasksIt(t *testing.T) {
	mask, body := maskOf(t, ADESettingsChange{SetupOptions: ptr([]string{}), DefaultGroupIDs: ptr([]string{})})
	if strings.Join(mask, ",") != MaskSetupOptions+","+MaskDefaultGroupIDs {
		t.Errorf("mask = %v", mask)
	}
	// Empty and absent are the same to the service once masked — both clear —
	// so the empty list may be omitted from the body. What matters is the mask.
	_ = body
}

func TestADESettingsChange_Refusals(t *testing.T) {
	long := strings.Repeat("x", WelcomeTextMax+1)
	cases := map[string]struct {
		c    ADESettingsChange
		want string
	}{
		"nothing":       {ADESettingsChange{}, "nothing to change"},
		"blank title":   {ADESettingsChange{WelcomeTitle: ptr("  ")}, "cannot be blank"},
		"set and reset": {ADESettingsChange{WelcomeButton: ptr("Go"), ResetButton: true}, "both set and reset"},
		"too long":      {ADESettingsChange{WelcomeText: ptr(long)}, "limit is 255"},
		"UNKNOWN":       {ADESettingsChange{SetupOptions: ptr([]string{"unknown"})}, "500"},
		"typo option":   {ADESettingsChange{SetupOptions: ptr([]string{"SIRI"})}, "not a setup-assistant option"},
		"bad group":     {ADESettingsChange{DefaultGroupIDs: ptr([]string{"Engineering"})}, "not a JumpCloud object id"},
		"traversal":     {ADESettingsChange{DefaultGroupIDs: ptr([]string{"../../x"})}, "not a JumpCloud object id"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tc.c.Body()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestSetupOptionsExcludeUnknown(t *testing.T) {
	for _, o := range SetupOptions {
		if o == "UNKNOWN" {
			t.Fatal("UNKNOWN answers 500 and must not be offered")
		}
	}
	if len(SetupOptions) != 44 {
		t.Errorf("SetupOptions has %d entries; the spec enum has 45 including UNKNOWN", len(SetupOptions))
	}
}

func TestEffectsShowBeforeAndAfterInsideThePlanBox(t *testing.T) {
	cur, err := ParseADESettingValues(json.RawMessage(liveIOSSetting))
	if err != nil {
		t.Fatal(err)
	}
	effects := ADESettingsChange{
		ZeroTouch:       ptr(true),
		WelcomeText:     ptr("A much longer paragraph than fits in the box"),
		ResetButton:     true,
		SetupOptions:    ptr([]string{"SIRI_SETUP", "FILEVAULT"}),
		DefaultGroupIDs: ptr([]string{"609384d91f24757289141557"}),
	}.Effects(cur)
	joined := strings.Join(effects, "\n")
	for _, want := range []string{"zero-touch: false → true", "→ stock text", "setup options: 0 → 2", "default groups: 0 → 1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("effects missing %q:\n%s", want, joined)
		}
	}
	for _, e := range effects {
		// plan.RenderHuman pads effects to 46 columns and does not wrap.
		if n := len([]rune(e)); n > 46 {
			t.Errorf("effect %q is %d columns, over the plan box's 46", e, n)
		}
	}
}

func TestParseADESettingValuesRefusesAMissingADE(t *testing.T) {
	if _, err := ParseADESettingValues(json.RawMessage(`{"osFamily":"IOS"}`)); err == nil {
		t.Error("a setting without \"ade\" must not read as all-defaults")
	}
}

func TestParseServiceDiscoveryURL(t *testing.T) {
	url, err := ParseServiceDiscoveryURL(json.RawMessage(
		`{"serviceDiscoveryUrl":"https://apple.mdm.jumpcloud.com/account-driven-service-discovery/5ec71e8e96bfda0611fc6c5b"}`))
	if err != nil || !strings.HasSuffix(url, "/5ec71e8e96bfda0611fc6c5b") {
		t.Errorf("url = %q, err = %v", url, err)
	}
	// The disable answer is {} — it must not parse as an enable.
	if _, err := ParseServiceDiscoveryURL(json.RawMessage(`{}`)); err == nil {
		t.Error("an empty answer carries no URL and must error")
	}
}

func TestWriteEndpoints(t *testing.T) {
	cases := map[string]string{
		ServiceDiscoveryURLEndpoint("690b6c939c0bf5000100039a"): "/applemdms/690b6c939c0bf5000100039a/service-discovery-url",
		RefreshABMDevicesEndpoint("6a7e1bb88cf0be0001a4e3cd"):   "/applemdms/ade_tokens/6a7e1bb88cf0be0001a4e3cd/refresh_abm_devices",
		DeviceGroupEndpoint("609384d91f24757289141557"):         "/systemgroups/609384d91f24757289141557",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

func TestUnappliedNamesWhatDidNotTake(t *testing.T) {
	after, err := ParseADESettingValues(json.RawMessage(liveIOSSetting))
	if err != nil {
		t.Fatal(err)
	}
	// Everything asked for is already the live value: nothing missing.
	same := ADESettingsChange{ZeroTouch: ptr(false), WelcomeButton: ptr("continue"),
		SetupOptions: ptr([]string{}), DefaultGroupIDs: ptr([]string{})}
	if miss := same.Unapplied(after); len(miss) != 0 {
		t.Errorf("nothing should be missing, got %v", miss)
	}
	// The shape of a silently dropped write: 200, and the old values back.
	dropped := ADESettingsChange{ZeroTouch: ptr(true), WelcomeTitle: ptr("Hi"),
		SetupOptions: ptr([]string{"SIRI_SETUP"}), DefaultGroupIDs: ptr([]string{"609384d91f24757289141557"})}
	got := strings.Join(dropped.Unapplied(after), ",")
	if got != "zero-touch,welcome title,setup options,default groups" {
		t.Errorf("Unapplied = %s", got)
	}
	// A reset is the service's to fill in, so it is never reported.
	if miss := (ADESettingsChange{ResetTitle: true}).Unapplied(after); len(miss) != 0 {
		t.Errorf("a reset must not be checked, got %v", miss)
	}
}
