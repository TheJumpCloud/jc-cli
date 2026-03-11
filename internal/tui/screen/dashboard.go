package screen

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/component"
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

// Widget data keys for aggregation fetches.
const (
	wkUserList   = "dash:users-list"
	wkDeviceList = "dash:devices-list"
	wkEvents     = "dash:events"
)

// userAggregation holds client-side bucketed user data.
type userAggregation struct {
	Active    int
	Suspended int
	Locked    int
	MFAOn     int
	Total     int
}

// deviceAggregation holds client-side bucketed device data.
type deviceAggregation struct {
	OSCounts    map[string]int // os name → count
	Online      int            // lastContact < 1h
	Recent      int            // lastContact < 24h
	Stale       int            // lastContact < 7d
	Offline     int            // lastContact >= 7d
	Total       int
}

// DashboardScreen shows an org overview with resource counts and widgets.
type DashboardScreen struct {
	fetcher     *fetch.Fetcher
	counts      map[string]int
	loading     map[string]bool
	errors      map[string]string
	generations map[string]int64

	userAgg    *userAggregation
	deviceAgg  *deviceAggregation
	eventCount int
	eventsErr  string

	spinner  spinner.Model
	viewport viewport.Model
	ready    bool
	width    int
	height   int
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

	// Fetch resource counts.
	for _, res := range dashResources {
		d.loading[res.Key] = true
		gen := fetch.NextGeneration()
		d.generations[res.Key] = gen

		switch res.ClientType {
		case tui.ClientV1:
			cmds = append(cmds, d.fetcher.FetchV1Count(res.Key, res.Endpoint, gen))
		case tui.ClientV2:
			cmds = append(cmds, d.fetcher.FetchV2List(res.Key, res.Endpoint, api.V2ListOptions{}, gen))
		}
	}

	// Fetch full user list for aggregation (user status + MFA).
	d.loading[wkUserList] = true
	userGen := fetch.NextGeneration()
	d.generations[wkUserList] = userGen
	cmds = append(cmds, d.fetcher.FetchV1List(wkUserList, "/systemusers", api.ListOptions{}, userGen))

	// Fetch full device list for aggregation (OS + connectivity).
	d.loading[wkDeviceList] = true
	deviceGen := fetch.NextGeneration()
	d.generations[wkDeviceList] = deviceGen
	cmds = append(cmds, d.fetcher.FetchV1List(wkDeviceList, "/systems", api.ListOptions{}, deviceGen))

	// Fetch event count (last 24h).
	d.loading[wkEvents] = true
	eventsGen := fetch.NextGeneration()
	d.generations[wkEvents] = eventsGen
	now := time.Now().UTC()
	cmds = append(cmds, d.fetcher.FetchInsightsCount(wkEvents, api.InsightsQuery{
		Service:   "all",
		StartTime: now.Add(-24 * time.Hour).Format(time.RFC3339),
		EndTime:   now.Format(time.RFC3339),
	}, eventsGen))

	return tea.Batch(cmds...)
}

