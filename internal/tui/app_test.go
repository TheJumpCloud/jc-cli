package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestApp_Init(t *testing.T) {
	home := &mockScreen{title: "Home"}
	app := NewApp(home)
	cmd := app.Init()
	// Home screen returns nil Init, so cmd should be nil.
	if cmd != nil {
		t.Error("Init should return nil for mockScreen")
	}
}

func TestApp_QuitOnQ(t *testing.T) {
	app := NewApp(&mockScreen{title: "Home"})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("quit should return a command")
	}
	// The command should be tea.Quit.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("quit command should produce tea.QuitMsg, got %T", msg)
	}
}

func TestApp_PushScreen(t *testing.T) {
	app := NewApp(&mockScreen{title: "Home"})

	newScreen := &mockScreen{title: "Users"}
	app.Update(PushScreenMsg{Screen: newScreen})

	view := app.View()
	if !strings.Contains(view, "Users") {
		t.Error("view should contain 'Users' after push")
	}
}

func TestApp_PopScreen(t *testing.T) {
	app := NewApp(&mockScreen{title: "Home"})
	app.Update(PushScreenMsg{Screen: &mockScreen{title: "Users"}})
	app.Update(PopScreenMsg{})

	view := app.View()
	if !strings.Contains(view, "Home") {
		t.Error("view should contain 'Home' after pop")
	}
}

func TestApp_PopLastScreenQuits(t *testing.T) {
	app := NewApp(&mockScreen{title: "Home"})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	_, cmd := app.Update(PopScreenMsg{})
	if cmd == nil {
		t.Fatal("pop on last screen should return quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("pop on last screen should produce tea.QuitMsg, got %T", msg)
	}
}

func TestApp_WindowResize(t *testing.T) {
	app := NewApp(&mockScreen{title: "Home"})
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if app.width != 120 || app.height != 40 {
		t.Errorf("size = %dx%d, want 120x40", app.width, app.height)
	}
}

func TestApp_BreadcrumbsInView(t *testing.T) {
	app := NewApp(&mockScreen{title: "Home"})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(PushScreenMsg{Screen: &mockScreen{title: "Users"}})

	view := app.View()
	if !strings.Contains(view, "Home") || !strings.Contains(view, "Users") {
		t.Errorf("view should contain breadcrumbs 'Home' and 'Users', got:\n%s", view)
	}
}

func TestApp_HelpTextChangesWithDepth(t *testing.T) {
	app := NewApp(&mockScreen{title: "Home"})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	homeHelp := app.helpText()
	if !strings.Contains(homeHelp, "quit") {
		t.Errorf("home help should contain 'quit', got %q", homeHelp)
	}

	app.Update(PushScreenMsg{Screen: &mockScreen{title: "Users"}})
	deepHelp := app.helpText()
	if !strings.Contains(deepHelp, "back") {
		t.Errorf("deep help should contain 'back', got %q", deepHelp)
	}
}
