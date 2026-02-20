# TUI Menu Restructure Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Restructure the TUI home screen from a single-column grouped list to a responsive three-column grid layout mirroring the JumpCloud Admin Console, with placeholder items for unimplemented features and sub-menu support.

**Architecture:** Modify registry.go to define new categories, placeholder entries, and sub-menu groupings. Rewrite home.go's View() to render a multi-column grid using lipgloss.JoinHorizontal, with a new (col, row) cursor model. Add a small SubMenuScreen for Cloud Directories. Responsive fallback to fewer columns based on terminal width.

**Tech Stack:** Go, Bubbletea (tea.Model), Lipgloss (layout/styling), existing tui/style package

**Design doc:** `docs/plans/2026-02-20-tui-menu-restructure-design.md`

---

### Task 1: Add Placeholder and SubMenu fields to ResourceEntry

**Files:**
- Modify: `internal/tui/registry.go:42-56`
- Test: `internal/tui/registry_test.go`

**Step 1: Write the failing test**

Add to `internal/tui/registry_test.go`:

```go
func TestResourceEntry_PlaceholderField(t *testing.T) {
	e := tui.ResourceEntry{
		Key:         "test",
		DisplayName: "Test",
		Placeholder: true,
	}
	if !e.Placeholder {
		t.Error("Placeholder field should be true")
	}
}

func TestResourceEntry_SubMenuField(t *testing.T) {
	e := tui.ResourceEntry{
		Key:         "cloud-dirs",
		DisplayName: "Cloud Directories",
		SubMenu: []tui.ResourceEntry{
			{Key: "gsuite", DisplayName: "Google Workspace"},
			{Key: "office365", DisplayName: "M365"},
		},
	}
	if len(e.SubMenu) != 2 {
		t.Errorf("SubMenu length = %d, want 2", len(e.SubMenu))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestResourceEntry_PlaceholderField -v -count=1`
Expected: FAIL — `Placeholder` field doesn't exist

**Step 3: Add fields to ResourceEntry struct**

In `internal/tui/registry.go`, add two fields to the `ResourceEntry` struct after `Schema`:

```go
type ResourceEntry struct {
	Key             string
	DisplayName     string
	Category        Category
	ClientType      ClientType
	ListEndpoint    string
	GetEndpoint     string
	GraphSourceType string
	PivotField      string
	PivotTargetKey  string
	SearchEndpoint  string
	SearchFields    []string
	Schema          schema.ResourceSchema
	Placeholder     bool              // True for "Coming soon" items
	SubMenu         []ResourceEntry   // Non-nil for sub-menu groupings (e.g. Cloud Directories)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run TestResourceEntry_ -v -count=1`
Expected: PASS

**Step 5: Commit**

```
feat(tui): add Placeholder and SubMenu fields to ResourceEntry
```

---

### Task 2: Update categories and resource mappings

**Files:**
- Modify: `internal/tui/registry.go:9-31` (categories), `internal/tui/registry.go:192-226` (resourceCategory), `internal/tui/registry.go:228-256` (displayNames)
- Test: `internal/tui/registry_test.go`

**Step 1: Write the failing test**

Add to `internal/tui/registry_test.go`:

```go
func TestBuildRegistry_NewCategories(t *testing.T) {
	entries := BuildRegistry()
	cats := make(map[Category]bool)
	for _, e := range entries {
		cats[e.Category] = true
	}

	want := []Category{
		CategoryUserMgmt,
		CategoryDeviceMgmt,
		CategoryAccess,
		CategorySecurity,
		CategoryInsights,
		CategorySettings,
	}
	for _, c := range want {
		if !cats[c] {
			t.Errorf("missing category %q", c)
		}
	}
}

func TestBuildRegistry_UserMgmtContainsUsers(t *testing.T) {
	m := RegistryByKey()
	e, ok := m["users"]
	if !ok {
		t.Fatal("missing 'users'")
	}
	if e.Category != CategoryUserMgmt {
		t.Errorf("users category = %q, want %q", e.Category, CategoryUserMgmt)
	}
}

func TestBuildRegistry_DeviceMgmtContainsCommands(t *testing.T) {
	m := RegistryByKey()
	e, ok := m["commands"]
	if !ok {
		t.Fatal("missing 'commands'")
	}
	if e.Category != CategoryDeviceMgmt {
		t.Errorf("commands category = %q, want %q", e.Category, CategoryDeviceMgmt)
	}
}

func TestBuildRegistry_AccessContainsApps(t *testing.T) {
	m := RegistryByKey()
	e, ok := m["apps"]
	if !ok {
		t.Fatal("missing 'apps'")
	}
	if e.Category != CategoryAccess {
		t.Errorf("apps category = %q, want %q", e.Category, CategoryAccess)
	}
}

func TestBuildRegistry_Office365RenamedToM365(t *testing.T) {
	m := RegistryByKey()
	e, ok := m["office365"]
	if !ok {
		t.Fatal("missing 'office365'")
	}
	if e.DisplayName != "M365" {
		t.Errorf("office365 display name = %q, want 'M365'", e.DisplayName)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestBuildRegistry_NewCategories -v -count=1`