func (d *DashboardScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		headerHeight := 2 // title + blank line
		vpHeight := msg.Height - headerHeight - 2
		if vpHeight < 1 {
			vpHeight = 1
		}
		if !d.ready {
			d.viewport = viewport.New(msg.Width, vpHeight)
			d.ready = true
		} else {
			d.viewport.Width = msg.Width
			d.viewport.Height = vpHeight
		}
		d.viewport.SetContent(d.renderContent())
		return d, nil

	case fetch.CountResultMsg:
		gen, ok := d.generations[msg.ResourceKey]
		if !ok || msg.Generation != gen {
			return d, nil
		}
		d.loading[msg.ResourceKey] = false
		if msg.Err != nil {
			d.errors[msg.ResourceKey] = msg.Err.Error()
		} else {
			delete(d.errors, msg.ResourceKey)
			d.counts[msg.ResourceKey] = msg.Count
		}
		d.updateViewport()
		return d, nil

	case fetch.ListResultMsg:
		gen, ok := d.generations[msg.ResourceKey]
		if !ok || msg.Generation != gen {
			return d, nil
		}
		d.loading[msg.ResourceKey] = false
		if msg.Err != nil {
			d.errors[msg.ResourceKey] = msg.Err.Error()
			d.updateViewport()
			return d, nil
		}
		delete(d.errors, msg.ResourceKey)

		switch msg.ResourceKey {
		case wkUserList:
			d.userAgg = aggregateUsers(msg.Data)
		case wkDeviceList:
			d.deviceAgg = aggregateDevices(msg.Data)
		default:
			d.counts[msg.ResourceKey] = msg.TotalCount
		}
		d.updateViewport()
		return d, nil

	case fetch.InsightsCountResultMsg:
		gen, ok := d.generations[msg.ResourceKey]
		if !ok || msg.Generation != gen {
			return d, nil
		}
		d.loading[msg.ResourceKey] = false
		if msg.Err != nil {
			d.eventsErr = msg.Err.Error()
		} else {
			d.eventsErr = ""
			d.eventCount = msg.Count
		}
		d.updateViewport()
		return d, nil

	case spinner.TickMsg:
		if d.anyLoading() {
			var cmd tea.Cmd
			d.spinner, cmd = d.spinner.Update(msg)
			d.updateViewport()
			return d, cmd
		}
		return d, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return d, func() tea.Msg { return tui.PopScreenMsg{} }
		case "r":
			d.userAgg = nil
			d.deviceAgg = nil
			d.eventCount = 0
			d.eventsErr = ""
			return d, d.Init()
		}
		// Scroll keys forwarded to viewport.
		var cmd tea.Cmd
		d.viewport, cmd = d.viewport.Update(msg)
		return d, cmd
	}

	return d, nil
}

func (d *DashboardScreen) updateViewport() {
	if d.ready {
		d.viewport.SetContent(d.renderContent())
	}
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
	sb.WriteString("\n")

	if d.ready {
		sb.WriteString(d.viewport.View())
	}

	sb.WriteString("\n")
	if d.anyLoading() {
		sb.WriteString(style.Help.Render("Fetching data... " + d.spinner.View()))
	} else {
		sb.WriteString(style.Help.Render("r:refresh  esc:back  j/k:scroll"))
	}
	sb.WriteString("\n")

	return sb.String()
}

// renderContent builds the full scrollable content for the viewport.
func (d *DashboardScreen) renderContent() string {
	var sb strings.Builder

	// --- Stat Cards Row ---
	sb.WriteString(d.renderStatCards())
	sb.WriteString("\n")

	// --- Widget Sections ---
	columns := d.responsiveColumns()
	leftWidgets := d.renderLeftWidgets(columns)
	rightWidgets := d.renderRightWidgets(columns)

	if columns == 1 {
		sb.WriteString(leftWidgets)
		sb.WriteString("\n")
		sb.WriteString(rightWidgets)
	} else {
		colWidth := d.width / 2
		leftCol := lipgloss.NewStyle().Width(colWidth).Render(leftWidgets)
		rightCol := lipgloss.NewStyle().Width(colWidth).Render(rightWidgets)
		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftCol, rightCol))
	}

	return sb.String()
}

func (d *DashboardScreen) responsiveColumns() int {
	if d.width >= 90 {
		return 2
	}
	return 1
}

// renderStatCards renders resource counts as a row of styled cards.
func (d *DashboardScreen) renderStatCards() string {
	type cardInfo struct {
		label string
		key   string
		color lipgloss.Color
	}
	cards := []cardInfo{
		{"Users", "users", style.ColorSecondary},
		{"Devices", "devices", style.ColorSuccess},
		{"User Groups", "user-groups", style.ColorPrimary},
		{"Device Groups", "device-groups", style.ColorHighlight},
		{"Commands", "commands", style.ColorWarning},
		{"Policies", "policies", style.ColorSecondary},
		{"Apps", "apps", style.ColorSuccess},
	}

	columns := d.responsiveColumns()
	cardsPerRow := 4
	if columns == 1 {
		cardsPerRow = 3
	}
	cardWidth := 0
	if d.width > 0 {
		cardWidth = d.width / cardsPerRow
		if cardWidth < 14 {
			cardWidth = 14
		}
	} else {
		cardWidth = 18
	}

	var rows []string
	var currentRow []string

	for _, c := range cards {
		value := d.spinner.View()
		if !d.loading[c.key] {
			if errMsg, ok := d.errors[c.key]; ok {
				value = style.Error.Render("err")
				_ = errMsg
			} else {
				value = fmt.Sprintf("%d", d.counts[c.key])
			}
		}

		card := component.StatCard{
			Label: c.label,
			Value: value,
			Color: c.color,
			Width: cardWidth,
		}
		currentRow = append(currentRow, card.View())

		if len(currentRow) >= cardsPerRow {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
			currentRow = nil
		}
	}
	if len(currentRow) > 0 {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
	}

	return strings.Join(rows, "\n")
}

