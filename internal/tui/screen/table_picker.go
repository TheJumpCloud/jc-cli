package screen

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// TablePickerScreen lets the user select a System Insights osquery table.
type TablePickerScreen struct {
	entry     tui.ResourceEntry
	tables    []string // all sorted table names
	filtered  []string // after filter
	cursor    int
	filter    textinput.Model
	filtering bool
	width     int
	height    int
}

// NewTablePickerScreen creates a table picker for system insights.
func NewTablePickerScreen(entry tui.ResourceEntry, tables []string) *TablePickerScreen {
	sorted := make([]string, len(tables))
	copy(sorted, tables)
	sort.Strings(sorted)

	ti := textinput.New()
	ti.Placeholder = "Type to filter tables..."
	ti.CharLimit = 64

	return &TablePickerScreen{
		entry:    entry,
		tables:   sorted,
		filtered: sorted,
		filter:   ti,
	}
}

func (t *TablePickerScreen) Title() string { return "System Insights" }

func (t *TablePickerScreen) Init() tea.Cmd { return nil }

func (t *TablePickerScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width = msg.Width
		t.height = msg.Height
		return t, nil

	case tea.KeyMsg:
		if t.filtering {
			switch msg.String() {
			case "esc":
				t.filtering = false
				t.filter.Blur()
				t.filter.SetValue("")
				t.applyFilter()
				return t, nil
			case "enter":
				t.filtering = false
				t.filter.Blur()
				if t.cursor < len(t.filtered) {
					return t, t.selectTable()
				}
				return t, nil
			case "up", "k":
				t.moveCursor(-1)
				return t, nil
			case "down", "j":
				t.moveCursor(1)
				return t, nil
			default:
				var cmd tea.Cmd
				t.filter, cmd = t.filter.Update(msg)
				t.applyFilter()
				return t, cmd
			}
		}

		switch {
		case key.Matches(msg, tui.GlobalKeyMap.Back):
			return t, func() tea.Msg { return tui.PopScreenMsg{} }
		case key.Matches(msg, tui.NavKeyMap.Up):
			t.moveCursor(-1)
		case key.Matches(msg, tui.NavKeyMap.Down):
			t.moveCursor(1)
		case key.Matches(msg, tui.NavKeyMap.Top):
			t.cursor = 0
		case key.Matches(msg, tui.NavKeyMap.Bottom):
			t.cursor = len(t.filtered) - 1
			if t.cursor < 0 {
				t.cursor = 0
			}
		case key.Matches(msg, tui.NavKeyMap.Enter):
			if t.cursor < len(t.filtered) {
				return t, t.selectTable()
			}
		case key.Matches(msg, tui.ListKeyMap.Filter):
			t.filtering = true
			t.filter.Focus()
			return t, t.filter.Focus()
		}
	}

	return t, nil
}

func (t *TablePickerScreen) selectTable() tea.Cmd {
	tableName := t.filtered[t.cursor]
	entry := t.entry
	entry.ListEndpoint = "/systeminsights/" + tableName
	entry.DisplayName = "System Insights: " + tableName
	entry.Schema.DefaultFields = nil // Tables have varying columns.

	return func() tea.Msg {
		return tui.PushScreenMsg{Screen: NewListScreen(entry)}
	}
}

func (t *TablePickerScreen) moveCursor(delta int) {
	t.cursor += delta
	if t.cursor < 0 {
		t.cursor = 0
	}
	if t.cursor >= len(t.filtered) {
		t.cursor = len(t.filtered) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

func (t *TablePickerScreen) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(t.filter.Value()))
	if query == "" {
		t.filtered = t.tables
		t.cursor = 0
		return
	}

	t.filtered = nil
	for _, name := range t.tables {
		if strings.Contains(name, query) {
			t.filtered = append(t.filtered, name)
		}
	}
	t.cursor = 0
}

func (t *TablePickerScreen) View() string {
	var sb strings.Builder

	sb.WriteString(style.Subtitle.Render("System Insights"))
	sb.WriteString(style.ResourceVerbs.Render(" — Select a table"))
	sb.WriteString("\n\n")

	if t.filtering {
		t.filter.Width = t.width - 4
		sb.WriteString(style.FilterInput.Render(t.filter.View()))
		sb.WriteString("\n")
	}

	maxVisible := t.height - 6
	if t.filtering {
		maxVisible -= 2
	}
	if maxVisible < 1 {
		maxVisible = 10
	}

	// Compute scroll offset.
	offset := 0
	if t.cursor >= maxVisible {
		offset = t.cursor - maxVisible + 1
	}

	end := offset + maxVisible
	if end > len(t.filtered) {
		end = len(t.filtered)
	}

	for i := offset; i < end; i++ {
		name := t.filtered[i]
		prefix := "  "
		rowStyle := style.ResourceName
		if i == t.cursor {
			prefix = "> "
			rowStyle = style.SelectedRow
		}
		sb.WriteString(rowStyle.Render(prefix + name))
		sb.WriteString("\n")
	}

	if len(t.filtered) == 0 {
		sb.WriteString(style.DimRow.Render("  No matching tables"))
		sb.WriteString("\n")
	}

	return sb.String()
}
