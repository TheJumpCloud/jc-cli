package screen

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/tui"
)

// TestApp_PasswordPolicy_SpaceTogglesThroughFullStack drives a REAL
// tui.App wrapping a REAL PasswordPolicyScreen through the exact
// runtime path `jc tui` uses (Batch unwrapped like bubbletea does),
// and asserts space toggles a bool + the change renders.
func TestApp_PasswordPolicy_SpaceTogglesThroughFullStack(t *testing.T) {
	var putBody []byte
	srv := startOrgServer(t, &putBody)
	overridePasswordPolicyClient(t, srv.URL)

	pp := NewPasswordPolicyScreen()
	app := tui.NewApp(pp)
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app = drainApp(t, app, pp.Init()).(*tui.App)

	if pp.stage != ppStageEdit {
		t.Fatalf("screen did not reach edit stage: %v (err %q)", pp.stage, pp.err)
	}
	before := pp.policy["enableMinLength"]

	m, _ := app.Update(tea.KeyMsg{Type: tea.KeySpace})
	app = m.(*tui.App)

	if pp.policy["enableMinLength"] == before {
		t.Fatalf("space did not toggle through the app: still %v", pp.policy["enableMinLength"])
	}
	if !strings.Contains(app.View(), "> ") {
		t.Errorf("cursor marker not visible:\n%s", app.View())
	}
}

// drainApp runs a cmd chain through app.Update, unwrapping Batch the
// way the bubbletea runtime does; spinner ticks are dropped so the
// drain terminates.
func drainApp(t *testing.T, app tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	if cmd == nil {
		return app
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			app = drainApp(t, app, c)
		}
		return app
	}
	if _, isTick := msg.(spinner.TickMsg); isTick {
		return app
	}
	var next tea.Cmd
	app, next = app.Update(msg)
	return drainApp(t, app, next)
}