// renderLeftWidgets renders the left column: user status, MFA, policy compliance.
func (d *DashboardScreen) renderLeftWidgets(columns int) string {
	var sb strings.Builder
	widgetWidth := d.width
	if columns > 1 {
		widgetWidth = d.width / 2
	}
	if widgetWidth < 20 {
		widgetWidth = 20
	}

	// User Status bar chart.
	if d.loading[wkUserList] {
		sb.WriteString(d.renderLoadingWidget("User Status", widgetWidth))
	} else if _, ok := d.errors[wkUserList]; ok {
		sb.WriteString(d.renderErrorWidget("User Status", d.errors[wkUserList], widgetWidth))
	} else if d.userAgg != nil {
		chart := component.BarChart{
			Title: "User Status",
			Width: widgetWidth,
			Items: []component.BarItem{
				{Label: "Active", Value: d.userAgg.Active, Color: style.ColorSuccess},
				{Label: "Suspended", Value: d.userAgg.Suspended, Color: style.ColorWarning},
				{Label: "Locked", Value: d.userAgg.Locked, Color: style.ColorError},
			},
		}
		sb.WriteString(chart.View())
	}
	sb.WriteString("\n")

	// MFA Adoption progress.
	if d.loading[wkUserList] {
		sb.WriteString(d.renderLoadingWidget("MFA Adoption", widgetWidth))
	} else if _, ok := d.errors[wkUserList]; ok {
		// Already showed error above, skip.
	} else if d.userAgg != nil {
		prog := component.ProgressRing{
			Title:   "MFA Adoption",
			Current: d.userAgg.MFAOn,
			Total:   d.userAgg.Total,
			Color:   style.ColorSuccess,
			Width:   widgetWidth,
		}
		sb.WriteString(prog.View())
	}

	return sb.String()
}

// renderRightWidgets renders the right column: device OS, connectivity, events.
func (d *DashboardScreen) renderRightWidgets(columns int) string {
	var sb strings.Builder
	widgetWidth := d.width
	if columns > 1 {
		widgetWidth = d.width / 2
	}
	if widgetWidth < 20 {
		widgetWidth = 20
	}

	// Device OS Distribution bar chart.
	if d.loading[wkDeviceList] {
		sb.WriteString(d.renderLoadingWidget("Device OS Distribution", widgetWidth))
	} else if _, ok := d.errors[wkDeviceList]; ok {
		sb.WriteString(d.renderErrorWidget("Device OS Distribution", d.errors[wkDeviceList], widgetWidth))
	} else if d.deviceAgg != nil {
		items := buildOSBarItems(d.deviceAgg.OSCounts)
		chart := component.BarChart{
			Title: "Device OS Distribution",
			Width: widgetWidth,
			Items: items,
		}
		sb.WriteString(chart.View())
	}
	sb.WriteString("\n")

	// Device Connectivity.
	if d.loading[wkDeviceList] {
		sb.WriteString(d.renderLoadingWidget("Device Connectivity", widgetWidth))
	} else if d.deviceAgg != nil {
		chart := component.BarChart{
			Title: "Device Connectivity",
			Width: widgetWidth,
			Items: []component.BarItem{
				{Label: "Online (<1h)", Value: d.deviceAgg.Online, Color: style.ColorSuccess},
				{Label: "Recent (<24h)", Value: d.deviceAgg.Recent, Color: style.ColorSecondary},
				{Label: "Stale (<7d)", Value: d.deviceAgg.Stale, Color: style.ColorWarning},
				{Label: "Offline (>7d)", Value: d.deviceAgg.Offline, Color: style.ColorError},
			},
		}
		sb.WriteString(chart.View())
	}
	sb.WriteString("\n")

	// Recent Events (24h).
	if d.loading[wkEvents] {
		sb.WriteString(d.renderLoadingWidget("Recent Events (24h)", widgetWidth))
	} else if d.eventsErr != "" {
		sb.WriteString(d.renderErrorWidget("Recent Events (24h)", d.eventsErr, widgetWidth))
	} else {
		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(style.ColorSecondary)
		sb.WriteString(titleStyle.Render("Recent Events (24h)"))
		sb.WriteString("\n")
		countStyle := lipgloss.NewStyle().Bold(true).Foreground(style.ColorText)
		dimStyle := lipgloss.NewStyle().Foreground(style.ColorDimText)
		sb.WriteString(countStyle.Render(fmt.Sprintf("%d", d.eventCount)))
		sb.WriteString(dimStyle.Render(" events"))
		sb.WriteString("\n")
	}

	return sb.String()
}

