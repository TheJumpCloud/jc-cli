package windows_mdm

import (
	"encoding/json"
	"strings"
	"testing"
)

// The JSON fixtures mirror the shape `jc policies get` returns —
// verified live during the KLA-459 e2e (stored uriList round-trip).

const decodeOMAURIFixture = `{
	"id": "pol-1",
	"name": "Camera lockdown",
	"template": {"id": "tmpl-oma", "name": "custom_oma_uri_mdm_windows"},
	"values": [{
		"configFieldID": "urifid",
		"configFieldName": "uriList",
		"value": [
			{"uri": "./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera", "format": "int", "value": "0"},
			{"uri": "./Device/Vendor/MSFT/Policy/Config/Experience/AllowScreenCapture", "value": "1", "format": "int"}
		]
	}]
}`

const decodeRegistryFixture = `{
	"id": "pol-2",
	"name": "Disable Autorun",
	"template": {"id": "tmpl-reg", "name": "custom_registry_keys_policy_windows"},
	"values": [{
		"configFieldID": "regfid",
		"configFieldName": "customRegTable",
		"value": [
			{"customLocation": "SOFTWARE\\Policies\\Microsoft\\Windows\\Explorer", "customValueName": "NoAutorun", "customRegType": "DWORD", "customData": "1"}
		]
	}]
}`

func TestDecodeCustomWindowsPolicy_OMAURI(t *testing.T) {
	d, err := DecodeCustomWindowsPolicy(json.RawMessage(decodeOMAURIFixture))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Kind != PolicyKindOMAURI || d.PolicyID != "pol-1" || d.PolicyName != "Camera lockdown" {
		t.Errorf("header wrong: %+v", d)
	}
	if len(d.Settings) != 2 || d.Settings[0].URI != "./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera" ||
		d.Settings[0].Value != "0" || d.Settings[1].Format != "int" {
		t.Errorf("settings wrong: %+v", d.Settings)
	}
	if len(d.Keys) != 0 {
		t.Error("Keys must be empty for an OMA-URI policy")
	}
}

func TestDecodeCustomWindowsPolicy_Registry(t *testing.T) {
	d, err := DecodeCustomWindowsPolicy(json.RawMessage(decodeRegistryFixture))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Kind != PolicyKindRegistry || len(d.Keys) != 1 {
		t.Fatalf("registry decode wrong: %+v", d)
	}
	k := d.Keys[0]
	if k.Location != `SOFTWARE\Policies\Microsoft\Windows\Explorer` ||
		k.ValueName != "NoAutorun" || k.RegType != "DWORD" || k.Data != "1" {
		t.Errorf("key wrong: %+v", k)
	}
}

func TestDecodeCustomWindowsPolicy_RoundTripsThroughBuild(t *testing.T) {
	// decode → Build must reproduce the identical wire entries — the
	// edit path's no-data-loss contract.
	d, err := DecodeCustomWindowsPolicy(json.RawMessage(decodeOMAURIFixture))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := CustomTemplate{ID: "t", FieldID: "f", FieldName: fieldNameOMAURI}
	body := BuildOMAURIPolicyBody(d.PolicyName, tmpl, d.Settings)
	b, _ := json.Marshal(body)
	for _, want := range []string{
		`"uri":"./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera"`,
		`"value":"0"`,
		`"uri":"./Device/Vendor/MSFT/Policy/Config/Experience/AllowScreenCapture"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("round-trip body missing %s:\n%s", want, b)
		}
	}
}

func TestDecodeCustomWindowsPolicy_Errors(t *testing.T) {
	// Non-Windows template → actionable error naming both templates.
	_, err := DecodeCustomWindowsPolicy(json.RawMessage(
		`{"id":"x","name":"Apple thing","template":{"name":"custom_mdm_profile_darwin"},"values":[]}`))
	if err == nil || !strings.Contains(err.Error(), TemplateNameOMAURI) {
		t.Errorf("wrong-template error should name the Windows templates: %v", err)
	}

	// Right template, missing field → renamed-field hint.
	_, err = DecodeCustomWindowsPolicy(json.RawMessage(
		`{"id":"x","name":"P","template":{"name":"custom_oma_uri_mdm_windows"},"values":[{"configFieldName":"other","value":[]}]}`))
	if err == nil || !strings.Contains(err.Error(), "uriList") {
		t.Errorf("missing-field error should name uriList: %v", err)
	}

	// Right template, empty list → nothing-to-edit error.
	_, err = DecodeCustomWindowsPolicy(json.RawMessage(
		`{"id":"x","name":"P","template":{"name":"custom_oma_uri_mdm_windows"},"values":[{"configFieldName":"uriList","value":[]}]}`))
	if err == nil || !strings.Contains(err.Error(), "nothing to edit") {
		t.Errorf("empty-list error wrong: %v", err)
	}
}
