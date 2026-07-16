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
	"github.com/spf13/viper"
)

// dashResource defines a resource to show on the dashboard.
type dashResource struct {
	Key        string
	Label      string
	ClientType tui.ClientType
	Endpoint   string
}

// dashResources lists the resources displayed on the dashboard.
// Note: "users" and "devices" counts are derived from the full-list
// aggregation fetches (wkUserList/wkDeviceList), so they are excluded
// here to avoid redundant API calls.
var dashResources = []dashResource{
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
	wkPolicies   = "dash:policies"
)

// wkPolicyStatus returns the widget key for a policy's status fetch.
func wkPolicyStatus(policyID string) string {
	return "dash:policy-status:" + policyID
}

// wkEventDay returns the widget key for a daily event count.
func wkEventDay(dayOffset int) string {
	return fmt.Sprintf("dash:events-day:%d", dayOffset)
}

// sparklineDays is the number of days shown in the events sparkline.
const sparklineDays = 7

// maxDashPolicies limits the number of policies fetched for the compliance widget.
const maxDashPolicies = 5

// eventServices lists the Insights services shown in the events breakdown chart.
var eventServices = []string{"sso", "directory", "ldap", "radius", "mdm"}

// wkEventService returns the widget key for a per-service event count.
func wkEventService(service string) string {
	return "dash:events:" + service
}

// retryWidgetMsg triggers a retry for a specific widget.
type retryWidgetMsg struct {
	Key string
}

// autoRefreshMsg triggers a full dashboard refresh on a timer.
type autoRefreshMsg struct{}

// maxRetries is the maximum number of retry attempts per widget.
const maxRetries = 3

// cardKeys maps stat card index to resource key for navigation.
var cardKeys = []string{
	"users", "devices", "user-groups", "device-groups",
	"commands", "policies", "apps",
}

// focusZones lists the widget zones available for Tab navigation.
// Index -1 = stat cards (uses gridCur), 0+ = these zones.
var focusZones = []string{
	"user-status", "mfa", "policies", "device-os", "connectivity", "events",
}

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
	OSCounts map[string]int // os name → count
	Online   int            // lastContact < 1h
	Recent   int            // lastContact < 24h
	Stale    int            // lastContact < 7d
	Offline  int            // lastContact >= 7d
	Total    int
}

// policyComplianceAgg holds aggregated policy compliance status counts.
type policyComplianceAgg struct {
	Applied int
	Pending int
	Failed  int
}

