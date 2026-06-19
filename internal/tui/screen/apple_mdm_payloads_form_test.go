package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

func TestBuildMDMFormField_MapsApplleTypesToKinds(t *testing.T) {
	tests := []struct {
		in   apple_mdm.Key
		want mdmFormFieldKind
	}{
		{apple_mdm.Key{Name: "s", Type: "string"}, mdmFieldKindString},
		{apple_mdm.Key{Name: "b", Type: "boolean"}, mdmFieldKindBool},
		{apple_mdm.Key{Name: "i", Type: "integer"}, mdmFieldKindInteger},
		{apple_mdm.Key{Name: "r", Type: "real"}, mdmFieldKindReal},
		{apple_mdm.Key{Name: "e", Type: "string", RangeList: []any{"a", "b"}}, mdmFieldKindRangeList},
		{apple_mdm.Key{Name: "d", Type: "dictionary"}, mdmFieldKindUnsupported},
		{apple_mdm.Key{Name: "a", Type: "array"}, mdmFieldKindUnsupported},
		{apple_mdm.Key{Name: "t", Type: "date"}, mdmFieldKindUnsupported},
		{apple_mdm.Key{Name: "x", Type: "data"}, mdmFieldKindUnsupported},
	}
	for _, tc := range tests {
		f := buildMDMFormField(tc.in)
		if f.kind != tc.want {
			t.Errorf("type %q: got kind %d, want %d", tc.in.Type, f.kind, tc.want)
		}
	}
}

func TestBuildMDMFormField_RangeListSelectsDefault(t *testing.T) {
	f := buildMDMFormField(apple_mdm.Key{
		Name: "Mode", Type: "string",
		RangeList: []any{"none", "soft", "hard"},
		Default:   "soft",
	})
	if f.kind != mdmFieldKindRangeList {
		t.Fatalf("kind = %d, want rangelist", f.kind)
	}
	if f.selectedIdx != 1 {
		t.Errorf("selectedIdx = %d, want 1 (soft)", f.selectedIdx)
	}
}

func TestBuildMDMFormField_BoolDefault(t *testing.T) {
	f := buildMDMFormField(apple_mdm.Key{Name: "Flag", Type: "boolean", Default: true})
	if !f.boolValue {
		t.Error("bool default not applied")
	}
}

func TestFormScreen_AdvanceFocusSkipsUnsupported(t *testing.T) {
	p := apple_mdm.Payload{
		Keys: []apple_mdm.Key{
			{Name: "Scalar1", Type: "string"},
			{Name: "Complex", Type: "dictionary"},
			{Name: "Scalar2", Type: "boolean"},
		},
	}
	s := NewAppleMDMPayloadsFormScreen(p)
	// focus starts at 0 (name input). Advance once.
	s.advanceFocus(1)
	if s.focusIdx != 1 {
		t.Errorf("focusIdx = %d, want 1 (Scalar1)", s.focusIdx)
	}
	// Advance again — should skip the dictionary field at idx 2 and
	// land on Scalar2 at idx 3.
	s.advanceFocus(1)
	if s.focusIdx != 3 {
		t.Errorf("focusIdx = %d, want 3 (Scalar2 skipping dict)", s.focusIdx)
	}
	// Once more wraps back to name input.
	s.advanceFocus(1)
	if s.focusIdx != 0 {
		t.Errorf("focusIdx = %d, want 0 (wrap to name)", s.focusIdx)
	}
}

func TestFormScreen_CollectValues(t *testing.T) {
	p := apple_mdm.Payload{
		Keys: []apple_mdm.Key{
			{Name: "Name", Type: "string"},
			{Name: "Enabled", Type: "boolean", Default: false},
			{Name: "Count", Type: "integer"},
			{Name: "Mode", Type: "string", RangeList: []any{"a", "b", "c"}},
			{Name: "ComplexThing", Type: "dictionary"}, // unsupported, skipped
		},
	}
	s := NewAppleMDMPayloadsFormScreen(p)
	// Find each field by name and set values.
	for i := range s.fields {
		switch s.fields[i].key.Name {
		case "Name":
			s.fields[i].text.SetValue("hello")
		case "Enabled":
			s.fields[i].boolValue = true
		case "Count":
			s.fields[i].text.SetValue("42")
		case "Mode":
			s.fields[i].selectedIdx = 2 // "c"
		}
	}
	v := s.collectValues()
	if v["Name"] != "hello" {
		t.Errorf("Name = %v, want hello", v["Name"])
	}
	if v["Enabled"] != true {
		t.Errorf("Enabled = %v, want true", v["Enabled"])
	}
	if v["Count"] != 42 {
		t.Errorf("Count = %v, want 42", v["Count"])
	}
	if v["Mode"] != "c" {
		t.Errorf("Mode = %v, want c", v["Mode"])
	}
	// Unsupported fields don't show up in the values map.
	if _, ok := v["ComplexThing"]; ok {
		t.Errorf("unsupported field leaked into values map")
	}
}

func TestFormScreen_SubmitTransitionsToPreview(t *testing.T) {
	p := apple_mdm.Payload{
		Type:  "com.example.test",
		Title: "Test",
		Keys: []apple_mdm.Key{
			{Name: "Enabled", Type: "boolean", Default: true},
		},
	}
	s := NewAppleMDMPayloadsFormScreen(p)
	s.nameInput.SetValue("My Policy")
	_, _ = s.submit()
	if s.stage != mdmFormStagePreview {
		t.Errorf("stage = %d, want preview", s.stage)
	}
	if len(s.mobileconfig) == 0 {
		t.Error("mobileconfig empty after submit")
	}
	if !strings.Contains(string(s.mobileconfig), "com.example.test") {
		t.Error("mobileconfig missing payloadtype")
	}
}

