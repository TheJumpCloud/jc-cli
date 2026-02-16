package screen

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/fetch"
)

func TestDashboardScreen_Title(t *testing.T) {
	d := NewDashboardScreen()
	if d.Title() != "Dashboard" {
		t.Errorf("Title = %q, want 'Dashboard'", d.Title())
	}
}

func TestDashboardScreen_InitFiresFetches(t *testing.T) {
	d := NewDashboardScreen()
	cmd := d.Init()
	if cmd == nil {
		t.Fatal("Init should return a batch command")
	}

	// All resources should be marked loading.
	for _, res := range dashResources {
		if !d.loading[res.Key] {
			t.Errorf("resource %q should be loading after Init", res.Key)
		}
	}
}

func TestDashboardScreen_HandlesListResult(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()

	gen := d.generations["users"]
	d.Update(fetch.ListResultMsg{
		ResourceKey: "users",
		Data: []json.RawMessage{
			json.RawMessage(`{"_id":"u1"}`),
			json.RawMessage(`{"_id":"u2"}`),
		},
		TotalCount: 42,
		Generation: gen,
	})

	if d.loading["users"] {
		t.Error("users should not be loading after result")
	}
	if d.counts["users"] != 42 {
		t.Errorf("users count = %d, want 42", d.counts["users"])
	}
}

func TestDashboardScreen_IgnoresStaleResult(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()

	d.Update(fetch.ListResultMsg{
		ResourceKey: "users",
		TotalCount:  99,
		Generation:  d.generations["users"] - 1,
	})

	if !d.loading["users"] {
		t.Error("users should still be loading (stale result)")
	}
}

func TestDashboardScreen_HandlesError(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()

	gen := d.generations["devices"]
	d.Update(fetch.ListResultMsg{
		ResourceKey: "devices",
		Generation:  gen,
		Err:         errTest,
	})

	if d.loading["devices"] {
		t.Error("devices should not be loading after error")
	}
	if d.errors["devices"] == "" {
		t.Error("devices should have an error message")
	}
}

func TestDashboardScreen_EscPops(t *testing.T) {
	d := NewDashboardScreen()
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should return a command")
	}

	msg := cmd()
	if _, ok := msg.(tui.PopScreenMsg); !ok {
		t.Errorf("expected PopScreenMsg, got %T", msg)
	}
}

func TestDashboardScreen_ViewShowsLabels(t *testing.T) {
	d := NewDashboardScreen()
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := d.View()
	if !strings.Contains(view, "Dashboard") {
		t.Error("view should contain 'Dashboard'")
	}
	if !strings.Contains(view, "Users") {
		t.Error("view should contain 'Users'")
	}
	if !strings.Contains(view, "Devices") {
		t.Error("view should contain 'Devices'")
	}
}

func TestDashboardScreen_ViewShowsCounts(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	gen := d.generations["users"]
	d.Update(fetch.ListResultMsg{
		ResourceKey: "users",
		TotalCount:  15,
		Generation:  gen,
	})

	view := d.View()
	if !strings.Contains(view, "15") {
		t.Error("view should show count '15' for users")
	}
}

// errTest is a test error.
var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }
