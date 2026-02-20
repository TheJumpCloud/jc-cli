package screen

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// SubMenuScreen shows a small list of items within a grouping (e.g. Cloud Directories).
// Selecting an item pushes a ListScreen for the chosen resource.
type SubMenuScreen struct {
	title   string
	entries []tui.ResourceEntry
	cursor  int
	width   int
	height  int
}

// NewSubMenuScreen creates a sub-menu screen with the given title and entries.
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
		case key.Matches(msg, tui.GlobalKeyMap.Back):
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
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
