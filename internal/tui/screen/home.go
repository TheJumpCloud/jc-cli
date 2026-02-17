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

// HomeScreen shows a categorized list of resources.
type HomeScreen struct {
	entries    []tui.ResourceEntry
	filtered  []tui.ResourceEntry
	cursor    int
	filter    textinput.Model
	filtering bool
	width     int
	height    int
	bookmarks map[string]bool
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
	}
	h.filtered = entries
	return h
}

func (h *HomeScreen) Title() string { return "Home" }

func (h *HomeScreen) Init() tea.Cmd { return nil }

// displayEntries returns the combined list for cursor indexing:
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
		return h, nil

	case tea.KeyMsg:
		if h.filtering {
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

		switch {
		case key.Matches(msg, tui.NavKeyMap.Up):
			h.moveCursor(-1)
		case key.Matches(msg, tui.NavKeyMap.Down):
			h.moveCursor(1)
		case key.Matches(msg, tui.NavKeyMap.Enter):
			display := h.displayEntries()
			if h.cursor < len(display) {
				return h, h.openEntry(display[h.cursor])
			}
		case key.Matches(msg, tui.ListKeyMap.Filter):
			h.filtering = true
			h.filter.Focus()
			return h, h.filter.Focus()
		case msg.String() == "d":
			return h, func() tea.Msg {
				return tui.PushScreenMsg{Screen: NewDashboardScreen()}
			}
		case msg.String() == "b":
			return h, h.toggleBookmark()
		case key.Matches(msg, tui.NavKeyMap.Top):
			h.cursor = 0
		case key.Matches(msg, tui.NavKeyMap.Bottom):
			display := h.displayEntries()
			h.cursor = len(display) - 1
			if h.cursor < 0 {
				h.cursor = 0
			}
		}
	}

	return h, nil
}

func (h *HomeScreen) openEntry(entry tui.ResourceEntry) tea.Cmd {
	switch entry.Key {
	case "system-insights":
		return func() tea.Msg {
			return tui.PushScreenMsg{Screen: NewTablePickerScreen(entry, tui.SystemInsightsTables)}
		}
	case "insights":
		return func() tea.Msg {
			return tui.PushScreenMsg{Screen: NewInsightsFormScreen(entry)}
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
		key := strings.ToLower(e.Key)
		cat := strings.ToLower(string(e.Category))
		if strings.Contains(name, query) || strings.Contains(key, query) || strings.Contains(cat, query) {
			h.filtered = append(h.filtered, e)
		}
	}
	h.cursor = 0
}

func (h *HomeScreen) toggleBookmark() tea.Cmd {
	display := h.displayEntries()
	if h.cursor >= len(display) {
		return nil
	}
	entry := display[h.cursor]
	var flashText string
	if h.bookmarks[entry.Key] {
		delete(h.bookmarks, entry.Key)
		flashText = "Removed bookmark"
	} else {
		h.bookmarks[entry.Key] = true
		flashText = "Bookmarked " + entry.DisplayName
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
	var sb strings.Builder

	title := style.Title.Render("JumpCloud TUI")
	sb.WriteString(title)
	sb.WriteString("\n")

	if h.filtering {
		h.filter.Width = h.width - 4
		sb.WriteString(style.FilterInput.Render(h.filter.View()))
		sb.WriteString("\n")
	}

	// Group by category.
	type group struct {
		category tui.Category
		entries  []tui.ResourceEntry
	}

	var groups []group
	catMap := make(map[tui.Category]*group)

	// Add bookmarks section at the top (only when not filtering and bookmarks exist).
	bmEntries := h.bookmarkedEntries()
	if !h.filtering && len(bmEntries) > 0 {
		groups = append(groups, group{category: "Bookmarks", entries: bmEntries})
	}

	for _, e := range h.filtered {
		g, ok := catMap[e.Category]
		if !ok {
			groups = append(groups, group{category: e.Category})
			g = &groups[len(groups)-1]
			catMap[e.Category] = g
		}
		g.entries = append(g.entries, e)
	}

	// Render each category.
	globalIdx := 0
	maxHeight := h.height - 6 // Reserve for title, filter, statusbar
	if h.filtering {
		maxHeight -= 2
	}
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

			prefix := "  "
			rowStyle := style.ResourceName
			if globalIdx == h.cursor {
				prefix = "> "
				rowStyle = style.SelectedRow
			}

			verbCount := len(e.Schema.Verbs)
			verbs := style.ResourceVerbs.Render(fmt.Sprintf(" (%d ops)", verbCount))
			name := rowStyle.Render(prefix + e.DisplayName)
			line := lipgloss.JoinHorizontal(lipgloss.Left, name, verbs)
			sb.WriteString(line)
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
