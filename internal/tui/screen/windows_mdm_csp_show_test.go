package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWindowsCSPShow_RendersMetadata(t *testing.T) {
	settings := testCSPSettings()
	s := NewWindowsMDMCSPShowScreen(settings[0], &windowsMDMDraft{})
	view := s.View()
	for _, want := range []string{
		"./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera",
		"Allowed values (ENUM)",
		"Not allowed.",
		"a add to draft",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestWindowsCSPShow_AddToDraft(t *testing.T) {
	settings := testCSPSettings()
	draft := &windowsMDMDraft{}
	s := NewWindowsMDMCSPShowScreen(settings[0], draft)

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if len(draft.settings) != 1 {
		t.Fatalf("draft should hold 1 after add, got %d", len(draft.settings))
	}
	if s.noteIsErr || !strings.Contains(s.note, "Added") {
		t.Errorf("add note wrong: %q (err=%v)", s.note, s.noteIsErr)
	}

	// Second add is refused as a duplicate, not an error.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if len(draft.settings) != 1 || !strings.Contains(s.note, "Already") {
		t.Errorf("duplicate add handling wrong: draft=%d note=%q", len(draft.settings), s.note)
	}
}

func TestWindowsCSPShow_ADMXRefusedLoudly(t *testing.T) {
	settings := testCSPSettings()
	draft := &windowsMDMDraft{}
	s := NewWindowsMDMCSPShowScreen(settings[2], draft) // ADMX_Sample/LegacyThing

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if len(draft.settings) != 0 {
		t.Fatal("ADMX-backed setting must not enter the draft")
	}
	if !s.noteIsErr || !strings.Contains(s.note, "ADMX") {
		t.Errorf("refusal must be loud and name ADMX: %q", s.note)
	}
	// The detail view itself flags it too.
	if !strings.Contains(s.View(), "ADMX-backed") {
		t.Error("detail view should flag ADMX-backed")
	}
}

func TestWindowsCSPShow_UserScopeWarnsOnAdd(t *testing.T) {
	settings := testCSPSettings()
	draft := &windowsMDMDraft{}
	s := NewWindowsMDMCSPShowScreen(settings[3], draft) // user-scoped

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if len(draft.settings) != 1 {
		t.Fatal("user-scoped settings are addable (with a warning)")
	}
	if !strings.Contains(s.note, "user-scoped") || !strings.Contains(s.note, "device-scoped") {
		t.Errorf("add note should carry the scope warning: %q", s.note)
	}
}

func TestWindowsCSPShow_AuthorRequiresDraft(t *testing.T) {
	settings := testCSPSettings()
	s := NewWindowsMDMCSPShowScreen(settings[0], &windowsMDMDraft{})
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd != nil || !s.noteIsErr {
		t.Error("c on empty draft should inline-error, not push")
	}
	s.draft.add(settings[0])
	_, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd == nil {
		t.Error("c with a draft should push the form")
	}
}
