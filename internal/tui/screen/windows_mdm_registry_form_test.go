package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWindowsRegistryForm_SlotMathAndRowOps(t *testing.T) {
	s := NewWindowsMDMRegistryFormScreen()
	if s.slotCount() != 5 { // name + 1 row * 4
		t.Fatalf("slotCount = %d", s.slotCount())
	}
	row, sub := s.slotFor(0)
	if row != -1 || sub != 0 {
		t.Errorf("slotFor(0) = %d,%d", row, sub)
	}
	row, sub = s.slotFor(3) // 1 + 0*4 + 2 → row 0 type slot
	if row != 0 || sub != 2 {
		t.Errorf("slotFor(3) = %d,%d", row, sub)
	}

	// Ctrl-N adds a row and focuses its location.
	s.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if len(s.rows) != 2 || s.focusIdx != 5 {
		t.Errorf("ctrl+n wrong: rows=%d focus=%d", len(s.rows), s.focusIdx)
	}

	// Ctrl-D removes the focused row.
	s.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if len(s.rows) != 1 {
		t.Errorf("ctrl+d should remove the row, rows=%d", len(s.rows))
	}
	// The last row can't be removed.
	s.focusIdx = 1
	s.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if len(s.rows) != 1 {
		t.Error("the last row must not be removable")
	}
}

func TestWindowsRegistryForm_SubmitMapsErrorsPerRow(t *testing.T) {
	s := NewWindowsMDMRegistryFormScreen()
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	// Hive prefix + missing name/data — all aggregate errors land on
	// the row.
	s.rows[0].location.SetValue(`HKEY_LOCAL_MACHINE\SOFTWARE\X`)
	s.submit()
	if s.stage != mdmFormStageEdit {
		t.Fatal("invalid submit must stay on edit")
	}
	if !strings.Contains(s.rows[0].err, "HKEY_LOCAL_MACHINE is implied") {
		t.Errorf("hive-prefix error missing: %q", s.rows[0].err)
	}

	// Fix the row → preview, stale errors cleared, type from the
	// cycle (index 0 = DWORD).
	s.rows[0].location.SetValue(`SOFTWARE\Policies\X`)
	s.rows[0].name.SetValue("V")
	s.rows[0].data.SetValue("1")
	s.submit()
	if s.stage != mdmFormStagePreview || s.rows[0].err != "" {
		t.Fatalf("valid submit wrong: stage=%d err=%q", s.stage, s.rows[0].err)
	}
	if len(s.normalized) != 1 || s.normalized[0].RegType != "DWORD" {
		t.Errorf("normalized keys wrong: %+v", s.normalized)
	}
}

func TestWindowsRegistryForm_TypeCycles(t *testing.T) {
	s := NewWindowsMDMRegistryFormScreen()
	s.focusIdx = 3 // row 0, type slot
	s.refocus()
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if s.rows[0].typeIdx != 2 {
		t.Errorf("type cycle wrong: %d", s.rows[0].typeIdx)
	}
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if s.rows[0].typeIdx != 1 {
		t.Errorf("reverse cycle wrong: %d", s.rows[0].typeIdx)
	}
}

func TestWindowsRegistryForm_CreateFlow(t *testing.T) {
	posted := false
	srv := startWindowsPolicyServer(t, func() { posted = true })
	stubWindowsMDMClient(t, srv.URL)

	s := NewWindowsMDMRegistryFormScreen()
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	s.nameInput.SetValue("Registry TUI test")
	s.rows[0].location.SetValue(`SOFTWARE\Policies\X`)
	s.rows[0].name.SetValue("V")
	s.rows[0].data.SetValue("1")
	s.submit()
	if s.stage != mdmFormStagePreview {
		t.Fatalf("expected preview, stage=%d err=%q", s.stage, s.rows[0].err)
	}

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg := s.createCmd()()
	model, _ := s.Update(msg)
	s = model.(*WindowsMDMRegistryFormScreen)

	if !posted || s.stage != mdmFormStageSuccess || s.policyID != "pol-9999" {
		t.Errorf("create flow wrong: posted=%v stage=%d id=%q err=%q",
			posted, s.stage, s.policyID, s.createErr)
	}
}