// DashboardScreen shows an org overview with resource counts and widgets.
type DashboardScreen struct {
	fetcher     *fetch.Fetcher
	counts      map[string]int
	loading     map[string]bool
	errors      map[string]string
	generations map[string]int64

	userAgg         *userAggregation
	deviceAgg       *deviceAggregation
	eventCount      int
	eventsErr       string
	eventsByService map[string]int

	policyCompliance *policyComplianceAgg // aggregated policy compliance data
	eventSparkline   [sparklineDays]int   // daily event counts for sparkline (index 0 = oldest)
	sparklineReady   int                  // count of received sparkline day results

	retries map[string]int // retry count per widget key

	refreshInterval time.Duration // auto-refresh interval (0 = disabled)

	gridCur  int // selected stat card index (-1 = none)
	focusIdx int // focused widget zone index (-1 = none / cards)

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
		fetcher:         fetch.NewFetcher(),
		counts:          make(map[string]int),
		loading:         make(map[string]bool),
		errors:          make(map[string]string),
		generations:     make(map[string]int64),
		eventsByService: make(map[string]int),
		retries:         make(map[string]int),
		refreshInterval: refreshIntervalFromConfig(),
		gridCur:         0,
		focusIdx:        -1, // start with cards focused
		spinner:         s,
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

	// Fetch first 5 policies for compliance widget.
	d.loading[wkPolicies] = true
	policiesGen := fetch.NextGeneration()
	d.generations[wkPolicies] = policiesGen
	cmds = append(cmds, d.fetcher.FetchV2List(wkPolicies, "/policies", api.V2ListOptions{Limit: maxDashPolicies}, policiesGen))

	// Fetch event count (last 24h) — total.
	d.loading[wkEvents] = true
	eventsGen := fetch.NextGeneration()
	d.generations[wkEvents] = eventsGen
	now := time.Now().UTC()
	cmds = append(cmds, d.fetcher.FetchInsightsCount(wkEvents, api.InsightsQuery{
		Service:   "all",
		StartTime: now.Add(-24 * time.Hour).Format(time.RFC3339),
		EndTime:   now.Format(time.RFC3339),
	}, eventsGen))

	// Fetch per-service event counts for breakdown chart.
	for _, svc := range eventServices {
		key := wkEventService(svc)
		d.loading[key] = true
		svcGen := fetch.NextGeneration()
		d.generations[key] = svcGen
		cmds = append(cmds, d.fetcher.FetchInsightsCount(key, api.InsightsQuery{
			Service:   svc,
			StartTime: now.Add(-24 * time.Hour).Format(time.RFC3339),
			EndTime:   now.Format(time.RFC3339),
		}, svcGen))
	}

	// Fetch daily event counts for sparkline (past 7 days).
	for i := 0; i < sparklineDays; i++ {
		dayOffset := sparklineDays - 1 - i // oldest first
		key := wkEventDay(dayOffset)
		d.loading[key] = true
		dayGen := fetch.NextGeneration()
		d.generations[key] = dayGen
		dayStart := now.AddDate(0, 0, -(dayOffset + 1))
		dayEnd := now.AddDate(0, 0, -dayOffset)
		cmds = append(cmds, d.fetcher.FetchInsightsCount(key, api.InsightsQuery{
			Service:   "all",
			StartTime: dayStart.Format(time.RFC3339),
			EndTime:   dayEnd.Format(time.RFC3339),
		}, dayGen))
	}

	// Schedule auto-refresh if configured.
	if d.refreshInterval > 0 {
		cmds = append(cmds, tea.Tick(d.refreshInterval, func(time.Time) tea.Msg {
			return autoRefreshMsg{}
		}))
	}

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
			d.updateViewport()
			return d, d.scheduleRetry(msg.ResourceKey)
		}
		delete(d.errors, msg.ResourceKey)
		delete(d.retries, msg.ResourceKey)
		d.counts[msg.ResourceKey] = msg.Count
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
			return d, d.scheduleRetry(msg.ResourceKey)
		}
		delete(d.errors, msg.ResourceKey)
		delete(d.retries, msg.ResourceKey)

		switch msg.ResourceKey {
		case wkUserList:
			d.userAgg = aggregateUsers(msg.Data)
		case wkDeviceList:
			d.deviceAgg = aggregateDevices(msg.Data, time.Now())
		case wkPolicies:
			// Fire status fetches for each policy.
			var statusCmds []tea.Cmd
			for _, raw := range msg.Data {
				var p struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(raw, &p); err != nil || p.ID == "" {
					continue
				}
				key := wkPolicyStatus(p.ID)
				d.loading[key] = true
				statusGen := fetch.NextGeneration()
				d.generations[key] = statusGen
				endpoint := fmt.Sprintf("/policies/%s/policystatuses", p.ID)
				statusCmds = append(statusCmds, d.fetcher.FetchV2List(key, endpoint, api.V2ListOptions{}, statusGen))
			}
			d.updateViewport()
			if len(statusCmds) > 0 {
				return d, tea.Batch(statusCmds...)
			}
			return d, nil
		default:
			// Check if this is a policy status result.
			if strings.HasPrefix(msg.ResourceKey, "dash:policy-status:") {
				d.aggregatePolicyStatus(msg.Data)
				d.updateViewport()
				return d, nil
			}
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
			if msg.ResourceKey == wkEvents {
				d.eventsErr = msg.Err.Error()
			}
			d.errors[msg.ResourceKey] = msg.Err.Error()
			d.updateViewport()
			return d, d.scheduleRetry(msg.ResourceKey)
		}
		delete(d.errors, msg.ResourceKey)
		delete(d.retries, msg.ResourceKey)
		if msg.ResourceKey == wkEvents {
			d.eventsErr = ""
			d.eventCount = msg.Count
		} else if strings.HasPrefix(msg.ResourceKey, "dash:events-day:") {
			var dayOffset int
			fmt.Sscanf(strings.TrimPrefix(msg.ResourceKey, "dash:events-day:"), "%d", &dayOffset)
			idx := sparklineDays - 1 - dayOffset
			if idx >= 0 && idx < sparklineDays {
				d.eventSparkline[idx] = msg.Count
				d.sparklineReady++
			}
		} else if strings.HasPrefix(msg.ResourceKey, "dash:events:") {
			svc := strings.TrimPrefix(msg.ResourceKey, "dash:events:")
			d.eventsByService[svc] = msg.Count
		}
		d.updateViewport()
		return d, nil

	case retryWidgetMsg:
		return d, d.retryWidget(msg.Key)

	case autoRefreshMsg:
		d.userAgg = nil
		d.deviceAgg = nil
		d.eventCount = 0
		d.eventsErr = ""
		d.eventsByService = make(map[string]int)
		d.policyCompliance = nil
		d.eventSparkline = [sparklineDays]int{}
		d.sparklineReady = 0
		return d, d.Init()

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
			d.eventsByService = make(map[string]int)
			d.policyCompliance = nil
			d.eventSparkline = [sparklineDays]int{}
			d.sparklineReady = 0
			d.retries = make(map[string]int)
			return d, d.Init()
		case "left", "h":
			if d.focusIdx < 0 && d.gridCur > 0 {
				d.gridCur--
				d.updateViewport()
			}
			return d, nil
		case "right", "l":
			if d.focusIdx < 0 && d.gridCur < len(cardKeys)-1 {
				d.gridCur++
				d.updateViewport()
			}
			return d, nil
		case "tab":
			d.focusIdx++
			if d.focusIdx >= len(focusZones) {
				d.focusIdx = -1 // wrap back to cards
			}
			d.updateViewport()
			return d, nil
		case "shift+tab":
			d.focusIdx--
			if d.focusIdx < -1 {
				d.focusIdx = len(focusZones) - 1
			}
			d.updateViewport()
			return d, nil
		case "enter":
			if d.focusIdx < 0 {
				return d, d.pushCardScreen()
			}
			return d, nil
		case "c":
			return d, func() tea.Msg {
				return tui.PushScreenMsg{Screen: NewDashboardConfigScreen()}
			}
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

