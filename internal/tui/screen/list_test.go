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
