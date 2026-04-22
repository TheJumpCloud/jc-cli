package screen

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// bookmarkLoader loads bookmark keys from config. Replaceable for testing.
var bookmarkLoader = func() []string { return config.TUIBookmarks() }

// bookmarkSaver persists bookmark keys to config. Replaceable for testing.
var bookmarkSaver = func(keys []string) error { return config.SetTUIBookmarks(keys) }

// gridCursor tracks position in the multi-column grid layout.
type gridCursor struct {
	col int // 0-2 column index
	row int // row within the column (selectable items only, skipping headers)
}

// columnItem is a single item in a grid column: either a category header or a resource entry.
type columnItem struct {
	isHeader bool
	category string
	entry    *tui.ResourceEntry
}

// HomeScreen shows a categorized grid of resources.
type HomeScreen struct {
	entries     []tui.ResourceEntry
	filtered    []tui.ResourceEntry
	cursor      int // flat cursor for filter mode and bookmarks
	gridCur     gridCursor
	filter      textinput.Model
	filtering   bool
	inBookmarks bool // true when cursor is in the bookmarks section
	width       int
	height      int
	columns     int // responsive column count (1-3)
	bookmarks   map[string]bool
}

// NewHomeScreen creates the home screen with all registry entries.
func NewHomeScreen(entries []tui.ResourceEntry) *HomeScreen {
	ti := textinput.New()
	ti.Placeholder = "Type to filter resources..."
	ti.CharLimit = 64

	bm := make(map[string]bool)
	for _, k := range bookmarkLoader() {
		bm[k] = true
	}

	h := &HomeScreen{
		entries:   entries,
		filter:    ti,
		bookmarks: bm,
		columns:   1, // default until we get a WindowSizeMsg
	}
	h.filtered = entries
	return h
}

func (h *HomeScreen) Title() string { return "Home" }

// TextInputActive reports whether the home screen has active text input.
func (h *HomeScreen) TextInputActive() bool {
	return h.filtering
}

func (h *HomeScreen) Init() tea.Cmd { return nil }

// updateColumns recalculates the responsive column count based on terminal width.
func (h *HomeScreen) updateColumns() {
	switch {
	case h.width >= 120:
		h.columns = 3
	case h.width >= 90:
		h.columns = 2
	default:
		h.columns = 1
	}
}

// buildColumns groups entries into columns based on category-to-column mapping.
// Each column contains category headers followed by their entries, ordered by CategoryOrder.
func (h *HomeScreen) buildColumns() [3][]columnItem {
	var cols [3][]columnItem

	// Collect entries by category, preserving CategoryOrder within each column.
	catEntries := make(map[tui.Category][]tui.ResourceEntry)
	for i := range h.filtered {
		e := &h.filtered[i]
		catEntries[e.Category] = append(catEntries[e.Category], *e)
	}

	// Build columns following CategoryOrder.
	for _, cat := range tui.CategoryOrder {
		entries, ok := catEntries[cat]
		if !ok || len(entries) == 0 {
			continue
		}

		targetCol := tui.CategoryColumn(cat)
		// Collapse into fewer columns when responsive layout requires it.
		if h.columns == 1 {
			targetCol = 0
		} else if h.columns == 2 {
			// Map 3-column layout to 2 columns:
			// col 0 stays col 0, col 1 stays col 1, col 2 folds into col 1.
			if targetCol == 2 {
				targetCol = 1
			}
		}

		// Add category header.
		cols[targetCol] = append(cols[targetCol], columnItem{
			isHeader: true,
			category: string(cat),
		})

		// Add entries.
		for i := range entries {
			e := entries[i]
			cols[targetCol] = append(cols[targetCol], columnItem{
				entry: &e,
			})
		}
	}

	return cols
}

// selectableItems returns only the non-header items in a column.
func selectableItems(col []columnItem) []*tui.ResourceEntry {
	var items []*tui.ResourceEntry
	for i := range col {
		if !col[i].isHeader {
			items = append(items, col[i].entry)
		}
	}
	return items
}

// currentGridEntry returns the entry at the current grid cursor position, or nil.
func (h *HomeScreen) currentGridEntry() *tui.ResourceEntry {
	cols := h.buildColumns()
	c := h.gridCur.col
	if c < 0 || c >= h.columns {
		return nil
	}
	items := selectableItems(cols[c])
	if h.gridCur.row < 0 || h.gridCur.row >= len(items) {
		return nil
	}
	return items[h.gridCur.row]
}

