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

func TestListScreen_CopyIDProducesFlash(t *testing.T) {
	// Override clipboard to avoid real clipboard access.
	var copied string
	origClip := clipboardWriteFunc
	clipboardWriteFunc = func(s string) error { copied = s; return nil }
	defer func() { clipboardWriteFunc = origClip }()

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

	gen := ls.generation
	data := []json.RawMessage{
		json.RawMessage(`{"_id":"abc123def456abc123def456","username":"alice"}`),
	}
	ls.Update(fetch.ListResultMsg{
		ResourceKey: "users",
		Data:        data,
		TotalCount:  1,
		Generation:  gen,
	})

	// Press 'c' to copy.
	_, cmd := ls.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd == nil {
		t.Fatal("'c' should return a command")
	}

	msg := cmd()
	flash, ok := msg.(tui.FlashMsg)
	if !ok {
		t.Fatalf("expected FlashMsg, got %T", msg)
	}
	if flash.Text != "Copied: abc123def456abc123def456" {
		t.Errorf("flash text = %q, want 'Copied: abc123def456abc123def456'", flash.Text)
	}
	if copied != "abc123def456abc123def456" {
		t.Errorf("clipboard = %q, want 'abc123def456abc123def456'", copied)
	}
}

func TestListScreen_CopyNoRowsIsNoop(t *testing.T) {
	origClip := clipboardWriteFunc
	clipboardWriteFunc = func(s string) error { return nil }
	defer func() { clipboardWriteFunc = origClip }()

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

	_, cmd := ls.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd != nil {
		t.Error("copy with no rows should be nil")
	}
}

func TestListScreen_CopyPivotField(t *testing.T) {
	var copied string
	origClip := clipboardWriteFunc
	clipboardWriteFunc = func(s string) error { copied = s; return nil }
	defer func() { clipboardWriteFunc = origClip }()

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

	gen := ls.generation
	data := []json.RawMessage{
		json.RawMessage(`{"system_id":"abc123def456abc123def456","name":"macOS"}`),
	}
	ls.Update(fetch.ListResultMsg{
		ResourceKey: "system-insights",
		Data:        data,
		TotalCount:  1,
		Generation:  gen,
	})

	_, cmd := ls.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd == nil {
		t.Fatal("copy should return a command for pivot field")
	}

	msg := cmd()
	flash, ok := msg.(tui.FlashMsg)
	if !ok {
		t.Fatalf("expected FlashMsg, got %T", msg)
	}
	if !ok || flash.Text != "Copied: abc123def456abc123def456" {
		t.Errorf("flash text = %q", flash.Text)
	}
	if copied != "abc123def456abc123def456" {
		t.Errorf("clipboard = %q", copied)
	}
}

func TestListScreen_SearchUsesPostEndpoint(t *testing.T) {
	entry := tui.ResourceEntry{
		Key:            "users",
		DisplayName:    "Users",
		Category:       tui.CategoryIdentity,
		ClientType:     tui.ClientV1,
		ListEndpoint:   "/systemusers",
		SearchEndpoint: "/search/systemusers",
		SearchFields:   []string{"username", "email", "firstname", "lastname"},
		Schema:         schema.Resources["users"],
	}

	ls := NewListScreen(entry)
	ls.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Verify that the entry has SearchEndpoint populated.
	if ls.entry.SearchEndpoint == "" {
		t.Error("expected SearchEndpoint to be set")
	}
	if len(ls.entry.SearchFields) != 4 {
		t.Errorf("expected 4 search fields, got %d", len(ls.entry.SearchFields))
	}
}

func TestListScreen_NoSearchEndpointForPolicies(t *testing.T) {
	entry := tui.ResourceEntry{
		Key:          "policies",
		DisplayName:  "Policies",
		Category:     tui.CategoryManagement,
		ClientType:   tui.ClientV2,
		ListEndpoint: "/policies",
		Schema:       schema.Resources["policies"],
	}

	ls := NewListScreen(entry)

	// Policies should not have a search endpoint.
	if ls.entry.SearchEndpoint != "" {
		t.Errorf("policies should have empty SearchEndpoint, got %q", ls.entry.SearchEndpoint)
	}
	if len(ls.entry.SearchFields) != 0 {
		t.Errorf("policies should have no SearchFields, got %d", len(ls.entry.SearchFields))
	}
}

