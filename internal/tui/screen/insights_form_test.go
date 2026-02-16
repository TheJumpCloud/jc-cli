package screen

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/fetch"
)

func testInsightsEntry() tui.ResourceEntry {
	return tui.ResourceEntry{
		Key:         "insights",
		DisplayName: "Directory Insights",
		Category:    tui.CategoryAudit,
		ClientType:  tui.ClientInsights,
	}
}

func TestInsightsFormScreen_Title(t *testing.T) {
	f := NewInsightsFormScreen(testInsightsEntry())
	if f.Title() != "Directory Insights" {
		t.Errorf("Title = %q, want 'Directory Insights'", f.Title())
	}
}

func TestInsightsFormScreen_ViewShowsForm(t *testing.T) {
	f := NewInsightsFormScreen(testInsightsEntry())
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := f.View()
	if !strings.Contains(view, "Service") {
		t.Error("view should contain 'Service' field")
	}
	if !strings.Contains(view, "Time Range") {
		t.Error("view should contain 'Time Range' field")
	}
	if !strings.Contains(view, "Event Type") {
		t.Error("view should contain 'Event Type' field")
	}
}

func TestInsightsFormScreen_ServiceCycling(t *testing.T) {
	f := NewInsightsFormScreen(testInsightsEntry())
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Initial service is "all" (index 0).
	if f.serviceIdx != 0 {
		t.Errorf("initial serviceIdx = %d, want 0", f.serviceIdx)
	}

	// Cycle right.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if f.serviceIdx != 1 {
		t.Errorf("serviceIdx after l = %d, want 1", f.serviceIdx)
	}

	// Cycle left.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if f.serviceIdx != 0 {
		t.Errorf("serviceIdx after h = %d, want 0", f.serviceIdx)
	}

	// Cycle left wraps.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if f.serviceIdx != len(api.ValidInsightsServices)-1 {
		t.Errorf("serviceIdx after wrap = %d, want %d", f.serviceIdx, len(api.ValidInsightsServices)-1)
	}
}

func TestInsightsFormScreen_TimeRangeCycling(t *testing.T) {
	f := NewInsightsFormScreen(testInsightsEntry())
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Move focus to time range.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if f.focusedField != 1 {
		t.Fatalf("focusedField = %d, want 1 (time range)", f.focusedField)
	}

	// Initial time range is "24h" (index 2).
	if f.timeRangeIdx != 2 {
		t.Errorf("initial timeRangeIdx = %d, want 2", f.timeRangeIdx)
	}

	// Cycle right.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if f.timeRangeIdx != 3 {
		t.Errorf("timeRangeIdx after l = %d, want 3", f.timeRangeIdx)
	}
}

func TestInsightsFormScreen_EnterSubmitsQuery(t *testing.T) {
	f := NewInsightsFormScreen(testInsightsEntry())
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should produce a command")
	}
	if !f.submitted {
		t.Error("submitted should be true after Enter")
	}
	if !f.loading {
		t.Error("loading should be true after Enter")
	}
}

func TestInsightsFormScreen_ResultsPopulateTable(t *testing.T) {
	f := NewInsightsFormScreen(testInsightsEntry())
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Submit query.
	f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	gen := f.generation

	// Simulate results.
	events := []json.RawMessage{
		json.RawMessage(`{"timestamp":"2024-01-01T00:00:00Z","event_type":"user_login","success":true}`),
		json.RawMessage(`{"timestamp":"2024-01-01T01:00:00Z","event_type":"user_logout","success":true}`),
	}
	f.Update(fetch.InsightsResultMsg{
		ResourceKey: "insights",
		Data:        events,
		Generation:  gen,
	})

	if f.loading {
		t.Error("loading should be false after results")
	}
	if len(f.table.Rows) != 2 {
		t.Errorf("table rows = %d, want 2", len(f.table.Rows))
	}

	view := f.View()
	if !strings.Contains(view, "2 events") {
		t.Error("view should show event count")
	}
}

func TestInsightsFormScreen_ResultsError(t *testing.T) {
	f := NewInsightsFormScreen(testInsightsEntry())
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Submit query.
	f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	gen := f.generation

	// Simulate error.
	f.Update(fetch.InsightsResultMsg{
		ResourceKey: "insights",
		Generation:  gen,
		Err:         fmt.Errorf("API error"),
	})

	if f.err == "" {
		t.Error("err should be set after error result")
	}
	view := f.View()
	if !strings.Contains(view, "API error") {
		t.Error("view should show error message")
	}
}

func TestInsightsFormScreen_EscBackToForm(t *testing.T) {
	f := NewInsightsFormScreen(testInsightsEntry())
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Submit and get results.
	f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	gen := f.generation
	f.Update(fetch.InsightsResultMsg{
		ResourceKey: "insights",
		Data:        []json.RawMessage{json.RawMessage(`{"id":"1"}`)},
		Generation:  gen,
	})

	if !f.submitted {
		t.Fatal("should be in submitted mode")
	}

	// Press Esc to go back to form.
	f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if f.submitted {
		t.Error("Esc should return to form mode")
	}
}

func TestInsightsFormScreen_EscFromFormPops(t *testing.T) {
	f := NewInsightsFormScreen(testInsightsEntry())
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc from form should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(tui.PopScreenMsg); !ok {
		t.Fatalf("expected PopScreenMsg, got %T", msg)
	}
}

func TestInsightsFormScreen_StaleResultIgnored(t *testing.T) {
	f := NewInsightsFormScreen(testInsightsEntry())
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Submit query.
	f.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Send result with wrong generation.
	f.Update(fetch.InsightsResultMsg{
		ResourceKey: "insights",
		Data:        []json.RawMessage{json.RawMessage(`{"id":"1"}`)},
		Generation:  f.generation - 1, // stale
	})

	if !f.loading {
		t.Error("loading should still be true (stale result ignored)")
	}
}