// clampGridCursor ensures the grid cursor is within valid bounds.
func (h *HomeScreen) clampGridCursor() {
	cols := h.buildColumns()
	if h.gridCur.col >= h.columns {
		h.gridCur.col = h.columns - 1
	}
	if h.gridCur.col < 0 {
		h.gridCur.col = 0
	}
	items := selectableItems(cols[h.gridCur.col])
	if h.gridCur.row >= len(items) {
		h.gridCur.row = len(items) - 1
	}
	if h.gridCur.row < 0 {
		h.gridCur.row = 0
	}
}

// displayEntries returns the combined list for cursor indexing in filter mode:
// bookmarked entries first (when not filtering), then filtered entries.
func (h *HomeScreen) displayEntries() []tui.ResourceEntry {
	if h.filtering || len(h.bookmarks) == 0 {
		return h.filtered
	}
	bmEntries := h.bookmarkedEntries()
	if len(bmEntries) == 0 {
		return h.filtered
	}
	result := make([]tui.ResourceEntry, 0, len(bmEntries)+len(h.filtered))
	result = append(result, bmEntries...)
	result = append(result, h.filtered...)
	return result
}

func (h *HomeScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width = msg.Width
		h.height = msg.Height
		h.updateColumns()
		h.clampGridCursor()
		return h, nil

	case tea.KeyMsg:
		if h.filtering {
			return h.updateFilterMode(msg)
		}
		return h.updateGridMode(msg)
	}

	return h, nil
}

// updateFilterMode handles key events during filter mode (single-column flat cursor).
func (h *HomeScreen) updateFilterMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		h.filtering = false
		h.filter.Blur()
		h.filter.SetValue("")
		h.applyFilter()
		return h, nil
	case "enter":
		h.filtering = false
		h.filter.Blur()
		display := h.displayEntries()
		if h.cursor < len(display) {
			return h, h.openEntry(display[h.cursor])
		}
		return h, nil
	case "up", "k":
		h.moveCursor(-1)
		return h, nil
	case "down", "j":
		h.moveCursor(1)
		return h, nil
	default:
		var cmd tea.Cmd
		h.filter, cmd = h.filter.Update(msg)
		h.applyFilter()
		return h, cmd
	}
}

// updateGridMode handles key events in grid mode (multi-column navigation).
func (h *HomeScreen) updateGridMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	bmEntries := h.bookmarkedEntries()
	hasBM := !h.filtering && len(bmEntries) > 0

	// Handle bookmarks navigation.
	if h.inBookmarks && hasBM {
		return h.updateBookmarksNavigation(msg, bmEntries)
	}

	switch {
	case key.Matches(msg, tui.NavKeyMap.Up):
		if h.gridCur.row > 0 {
			h.gridCur.row--
		} else if hasBM {
			// Move from grid row 0 to bookmarks section.
			h.inBookmarks = true
			h.cursor = len(bmEntries) - 1
		}
	case key.Matches(msg, tui.NavKeyMap.Down):
		cols := h.buildColumns()
		items := selectableItems(cols[h.gridCur.col])
		if h.gridCur.row < len(items)-1 {
			h.gridCur.row++
		}
	case msg.Type == tea.KeyLeft:
		if h.gridCur.col > 0 {
			h.gridCur.col--
			h.clampGridCursor()
		}
	case msg.Type == tea.KeyRight:
		if h.gridCur.col < h.columns-1 {
			h.gridCur.col++
			h.clampGridCursor()
		}
	case key.Matches(msg, tui.NavKeyMap.Enter):
		entry := h.currentGridEntry()
		if entry != nil {
			return h, h.openEntry(*entry)
		}
	case key.Matches(msg, tui.ListKeyMap.Filter):
		h.filtering = true
		h.inBookmarks = false
		h.filter.Focus()
		return h, h.filter.Focus()
	case msg.String() == "d":
		return h, func() tea.Msg {
			return tui.PushScreenMsg{Screen: NewDashboardScreen()}
		}
	case msg.String() == "b":
		return h, h.toggleBookmarkGrid()
	case key.Matches(msg, tui.NavKeyMap.Top):
		h.gridCur.row = 0
	case key.Matches(msg, tui.NavKeyMap.Bottom):
		cols := h.buildColumns()
		items := selectableItems(cols[h.gridCur.col])
		h.gridCur.row = len(items) - 1
		if h.gridCur.row < 0 {
			h.gridCur.row = 0
		}
	}

	return h, nil
}

