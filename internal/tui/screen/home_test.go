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

	// Initial grid cursor at {0,0}.
	if h.gridCur.row != 0 || h.gridCur.col != 0 {
		t.Errorf("initial gridCur = {%d,%d}, want {0,0}", h.gridCur.col, h.gridCur.row)
	}

	// Move down.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if h.gridCur.row != 1 {
		t.Errorf("gridCur.row after j = %d, want 1", h.gridCur.row)
	}

	// Move up.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if h.gridCur.row != 0 {
		t.Errorf("gridCur.row after k = %d, want 0", h.gridCur.row)
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

	// Bookmark "Users" (gridCur at {0,0}).
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

	// Toggle again to remove. Grid cursor is still at {0,0} = Users.
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

	// In 1-column mode (width=80), the grid has:
	// Row 0: Users (UserMgmt), Row 1: Devices (DeviceMgmt), Row 2: Policies (DeviceMgmt)
	// Move to "Devices" (row 1) and bookmark.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})

	if len(*saved) != 1 {
		t.Fatalf("saved bookmarks count = %d, want 1", len(*saved))
	}
	if (*saved)[0] != "devices" {
		t.Errorf("saved[0] = %q, want 'devices'", (*saved)[0])
	}

	// Move down to Policies (row 2) and bookmark.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if h.gridCur.row != 2 {
		t.Fatalf("gridCur.row = %d, want 2 (Policies)", h.gridCur.row)
	}
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

// --- New grid-specific tests ---

func TestHomeScreen_GridLayoutAtWideTerminal(t *testing.T) {
	overrideBookmarks(t, nil)
	entries := tui.BuildRegistry()
	h := NewHomeScreen(entries)
	h.Update(tea.WindowSizeMsg{Width: 140, Height: 50})

	view := h.View()
	for _, cat := range []string{"User Management", "Device Management", "Access", "Security", "Insights", "Settings"} {
		if !strings.Contains(view, cat) {
			t.Errorf("view should contain category %q", cat)
		}
	}
}

func TestHomeScreen_PlaceholderNotOpenable(t *testing.T) {
	overrideBookmarks(t, nil)
	placeholder := tui.ResourceEntry{
		Key:         "vault",
		DisplayName: "Vault",
		Category:    tui.CategoryAccess,
		Placeholder: true,
	}
	h := NewHomeScreen([]tui.ResourceEntry{placeholder})
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on placeholder should produce flash command")
	}
	msg := cmd()
	flash, ok := msg.(tui.FlashMsg)
	if !ok {
		t.Fatalf("expected FlashMsg for placeholder, got %T", msg)
	}
	if !strings.Contains(flash.Text, "Coming soon") {
		t.Errorf("flash = %q, want contains 'Coming soon'", flash.Text)
	}
}

func TestHomeScreen_PlaceholderRenderedDim(t *testing.T) {
	overrideBookmarks(t, nil)
	entries := []tui.ResourceEntry{
		{Key: "vault", DisplayName: "Vault", Category: tui.CategoryAccess, Placeholder: true},
		{Key: "apps", DisplayName: "Applications", Category: tui.CategoryAccess, Schema: schema.Resources["apps"]},
	}
	h := NewHomeScreen(entries)
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := h.View()
	if !strings.Contains(view, "Vault") {
		t.Error("view should contain placeholder 'Vault'")
	}
	if !strings.Contains(view, "Applications") {
		t.Error("view should contain regular 'Applications'")
	}
}

func TestHomeScreen_SubMenuPushesSubMenuScreen(t *testing.T) {
	overrideBookmarks(t, nil)
	subMenu := tui.ResourceEntry{
		Key:         "cloud-directories",
		DisplayName: "Cloud Directories",
		Category:    tui.CategoryUserMgmt,
		SubMenu: []tui.ResourceEntry{
			{Key: "gsuite", DisplayName: "Google Workspace"},
			{Key: "office365", DisplayName: "M365"},
		},
	}
	h := NewHomeScreen([]tui.ResourceEntry{subMenu})
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on sub-menu should produce a command")
	}
	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}
	if pushMsg.Screen.Title() != "Cloud Directories" {
		t.Errorf("pushed screen = %q, want 'Cloud Directories'", pushMsg.Screen.Title())
	}
}