// scrollIndicator returns a position label based on viewport scroll percentage.
func (d *DashboardScreen) scrollIndicator() string {
	if !d.ready {
		return ""
	}
	pct := d.viewport.ScrollPercent()
	switch {
	case pct <= 0:
		return "[Top]"
	case pct >= 1:
		return "[Bot]"
	default:
		return fmt.Sprintf("[%d%%]", int(pct*100))
	}
}

// cardColor returns a health-coded color for a stat card based on aggregation data.
// When no data is available, the static fallback color is returned.
func (d *DashboardScreen) cardColor(key string, fallback lipgloss.Color) lipgloss.Color {
	switch key {
	case "users":
		if d.userAgg != nil && d.userAgg.Total > 0 {
			badPct := float64(d.userAgg.Suspended+d.userAgg.Locked) / float64(d.userAgg.Total) * 100
			if badPct > 25 {
				return style.ColorError
			}
			if badPct > 10 {
				return style.ColorWarning
			}
		}
	case "devices":
		if d.deviceAgg != nil && d.deviceAgg.Total > 0 {
			offlinePct := float64(d.deviceAgg.Offline) / float64(d.deviceAgg.Total) * 100
			if offlinePct > 50 {
				return style.ColorError
			}
			if offlinePct > 25 {
				return style.ColorWarning
			}
		}
	}
	return fallback
}