func TestFormScreen_NumericRangeInlineError(t *testing.T) {
	p := apple_mdm.Payload{
		Keys: []apple_mdm.Key{
			{Name: "N", Type: "integer", Range: &apple_mdm.Range{Min: 1, Max: 10}},
		},
	}
	s := NewAppleMDMPayloadsFormScreen(p)
	// Focus the numeric field.
	s.advanceFocus(1)
	// Type "100" — above max.
	s.fields[0].text.SetValue("100")
	s.fields[0].err = validateMDMFieldNumeric(&s.fields[0])
	if s.fields[0].err == "" {
		t.Error("expected an inline error for value above max")
	}
	if !strings.Contains(s.fields[0].err, "Above maximum") {
		t.Errorf("error text = %q, want 'Above maximum' message", s.fields[0].err)
	}
	// Empty value clears the error.
	s.fields[0].text.SetValue("")
	s.fields[0].err = validateMDMFieldNumeric(&s.fields[0])
	if s.fields[0].err != "" {
		t.Errorf("empty value should clear inline error, got %q", s.fields[0].err)
	}
}

func TestFormScreen_BoolToggleViaSpace(t *testing.T) {
	p := apple_mdm.Payload{
		Keys: []apple_mdm.Key{
			{Name: "Flag", Type: "boolean", Default: false},
		},
	}
	s := NewAppleMDMPayloadsFormScreen(p)
	s.advanceFocus(1) // focus the bool field
	_, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !s.fields[0].boolValue {
		t.Error("space should toggle bool to true")
	}
	_, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if s.fields[0].boolValue {
		t.Error("space again should toggle back to false")
	}
}

func TestFormScreen_RangeListCycle(t *testing.T) {
	p := apple_mdm.Payload{
		Keys: []apple_mdm.Key{
			{Name: "Mode", Type: "string", RangeList: []any{"a", "b", "c"}, Default: "a"},
		},
	}
	s := NewAppleMDMPayloadsFormScreen(p)
	s.advanceFocus(1)
	if s.fields[0].selectedIdx != 0 {
		t.Fatalf("init selectedIdx = %d, want 0", s.fields[0].selectedIdx)
	}
	// 'l' cycles forward.
	_, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if s.fields[0].selectedIdx != 1 {
		t.Errorf("after l: selectedIdx = %d, want 1", s.fields[0].selectedIdx)
	}
	// 'h' cycles back.
	_, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if s.fields[0].selectedIdx != 0 {
		t.Errorf("after h: selectedIdx = %d, want 0", s.fields[0].selectedIdx)
	}
}

func TestFormScreen_SubmitClearsStaleErrors(t *testing.T) {
	// Bugbot PR #53 review: after a failed submit + a corrective edit
	// + a successful submit, the old per-field err strings used to
	// linger on the view. Esc-from-preview back to the form then
	// showed stale errors on fields that had since become valid.
	p := apple_mdm.Payload{
		Type: "com.example.test",
		Keys: []apple_mdm.Key{
			{Name: "N", Type: "integer", Range: &apple_mdm.Range{Min: 1, Max: 10}},
		},
	}
	s := NewAppleMDMPayloadsFormScreen(p)
	s.nameInput.SetValue("Test")
	// First submit with bad value — error gets set.
	s.advanceFocus(1)
	s.fields[0].text.SetValue("100")
	s.submit()
	if s.fields[0].err == "" {
		t.Fatal("expected initial range error")
	}
	if s.stage == mdmFormStagePreview {
		t.Fatal("stage should not advance with bad value")
	}
	// Fix the value + submit again. The stale error must clear.
	s.fields[0].text.SetValue("5")
	s.submit()
	if s.fields[0].err != "" {
		t.Errorf("stale error not cleared: %q", s.fields[0].err)
	}
	if s.stage != mdmFormStagePreview {
		t.Errorf("stage = %d, want preview after valid submit", s.stage)
	}
}

func TestFormScreen_RequiredComplexKeysShowMarker(t *testing.T) {
	// Bugbot PR #53 review: a required dictionary/array key landed in
	// the "Complex types" section without the `*` required marker;
	// operators couldn't tell that Ctrl-E was mandatory before
	// submit could succeed. The marker must persist even when the
	// field is rendered in the unsupported section.
	p := apple_mdm.Payload{
		Type: "com.example.test",
		Keys: []apple_mdm.Key{
			{Name: "RequiredDict", Type: "dictionary", Presence: "required"},
			{Name: "OptionalDict", Type: "dictionary", Presence: "optional"},
		},
	}
	s := NewAppleMDMPayloadsFormScreen(p)
	view := s.View()
	// Look for the section header — sanity check we're in the right
	// branch.
	if !strings.Contains(view, "Complex types") {
		t.Fatal("expected complex-types section in view")
	}
	// The required complex key should carry the marker. We render `*`
	// via style.Error which strips to bare `*` in ANSI-stripped form;
	// pattern-match on "* RequiredDict" with arbitrary intermediate
	// whitespace.
	if !strings.Contains(view, "* RequiredDict") &&
		!strings.Contains(view, "*  RequiredDict") {
		t.Errorf("expected required marker before RequiredDict, view:\n%s", view)
	}
}

func TestFormScreen_CtrlEEscapesToEditor(t *testing.T) {
	p := apple_mdm.Payload{Type: "com.example.test"}
	s := NewAppleMDMPayloadsFormScreen(p)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if cmd == nil {
		t.Fatal("Ctrl-E should produce a replace cmd")
	}
	if cmd() == nil {
		t.Error("replace cmd returned nil msg")
	}
}
