package screen

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// HomeScreen shows a categorized list of resources.
type HomeScreen struct {
	entries    []tui.ResourceEntry
	filtered  []tui.ResourceEntry
	cursor    int
	filter    textinput.Model
	filtering bool
	width     int
	height    int
}

// NewHomeScreen creates the home screen with all registry entries.
func NewHomeScreen(entries []tui.ResourceEntry) *HomeScreen {
	ti := textinput.New()
	ti.Placeholder = "Type to filter resources..."
	ti.CharLimit = 64

	h := &HomeScreen{
		entries: entries,
		filter:  ti,
	}
	h.filtered = entries
	return h
}

func (h *HomeScreen) Title() string { return "Home" }

func (h *HomeScreen) Init() tea.Cmd { return nil }

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
				if h.cursor < len(h.filtered) {
					return h, h.openResource()
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
			if h.cursor < len(h.filtered) {
				return h, h.openResource()
			}
		case key.Matches(msg, tui.ListKeyMap.Filter):
			h.filtering = true
			h.filter.Focus()
			return h, h.filter.Focus()
		case key.Matches(msg, tui.NavKeyMap.Top):
			h.cursor = 0
		case key.Matches(msg, tui.NavKeyMap.Bottom):
			h.cursor = len(h.filtered) - 1
			if h.cursor < 0 {
				h.cursor = 0
			}
		}
	}

	return h, nil
}

func (h *HomeScreen) openResource() tea.Cmd {
	entry := h.filtered[h.cursor]
	return func() tea.Msg {
		return tui.PushScreenMsg{Screen: NewListScreen(entry)}
	}
}

func (h *HomeScreen) moveCursor(delta int) {
	h.cursor += delta
	if h.cursor < 0 {
		h.cursor = 0
	}
	if h.cursor >= len(h.filtered) {
		h.cursor = len(h.filtered) - 1
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