// isZoneFocused returns true if the given focus zone name is currently focused.
func (d *DashboardScreen) isZoneFocused(zone string) bool {
	if d.focusIdx < 0 || d.focusIdx >= len(focusZones) {
		return false
	}
	return focusZones[d.focusIdx] == zone
}

// pushCardScreen navigates to the ListScreen for the currently selected stat card.
func (d *DashboardScreen) pushCardScreen() tea.Cmd {
	if d.gridCur < 0 || d.gridCur >= len(cardKeys) {
		return nil
	}
	key := cardKeys[d.gridCur]
	registry := tui.RegistryByKey()
	entry, ok := registry[key]
	if !ok {
		return nil
	}
	return func() tea.Msg {
		return tui.PushScreenMsg{Screen: NewListScreen(entry)}
	}
}

// scheduleRetry schedules a retry for a failed widget with exponential backoff.
// Returns nil if max retries exceeded.
func (d *DashboardScreen) scheduleRetry(key string) tea.Cmd {
	count := d.retries[key]
	if count >= maxRetries {
		return nil
	}
	d.retries[key] = count + 1
	delay := time.Duration(10<<uint(count)) * time.Second // 10s, 20s, 40s
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return retryWidgetMsg{Key: key}
	})
}

// retryWidget re-fires the fetch for a specific widget key.
func (d *DashboardScreen) retryWidget(key string) tea.Cmd {
	gen := fetch.NextGeneration()
	d.generations[key] = gen
	d.loading[key] = true
	delete(d.errors, key)

	now := time.Now().UTC()

	// Per-service event count.
	if strings.HasPrefix(key, "dash:events:") {
		svc := strings.TrimPrefix(key, "dash:events:")
		return d.fetcher.FetchInsightsCount(key, api.InsightsQuery{
			Service:   svc,
			StartTime: now.Add(-24 * time.Hour).Format(time.RFC3339),
			EndTime:   now.Format(time.RFC3339),
		}, gen)
	}

	switch key {
	case wkEvents:
		return d.fetcher.FetchInsightsCount(key, api.InsightsQuery{
			Service:   "all",
			StartTime: now.Add(-24 * time.Hour).Format(time.RFC3339),
			EndTime:   now.Format(time.RFC3339),
		}, gen)
	case wkUserList:
		return d.fetcher.FetchV1List(key, "/systemusers", api.ListOptions{}, gen)
	case wkDeviceList:
		return d.fetcher.FetchV1List(key, "/systems", api.ListOptions{}, gen)
	}

	// Resource counts.
	for _, res := range dashResources {
		if res.Key == key {
			switch res.ClientType {
			case tui.ClientV1:
				return d.fetcher.FetchV1Count(key, res.Endpoint, gen)
			case tui.ClientV2:
				return d.fetcher.FetchV2List(key, res.Endpoint, api.V2ListOptions{}, gen)
			}
		}
	}
	return nil
}