// updateBookmarksNavigation handles key events when the cursor is in the bookmarks section.
func (h *HomeScreen) updateBookmarksNavigation(msg tea.KeyMsg, bmEntries []tui.ResourceEntry) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, tui.NavKeyMap.Up):
		if h.cursor > 0 {
			h.cursor--
		}
	case key.Matches(msg, tui.NavKeyMap.Down):
		if h.cursor < len(bmEntries)-1 {
			h.cursor++
		} else {
			// Move from bookmarks to grid.
			h.inBookmarks = false
			h.gridCur.row = 0
		}
	case key.Matches(msg, tui.NavKeyMap.Enter):
		if h.cursor < len(bmEntries) {
			return h, h.openEntry(bmEntries[h.cursor])
		}
	case key.Matches(msg, tui.ListKeyMap.Filter):
		h.filtering = true
		h.inBookmarks = false
		h.filter.Focus()
		return h, h.filter.Focus()
	case msg.String() == "d":
		return h, func() tea.Msg {
			return tui.PushScreenMsg{Screen: NewDashboardScreen()}
		}
	case msg.String() == "b":
		if h.cursor < len(bmEntries) {
			entry := bmEntries[h.cursor]
			return h, h.toggleBookmarkFor(entry.Key, entry.DisplayName)
		}
	case key.Matches(msg, tui.NavKeyMap.Top):
		h.cursor = 0
	case key.Matches(msg, tui.NavKeyMap.Bottom):
		h.cursor = len(bmEntries) - 1
		if h.cursor < 0 {
			h.cursor = 0
		}
	}
	return h, nil
}

func (h *HomeScreen) openEntry(entry tui.ResourceEntry) tea.Cmd {
	// Placeholder entries are not openable.
	if entry.Placeholder {
		return func() tea.Msg {
			return tui.FlashMsg{Text: "Coming soon"}
		}
	}

	// Sub-menu entries push a sub-menu screen.
	if len(entry.SubMenu) > 0 {
		return func() tea.Msg {
			return tui.PushScreenMsg{Screen: NewSubMenuScreen(entry.DisplayName, entry.SubMenu)}
		}
	}

	switch entry.Key {
	case "system-insights":
		return func() tea.Msg {
			return tui.PushScreenMsg{Screen: NewTablePickerScreen(entry, tui.SystemInsightsTables)}
		}
	case "insights":
		return func() tea.Msg {
			return tui.PushScreenMsg{Screen: NewInsightsFormScreen(entry)}
		}
	case "recipes":
		return func() tea.Msg {
			return tui.PushScreenMsg{Screen: NewRecipeListScreen()}
		}
	default:
		return func() tea.Msg {
			return tui.PushScreenMsg{Screen: NewListScreen(entry)}
		}
	}
}

func (h *HomeScreen) moveCursor(delta int) {
	display := h.displayEntries()
	h.cursor += delta
	if h.cursor < 0 {
		h.cursor = 0
	}
	if h.cursor >= len(display) {
		h.cursor = len(display) - 1
	}
	if h.cursor < 0 {
		h.cursor = 0
	}
}

func (h *HomeScreen) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(h.filter.Value()))
	if query == "" {
		h.filtered = h.entries
		h.cursor = 0
		return
	}

	h.filtered = nil
	for _, e := range h.entries {
		name := strings.ToLower(e.DisplayName)
		k := strings.ToLower(e.Key)
		cat := strings.ToLower(string(e.Category))
		if strings.Contains(name, query) || strings.Contains(k, query) || strings.Contains(cat, query) {
			h.filtered = append(h.filtered, e)
		}
	}
	h.cursor = 0
}

// toggleBookmarkGrid toggles a bookmark on the current grid entry.
func (h *HomeScreen) toggleBookmarkGrid() tea.Cmd {
	entry := h.currentGridEntry()
	if entry == nil {
		return nil
	}
	return h.toggleBookmarkFor(entry.Key, entry.DisplayName)
}

// toggleBookmarkFor toggles a bookmark for a specific resource key.
func (h *HomeScreen) toggleBookmarkFor(resourceKey, displayName string) tea.Cmd {
	var flashText string
	if h.bookmarks[resourceKey] {
		delete(h.bookmarks, resourceKey)
		flashText = "Removed bookmark"
	} else {
		h.bookmarks[resourceKey] = true
		flashText = "Bookmarked " + displayName
	}

	// Persist bookmark keys.
	keys := make([]string, 0, len(h.bookmarks))
	for k := range h.bookmarks {
		keys = append(keys, k)
	}
	_ = bookmarkSaver(keys)

	return func() tea.Msg { return tui.FlashMsg{Text: flashText} }
}

func (h *HomeScreen) bookmarkedEntries() []tui.ResourceEntry {
	var result []tui.ResourceEntry
	for _, e := range h.entries {
		if h.bookmarks[e.Key] {
			result = append(result, e)
		}
	}
	return result
}

func (h *HomeScreen) View() string {
	if h.filtering {
		return h.viewFilterMode()
	}
	return h.viewGridMode()
}

