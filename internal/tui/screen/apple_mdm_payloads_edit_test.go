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

func TestNewAppleMDMPayloadsFormScreenForEdit_PreservesEnvelopeFlags(t *testing.T) {
	// Bugbot PR #54 review caught three silent data-loss bugs:
	// edit save always rewrote redispatch to true, dropped the
	// removal lock, and resolved the macOS template family for any
	// policy. Verify the form now captures all three from the
	// decoded policy.
	decoded := apple_mdm.DecodedPolicy{
		PolicyID:          "abc",
		PolicyName:        "iOS policy",
		TemplateName:      "custom_mdm_profile_iphone",
		Schema:            apple_mdm.Payload{Type: "com.apple.x"},
		Redispatch:        false, // off — must be preserved
		RemovalDisallowed: true,  // on — must be preserved
	}
	s := NewAppleMDMPayloadsFormScreenForEdit(decoded)
	if s.editRedispatch != false {
		t.Errorf("editRedispatch = %v, want false (preserved from decoded)", s.editRedispatch)
	}
	if !s.editRemovalDisallowed {
		t.Errorf("editRemovalDisallowed = %v, want true (preserved from decoded)", s.editRemovalDisallowed)
	}
	if s.editOSFamily != "iphone" {
		t.Errorf("editOSFamily = %q, want iphone (parsed from template name)", s.editOSFamily)
	}
}

func TestNewAppleMDMPayloadsFormScreenForEdit_UnparseableTemplateFallsBackToDarwin(t *testing.T) {
	// A weird template name (older policies, hand-rolled records)
	// shouldn't break the edit; default to darwin since that's the
	// dominant case.
	decoded := apple_mdm.DecodedPolicy{
		PolicyID:     "abc",
		TemplateName: "weird-old-thing",
		Schema:       apple_mdm.Payload{Type: "x"},
	}
	s := NewAppleMDMPayloadsFormScreenForEdit(decoded)
	if s.editOSFamily != apple_mdm.OSFamilyDarwin {
		t.Errorf("editOSFamily = %q, want fallback to darwin", s.editOSFamily)
	}
}

func TestFormScreen_EditPreservesUnsupportedKeysFromOriginal(t *testing.T) {
	// Bugbot PR #54 re-review: edit save only emitted scalar form
	// fields; nested settings (dict, array, date, data) decoded from
	// the original policy were silently dropped. A wifi edit would
	// strip EAPClientConfiguration; an MCX edit would strip every
	// bundle-keyed sub-preference.
	//
	// Verify that submitting an edit re-emits a mobileconfig that
	// still contains the original nested values for fields the form
	// can't render.
	p := apple_mdm.Payload{
		Type: "com.example.test",
		Keys: []apple_mdm.Key{
			{Name: "Flag", Type: "boolean", Default: false},
			{Name: "NestedCfg", Type: "dictionary"}, // unsupported in form
		},
	}
	decoded := apple_mdm.DecodedPolicy{
		PolicyID:     "abc",
		PolicyName:   "test",
		TemplateName: "custom_mdm_profile_darwin",
		Schema:       p,
		Values: map[string]any{
			"Flag":      true,
			"NestedCfg": map[string]any{"InnerKey": "preserved"},
		},
	}
	s := NewAppleMDMPayloadsFormScreenForEdit(decoded)
	if s.editOriginalValues == nil {
		t.Fatal("editOriginalValues should snapshot decoded.Values")
	}
	// Confirm the snapshot has the nested value.
	if nested, ok := s.editOriginalValues["NestedCfg"].(map[string]any); !ok || nested["InnerKey"] != "preserved" {
		t.Errorf("nested value not snapshotted: %v", s.editOriginalValues)
	}

	// Submit — should NOT drop NestedCfg.
	_, _ = s.submit()
	if s.stage != mdmFormStagePreview {
		t.Fatalf("stage = %d, want preview after submit", s.stage)
	}
	// The emitted mobileconfig should contain the InnerKey marker.
	if !strings.Contains(string(s.mobileconfig), "InnerKey") {
		t.Errorf("mobileconfig dropped the unsupported NestedCfg key:\n%s", s.mobileconfig)
	}
	if !strings.Contains(string(s.mobileconfig), "preserved") {
		t.Errorf("mobileconfig dropped the inner value:\n%s", s.mobileconfig)
	}
}

func TestAppleMDMPoliciesListScreen_DrillingGuardsDoubleEnter(t *testing.T) {
	// Bugbot PR #54 review: openSelected didn't check s.drilling, so
	// repeated Enter taps queued multiple GET+decode goroutines that
	// each fired PushScreenMsg on success.
	s := NewAppleMDMPoliciesListScreen()
	s.loading = false
	s.all = []policyRow{{ID: "1", Name: "test", Template: "custom_mdm_profile_darwin"}}
	s.applyFilter()
	// First Enter starts a drill-in.
	_, cmd1 := s.openSelected()
	if cmd1 == nil {
		t.Fatal("first Enter should produce a decode cmd")
	}
	if !s.drilling {
		t.Fatal("drilling should be set after first Enter")
	}
	// Second Enter while drilling should return no cmd (no new
	// goroutine).
	_, cmd2 := s.openSelected()
	if cmd2 != nil {
		t.Error("second Enter should be a no-op while drilling")
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
