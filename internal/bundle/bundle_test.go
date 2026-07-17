package bundle

import (
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

const validBundleYAML = `
name: test-baseline
version: "1.0.0"
description: test
policies:
  - name: Firewall
    type: apple_profile
    profile:
      payloads:
        - type: com.apple.security.firewall
          values:
            EnableFirewall: true
  - name: Camera off
    type: windows_oma_uri
    settings:
      - uri: ./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera
        format: int
        value: "0"
  - name: Autorun off
    type: windows_registry
    keys:
      - location: SOFTWARE\Policies\Microsoft\Windows\Explorer
        name: NoAutorun
        type: DWORD
        data: "1"
`

func TestParse_ValidBundle(t *testing.T) {
	b, err := Parse([]byte(validBundleYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if b.Name != "test-baseline" || b.Version != "1.0.0" || len(b.Policies) != 3 {
		t.Errorf("parsed wrong: %+v", b)
	}
	// Profile name defaults to the unit name.
	if got := b.Policies[0].Profile.Name; got != "Firewall" {
		t.Errorf("profile name fallback = %q, want unit name", got)
	}
	// OS defaults to macOS on apple units.
	if b.Policies[0].OS != "macOS" {
		t.Errorf("os default = %q, want macOS", b.Policies[0].OS)
	}
	if got := b.Platforms(); len(got) != 2 || got[0] != "macOS" || got[1] != "windows" {
		t.Errorf("Platforms() = %v", got)
	}
}

func TestParse_OSNormalization(t *testing.T) {
	yaml := `
name: n
version: "1"
policies:
  - name: p
    type: apple_profile
    os: ios
    profile:
      payloads:
        - type: com.apple.security.firewall
          values: {EnableFirewall: true}
`
	b, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if b.Policies[0].OS != "iOS" {
		t.Errorf("os = %q, want iOS", b.Policies[0].OS)
	}
}

// TestParse_StructuralErrorsAggregate pins the aggregate-errors
// convention: every structural problem reported in one pass.
func TestParse_StructuralErrorsAggregate(t *testing.T) {
	yaml := `
description: no name or version
policies:
  - name: dup
    type: windows_oma_uri
    os: macOS
    settings: [{uri: ./x, format: int, value: "1"}]
  - name: dup
    type: apple_profile
  - name: mixed
    type: windows_registry
    keys: [{location: SOFTWARE\X, name: v, type: DWORD, data: "1"}]
    settings: [{uri: ./x, format: int, value: "1"}]
  - name: mystery
    type: teleport
  - type: windows_oma_uri
    settings: [{uri: ./y, format: int, value: "2"}]
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected structural errors")
	}
	for _, want := range []string{
		"'name' is required",
		"'version' is required",
		"'os' applies to apple_profile units only",
		"duplicate unit name",
		"requires a 'profile' block",
		"windows_registry takes 'keys' only",
		`unknown type "teleport"`,
		"policies[4]: 'name' is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("aggregated error missing %q:\n%v", want, err)
		}
	}
}

func TestParse_EmptyPolicies(t *testing.T) {
	_, err := Parse([]byte("name: n\nversion: \"1\"\n"))
	if err == nil || !strings.Contains(err.Error(), "'policies' must contain at least one entry") {
		t.Errorf("empty policies should be refused: %v", err)
	}
}

func TestParse_BadOS(t *testing.T) {
	yaml := `
name: n
version: "1"
policies:
  - name: p
    type: apple_profile
    os: windows
    profile:
      payloads: [{type: com.apple.security.firewall}]
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), `os "windows": want macOS or iOS`) {
		t.Errorf("bad os should be refused: %v", err)
	}
}

func TestValidate_DeepErrorsAggregate(t *testing.T) {
	yaml := `
name: broken
version: "1"
policies:
  - name: bad payload key
    type: apple_profile
    profile:
      payloads:
        - type: com.apple.security.firewall
          values: {NoSuchKey: true}
  - name: bad format
    type: windows_oma_uri
    settings:
      - {uri: ./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera, format: quantum, value: "0"}
  - name: bad dword
    type: windows_registry
    keys:
      - {location: SOFTWARE\X, name: v, type: DWORD, data: "not-a-number"}
`
	b, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse (structure is fine): %v", err)
	}
	cat, err := apple_mdm.Default()
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	err = Validate(b, cat)
	if err == nil {
		t.Fatal("expected deep validation errors")
	}
	// One problem from each unit, all in one error.
	for _, want := range []string{"bad payload key", "bad format", "bad dword"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("deep error missing unit %q:\n%v", want, err)
		}
	}
}

func TestValidate_ValidBundle(t *testing.T) {
	b, err := Parse([]byte(validBundleYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cat, err := apple_mdm.Default()
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if err := Validate(b, cat); err != nil {
		t.Errorf("valid bundle failed deep validation: %v", err)
	}
}

// TestWindowsConversions pins the friendly-name → wire-name mapping.
func TestWindowsConversions(t *testing.T) {
	b, err := Parse([]byte(validBundleYAML))
	if err != nil {
		t.Fatal(err)
	}
	s := b.Policies[1].WindowsSettings()
	if len(s) != 1 || s[0].URI != "./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera" || s[0].Format != "int" || s[0].Value != "0" {
		t.Errorf("WindowsSettings wrong: %+v", s)
	}
	k := b.Policies[2].WindowsKeys()
	if len(k) != 1 || k[0].Location != `SOFTWARE\Policies\Microsoft\Windows\Explorer` ||
		k[0].ValueName != "NoAutorun" || k[0].RegType != "DWORD" || k[0].Data != "1" {
		t.Errorf("WindowsKeys wrong: %+v", k)
	}
}

func TestMarshalYAML_RoundTrip(t *testing.T) {
	b, err := Parse([]byte(validBundleYAML))
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalYAML(b)
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	again, err := Parse(data)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if again.Name != b.Name || len(again.Policies) != len(b.Policies) {
		t.Errorf("round trip lost data: %+v", again)
	}
}

// TestValidate_PlatformMismatch is the review regression (2026-07-17):
// Validate must enforce the same platform-support check apply does, so
// a bundle that validates clean creates cleanly. A macOS-only payload
// declared os: iOS previously passed validate then failed at apply.
func TestValidate_PlatformMismatch(t *testing.T) {
	yaml := `
name: mismatch
version: "1"
policies:
  - name: firewall on ios
    type: apple_profile
    os: iOS
    profile:
      payloads:
        - type: com.apple.security.firewall
          values: {EnableFirewall: true}
`
	b, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse (structure valid): %v", err)
	}
	cat, err := apple_mdm.Default()
	if err != nil {
		t.Fatal(err)
	}
	err = Validate(b, cat)
	if err == nil || !strings.Contains(err.Error(), "do not declare support for iOS") {
		t.Errorf("iOS/macOS-only mismatch must fail validate: %v", err)
	}
}
