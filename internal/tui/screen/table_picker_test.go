package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
)

func testSystemInsightsEntry() tui.ResourceEntry {
	return tui.ResourceEntry{
		Key:          "system-insights",
		DisplayName:  "System Insights",
		Category:     tui.CategoryDevices,
		ClientType:   tui.ClientV2,
		ListEndpoint: "/systeminsights",
	}
}

var testTables = []string{"os_version", "users", "battery", "disk_info", "apps"}

func TestTablePickerScreen_Title(t *testing.T) {
	tp := NewTablePickerScreen(testSystemInsightsEntry(), testTables)
	if tp.Title() != "System Insights" {
		t.Errorf("Title = %q, want 'System Insights'", tp.Title())
	}
}

func TestTablePickerScreen_ViewShowsTables(t *testing.T) {
	tp := NewTablePickerScreen(testSystemInsightsEntry(), testTables)
	tp.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := tp.View()
	for _, table := range testTables {
		if !strings.Contains(view, table) {
			t.Errorf("view should contain table %q", table)
		}
	}
}

func TestTablePickerScreen_ViewSortsTables(t *testing.T) {
	tp := NewTablePickerScreen(testSystemInsightsEntry(), testTables)
	tp.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := tp.View()
	// Tables should be sorted: apps, battery, disk_info, os_version, users
	appsIdx := strings.Index(view, "apps")
	usersIdx := strings.Index(view, "users")
	if appsIdx >= usersIdx {
		t.Error("tables should be sorted alphabetically")
	}
}

func TestTablePickerScreen_CursorMovement(t *testing.T) {
	tp := NewTablePickerScreen(testSystemInsightsEntry(), testTables)
	tp.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	if tp.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", tp.cursor)
	}

	tp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if tp.cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", tp.cursor)
	}

	tp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if tp.cursor != 0 {
		t.Errorf("cursor after k = %d, want 0", tp.cursor)
	}
}

func TestTablePickerScreen_FilterNarrowsList(t *testing.T) {
	tp := NewTablePickerScreen(testSystemInsightsEntry(), testTables)
	tp.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Activate filter.
	tp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !tp.filtering {
		t.Fatal("expected filtering mode")
	}

	// Type "os".
	tp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	tp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})

	if len(tp.filtered) != 1 {
		t.Errorf("filtered length = %d, want 1 (os_version)", len(tp.filtered))
	}
	if len(tp.filtered) > 0 && tp.filtered[0] != "os_version" {
		t.Errorf("filtered[0] = %q, want 'os_version'", tp.filtered[0])
	}
}

func TestTablePickerScreen_EnterPushesListScreen(t *testing.T) {
	tp := NewTablePickerScreen(testSystemInsightsEntry(), testTables)
	tp.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Tables are sorted, so first is "apps".
	_, cmd := tp.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should produce a command")
	}

	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}

	ls, ok := pushMsg.Screen.(*ListScreen)
	if !ok {
		t.Fatalf("expected *ListScreen, got %T", pushMsg.Screen)
	}

	if ls.entry.ListEndpoint != "/systeminsights/apps" {
		t.Errorf("endpoint = %q, want '/systeminsights/apps'", ls.entry.ListEndpoint)
	}
	if !strings.Contains(ls.entry.DisplayName, "apps") {
		t.Errorf("display name = %q, should contain 'apps'", ls.entry.DisplayName)
	}
}

func TestTablePickerScreen_EscPops(t *testing.T) {
	tp := NewTablePickerScreen(testSystemInsightsEntry(), testTables)
	tp.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := tp.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should produce a command")
	}

	msg := cmd()
	if _, ok := msg.(tui.PopScreenMsg); !ok {
		t.Fatalf("expected PopScreenMsg, got %T", msg)
	}
}
