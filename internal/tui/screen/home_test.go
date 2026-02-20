package screen

import (
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/schema"
	"github.com/klaassen-consulting/jc/internal/tui"
)

// overrideBookmarks replaces the bookmark loader/saver for testing.
// Returns the saved keys slice (populated by the saver).
func overrideBookmarks(t *testing.T, initial []string) *[]string {
	t.Helper()
	origLoader := bookmarkLoader
	origSaver := bookmarkSaver
	t.Cleanup(func() {
		bookmarkLoader = origLoader
		bookmarkSaver = origSaver
	})

	bookmarkLoader = func() []string { return initial }
	var saved []string
	bookmarkSaver = func(keys []string) error {
		saved = append(saved[:0], keys...)
		return nil
	}
	return &saved
}

func testEntries() []tui.ResourceEntry {
	return []tui.ResourceEntry{
		{
			Key:         "users",
			DisplayName: "Users",
			Category:    tui.CategoryUserMgmt,
			ClientType:  tui.ClientV1,
			Schema:      schema.Resources["users"],
		},
		{
			Key:         "devices",
			DisplayName: "Devices",
			Category:    tui.CategoryDeviceMgmt,
			ClientType:  tui.ClientV1,
			Schema:      schema.Resources["devices"],
		},
		{
			Key:         "policies",
			DisplayName: "Policies",
			Category:    tui.CategoryDeviceMgmt,
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
	if !strings.Contains(view, "User Management") {
		t.Error("view should contain 'User Management' category")
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

func TestHomeScreen_BookmarkToggle(t *testing.T) {
	saved := overrideBookmarks(t, nil)
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Bookmark "Users" (cursor at 0).
	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if cmd == nil {
		t.Fatal("'b' should produce a command")
	}
	msg := cmd()
	flash, ok := msg.(tui.FlashMsg)
	if !ok {
		t.Fatalf("expected FlashMsg, got %T", msg)
	}
	if !strings.Contains(flash.Text, "Bookmarked") {
		t.Errorf("flash = %q, want contains 'Bookmarked'", flash.Text)
	}
	if !h.bookmarks["users"] {
		t.Error("'users' should be bookmarked")
	}
	if len(*saved) != 1 || (*saved)[0] != "users" {
		t.Errorf("saved bookmarks = %v, want [users]", *saved)
	}

	// Toggle again to remove.
	// Cursor is at 0 which now points to bookmark "Users" in display.
	_, cmd = h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	msg = cmd()
	flash = msg.(tui.FlashMsg)
	if !strings.Contains(flash.Text, "Removed") {
		t.Errorf("flash = %q, want contains 'Removed'", flash.Text)
	}
	if h.bookmarks["users"] {
		t.Error("'users' should no longer be bookmarked")
	}
	if len(*saved) != 0 {
		t.Errorf("saved bookmarks = %v, want empty", *saved)
	}
}

func TestHomeScreen_BookmarksSectionInView(t *testing.T) {
	overrideBookmarks(t, []string{"devices"})
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := h.View()
	if !strings.Contains(view, "Bookmarks") {
		t.Error("view should contain 'Bookmarks' section")
	}

	// "Devices" should appear in Bookmarks section AND in its regular category.
	devicesCount := strings.Count(view, "Devices")
	if devicesCount < 2 {
		t.Errorf("'Devices' appears %d times, want >= 2 (bookmarks + regular)", devicesCount)
	}
}

func TestHomeScreen_BookmarksHiddenWhenFiltering(t *testing.T) {
	overrideBookmarks(t, []string{"users"})
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Verify bookmarks visible before filtering.
	view := h.View()
	if !strings.Contains(view, "Bookmarks") {
		t.Fatal("view should contain 'Bookmarks' before filtering")
	}

	// Start filtering.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	view = h.View()
	if strings.Contains(view, "Bookmarks") {
		t.Error("view should NOT contain 'Bookmarks' during filtering")
	}
}

func TestHomeScreen_BookmarkPersists(t *testing.T) {
	saved := overrideBookmarks(t, nil)
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Move to "Devices" (index 1) and bookmark.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})

	if len(*saved) != 1 {
		t.Fatalf("saved bookmarks count = %d, want 1", len(*saved))
	}
	if (*saved)[0] != "devices" {
		t.Errorf("saved[0] = %q, want 'devices'", (*saved)[0])
	}

	// Also bookmark "Policies" (move down once more from current position).
	// After bookmarking devices, the display now has bookmarks section.
	// Cursor is still at 1, which in the new display is still "devices" in bookmarks.
	// We need to navigate to "Policies" in the regular section.
	// Display: [bm:Devices, Users, Devices, Policies] → cursor 1 is bookmark Devices,
	// we need cursor at 3 for Policies.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // 2 = Users
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // 3 = Devices (regular)
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // 4 = Policies
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})

	sort.Strings(*saved)
	if len(*saved) != 2 {
		t.Fatalf("saved bookmarks = %v, want 2 entries", *saved)
	}
	want := []string{"devices", "policies"}
	for i, k := range want {
		if (*saved)[i] != k {
			t.Errorf("saved[%d] = %q, want %q", i, (*saved)[i], k)
		}
	}
}

func TestHomeScreen_BookmarkLoadsFromConfig(t *testing.T) {
	overrideBookmarks(t, []string{"users", "policies"})
	h := NewHomeScreen(testEntries())

	if !h.bookmarks["users"] {
		t.Error("'users' should be loaded from config bookmarks")
	}
	if !h.bookmarks["policies"] {
		t.Error("'policies' should be loaded from config bookmarks")
	}
	if h.bookmarks["devices"] {
		t.Error("'devices' should NOT be bookmarked")
	}
}
