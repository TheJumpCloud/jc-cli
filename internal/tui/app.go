package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui/component"
)

// App is the top-level Bubbletea model.
type App struct {
	nav       NavStack
	statusBar component.StatusBar
	width     int
	height    int
	quitting  bool
}

// NewApp creates the TUI application with the given initial screen.
func NewApp(homeScreen Screen) *App {
	app := &App{}
	app.nav.Push(homeScreen)
	return app
}

func (a *App) Init() tea.Cmd {
	screen := a.nav.Current()
	if screen == nil {
		return nil
	}
	return screen.Init()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.statusBar.Width = msg.Width
		// Forward to active screen.
		if screen := a.nav.Current(); screen != nil {
			updated, cmd := screen.Update(msg)
			a.nav.Replace(updated.(Screen))
			return a, cmd
		}
		return a, nil

	case tea.KeyMsg:
		// Global quit.
		if key.Matches(msg, GlobalKeyMap.Quit) {
			a.quitting = true
			return a, tea.Quit
		}

		// Help toggle (handled at app level).
		if key.Matches(msg, GlobalKeyMap.Help) {
			// Toggle help display — for now just pass through.
		}

	case PushScreenMsg:
		a.nav.Push(msg.Screen)
		return a, msg.Screen.Init()

	case PopScreenMsg:
		if a.nav.Depth() <= 1 {
			a.quitting = true
			return a, tea.Quit
		}
		a.nav.Pop()
		// Re-send window size to the now-active screen.
		if screen := a.nav.Current(); screen != nil {
			updated, cmd := screen.Update(tea.WindowSizeMsg{
				Width:  a.width,
				Height: a.height,
			})
			a.nav.Replace(updated.(Screen))
			return a, cmd
		}
		return a, nil

	case ReplaceScreenMsg:
		a.nav.Replace(msg.Screen)
		return a, msg.Screen.Init()
	}

	// Delegate to active screen.
	if screen := a.nav.Current(); screen != nil {
		updated, cmd := screen.Update(msg)
		a.nav.Replace(updated.(Screen))
		return a, cmd
	}

	return a, nil
}

func (a *App) View() string {
	if a.quitting {
		return ""
	}

	screen := a.nav.Current()
	if screen == nil {
		return ""
	}

	// Update statusbar.
	a.statusBar.Breadcrumbs = a.nav.Breadcrumbs()
	a.statusBar.Help = a.helpText()

	content := screen.View()

	return content + "\n" + a.statusBar.View()
}

func (a *App) helpText() string {
	depth := a.nav.Depth()
	if depth <= 1 {
		return "q:quit  /:filter  enter:open  ?:help"
	}
	return "esc:back  /:filter  s:sort  a:all fields  r:refresh  ?:help"
}
