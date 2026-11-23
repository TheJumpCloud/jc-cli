package gcep

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
)

func TestNormalizeState(t *testing.T) {
	for in, want := range map[string]string{
		"enabled": StateEnabled, "Disabled": StateDisabled, StateEnabled: StateEnabled,
	} {
		if got, err := NormalizeState(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	_, err := NormalizeState("bogus")
	if err == nil || !strings.Contains(err.Error(), "enabled, disabled") || strings.Contains(err.Error(), "configuration_state_bogus") {
		t.Fatalf("got %v", err)
	}
}

func TestNormalizeOSFamilies(t *testing.T) {
	got, err := NormalizeOSFamilies([]string{"windows", "MAC_OS", "windows"})
	if err != nil || strings.Join(got, ",") != "windows,mac_os" {
		t.Fatalf("got %v, %v", got, err)
	}
	_, err = NormalizeOSFamilies([]string{"mac_os", "solaris", "linux"})
	if err == nil || !strings.Contains(err.Error(), `"solaris", "linux"`) || !strings.Contains(err.Error(), "mac_os, windows") {
		t.Fatalf("got %v", err)
	}
	if _, err := NormalizeOSFamilies([]string{""}); err == nil {
		t.Fatal("empty value accepted")
	}
}

func TestMergeKeepsUngivenFields(t *testing.T) {
	cur := &Configuration{CustomerID: "C1", GoogleUserEmail: "a@example.com", State: StateEnabled, ApplicableOsFamilies: []string{"mac_os", "windows"}}
	st := StateDisabled
	body := Merge(cur, Changes{State: &st})
	want := UpdateBody{CustomerID: "C1", GoogleUserEmail: "a@example.com", State: StateDisabled, ApplicableOsFamilies: []string{"mac_os", "windows"}}
	gb, _ := json.Marshal(body)
	wb, _ := json.Marshal(want)
	if string(gb) != string(wb) {
		t.Fatalf("got %s want %s", gb, wb)
	}
	if len(Mismatches(body, cur)) != 1 {
		t.Fatalf("mismatches: %v", Mismatches(body, cur))
	}
	reordered := &Configuration{CustomerID: "C1", GoogleUserEmail: "a@example.com", State: StateDisabled, ApplicableOsFamilies: []string{"windows", "mac_os"}}
	if m := Mismatches(body, reordered); len(m) != 0 {
		t.Fatalf("OS order should not matter: %v", m)
	}
}

func TestParseConfiguration_NotConfigured(t *testing.T) {
	for _, raw := range []string{``, `null`, `{}`, `{"state":"configuration_state_unspecified","applicableOsFamilies":[]}`} {
		if !NotConfigured(json.RawMessage(raw)) {
			t.Errorf("%q should be not configured", raw)
		}
	}
	if NotConfigured(json.RawMessage(`{"customerId":"C1","state":"configuration_state_pending_delete"}`)) {
		t.Error("a configuration was treated as not configured")
	}
}

func TestIsNotConfiguredError(t *testing.T) {
	cases := map[error]bool{
		&api.APIError{StatusCode: 404}: true,
		&api.APIError{StatusCode: 400, Message: `{"message":"configuration not found"}`}: true,
		&api.APIError{StatusCode: 400, Message: "invalid request"}:                       false,
		&api.APIError{StatusCode: 403, Message: "not found"}:                             false,
		errors.New("not found"): false,
	}
	for err, want := range cases {
		if got := IsNotConfiguredError(err); got != want {
			t.Errorf("%v: got %v", err, got)
		}
	}
}

func TestLooksLikeMalformedID(t *testing.T) {
	for s, want := range map[string]bool{
		"5f0c1a2b3c4d5e6f7a8b9c":     true,
		"5f0c1a2b3c4d5e6f7a8b9c0d1e": true,
		"5f0c1a2b3c4d5e6f7a8b9c0d":   false, // a valid id
		"ada-macbook":                false,
		"cafe":                       false,
	} {
		if got := LooksLikeMalformedID(s); got != want {
			t.Errorf("%q: got %v", s, got)
		}
	}
}