Expected: FAIL — `CategoryUserMgmt` undefined

**Step 3: Update categories, mappings, and display names**

Replace the category constants, CategoryOrder, resourceCategory map, and displayNames map in `internal/tui/registry.go`:

```go
const (
	CategoryUserMgmt   Category = "User Management"
	CategoryDeviceMgmt Category = "Device Management"
	CategoryAccess     Category = "Access"
	CategorySecurity   Category = "Security"
	CategoryInsights   Category = "Insights"
	CategorySettings   Category = "Settings"
)

var CategoryOrder = []Category{
	CategoryUserMgmt,
	CategoryDeviceMgmt,
	CategoryAccess,
	CategorySecurity,
	CategoryInsights,
	CategorySettings,
}
```

Update `resourceCategory`:

```go
var resourceCategory = map[string]Category{
	// User Management
	"users":       CategoryUserMgmt,
	"user-groups": CategoryUserMgmt,
	"ad":          CategoryUserMgmt,

	// Device Management
	"devices":          CategoryDeviceMgmt,
	"device-groups":    CategoryDeviceMgmt,
	"commands":         CategoryDeviceMgmt,
	"policies":         CategoryDeviceMgmt,
	"policy-groups":    CategoryDeviceMgmt,
	"software":         CategoryDeviceMgmt,
	"apple-mdm":        CategoryDeviceMgmt,
	"system-insights":  CategoryDeviceMgmt,
	"policy-templates": CategoryDeviceMgmt,

	// Access
	"apps":           CategoryAccess,
	"app-templates":  CategoryAccess,
	"ldap":           CategoryAccess,
	"radius":         CategoryAccess,

	// Security
	"auth-policies": CategorySecurity,
	"iplists":       CategorySecurity,

	// Insights
	"insights": CategoryInsights,

	// Settings
	"admins":        CategorySettings,
	"org":           CategorySettings,
	"custom-emails": CategorySettings,
	"user-states":   CategorySettings,
	"bulk":          CategorySettings,
	"duo":           CategorySettings,
	"gsuite":        CategorySettings,
	"office365":     CategorySettings,
}
```

Update `displayNames` — rename Office 365 to M365:

```go
"office365":        "M365",
```

**Step 4: Fix the existing tests that reference old category names**

Update `TestBuildRegistry_GroupsSplit` in `registry_test.go` — change `CategoryIdentity` references to `CategoryUserMgmt` and `CategoryDevices` to `CategoryDeviceMgmt`.

Update `TestHomeScreen_ViewShowsCategories` in `home_test.go` — change "Identity" check to "User Management".

**Step 5: Run all TUI tests**

Run: `go test ./internal/tui/... -v -count=1`
Expected: PASS

**Step 6: Commit**

```
feat(tui): restructure categories to match JC Admin Console (KLA-151)
```

---

### Task 3: Add placeholder entries and Cloud Directories sub-menu to BuildRegistry

**Files:**
- Modify: `internal/tui/registry.go:318-398` (BuildRegistry function)
- Test: `internal/tui/registry_test.go`

**Step 1: Write the failing tests**

Add to `internal/tui/registry_test.go`:

