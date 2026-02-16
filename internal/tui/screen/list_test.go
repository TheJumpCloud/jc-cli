package screen

import (
	"encoding/json"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/schema"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/fetch"
)

func TestListScreen_DeriveColumnsFromData(t *testing.T) {
	// Simulate a system insights table with no default fields.
	entry := tui.ResourceEntry{
		Key:          "system-insights",
		DisplayName:  "System Insights: os_version",
		Category:     tui.CategoryDevices,
		ClientType:   tui.ClientV2,
		ListEndpoint: "/systeminsights/os_version",
		Schema:       schema.ResourceSchema{DefaultFields: nil},
	}

	ls := NewListScreen(entry)
	ls.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Initially columns should be the fallback ["id"].
	if len(ls.table.Columns) != 1 || ls.table.Columns[0] != "id" {
		t.Errorf("initial columns = %v, want [id]", ls.table.Columns)
	}

	// Simulate data arriving.
	gen := ls.generation
	data := []json.RawMessage{
		json.RawMessage(`{"system_id":"sys001","name":"macOS","version":"14.0","platform":"darwin"}`),
	}
	ls.Update(fetch.ListResultMsg{
		ResourceKey: "system-insights",
		Data:        data,
		TotalCount:  1,
		Generation:  gen,
	})

	// Columns should now be derived from the data.
	if len(ls.table.Columns) != 4 {
		t.Fatalf("columns = %v, want 4 columns derived from data", ls.table.Columns)
	}
	// Should be sorted alphabetically.
	expected := []string{"name", "platform", "system_id", "version"}
	for i, want := range expected {
		if ls.table.Columns[i] != want {
			t.Errorf("columns[%d] = %q, want %q", i, ls.table.Columns[i], want)
		}
	}
}

func TestListScreen_PivotNavigation(t *testing.T) {
	entry := tui.ResourceEntry{
		Key:            "system-insights",
		DisplayName:    "System Insights: os_version",
		Category:       tui.CategoryDevices,
		ClientType:     tui.ClientV2,
		ListEndpoint:   "/systeminsights/os_version",
		PivotField:     "system_id",
		PivotTargetKey: "devices",
		Schema:         schema.ResourceSchema{DefaultFields: nil},
	}

	ls := NewListScreen(entry)
	ls.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Load data with a system_id field.
	gen := ls.generation
	data := []json.RawMessage{
		json.RawMessage(`{"system_id":"abc123def456abc123def456","name":"macOS","version":"14.0"}`),
	}
	ls.Update(fetch.ListResultMsg{
		ResourceKey: "system-insights",
		Data:        data,
		TotalCount:  1,
		Generation:  gen,
	})

	// Press Enter — should trigger pivot navigation.
	_, cmd := ls.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command from Enter key, got nil")
	}

	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}

	detail, ok := pushMsg.Screen.(*DetailScreen)
	if !ok {
		t.Fatalf("expected *DetailScreen, got %T", pushMsg.Screen)
	}

	// Should navigate to the devices resource, not system-insights.
	if detail.entry.Key != "devices" {
		t.Errorf("pivot target key = %q, want 'devices'", detail.entry.Key)
	}
	if detail.id != "abc123def456abc123def456" {
		t.Errorf("pivot id = %q, want 'abc123def456abc123def456'", detail.id)
	}
}

func TestListScreen_PivotWithEmptyFieldSkips(t *testing.T) {
	entry := tui.ResourceEntry{
		Key:            "system-insights",
		DisplayName:    "System Insights: os_version",
		Category:       tui.CategoryDevices,
		ClientType:     tui.ClientV2,
		ListEndpoint:   "/systeminsights/os_version",
		PivotField:     "system_id",
		PivotTargetKey: "devices",
		Schema:         schema.ResourceSchema{DefaultFields: nil},
	}

	ls := NewListScreen(entry)
	ls.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Load data without system_id field.
	gen := ls.generation
	data := []json.RawMessage{
		json.RawMessage(`{"name":"macOS","version":"14.0"}`),
	}
	ls.Update(fetch.ListResultMsg{
		ResourceKey: "system-insights",
		Data:        data,
		TotalCount:  1,
		Generation:  gen,
	})

	// Press Enter — should return nil (no pivot ID).
	_, cmd := ls.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("expected nil command when pivot field is missing, got non-nil")
	}
}

func TestListScreen_KeepsDefaultFieldsWhenPresent(t *testing.T) {
	entry := tui.ResourceEntry{
		Key:          "users",
		DisplayName:  "Users",
		Category:     tui.CategoryIdentity,
		ClientType:   tui.ClientV1,
		ListEndpoint: "/systemusers",
		Schema:       schema.Resources["users"],
	}

	ls := NewListScreen(entry)
	ls.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	origCols := make([]string, len(ls.table.Columns))
	copy(origCols, ls.table.Columns)

	gen := ls.generation
	data := []json.RawMessage{
		json.RawMessage(`{"_id":"u001","username":"alice","email":"alice@example.com","extra":"field"}`),
	}
	ls.Update(fetch.ListResultMsg{
		ResourceKey: "users",
		Data:        data,
		TotalCount:  1,
		Generation:  gen,
	})

	// Columns should NOT change — schema has default fields.
	if len(ls.table.Columns) != len(origCols) {
		t.Errorf("columns changed from %v to %v", origCols, ls.table.Columns)
	}
}
