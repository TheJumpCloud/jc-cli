package apple_mdm

import (
	"testing"
)

func TestUnwrapType(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"<boolean>", "boolean"},
		{"<string>", "string"},
		{"<integer>", "integer"},
		{"<array>", "array"},
		{"<dictionary>", "dictionary"},
		{"<any>", "any"},
		{"<data>", "data"},
		{"  <real>  ", "real"},
		// Pass-through for unrecognized shapes so parser drift surfaces.
		{"boolean", "boolean"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := unwrapType(tc.in); got != tc.want {
			t.Errorf("unwrapType(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOSSupport_Available(t *testing.T) {
	tests := []struct {
		name  string
		intro string
		want  bool
	}{
		{"shipped", "10.7", true},
		{"sentinel n/a", "n/a", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := OSSupport{Introduced: tc.intro}
			if got := s.Available(); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParsePayload_MinimalSchema(t *testing.T) {
	yaml := []byte(`title: Test
description: Test schema for unit tests.
payload:
  payloadtype: com.example.test
  supportedOS:
    macOS:
      introduced: '13.0'
      multiple: true
    iOS:
      introduced: 'n/a'
payloadkeys:
- key: RequiredKey
  type: <string>
  presence: required
  content: A required string key.
- key: OptionalEnum
  type: <string>
  presence: optional
  default: foo
  rangelist: [foo, bar, baz]
  content: |-
    Multi-line content.
    Second line.
`)
	p, err := ParsePayload("com.example.test", "test.yaml", yaml)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.ID != "com.example.test" {
		t.Errorf("ID = %q, want com.example.test", p.ID)
	}
	if p.Type != "com.example.test" {
		t.Errorf("Type = %q, want com.example.test", p.Type)
	}
	if p.Title != "Test" {
		t.Errorf("Title = %q", p.Title)
	}
	if len(p.Keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(p.Keys))
	}
	if p.Keys[0].Type != "string" {
		t.Errorf("Key[0].Type = %q, want string (angle brackets should be stripped)", p.Keys[0].Type)
	}
	if p.Keys[0].Presence != "required" {
		t.Errorf("Key[0].Presence = %q", p.Keys[0].Presence)
	}
	if got := p.Keys[1].Default; got != "foo" {
		t.Errorf("Key[1].Default = %v, want foo", got)
	}
	if want := []any{"foo", "bar", "baz"}; len(p.Keys[1].RangeList) != 3 {
		t.Errorf("Key[1].RangeList = %v, want %v", p.Keys[1].RangeList, want)
	}

	// Per-platform availability: macOS=10.7 → available; iOS=n/a → not.
	if !p.SupportedOS["macOS"].Available() {
		t.Error("macOS should be available")
	}
	if p.SupportedOS["iOS"].Available() {
		t.Error("iOS with introduced=n/a should not be available")
	}
}

func TestParsePayload_MissingPayloadType(t *testing.T) {
	yaml := []byte(`title: Bad
payload:
  supportedOS:
    macOS:
      introduced: '13.0'
payloadkeys: []
`)
	_, err := ParsePayload("bad", "bad.yaml", yaml)
	if err == nil {
		t.Fatal("expected error on missing payload.payloadtype")
	}
}

func TestKey_EffectiveSupport_OverridesParent(t *testing.T) {
	parent := SupportedOS{
		"macOS": {Introduced: "10.7", DeviceChannel: true},
		"iOS":   {Introduced: "4.0"},
	}
	k := Key{
		SupportedOS: SupportedOS{
			// Override macOS with stricter requirement; leave iOS to
			// inherit from parent.
			"macOS": {Introduced: "12.0", Supervised: true},
		},
	}
	got := k.EffectiveSupport(parent)
	if got["macOS"].Introduced != "12.0" {
		t.Errorf("macOS not overridden: %v", got["macOS"])
	}
	if !got["macOS"].Supervised {
		t.Error("macOS Supervised not overridden")
	}
	if got["iOS"].Introduced != "4.0" {
		t.Errorf("iOS not inherited: %v", got["iOS"])
	}
}