```go
func TestBuildRegistry_PlaceholderEntries(t *testing.T) {
	entries := BuildRegistry()
	placeholders := make(map[string]bool)
	for _, e := range entries {
		if e.Placeholder {
			placeholders[e.Key] = true
		}
	}

	want := []string{
		"hr-directories", "identity-providers",
		"asset-management", "patch-management",
		"access-requests", "ai-saas-management", "vault",
		"mfa-configurations", "device-trust", "password-policies",
	}
	for _, k := range want {
		if !placeholders[k] {
			t.Errorf("missing placeholder %q", k)
		}
	}
}

func TestBuildRegistry_CloudDirectoriesSubMenu(t *testing.T) {
	entries := BuildRegistry()
	var found bool
	for _, e := range entries {
		if e.Key == "cloud-directories" {
			found = true
			if len(e.SubMenu) != 2 {
				t.Errorf("cloud-directories SubMenu length = %d, want 2", len(e.SubMenu))
			}
			if !e.SubMenu[0].Key == "gsuite" || e.SubMenu[0].Key != "gsuite" {
				t.Errorf("SubMenu[0].Key = %q, want 'gsuite'", e.SubMenu[0].Key)
			}
			if e.SubMenu[1].Key != "office365" {
				t.Errorf("SubMenu[1].Key = %q, want 'office365'", e.SubMenu[1].Key)
			}
			break
		}
	}
	if !found {
		t.Error("missing 'cloud-directories' entry")
	}
}

func TestBuildRegistry_GsuiteOffice365NotTopLevel(t *testing.T) {
	entries := BuildRegistry()
	for _, e := range entries {
		if e.Key == "gsuite" || e.Key == "office365" {
			t.Errorf("resource %q should not be a top-level entry (it's inside Cloud Directories sub-menu)", e.Key)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestBuildRegistry_Placeholder -v -count=1`
Expected: FAIL

**Step 3: Update BuildRegistry to add placeholders and sub-menu**

At the end of `BuildRegistry()`, before the sort, add placeholder entries. Also intercept "gsuite" and "office365" to fold them into a "cloud-directories" sub-menu entry instead of top-level entries.

Add a `placeholderEntries` slice at the package level in `registry.go`:

```go
// placeholderEntries defines "Coming soon" items shown grayed out in the menu.
var placeholderEntries = []ResourceEntry{
	{Key: "hr-directories", DisplayName: "HR Directories", Category: CategoryUserMgmt, Placeholder: true},
	{Key: "identity-providers", DisplayName: "Identity Providers", Category: CategoryUserMgmt, Placeholder: true},
	{Key: "asset-management", DisplayName: "Asset Management", Category: CategoryDeviceMgmt, Placeholder: true},
	{Key: "patch-management", DisplayName: "Patch Management", Category: CategoryDeviceMgmt, Placeholder: true},
	{Key: "access-requests", DisplayName: "Access Requests", Category: CategoryAccess, Placeholder: true},
	{Key: "ai-saas-management", DisplayName: "AI & SaaS Management", Category: CategoryAccess, Placeholder: true},
	{Key: "vault", DisplayName: "Vault", Category: CategoryAccess, Placeholder: true},
	{Key: "mfa-configurations", DisplayName: "MFA Configurations", Category: CategorySecurity, Placeholder: true},
	{Key: "device-trust", DisplayName: "Device Trust", Category: CategorySecurity, Placeholder: true},
	{Key: "password-policies", DisplayName: "Password Policies", Category: CategorySecurity, Placeholder: true},
}
```

