package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/schema"
	"github.com/klaassen-consulting/jc/internal/tui"
)

func testEntries() []tui.ResourceEntry {
	return []tui.ResourceEntry{
		{
			Key:         "users",
			DisplayName: "Users",
			Category:    tui.CategoryIdentity,
			ClientType:  tui.ClientV1,
			Schema:      schema.Resources["users"],
		},
		{
			Key:         "devices",
			DisplayName: "Devices",
			Category:    tui.CategoryDevices,
			ClientType:  tui.ClientV1,
			Schema:      schema.Resources["devices"],
		},
		{
			Key:         "policies",
			DisplayName: "Policies",
			Category:    tui.CategoryManagement,
			ClientType:  tui.ClientV2,
			Schema:      schema.Resources["policies"],
		},
	}
}

func TestHomeScreen_Title(t *testing.T) {
	h := NewHomeScreen(testEntries())
	if h.Title() != "Home" {
		t.Errorf("Title = %q, want 'Home'", h.Title())
	}
}

func TestHomeScreen_ViewShowsResources(t *testing.T) {
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := h.View()
	if !strings.Contains(view, "Users") {
		t.Error("view should contain 'Users'")
	}
	if !strings.Contains(view, "Devices") {
		t.Error("view should contain 'Devices'")
	}
	if !strings.Contains(view, "Policies") {
		t.Error("view should contain 'Policies'")
	}
}

func TestHomeScreen_ViewShowsCategories(t *testing.T) {
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := h.View()
	if !strings.Contains(view, "Identity") {
		t.Error("view should contain 'Identity' category")
	}
}

func TestHomeScreen_ViewShowsTitle(t *testing.T) {
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := h.View()
	if !strings.Contains(view, "JumpCloud TUI") {
		t.Error("view should contain title")
	}
}

func TestHomeScreen_CursorMovement(t *testing.T) {
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Initial cursor at 0.
	if h.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", h.cursor)
	}

	// Move down.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if h.cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", h.cursor)
	}

	// Move up.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if h.cursor != 0 {
		t.Errorf("cursor after k = %d, want 0", h.cursor)
	}
}

func TestHomeScreen_EnterPushesListScreen(t *testing.T) {
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should produce a command")
	}

	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}
	if pushMsg.Screen.Title() != "Users" {
		t.Errorf("pushed screen title = %q, want 'Users'", pushMsg.Screen.Title())
	}
}

func TestHomeScreen_DKeyPushesDashboard(t *testing.T) {
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if cmd == nil {
		t.Fatal("'d' should produce a command")
	}

	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}
	if pushMsg.Screen.Title() != "Dashboard" {
		t.Errorf("pushed screen title = %q, want 'Dashboard'", pushMsg.Screen.Title())
	}
}

func TestHomeScreen_ShowsVerbCount(t *testing.T) {
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := h.View()
	if !strings.Contains(view, "ops)") {
		t.Error("view should contain verb count like '(N ops)'")
	}
}