// refreshIntervalFromConfig reads the auto-refresh interval from config.
func refreshIntervalFromConfig() time.Duration {
	secs := viper.GetInt("tui.refresh_interval")
	if secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
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
		scrollPos := d.scrollIndicator()
		helpText := "r:refresh  c:config  esc:back  j/k:scroll  tab:focus  ←→:cards  enter:open  " + scrollPos
		if d.refreshInterval > 0 {
			helpText += fmt.Sprintf("  auto:%s", d.refreshInterval)
		}
		sb.WriteString(style.Help.Render(helpText))
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

	for i, c := range cards {
		value := d.spinner.View()

		// Users and devices counts are derived from aggregation data
		// (no separate API calls needed).
		switch c.key {
		case "users":
			if d.userAgg != nil {
				value = fmt.Sprintf("%d", d.userAgg.Total)
			} else if _, ok := d.errors[wkUserList]; ok {
				value = style.Error.Render("err")
			}
		case "devices":
			if d.deviceAgg != nil {
				value = fmt.Sprintf("%d", d.deviceAgg.Total)
			} else if _, ok := d.errors[wkDeviceList]; ok {
				value = style.Error.Render("err")
			}
		default:
			if !d.loading[c.key] {
				if _, ok := d.errors[c.key]; ok {
					value = style.Error.Render("err")
				} else {
					value = fmt.Sprintf("%d", d.counts[c.key])
				}
			}
		}

		card := component.StatCard{
			Label:    c.label,
			Value:    value,
			Color:    d.cardColor(c.key, c.color),
			Width:    cardWidth,
			Selected: d.focusIdx < 0 && d.gridCur == i,
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
	if IsWidgetEnabled("user-status") {
		if d.loading[wkUserList] {
			sb.WriteString(d.renderLoadingWidget("User Status", widgetWidth))
		} else if _, ok := d.errors[wkUserList]; ok {
			sb.WriteString(d.renderErrorWidget("User Status", d.errors[wkUserList], widgetWidth))
		} else if d.userAgg != nil {
			chart := component.BarChart{
				Title:   "User Status",
				Width:   widgetWidth,
				Focused: d.isZoneFocused("user-status"),
				Items: []component.BarItem{
					{Label: "Active", Value: d.userAgg.Active, Color: style.ColorSuccess},
					{Label: "Suspended", Value: d.userAgg.Suspended, Color: style.ColorWarning},
					{Label: "Locked", Value: d.userAgg.Locked, Color: style.ColorError},
				},
			}
			sb.WriteString(chart.View())
		}
		sb.WriteString("\n")
	}

	// MFA Adoption progress.
	if IsWidgetEnabled("mfa") {
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
				Focused: d.isZoneFocused("mfa"),
			}
			sb.WriteString(prog.View())
		}
		sb.WriteString("\n")
	}

	// Policy Compliance.
	if IsWidgetEnabled("policy-compliance") {
		if d.loading[wkPolicies] {
			sb.WriteString(d.renderLoadingWidget("Policy Compliance", widgetWidth))
		} else if _, ok := d.errors[wkPolicies]; ok {
			sb.WriteString(d.renderErrorWidget("Policy Compliance", d.errors[wkPolicies], widgetWidth))
		} else if d.policyCompliance != nil {
			chart := component.BarChart{
				Title:   "Policy Compliance",
				Width:   widgetWidth,
				Focused: d.isZoneFocused("policies"),
				Items: []component.BarItem{
					{Label: "Applied", Value: d.policyCompliance.Applied, Color: style.ColorSuccess},
					{Label: "Pending", Value: d.policyCompliance.Pending, Color: style.ColorWarning},
					{Label: "Failed", Value: d.policyCompliance.Failed, Color: style.ColorError},
				},
			}
			sb.WriteString(chart.View())
		}
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
	if IsWidgetEnabled("device-os") {
		if d.loading[wkDeviceList] {
			sb.WriteString(d.renderLoadingWidget("Device OS Distribution", widgetWidth))
		} else if _, ok := d.errors[wkDeviceList]; ok {
			sb.WriteString(d.renderErrorWidget("Device OS Distribution", d.errors[wkDeviceList], widgetWidth))
		} else if d.deviceAgg != nil {
			items := buildOSBarItems(d.deviceAgg.OSCounts)
			chart := component.BarChart{
				Title:   "Device OS Distribution",
				Width:   widgetWidth,
				Focused: d.isZoneFocused("device-os"),
				Items:   items,
			}
			sb.WriteString(chart.View())
		}
		sb.WriteString("\n")
	}

	// Device Connectivity.
	if IsWidgetEnabled("connectivity") {
		if d.loading[wkDeviceList] {
			sb.WriteString(d.renderLoadingWidget("Device Connectivity", widgetWidth))
		} else if d.deviceAgg != nil {
			chart := component.BarChart{
				Title:   "Device Connectivity",
				Width:   widgetWidth,
				Focused: d.isZoneFocused("connectivity"),
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
	}

	// Events by Service (24h) — bar chart breakdown + sparkline.
	if IsWidgetEnabled("events") || IsWidgetEnabled("sparkline") {
		sb.WriteString(d.renderEventsWidget(widgetWidth))
	}

	return sb.String()
}

// renderEventsWidget renders the events breakdown by service as a bar chart,
// plus a sparkline showing daily event counts over the past 7 days.
func (d *DashboardScreen) renderEventsWidget(widgetWidth int) string {
	var sb strings.Builder

	// Check if any service data is available.
	anyLoaded := false
	allLoading := true
	for _, svc := range eventServices {
		key := wkEventService(svc)
		if !d.loading[key] {
			allLoading = false
		}
		if _, ok := d.eventsByService[svc]; ok {
			anyLoaded = true
		}
	}

	if allLoading && !anyLoaded {
		sb.WriteString(d.renderLoadingWidget("Events by Service (24h)", widgetWidth))
	} else {
		serviceColors := map[string]lipgloss.Color{
			"sso":       style.ColorPrimary,
			"directory": style.ColorSecondary,
			"ldap":      style.ColorWarning,
			"radius":    style.ColorHighlight,
			"mdm":       style.ColorSuccess,
		}

		var items []component.BarItem
		for _, svc := range eventServices {
			count := d.eventsByService[svc]
			color := serviceColors[svc]
			if color == "" {
				color = style.ColorMuted
			}
			items = append(items, component.BarItem{
				Label: strings.ToUpper(svc),
				Value: count,
				Color: color,
			})
		}

		chart := component.BarChart{
			Title:   "Events by Service (24h)",
			Width:   widgetWidth,
			Focused: d.isZoneFocused("events"),
			Items:   items,
		}
		sb.WriteString(chart.View())
	}
	sb.WriteString("\n")

	// Sparkline: daily event counts (past 7 days).
	if d.sparklineReady >= sparklineDays {
		now := time.Now().UTC()
		var labels []string
		data := make([]int, sparklineDays)
		for i := 0; i < sparklineDays; i++ {
			data[i] = d.eventSparkline[i]
			day := now.AddDate(0, 0, -(sparklineDays - 1 - i))
			labels = append(labels, day.Format("Mon")[0:2])
		}
		spark := component.Sparkline{
			Title:  "Event Trend (7 days)",
			Data:   data,
			Labels: labels,
			Color:  style.ColorSecondary,
			Width:  widgetWidth,
		}
		sb.WriteString(spark.View())
	} else if d.sparklineReady > 0 {
		sb.WriteString(d.renderLoadingWidget("Event Trend (7 days)", widgetWidth))
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
// The now parameter enables deterministic testing of time-based connectivity bucketing.
func aggregateDevices(data []json.RawMessage, now time.Time) *deviceAggregation {
	agg := &deviceAggregation{
		OSCounts: make(map[string]int),
	}
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

// aggregatePolicyStatus accumulates policy status counts from a single policy's statuses.
func (d *DashboardScreen) aggregatePolicyStatus(data []json.RawMessage) {
	if d.policyCompliance == nil {
		d.policyCompliance = &policyComplianceAgg{}
	}
	for _, raw := range data {
		var s struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		switch strings.ToLower(s.Status) {
		case "applied", "success":
			d.policyCompliance.Applied++
		case "pending":
			d.policyCompliance.Pending++
		case "failed", "error":
			d.policyCompliance.Failed++
		}
	}
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