In `BuildRegistry()`, skip "gsuite" and "office365" from the main loop (they'll be nested). After the loop, build the cloud-directories entry using the gsuite/office365 ResourceEntry data stashed during iteration. Then append all placeholder entries. Finally, update `TestBuildRegistry_Count` — the count will now include placeholders + cloud-directories and exclude gsuite/office365 from top level.

**Step 4: Run all TUI tests**

Run: `go test ./internal/tui/... -v -count=1`
Expected: PASS (after updating count test)

**Step 5: Commit**

```
feat(tui): add placeholder entries and Cloud Directories sub-menu (KLA-151)
```

---

### Task 4: Add column assignment data structure

**Files:**
- Modify: `internal/tui/registry.go` (add column assignment map)
- Test: `internal/tui/registry_test.go`

**Step 1: Write the failing test**

```go
func TestColumnAssignment(t *testing.T) {
	for _, cat := range CategoryOrder {
		col := CategoryColumn(cat)
		if col < 0 || col > 2 {
			t.Errorf("category %q has column %d, want 0-2", cat, col)
		}
	}

	// Verify specific assignments from design:
	if CategoryColumn(CategoryUserMgmt) != 0 {
		t.Error("User Management should be in column 0")
	}
	if CategoryColumn(CategorySecurity) != 0 {
		t.Error("Security should be in column 0")
	}
	if CategoryColumn(CategoryDeviceMgmt) != 1 {
		t.Error("Device Management should be in column 1")
	}
	if CategoryColumn(CategorySettings) != 1 {
		t.Error("Settings should be in column 1")
	}
	if CategoryColumn(CategoryAccess) != 2 {
		t.Error("Access should be in column 2")
	}
	if CategoryColumn(CategoryInsights) != 2 {
		t.Error("Insights should be in column 2")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestColumnAssignment -v -count=1`
Expected: FAIL — `CategoryColumn` undefined

**Step 3: Add CategoryColumn function and column map**

Add to `internal/tui/registry.go`:

```go
// categoryColumns maps each category to its grid column (0-indexed).
var categoryColumns = map[Category]int{
	CategoryUserMgmt:   0,
	CategorySecurity:   0,
	CategoryDeviceMgmt: 1,
	CategorySettings:   1,
	CategoryAccess:     2,
	CategoryInsights:   2,
}

// CategoryColumn returns the grid column (0-2) for a category.
func CategoryColumn(c Category) int {
	if col, ok := categoryColumns[c]; ok {
		return col
	}
	return 0
}
```

**Step 4: Run tests**

Run: `go test ./internal/tui/ -run TestColumnAssignment -v -count=1`
Expected: PASS

**Step 5: Commit**

```
feat(tui): add column assignment for three-column grid layout (KLA-151)
```

---

### Task 5: Create SubMenuScreen

**Files:**
- Create: `internal/tui/screen/submenu.go`
- Create: `internal/tui/screen/submenu_test.go`

**Step 1: Write the failing test**

Create `internal/tui/screen/submenu_test.go`:

```go
package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
)

func TestSubMenuScreen_Title(t *testing.T) {
	s := NewSubMenuScreen("Cloud Directories", []tui.ResourceEntry{
		{Key: "gsuite", DisplayName: "Google Workspace"},
		{Key: "office365", DisplayName: "M365"},
	})
	if s.Title() != "Cloud Directories" {
		t.Errorf("Title = %q, want 'Cloud Directories'", s.Title())
	}
}

func TestSubMenuScreen_ViewShowsItems(t *testing.T) {
	s := NewSubMenuScreen("Cloud Directories", []tui.ResourceEntry{
		{Key: "gsuite", DisplayName: "Google Workspace"},
		{Key: "office365", DisplayName: "M365"},
	})
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := s.View()
	if !strings.Contains(view, "Google Workspace") {
		t.Error("view should contain 'Google Workspace'")
	}
	if !strings.Contains(view, "M365") {
		t.Error("view should contain 'M365'")
	}
}

func TestSubMenuScreen_EnterPushesListScreen(t *testing.T) {
	s := NewSubMenuScreen("Cloud Directories", []tui.ResourceEntry{
		{Key: "gsuite", DisplayName: "Google Workspace", ClientType: tui.ClientV2, ListEndpoint: "/gsuites"},
		{Key: "office365", DisplayName: "M365", ClientType: tui.ClientV2, ListEndpoint: "/office365s"},
	})
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should produce a command")
	}
	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}
	if pushMsg.Screen.Title() != "Google Workspace" {
		t.Errorf("pushed screen title = %q, want 'Google Workspace'", pushMsg.Screen.Title())
	}
}

func TestSubMenuScreen_EscPops(t *testing.T) {
	s := NewSubMenuScreen("Cloud Directories", []tui.ResourceEntry{
		{Key: "gsuite", DisplayName: "Google Workspace"},
	})
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(tui.PopScreenMsg); !ok {
		t.Fatalf("expected PopScreenMsg, got %T", msg)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/screen/ -run TestSubMenuScreen -v -count=1`
Expected: FAIL — `NewSubMenuScreen` undefined

**Step 3: Implement SubMenuScreen**

Create `internal/tui/screen/submenu.go`:

```go
package screen

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// SubMenuScreen shows a small list of items within a grouping (e.g. Cloud Directories).
type SubMenuScreen struct {
	title   string
	entries []tui.ResourceEntry
	cursor  int
	width   int
	height  int
}

// NewSubMenuScreen creates a sub-menu screen.
func NewSubMenuScreen(title string, entries []tui.ResourceEntry) *SubMenuScreen {
	return &SubMenuScreen{
		title:   title,
		entries: entries,
	}
}

func (s *SubMenuScreen) Title() string { return s.title }
func (s *SubMenuScreen) Init() tea.Cmd { return nil }

func (s *SubMenuScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, tui.NavKeyMap.Up):
			if s.cursor > 0 {
				s.cursor--
			}
		case key.Matches(msg, tui.NavKeyMap.Down):
			if s.cursor < len(s.entries)-1 {
				s.cursor++
			}
		case key.Matches(msg, tui.NavKeyMap.Enter):
			if s.cursor < len(s.entries) {
				entry := s.entries[s.cursor]
				return s, func() tea.Msg {
					return tui.PushScreenMsg{Screen: NewListScreen(entry)}
				}
			}
		case key.Matches(msg, tui.GlobalKeyMap.Back):
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		}
	}
	return s, nil
}

func (s *SubMenuScreen) View() string {
	var sb strings.Builder

	sb.WriteString(style.Title.Render(s.title))
	sb.WriteString("\n\n")

	for i, e := range s.entries {
		prefix := "  "
		rowStyle := style.ResourceName
		if i == s.cursor {
			prefix = "> "
			rowStyle = style.SelectedRow
		}
		sb.WriteString(rowStyle.Render(prefix + e.DisplayName))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(style.Help.Render("enter:open  esc:back"))
	sb.WriteString("\n")

	return sb.String()
}
```

**Step 4: Run tests**

Run: `go test ./internal/tui/screen/ -run TestSubMenuScreen -v -count=1`
Expected: PASS

**Step 5: Commit**

```
feat(tui): add SubMenuScreen for grouped navigation (KLA-151)
```

---

### Task 6: Rewrite HomeScreen with three-column grid layout

**Files:**
- Modify: `internal/tui/screen/home.go` — Major rewrite of View(), Update(), cursor model
- Modify: `internal/tui/screen/home_test.go` — Update tests for new layout

**Step 1: Write new tests for grid layout**

Add to `internal/tui/screen/home_test.go`:

```go
func TestHomeScreen_GridLayoutAtWideTerminal(t *testing.T) {
	overrideBookmarks(t, nil)
	entries := tui.BuildRegistry()
	h := NewHomeScreen(entries)
	h.Update(tea.WindowSizeMsg{Width: 140, Height: 50})

	view := h.View()
	// All new categories should appear.
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

func TestHomeScreen_LeftRightNavigation(t *testing.T) {
	overrideBookmarks(t, nil)
	entries := tui.BuildRegistry()
	h := NewHomeScreen(entries)
	h.Update(tea.WindowSizeMsg{Width: 140, Height: 50})

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
	entries := tui.BuildRegistry()
	h := NewHomeScreen(entries)
	h.Update(tea.WindowSizeMsg{Width: 140, Height: 50})

	// Start filtering.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !h.filtering {
		t.Fatal("should be in filter mode")
	}

	// In filter mode, view should work (single column).
	view := h.View()
	if view == "" {
		t.Error("view should not be empty in filter mode")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/screen/ -run TestHomeScreen_GridLayout -v -count=1`
Expected: FAIL

**Step 3: Rewrite HomeScreen**

This is the largest change. Key modifications to `internal/tui/screen/home.go`:

1. Replace `cursor int` with `gridCur gridCursor` struct (`col`, `row` ints)
2. Add `columns int` field tracking current column count (1, 2, or 3)
3. Add `columnData` method that builds `[][]columnItem` — each column is a list of items (category headers + resource entries), computed from entries grouped by `CategoryColumn()`
4. Update `View()`:
   - When `filtering`: render single-column list (existing behavior)
   - Otherwise: compute column data, render each column with `lipgloss.JoinHorizontal(lipgloss.Top, col1, col2, col3)`, each column ~1/3 of terminal width
   - Placeholder items rendered with `style.DimRow`
   - Sub-menu items show `(▸)` instead of ops count
5. Update `Update()`:
   - Add left/right arrow handling that changes `gridCur.col`
   - Up/down moves `gridCur.row` within current column's items
   - Enter on placeholder → flash "Coming soon"
   - Enter on sub-menu → push SubMenuScreen
6. Update `moveCursor()` to work with grid coordinates
7. Compute column count from width: `< 90` → 1, `90-119` → 2, `120+` → 3
8. When column count is 1, all categories flow into single column (equivalent to old layout)
9. `displayEntries()` method updated to return flat list for current column (used by cursor)

The `gridCursor` struct:
```go
type gridCursor struct {
	col int // 0-2
	row int // row within the column (indexes into selectable items only, skipping headers)
}
```

The `columnItem` type for rendering:
```go
type columnItem struct {
	isHeader bool
	text     string
	entry    *tui.ResourceEntry // nil for headers
}
```

**Step 4: Update existing home_test.go tests**

Many existing tests reference `h.cursor` (int) which becomes `h.gridCur`. Update:
- `TestHomeScreen_CursorMovement`: Check `h.gridCur.row` instead of `h.cursor`
- `TestHomeScreen_EnterPushesListScreen`: Use `testEntries()` that all map to single column
- Bookmark tests: Adapt cursor navigation to grid model

**Step 5: Run all tests**

Run: `go test ./internal/tui/... -v -count=1`
Expected: PASS

**Step 6: Commit**

```
feat(tui): three-column grid layout for home screen (KLA-151)
```

---

### Task 7: Add DimRow style for placeholder items

**Files:**
- Modify: `internal/tui/style/style.go` (if needed — DimRow already exists)
- Verify existing `DimRow` style works for grayed-out placeholders

**Step 1: Verify DimRow exists**

Check `internal/tui/style/style.go:48-49` — `DimRow` already exists with `ColorDimText`. This is sufficient for grayed-out placeholder items.

No code change needed. Skip to commit if Task 6 already uses `DimRow`.

---

### Task 8: Final integration test and cleanup

**Files:**
- Modify: `internal/tui/registry_test.go` — Update `TestBuildRegistry_Count`
- Run: Full test suite

**Step 1: Update entry count in tests**

`TestBuildRegistry_Count` currently expects `len(schema.Resources) - len(skipInTUI) + 1`. With the restructure:
- gsuite and office365 are no longer top-level (they're in cloud-directories sub-menu): -2
- cloud-directories added: +1
- 10 placeholder entries added: +10
- Net change: +9

Update the count formula:
```go
func TestBuildRegistry_Count(t *testing.T) {
	entries := BuildRegistry()
	// "groups" → 2 entries (+1), gsuite/office365 folded into cloud-directories (-2+1=-1), plus 10 placeholders
	want := len(schema.Resources) - len(skipInTUI) + 1 - 1 + 10
	if len(entries) != want {
		t.Errorf("registry has %d entries, want %d", len(entries), want)
	}
}
```

**Step 2: Run full test suite**

Run: `go test ./internal/tui/... -v -count=1`
Expected: PASS

**Step 3: Run the TUI manually to verify visually**

Run: `go run ./cmd/jc/ tui`
Expected: Three-column grid with categories, placeholder items grayed out, Cloud Directories opens sub-menu

**Step 4: Commit**

```
test(tui): update entry counts and finalize menu restructure (KLA-151)
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Add Placeholder/SubMenu fields to ResourceEntry | registry.go |
| 2 | Update categories and resource mappings | registry.go, registry_test.go, home_test.go |
| 3 | Add placeholder entries and Cloud Directories sub-menu | registry.go, registry_test.go |
| 4 | Add column assignment data structure | registry.go, registry_test.go |
| 5 | Create SubMenuScreen | submenu.go (new), submenu_test.go (new) |
| 6 | Rewrite HomeScreen with three-column grid | home.go, home_test.go |
| 7 | Verify DimRow style (no-op if already sufficient) | style.go |
| 8 | Integration test and count updates | registry_test.go |
