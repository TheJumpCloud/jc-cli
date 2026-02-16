package screen

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/fetch"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// dashResource defines a resource to show on the dashboard.
type dashResource struct {
	Key        string
	Label      string
	ClientType tui.ClientType
	Endpoint   string
}

// dashResources lists the resources displayed on the dashboard.
var dashResources = []dashResource{
	{"users", "Users", tui.ClientV1, "/systemusers"},
	{"devices", "Devices", tui.ClientV1, "/systems"},
	{"user-groups", "User Groups", tui.ClientV2, "/usergroups"},
	{"device-groups", "Device Groups", tui.ClientV2, "/systemgroups"},
	{"commands", "Commands", tui.ClientV1, "/commands"},
	{"policies", "Policies", tui.ClientV2, "/policies"},
	{"apps", "Applications", tui.ClientV1, "/applications"},
}

// DashboardScreen shows an org overview with resource counts.
type DashboardScreen struct {
	fetcher     *fetch.Fetcher
	counts      map[string]int
	loading     map[string]bool
	errors      map[string]string
	generations map[string]int64
	spinner     spinner.Model
	width       int
	height      int
}

// NewDashboardScreen creates a new dashboard screen.
func NewDashboardScreen() *DashboardScreen {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = style.Spinner

	return &DashboardScreen{
		fetcher:     fetch.NewFetcher(),
		counts:      make(map[string]int),
		loading:     make(map[string]bool),
		errors:      make(map[string]string),
		generations: make(map[string]int64),
		spinner:     s,
	}
}

// SetFetcher allows injecting a custom fetcher (for tests).
func (d *DashboardScreen) SetFetcher(f *fetch.Fetcher) {
	d.fetcher = f
}

func (d *DashboardScreen) Title() string { return "Dashboard" }

func (d *DashboardScreen) Init() tea.Cmd {
	cmds := []tea.Cmd{d.spinner.Tick}
	for _, res := range dashResources {
		d.loading[res.Key] = true
		gen := fetch.NextGeneration()
		d.generations[res.Key] = gen

		switch res.ClientType {
		case tui.ClientV1:
			cmds = append(cmds, d.fetcher.FetchV1List(res.Key, res.Endpoint, api.ListOptions{}, gen))
		case tui.ClientV2:
			cmds = append(cmds, d.fetcher.FetchV2List(res.Key, res.Endpoint, api.V2ListOptions{}, gen))
		}
	}
	return tea.Batch(cmds...)
}

func (d *DashboardScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		return d, nil

	case fetch.ListResultMsg:
		gen, ok := d.generations[msg.ResourceKey]
		if !ok || msg.Generation != gen {
			return d, nil
		}
		d.loading[msg.ResourceKey] = false
		if msg.Err != nil {
			d.errors[msg.ResourceKey] = msg.Err.Error()
			return d, nil
		}
		d.counts[msg.ResourceKey] = msg.TotalCount
		return d, nil

	case spinner.TickMsg:
		if d.anyLoading() {
			var cmd tea.Cmd
			d.spinner, cmd = d.spinner.Update(msg)
			return d, cmd
		}
		return d, nil

	case tea.KeyMsg:
		switch {
		case msg.String() == "esc":
			return d, func() tea.Msg { return tui.PopScreenMsg{} }
		case msg.String() == "r":
			return d, d.Init()
		}
	}

	return d, nil
}

func (d *DashboardScreen) anyLoading() bool {
	for _, v := range d.loading {
		if v {
			return true
		}
	}
	return false
}

func (d *DashboardScreen) View() string {
	var sb strings.Builder

	sb.WriteString(style.Title.Render("Organization Dashboard"))
	sb.WriteString("\n\n")

	for _, res := range dashResources {
		label := style.FieldKey.Render(fmt.Sprintf("%-16s", res.Label))

		var value string
		if d.loading[res.Key] {
			value = d.spinner.View() + " loading..."
		} else if errMsg, ok := d.errors[res.Key]; ok {
			value = style.Error.Render("error: " + errMsg)
		} else {
			count := d.counts[res.Key]
			value = style.FieldValue.Render(fmt.Sprintf("%d", count))
		}

		sb.WriteString(label + "  " + value + "\n")
	}

	if d.anyLoading() {
		sb.WriteString("\n")
		sb.WriteString(style.Help.Render("Fetching resource counts..."))
	} else {
		sb.WriteString("\n")
		sb.WriteString(style.Help.Render("r:refresh  esc:back"))
	}
	sb.WriteString("\n")

	return sb.String()
}
