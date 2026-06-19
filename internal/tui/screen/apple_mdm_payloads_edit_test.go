package screen

import (
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

func TestNewAppleMDMPayloadsFormScreenForEdit_PrePopulatesValues(t *testing.T) {
	p := apple_mdm.Payload{
		Type:  "com.example.test",
		Title: "Test",
		Keys: []apple_mdm.Key{
			{Name: "Name", Type: "string"},
			{Name: "Enabled", Type: "boolean", Default: false},
			{Name: "Count", Type: "integer"},
			{Name: "Mode", Type: "string", RangeList: []any{"none", "soft", "hard"}},
		},
	}
	decoded := apple_mdm.DecodedPolicy{
		PolicyID:   "abc123",
		PolicyName: "Existing policy",
		Schema:     p,
		Values: map[string]any{
			"Name":    "from-policy",
			"Enabled": true,
			"Count":   uint64(42), // howett.net/plist decodes integers as uint64
			"Mode":    "hard",
		},
	}
	s := NewAppleMDMPayloadsFormScreenForEdit(decoded)

	if s.mode != formModeEdit {
		t.Error("mode should be edit")
	}
	if s.editPolicyID != "abc123" {
		t.Errorf("editPolicyID = %q", s.editPolicyID)
	}
	if s.nameInput.Value() != "Existing policy" {
		t.Errorf("nameInput = %q", s.nameInput.Value())
	}

	// Fields should reflect the decoded values.
	for _, f := range s.fields {
		switch f.key.Name {
		case "Name":
			if f.text.Value() != "from-policy" {
				t.Errorf("Name text = %q", f.text.Value())
			}
		case "Enabled":
			if !f.boolValue {
				t.Error("Enabled bool not pre-populated")
			}
		case "Count":
			if f.text.Value() != "42" {
				t.Errorf("Count text = %q, want 42", f.text.Value())
			}
		case "Mode":
			if f.selectedIdx != 2 {
				t.Errorf("Mode selectedIdx = %d, want 2 (hard)", f.selectedIdx)
			}
		}
	}
}

func TestNewAppleMDMPayloadsFormScreenForEdit_HandlesMissingValuesGracefully(t *testing.T) {
	// A real-world decoded policy can omit optional keys that weren't
	// set by the operator. Pre-population must skip those rather than
	// reset the field to a zero value or panic.
	p := apple_mdm.Payload{
		Type: "com.example.test",
		Keys: []apple_mdm.Key{
			{Name: "A", Type: "string"},
			{Name: "B", Type: "boolean", Default: true},
		},
	}
	decoded := apple_mdm.DecodedPolicy{
		Schema: p,
		Values: map[string]any{"A": "set"},
	}
	s := NewAppleMDMPayloadsFormScreenForEdit(decoded)
	for _, f := range s.fields {
		switch f.key.Name {
		case "A":
			if f.text.Value() != "set" {
				t.Errorf("A not pre-populated: %q", f.text.Value())
			}
		case "B":
			// B isn't in Values; the field should keep its schema
			// default rather than reset to false.
			if !f.boolValue {
				t.Error("B should retain its schema default when not in Values")
			}
		}
	}
}

func TestAppleMDMMultiPayloadGuardScreen_Renders(t *testing.T) {
	d := apple_mdm.DecodedPolicy{
		PolicyID:   "multi-id",
		PolicyName: "CIS bundle",
		IsMulti:    true,
	}
	s := NewAppleMDMMultiPayloadGuardScreen(d)
	view := s.View()
	if !strings.Contains(view, "CIS bundle") {
		t.Error("guard view should include policy name")
	}
	if !strings.Contains(view, "Multi-payload") {
		t.Error("guard view should mention multi-payload")
	}
	if !strings.Contains(view, "Admin Portal") {
		t.Error("guard view should point at the Admin Portal")
	}
}

func TestAppleMDMPoliciesListScreen_FilterByNameAndTemplate(t *testing.T) {
	s := NewAppleMDMPoliciesListScreen()
	// Inject synthetic policy rows so we don't go through the live
	// fetcher. The screen treats s.all + applyFilter as the source of
	// truth for what View() renders.
	s.all = []policyRow{
		{ID: "1", Name: "firewall enforce", Template: "custom_mdm_profile_darwin"},
		{ID: "2", Name: "wifi corp", Template: "custom_mdm_profile_darwin"},
		{ID: "3", Name: "iOS restrictions", Template: "custom_mdm_profile_iphone"},
	}
	s.loading = false
	s.applyFilter()
	if len(s.filtered) != 3 {
		t.Fatalf("pre-filter want 3, got %d", len(s.filtered))
	}
	s.filter.SetValue("fire")
	s.applyFilter()
	if len(s.filtered) != 1 || s.filtered[0].ID != "1" {
		t.Errorf("filter 'fire' should narrow to firewall, got %v", s.filtered)
	}
	s.filter.SetValue("iphone")
	s.applyFilter()
	if len(s.filtered) != 1 || s.filtered[0].ID != "3" {
		t.Errorf("filter 'iphone' should match by template, got %v", s.filtered)
	}
	s.filter.SetValue("")
	s.applyFilter()
	if len(s.filtered) != 3 {
		t.Errorf("clearing filter should restore 3, got %d", len(s.filtered))
	}
}