func TestHomeScreen_SubMenuShowsArrow(t *testing.T) {
	overrideBookmarks(t, nil)
	subMenu := tui.ResourceEntry{
		Key:         "cloud-directories",
		DisplayName: "Cloud Directories",
		Category:    tui.CategoryUserMgmt,
		SubMenu: []tui.ResourceEntry{
			{Key: "gsuite", DisplayName: "Google Workspace"},
		},
	}
	h := NewHomeScreen([]tui.ResourceEntry{subMenu})
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := h.View()
	// The arrow suffix should be present (Unicode right-pointing triangle).
	if !strings.Contains(view, "\u25b8") {
		t.Error("view should contain sub-menu arrow indicator")
	}
}

func TestHomeScreen_LeftRightNavigation(t *testing.T) {
	overrideBookmarks(t, nil)
	// Need entries in at least 2 different columns.
	entries := []tui.ResourceEntry{
		{Key: "users", DisplayName: "Users", Category: tui.CategoryUserMgmt, Schema: schema.Resources["users"]},         // col 0
		{Key: "devices", DisplayName: "Devices", Category: tui.CategoryDeviceMgmt, Schema: schema.Resources["devices"]}, // col 1
		{Key: "apps", DisplayName: "Apps", Category: tui.CategoryAccess, Schema: schema.Resources["apps"]},              // col 2
	}
	h := NewHomeScreen(entries)
	h.Update(tea.WindowSizeMsg{Width: 140, Height: 50})

	// Verify 3-column mode.
	if h.columns != 3 {
		t.Fatalf("columns = %d, want 3 at width 140", h.columns)
	}

	// Start in column 0. Move right.
	_, _ = h.Update(tea.KeyMsg{Type: tea.KeyRight})
	if h.gridCur.col != 1 {
		t.Errorf("after right arrow, col = %d, want 1", h.gridCur.col)
	}

	// Move right again.
	_, _ = h.Update(tea.KeyMsg{Type: tea.KeyRight})
	if h.gridCur.col != 2 {
		t.Errorf("after 2nd right, col = %d, want 2", h.gridCur.col)
	}

	// Move right at edge — should stay.
	_, _ = h.Update(tea.KeyMsg{Type: tea.KeyRight})
	if h.gridCur.col != 2 {
		t.Errorf("right at edge, col = %d, want 2", h.gridCur.col)
	}

	// Move left.
	_, _ = h.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if h.gridCur.col != 1 {
		t.Errorf("after left, col = %d, want 1", h.gridCur.col)
	}
}

func TestHomeScreen_FilterCollapsesToSingleColumn(t *testing.T) {
	overrideBookmarks(t, nil)
	entries := []tui.ResourceEntry{
		{Key: "users", DisplayName: "Users", Category: tui.CategoryUserMgmt, Schema: schema.Resources["users"]},
	}
	h := NewHomeScreen(entries)
	h.Update(tea.WindowSizeMsg{Width: 140, Height: 50})

	// Start filtering.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !h.filtering {
		t.Fatal("should be in filter mode")
	}

	// In filter mode, view should work.
	view := h.View()
	if view == "" {
		t.Error("view should not be empty in filter mode")
	}
	if !strings.Contains(view, "Users") {
		t.Error("filter mode view should contain 'Users'")
	}
}

func TestHomeScreen_ResponsiveColumns(t *testing.T) {
	overrideBookmarks(t, nil)
	entries := tui.BuildRegistry()
	h := NewHomeScreen(entries)

	// Narrow: 1 column.
	h.Update(tea.WindowSizeMsg{Width: 70, Height: 50})
	if h.columns != 1 {
		t.Errorf("at width 70, columns = %d, want 1", h.columns)
	}

	// Medium: 2 columns.
	h.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	if h.columns != 2 {
		t.Errorf("at width 100, columns = %d, want 2", h.columns)
	}

	// Wide: 3 columns.
	h.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
	if h.columns != 3 {
		t.Errorf("at width 140, columns = %d, want 3", h.columns)
	}
}

func TestHomeScreen_GridTopBottom(t *testing.T) {
	overrideBookmarks(t, nil)
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Move to bottom (G).
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if h.gridCur.row != 2 {
		t.Errorf("after G, gridCur.row = %d, want 2 (last row)", h.gridCur.row)
	}

	// Move to top (g).
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if h.gridCur.row != 0 {
		t.Errorf("after g, gridCur.row = %d, want 0", h.gridCur.row)
	}
}

func TestHomeScreen_UpFromGridToBookmarks(t *testing.T) {
	overrideBookmarks(t, []string{"devices"})
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Starts in grid mode (not bookmarks).
	if h.inBookmarks {
		t.Fatal("should start in grid mode, not bookmarks")
	}

	// Press up at row 0 should transition to bookmarks.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if !h.inBookmarks {
		t.Error("pressing up at grid row 0 should enter bookmarks mode")
	}

	// Press down past last bookmark should return to grid.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if h.inBookmarks {
		t.Error("pressing down past last bookmark should return to grid mode")
	}
	if h.gridCur.row != 0 {
		t.Errorf("gridCur.row = %d, want 0 after returning from bookmarks", h.gridCur.row)
	}
}

