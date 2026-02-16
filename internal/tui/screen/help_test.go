package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
)

func TestHelpScreen_Title(t *testing.T) {
	h := NewHelpScreen()
	if h.Title() != "Help" {
		t.Errorf("Title = %q, want 'Help'", h.Title())
	}
}

func TestHelpScreen_EscPops(t *testing.T) {
	h := NewHelpScreen()

	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("Esc should produce a command")
	}

	msg := cmd()
	if _, ok := msg.(tui.PopScreenMsg); !ok {
		t.Fatalf("expected PopScreenMsg, got %T", msg)
	}
}

func TestHelpScreen_QuestionMarkPops(t *testing.T) {
	h := NewHelpScreen()

	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if cmd == nil {
		t.Fatal("? should produce a command")
	}

	msg := cmd()
	if _, ok := msg.(tui.PopScreenMsg); !ok {
		t.Fatalf("expected PopScreenMsg, got %T", msg)
	}
}

func TestHelpScreen_ViewContent(t *testing.T) {
	h := NewHelpScreen()
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := h.View()

	// Check section headers are present.
	for _, header := range []string{"Global", "Navigation", "Home Screen", "List Screen", "Detail Screen", "Filter Syntax", "Search"} {
		if !strings.Contains(view, header) {
			t.Errorf("view should contain section header %q", header)
		}
	}

	// Check some key bindings are present.
	for _, binding := range []string{"Quit", "Go back", "Move down", "Filter or search", "Copy ID", "Toggle help"} {
		if !strings.Contains(view, binding) {
			t.Errorf("view should contain binding description %q", binding)
		}
	}

	// Check dismiss hint.
	if !strings.Contains(view, "Press ? or Esc to close") {
		t.Error("view should contain dismiss hint")
	}
}

func TestHelpScreen_IgnoresOtherKeys(t *testing.T) {
	h := NewHelpScreen()

	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if cmd != nil {
		t.Error("j key should not produce a command on help screen")
	}
}
