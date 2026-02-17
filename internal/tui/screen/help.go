package screen

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// helpBinding pairs a key label with a description.
type helpBinding struct {
	Key  string
	Desc string
}

// helpSection groups bindings under a header.
type helpSection struct {
	Title    string
	Bindings []helpBinding
}

var helpSections = []helpSection{
	{
		Title: "Global",
		Bindings: []helpBinding{
			{"q", "Quit"},
			{"?", "Toggle help"},
			{"esc", "Go back"},
		},
	},
	{
		Title: "Navigation",
		Bindings: []helpBinding{
			{"j / k", "Move down / up"},
			{"g / G", "Jump to top / bottom"},
			{"enter", "Open selected item"},
		},
	},
	{
		Title: "Home Screen",
		Bindings: []helpBinding{
			{"d", "Open dashboard"},
			{"b", "Toggle bookmark"},
			{"/", "Filter resources"},
		},
	},
	{
		Title: "List Screen",
		Bindings: []helpBinding{
			{"/", "Filter or search"},
			{"s", "Cycle sort field"},
			{"S", "Toggle sort direction"},
			{"r", "Refresh data"},
			{"a", "Toggle all fields"},
			{"c", "Copy ID to clipboard"},
			{"e", "Export data"},
		},
	},
	{
		Title: "Detail Screen",
		Bindings: []helpBinding{
			{"tab", "Switch to associations"},
			{"h / l", "Previous / next target type"},
			{"a", "Toggle all fields"},
			{"r", "Refresh data"},
			{"c", "Copy ID to clipboard"},
			{"e", "Export data"},
		},
	},
	{
		Title: "Filter Syntax",
		Bindings: []helpBinding{
			{"field:op:value", "Filter expression"},
			{"eq, ne", "Equal, not equal"},
			{"gt, lt", "Greater than, less than"},
			{"gte, lte", "Greater or equal, less or equal"},
		},
	},
	{
		Title: "Search",
		Bindings: []helpBinding{
			{"~term", "Text search across fields"},
		},
	},
}

// HelpScreen displays a static keybinding reference overlay.
type HelpScreen struct {
	width  int
	height int
}

// NewHelpScreen creates a new help screen.
func NewHelpScreen() *HelpScreen {
	return &HelpScreen{}
}

func (h *HelpScreen) Title() string { return "Help" }

func (h *HelpScreen) Init() tea.Cmd { return nil }

func (h *HelpScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width = msg.Width
		h.height = msg.Height
		return h, nil

	case tea.KeyMsg:
		if key.Matches(msg, tui.GlobalKeyMap.Back) || key.Matches(msg, tui.GlobalKeyMap.Help) {
			return h, func() tea.Msg { return tui.PopScreenMsg{} }
		}
	}

	return h, nil
}

func (h *HelpScreen) View() string {
	var sb strings.Builder

	sb.WriteString(style.Title.Render("Keyboard Shortcuts"))
	sb.WriteString("\n")

	for _, section := range helpSections {
		sb.WriteString(style.SectionHeader.Render(section.Title))
		sb.WriteString("\n")

		for _, b := range section.Bindings {
			keyStr := style.FieldKey.Render("  " + b.Key)
			desc := style.FieldValue.Render("  " + b.Desc)
			sb.WriteString(keyStr)
			sb.WriteString(desc)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(style.DimRow.Render("  Press ? or Esc to close"))
	sb.WriteString("\n")

	return sb.String()
}