func TestListScreen_ExportModeToggle(t *testing.T) {
	origClip := clipboardWriteFunc
	clipboardWriteFunc = func(s string) error { return nil }
	defer func() { clipboardWriteFunc = origClip }()

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

	gen := ls.generation
	data := []json.RawMessage{
		json.RawMessage(`{"_id":"abc123def456abc123def456","username":"alice"}`),
	}
	ls.Update(fetch.ListResultMsg{
		ResourceKey: "users",
		Data:        data,
		TotalCount:  1,
		Generation:  gen,
	})

	// Press 'e' to enter export mode.
	ls.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if !ls.exporting {
		t.Error("expected exporting to be true after 'e'")
	}

	// Press 'esc' to cancel.
	ls.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if ls.exporting {
		t.Error("expected exporting to be false after 'esc'")
	}
}

func TestListScreen_ExportJSON(t *testing.T) {
	var clipped string
	origClip := clipboardWriteFunc
	clipboardWriteFunc = func(s string) error { clipped = s; return nil }
	defer func() { clipboardWriteFunc = origClip }()

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

	gen := ls.generation
	data := []json.RawMessage{
		json.RawMessage(`{"_id":"abc123def456abc123def456","username":"alice"}`),
		json.RawMessage(`{"_id":"eee555fff666aaa777bbb888","username":"bob"}`),
	}
	ls.Update(fetch.ListResultMsg{
		ResourceKey: "users",
		Data:        data,
		TotalCount:  2,
		Generation:  gen,
	})

	// Press 'e' then 'j' to export JSON to clipboard.
	ls.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	_, cmd := ls.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if cmd == nil {
		t.Fatal("expected command from 'j' export")
	}

	msg := cmd()
	flash, ok := msg.(tui.FlashMsg)
	if !ok {
		t.Fatalf("expected FlashMsg, got %T", msg)
	}
	if flash.Text != "Copied 2 items as JSON" {
		t.Errorf("flash = %q, want 'Copied 2 items as JSON'", flash.Text)
	}
	if clipped == "" {
		t.Error("clipboard should not be empty")
	}
	if !ls.exporting == true {
		// exporting should be false after export.
	}
	if ls.exporting {
		t.Error("exporting should be false after export completes")
	}
}

func TestListScreen_ExportNoRowsIsNoop(t *testing.T) {
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

	// Press 'e' with no rows — should not enter export mode.
	ls.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if ls.exporting {
		t.Error("should not enter export mode with no rows")
	}
}

func TestListScreen_CreatePushesForm(t *testing.T) {
	entry := tui.ResourceEntry{
		Key:          "iplists",
		DisplayName:  "IP Lists",
		Category:     tui.CategorySecurity,
		ClientType:   tui.ClientV2,
		ListEndpoint: "/iplists",
		Schema:       schema.Resources["iplists"],
	}

	ls := NewListScreen(entry)
	ls.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := ls.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd == nil {
		t.Fatal("expected command from 'n' key")
	}

	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}

	form, ok := pushMsg.Screen.(*FormScreen)
	if !ok {
		t.Fatalf("expected *FormScreen, got %T", pushMsg.Screen)
	}
	if form.mode != "create" {
		t.Errorf("form mode = %q, want 'create'", form.mode)
	}
}

func TestListScreen_CreateNoVerb(t *testing.T) {
	// policy-templates don't have "create" verb.
	entry := tui.ResourceEntry{
		Key:          "policy-templates",
		DisplayName:  "Policy Templates",
		Category:     tui.CategoryApplications,
		ClientType:   tui.ClientV2,
		ListEndpoint: "/policytemplates",
		Schema:       schema.Resources["policy-templates"],
	}

	ls := NewListScreen(entry)
	ls.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := ls.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd != nil {
		t.Error("should not push form for resource without create verb")
	}
}

func TestListScreen_RefreshListMsg(t *testing.T) {
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

	// RefreshListMsg should trigger a re-fetch.
	_, cmd := ls.Update(tui.RefreshListMsg{})
	if cmd == nil {
		t.Error("RefreshListMsg should return a fetch command")
	}
	if !ls.loading {
		t.Error("loading should be true after RefreshListMsg")
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
