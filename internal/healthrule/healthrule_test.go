package healthrule

import (
	"encoding/json"
	"testing"
)

func TestNormalizeStatus(t *testing.T) {
	cases := map[string]string{
		"enabled":  "RULE_STATUS_ENABLED",
		"enable":   "RULE_STATUS_ENABLED",
		"DISABLED": "RULE_STATUS_DISABLED",
		" Disable": "RULE_STATUS_DISABLED",
	}
	for in, want := range cases {
		got, err := NormalizeStatus(in)
		if err != nil {
			t.Errorf("NormalizeStatus(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeStatus(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := NormalizeStatus("paused"); err == nil {
		t.Error("NormalizeStatus(\"paused\") expected error, got nil")
	}
}

func TestStatusBody(t *testing.T) {
	b := StatusBody("RULE_STATUS_ENABLED")
	if b["status"] != "RULE_STATUS_ENABLED" {
		t.Errorf("StatusBody status = %v", b["status"])
	}
}

func TestParseRuleFile(t *testing.T) {
	// Bare rule object.
	bare := []byte(`{"name":"r1","severity":"RULE_SEVERITY_HIGH"}`)
	got, err := ParseRuleFile(bare)
	if err != nil {
		t.Fatalf("ParseRuleFile(bare) error: %v", err)
	}
	var m map[string]any
	if json.Unmarshal(got, &m); m["name"] != "r1" {
		t.Errorf("bare: got %s", got)
	}

	// Wrapped in {rule} — should be unwrapped so round-tripping a `get` works.
	wrapped := []byte(`{"rule":{"name":"r2"}}`)
	got, err = ParseRuleFile(wrapped)
	if err != nil {
		t.Fatalf("ParseRuleFile(wrapped) error: %v", err)
	}
	m = nil
	if json.Unmarshal(got, &m); m["name"] != "r2" {
		t.Errorf("wrapped: expected inner rule, got %s", got)
	}

	if _, err := ParseRuleFile([]byte(`not json`)); err == nil {
		t.Error("ParseRuleFile(invalid) expected error")
	}
}

func TestRuleBody(t *testing.T) {
	b := RuleBody(json.RawMessage(`{"name":"x"}`))
	if _, ok := b["rule"]; !ok {
		t.Fatal("RuleBody missing rule key")
	}
	out, _ := json.Marshal(b)
	if string(out) != `{"rule":{"name":"x"}}` {
		t.Errorf("RuleBody marshalled = %s", out)
	}
}

func TestUnwrap(t *testing.T) {
	in := json.RawMessage(`{"rule":{"objectId":"abc","name":"r"}}`)
	out := Unwrap(in, "rule")
	var m map[string]any
	if json.Unmarshal(out, &m); m["objectId"] != "abc" {
		t.Errorf("Unwrap = %s", out)
	}
	// Missing key falls back to raw.
	bare := json.RawMessage(`{"objectId":"abc"}`)
	if got := Unwrap(bare, "rule"); string(got) != string(bare) {
		t.Errorf("Unwrap fallback = %s", got)
	}
}
