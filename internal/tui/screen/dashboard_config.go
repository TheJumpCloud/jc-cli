package screen

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
	"github.com/spf13/viper"
)

// widgetDef defines a toggleable dashboard widget.
type widgetDef struct {
	Key   string
	Label string
}

// allWidgets lists all available dashboard widgets in display order.
var allWidgets = []widgetDef{
	{"user-status", "User Status"},
	{"mfa", "MFA Adoption"},
	{"policy-compliance", "Policy Compliance"},
	{"device-os", "Device OS Distribution"},
	{"connectivity", "Device Connectivity"},
	{"events", "Events by Service"},
	{"sparkline", "Event Trend (7 days)"},
}

// DashboardConfigScreen lets the user toggle which widgets are visible.
type DashboardConfigScreen struct {
	cursor  int
	enabled map[string]bool
	width   int
	height  int
}

// NewDashboardConfigScreen creates a config screen with current widget settings.
func NewDashboardConfigScreen() *DashboardConfigScreen {
	enabled := make(map[string]bool)
	configured := viper.GetStringSlice("tui.dashboard.widgets")
	if len(configured) == 0 {
		// Default: all enabled.
		for _, w := range allWidgets {
			enabled[w.Key] = true
		}
	} else {
		for _, key := range configured {
			enabled[key] = true
		}
	}
	return &DashboardConfigScreen{enabled: enabled}
}

func (c *DashboardConfigScreen) Title() string { return "Dashboard Settings" }

func (c *DashboardConfigScreen) Init() tea.Cmd { return nil }

func (c *DashboardConfigScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		return c, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return c, func() tea.Msg { return tui.PopScreenMsg{} }
		case "j", "down":
			if c.cursor < len(allWidgets)-1 {
				c.cursor++
			}
			return c, nil
		case "k", "up":
			if c.cursor > 0 {
				c.cursor--
			}
			return c, nil
		case " ":
			key := allWidgets[c.cursor].Key
			c.enabled[key] = !c.enabled[key]
			return c, nil
		case "enter":
			c.save()
			return c, func() tea.Msg { return tui.PopScreenMsg{} }
		}
	}
	return c, nil
}

func (c *DashboardConfigScreen) View() string {
	var sb strings.Builder

	sb.WriteString(style.Title.Render("Dashboard Widget Settings"))
	sb.WriteString("\n\n")

	for i, w := range allWidgets {
		cursor := "  "
		if i == c.cursor {
			cursor = "▸ "
		}

		check := "☐"
		if c.enabled[w.Key] {
			check = "☑"
		}

		labelStyle := lipgloss.NewStyle().Foreground(style.ColorText)
		checkStyle := lipgloss.NewStyle().Foreground(style.ColorSuccess)
		if !c.enabled[w.Key] {
			checkStyle = lipgloss.NewStyle().Foreground(style.ColorMuted)
		}
		if i == c.cursor {
			labelStyle = lipgloss.NewStyle().Foreground(style.ColorHighlight).Bold(true)
		}

		sb.WriteString(cursor + checkStyle.Render(check) + " " + labelStyle.Render(w.Label) + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(style.Help.Render("j/k:navigate  space:toggle  enter:save  esc:cancel"))
	sb.WriteString("\n")

	return sb.String()
}

func (c *DashboardConfigScreen) save() {
	var keys []string
	for _, w := range allWidgets {
		if c.enabled[w.Key] {
			keys = append(keys, w.Key)
		}
	}
	viper.Set("tui.dashboard.widgets", keys)
}

// IsWidgetEnabled checks if a dashboard widget is enabled in config.
func IsWidgetEnabled(key string) bool {
	configured := viper.GetStringSlice("tui.dashboard.widgets")
	if len(configured) == 0 {
		return true // all enabled by default
	}
	for _, k := range configured {
		if k == key {
			return true
		}
	}
	return false
}
