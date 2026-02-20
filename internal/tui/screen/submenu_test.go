package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
)

func TestSubMenuScreen_Title(t *testing.T) {
	s := NewSubMenuScreen("Cloud Directories", []tui.ResourceEntry{
		{Key: "gsuite", DisplayName: "Google Workspace"},
		{Key: "office365", DisplayName: "M365"},
	})
	if s.Title() != "Cloud Directories" {
		t.Errorf("Title = %q, want 'Cloud Directories'", s.Title())
	}
}

func TestSubMenuScreen_ViewShowsItems(t *testing.T) {
	s := NewSubMenuScreen("Cloud Directories", []tui.ResourceEntry{
		{Key: "gsuite", DisplayName: "Google Workspace"},
		{Key: "office365", DisplayName: "M365"},
	})
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := s.View()
	if !strings.Contains(view, "Google Workspace") {
		t.Error("view should contain 'Google Workspace'")
	}
	if !strings.Contains(view, "M365") {
		t.Error("view should contain 'M365'")
	}
}

func TestSubMenuScreen_EnterPushesListScreen(t *testing.T) {
	s := NewSubMenuScreen("Cloud Directories", []tui.ResourceEntry{
		{Key: "gsuite", DisplayName: "Google Workspace", ClientType: tui.ClientV2, ListEndpoint: "/gsuites"},
		{Key: "office365", DisplayName: "M365", ClientType: tui.ClientV2, ListEndpoint: "/office365s"},
	})
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should produce a command")
	}
	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}
	if pushMsg.Screen.Title() != "Google Workspace" {
		t.Errorf("pushed screen title = %q, want 'Google Workspace'", pushMsg.Screen.Title())
	}
}

func TestSubMenuScreen_EscPops(t *testing.T) {
	s := NewSubMenuScreen("Cloud Directories", []tui.ResourceEntry{
		{Key: "gsuite", DisplayName: "Google Workspace"},
	})
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(tui.PopScreenMsg); !ok {
		t.Fatalf("expected PopScreenMsg, got %T", msg)
	}
}

func TestSubMenuScreen_CursorMovement(t *testing.T) {
	s := NewSubMenuScreen("Cloud Directories", []tui.ResourceEntry{
		{Key: "gsuite", DisplayName: "Google Workspace"},
		{Key: "office365", DisplayName: "M365"},
		{Key: "ldap", DisplayName: "LDAP"},
	})
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	if s.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", s.cursor)
	}

	// Move down.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if s.cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", s.cursor)
	}

	// Move up.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if s.cursor != 0 {
		t.Errorf("cursor after k = %d, want 0", s.cursor)
	}

	// Should not go below 0.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if s.cursor != 0 {
		t.Errorf("cursor should not go below 0, got %d", s.cursor)
	}

	// Move to last.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if s.cursor != 2 {
		t.Errorf("cursor at bottom = %d, want 2", s.cursor)
	}

	// Should not go past end.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if s.cursor != 2 {
		t.Errorf("cursor should not exceed max, got %d", s.cursor)
	}
}

func TestSubMenuScreen_EnterOnSecondItem(t *testing.T) {
	s := NewSubMenuScreen("Cloud Directories", []tui.ResourceEntry{
		{Key: "gsuite", DisplayName: "Google Workspace", ClientType: tui.ClientV2, ListEndpoint: "/gsuites"},
		{Key: "office365", DisplayName: "M365", ClientType: tui.ClientV2, ListEndpoint: "/office365s"},
	})
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Move to second item.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should produce a command")
	}
	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}
	if pushMsg.Screen.Title() != "M365" {
		t.Errorf("pushed screen title = %q, want 'M365'", pushMsg.Screen.Title())
	}
}