// viewFilterMode renders the single-column filtered list.
func (h *HomeScreen) viewFilterMode() string {
	var sb strings.Builder

	title := style.Title.Render("JumpCloud TUI")
	sb.WriteString(title)
	sb.WriteString("\n")

	h.filter.Width = h.width - 4
	sb.WriteString(style.FilterInput.Render(h.filter.View()))
	sb.WriteString("\n")

	// Group by category.
	type group struct {
		category tui.Category
		entries  []tui.ResourceEntry
	}

	var groups []group
	catMap := make(map[tui.Category]*group)

	for _, e := range h.filtered {
		g, ok := catMap[e.Category]
		if !ok {
			groups = append(groups, group{category: e.Category})
			g = &groups[len(groups)-1]
			catMap[e.Category] = g
		}
		g.entries = append(g.entries, e)
	}

	globalIdx := 0
	maxHeight := h.height - 8

	lineCount := 0
	for _, g := range groups {
		if lineCount >= maxHeight {
			break
		}

		sb.WriteString(style.Category.Render(string(g.category)))
		sb.WriteString("\n")
		lineCount++

		for _, e := range g.entries {
			if lineCount >= maxHeight {
				break
			}

			sb.WriteString(h.renderEntryLine(e, globalIdx == h.cursor))
			sb.WriteString("\n")
			globalIdx++
			lineCount++
		}
	}

	if len(h.filtered) == 0 {
		sb.WriteString(style.DimRow.Render("  No matching resources"))
		sb.WriteString("\n")
	}

	return sb.String()
}

// viewGridMode renders the multi-column grid layout.
func (h *HomeScreen) viewGridMode() string {
	var sb strings.Builder

	title := style.Title.Render("JumpCloud TUI")
	sb.WriteString(title)
	sb.WriteString("\n")

	// Render bookmarks section above the grid.
	bmEntries := h.bookmarkedEntries()
	if len(bmEntries) > 0 {
		sb.WriteString(style.Category.Render("Bookmarks"))
		sb.WriteString("\n")

		for i, e := range bmEntries {
			isSelected := h.inBookmarks && i == h.cursor
			sb.WriteString(h.renderEntryLine(e, isSelected))
			sb.WriteString("\n")
		}
	}

	// Build and render columns.
	cols := h.buildColumns()
	colWidth := h.width / h.columns
	if colWidth < 20 {
		colWidth = 20
	}

	// Pre-compute selectable items for cursor matching.
	colSelectables := [3][]*tui.ResourceEntry{}
	for c := 0; c < h.columns; c++ {
		colSelectables[c] = selectableItems(cols[c])
	}

	// Render each column as a string.
	colStrings := make([]string, h.columns)
	for c := 0; c < h.columns; c++ {
		colStrings[c] = h.renderColumn(cols[c], colSelectables[c], c, colWidth)
	}

	// Join columns horizontally.
	grid := lipgloss.JoinHorizontal(lipgloss.Top, colStrings...)
	sb.WriteString(grid)

	if len(h.filtered) == 0 {
		sb.WriteString(style.DimRow.Render("  No matching resources"))
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderColumn renders a single column of items.
func (h *HomeScreen) renderColumn(items []columnItem, selectables []*tui.ResourceEntry, colIdx, colWidth int) string {
	var sb strings.Builder
	selectableIdx := 0

	for _, item := range items {
		if item.isHeader {
			header := style.Category.Render(item.category)
			sb.WriteString(header)
			sb.WriteString("\n")
			continue
		}

		isSelected := !h.inBookmarks && h.gridCur.col == colIdx && h.gridCur.row == selectableIdx
		sb.WriteString(h.renderEntryLine(*item.entry, isSelected))
		sb.WriteString("\n")
		selectableIdx++
	}

	// Constrain column width.
	colStyle := lipgloss.NewStyle().Width(colWidth)
	return colStyle.Render(sb.String())
}

// renderEntryLine renders a single resource entry line.
func (h *HomeScreen) renderEntryLine(e tui.ResourceEntry, isSelected bool) string {
	prefix := "  "
	rowStyle := style.ResourceName

	if e.Placeholder {
		rowStyle = style.DimRow
	}

	if isSelected {
		prefix = "> "
		rowStyle = style.SelectedRow
	}

	name := rowStyle.Render(prefix + e.DisplayName)

	var suffix string
	if len(e.SubMenu) > 0 {
		suffix = style.ResourceVerbs.Render(" (\u25b8)")
	} else if e.Placeholder {
		suffix = ""
	} else if e.Key == "recipes" {
		// Virtual entry (no schema) — show a workflow marker instead of verb count.
		suffix = style.ResourceVerbs.Render(" (workflow)")
	} else {
		verbCount := len(e.Schema.Verbs)
		suffix = style.ResourceVerbs.Render(fmt.Sprintf(" (%d ops)", verbCount))
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, name, suffix)
}