func (d *DashboardScreen) renderLoadingWidget(title string, _ int) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(style.ColorSecondary)
	return titleStyle.Render(title) + "\n" + d.spinner.View() + " loading...\n"
}

func (d *DashboardScreen) renderErrorWidget(title, errMsg string, _ int) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(style.ColorSecondary)
	return titleStyle.Render(title) + "\n" + style.Error.Render("error: "+errMsg) + "\n"
}

// --- Data Aggregation ---

// aggregateUsers computes user status and MFA counts from raw user JSON.
func aggregateUsers(data []json.RawMessage) *userAggregation {
	agg := &userAggregation{}
	for _, raw := range data {
		var u struct {
			Activated     bool `json:"activated"`
			Suspended     bool `json:"suspended"`
			AccountLocked bool `json:"account_locked"`
			TOTPEnabled   bool `json:"totp_enabled"`
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			continue
		}
		agg.Total++
		switch {
		case u.AccountLocked:
			agg.Locked++
		case u.Suspended:
			agg.Suspended++
		default:
			agg.Active++
		}
		if u.TOTPEnabled {
			agg.MFAOn++
		}
	}
	return agg
}

// aggregateDevices computes OS distribution and connectivity buckets from raw device JSON.
func aggregateDevices(data []json.RawMessage) *deviceAggregation {
	agg := &deviceAggregation{
		OSCounts: make(map[string]int),
	}
	now := time.Now()
	for _, raw := range data {
		var dev struct {
			OS          string `json:"os"`
			LastContact string `json:"lastContact"`
		}
		if err := json.Unmarshal(raw, &dev); err != nil {
			continue
		}
		agg.Total++

		// OS distribution.
		osName := dev.OS
		if osName == "" {
			osName = "Unknown"
		}
		agg.OSCounts[osName]++

		// Connectivity bucketing based on lastContact.
		if dev.LastContact != "" {
			if t, err := time.Parse(time.RFC3339, dev.LastContact); err == nil {
				age := now.Sub(t)
				switch {
				case age < time.Hour:
					agg.Online++
				case age < 24*time.Hour:
					agg.Recent++
				case age < 7*24*time.Hour:
					agg.Stale++
				default:
					agg.Offline++
				}
				continue
			}
		}
		agg.Offline++
	}
	return agg
}

// buildOSBarItems converts an OS count map to sorted BarItems.
func buildOSBarItems(osCounts map[string]int) []component.BarItem {
	// Canonical order; any remaining appended at end.
	order := []string{"Mac OS X", "Windows", "Linux", "Ubuntu"}
	colors := map[string]lipgloss.Color{
		"Mac OS X": style.ColorSecondary,
		"Windows":  style.ColorPrimary,
		"Linux":    style.ColorWarning,
		"Ubuntu":   style.ColorWarning,
	}

	seen := make(map[string]bool)
	var items []component.BarItem

	for _, name := range order {
		if count, ok := osCounts[name]; ok {
			color := colors[name]
			if color == "" {
				color = style.ColorMuted
			}
			items = append(items, component.BarItem{Label: name, Value: count, Color: color})
			seen[name] = true
		}
	}
	// Add any remaining OS types.
	for name, count := range osCounts {
		if !seen[name] {
			items = append(items, component.BarItem{Label: name, Value: count, Color: style.ColorMuted})
		}
	}

	return items
}