func TestHomeScreen_BookmarkNavigationInGrid(t *testing.T) {
	overrideBookmarks(t, []string{"users", "devices"})
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Move up to enter bookmarks.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if !h.inBookmarks {
		t.Fatal("should be in bookmarks mode")
	}

	// Cursor should be at last bookmark (index 1).
	if h.cursor != 1 {
		t.Errorf("cursor in bookmarks = %d, want 1 (last bookmark)", h.cursor)
	}

	// Press Enter to open the bookmarked entry.
	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on bookmark should produce a command")
	}
	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}
	// Second bookmark is "Devices" (entries are in original order: users, devices).
	if pushMsg.Screen.Title() != "Devices" {
		t.Errorf("pushed screen = %q, want 'Devices'", pushMsg.Screen.Title())
	}
}

func TestHomeScreen_ColumnClampOnResize(t *testing.T) {
	overrideBookmarks(t, nil)
	entries := []tui.ResourceEntry{
		{Key: "users", DisplayName: "Users", Category: tui.CategoryUserMgmt, Schema: schema.Resources["users"]},
		{Key: "devices", DisplayName: "Devices", Category: tui.CategoryDeviceMgmt, Schema: schema.Resources["devices"]},
		{Key: "apps", DisplayName: "Apps", Category: tui.CategoryAccess, Schema: schema.Resources["apps"]},
	}
	h := NewHomeScreen(entries)
	h.Update(tea.WindowSizeMsg{Width: 140, Height: 50})

	// Move to column 2.
	h.Update(tea.KeyMsg{Type: tea.KeyRight})
	h.Update(tea.KeyMsg{Type: tea.KeyRight})
	if h.gridCur.col != 2 {
		t.Fatalf("col = %d, want 2", h.gridCur.col)
	}

	// Resize to 1 column — cursor should clamp.
	h.Update(tea.WindowSizeMsg{Width: 70, Height: 50})
	if h.gridCur.col != 0 {
		t.Errorf("after resize to 1 col, gridCur.col = %d, want 0", h.gridCur.col)
	}
}

func TestHomeScreen_FilterEscReturnsToGrid(t *testing.T) {
	overrideBookmarks(t, nil)
	h := NewHomeScreen(testEntries())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Enter filter mode.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !h.filtering {
		t.Fatal("should be in filter mode")
	}

	// Escape filter mode.
	h.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if h.filtering {
		t.Error("should have exited filter mode")
	}

	// Grid cursor should still work.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if h.gridCur.row != 1 {
		t.Errorf("gridCur.row = %d, want 1 after exiting filter", h.gridCur.row)
	}
}

func TestHomeScreen_TwoColumnLayout(t *testing.T) {
	overrideBookmarks(t, nil)
	entries := []tui.ResourceEntry{
		{Key: "users", DisplayName: "Users", Category: tui.CategoryUserMgmt, Schema: schema.Resources["users"]},
		{Key: "devices", DisplayName: "Devices", Category: tui.CategoryDeviceMgmt, Schema: schema.Resources["devices"]},
		{Key: "apps", DisplayName: "Apps", Category: tui.CategoryAccess, Schema: schema.Resources["apps"]},
	}
	h := NewHomeScreen(entries)
	h.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	if h.columns != 2 {
		t.Fatalf("columns = %d, want 2 at width 100", h.columns)
	}

	// In 2-column mode, col 2 (Access) folds into col 1.
	// Col 0: User Management (Users)
	// Col 1: Device Management (Devices), Access (Apps)
	// Move right to col 1.
	h.Update(tea.KeyMsg{Type: tea.KeyRight})
	if h.gridCur.col != 1 {
		t.Errorf("col = %d, want 1", h.gridCur.col)
	}

	// Move right again should clamp (only 2 cols).
	h.Update(tea.KeyMsg{Type: tea.KeyRight})
	if h.gridCur.col != 1 {
		t.Errorf("col = %d, want 1 (clamped)", h.gridCur.col)
	}

	view := h.View()
	if !strings.Contains(view, "Users") {
		t.Error("view should contain 'Users'")
	}
	if !strings.Contains(view, "Apps") {
		t.Error("view should contain 'Apps'")
	}
}
