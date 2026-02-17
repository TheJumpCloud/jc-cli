package screen

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/component"
	"github.com/klaassen-consulting/jc/internal/tui/fetch"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// Time range presets for the insights form.
var insightsTimeRangePresets = []string{"1h", "6h", "24h", "7d", "30d"}

// InsightsFormScreen provides a form for querying Directory Insights events.
type InsightsFormScreen struct {
	entry        tui.ResourceEntry
	serviceIdx   int
	timeRangeIdx int
	eventType    textinput.Model
	focusedField int // 0=service, 1=timeRange, 2=eventType
	fetcher      *fetch.Fetcher
	table        component.Table
	spinner      spinner.Model
	generation   int64
	loading      bool
	err          string
	submitted    bool
	width        int
	height       int
}

// NewInsightsFormScreen creates an insights query form screen.
func NewInsightsFormScreen(entry tui.ResourceEntry) *InsightsFormScreen {
	ti := textinput.New()
	ti.Placeholder = "e.g. user_login (optional)"
	ti.CharLimit = 128

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = style.Spinner

	return &InsightsFormScreen{
		entry:        entry,
		serviceIdx:   0, // "all"
		timeRangeIdx: 2, // "24h"
		eventType:    ti,
		spinner:      s,
		fetcher:      fetch.NewFetcher(),
		table:        component.Table{Columns: insightsDefaultFields},
	}
}

// insightsDefaultFields are the default columns for insights results.
var insightsDefaultFields = []string{"timestamp", "event_type", "initiated_by", "client_ip", "success"}

// SetFetcher allows injecting a custom fetcher (for tests).
// TextInputActive reports whether the insights form has active text input.
func (f *InsightsFormScreen) TextInputActive() bool {
	return f.focusedField == 2 // eventType text input
}

func (f *InsightsFormScreen) SetFetcher(ft *fetch.Fetcher) {
	f.fetcher = ft
}

func (f *InsightsFormScreen) Title() string { return "Directory Insights" }

func (f *InsightsFormScreen) Init() tea.Cmd { return nil }

func (f *InsightsFormScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.width = msg.Width
		f.height = msg.Height
		f.table.Width = msg.Width
		f.table.Height = msg.Height - 12
		return f, nil

	case fetch.InsightsResultMsg:
		if msg.Generation != f.generation {
			return f, nil
		}
		f.loading = false
		if msg.Err != nil {
			f.err = msg.Err.Error()
			return f, nil
		}
		f.table.Rows = msg.Data
		f.table.Cursor = 0
		f.table.Offset = 0
		return f, nil

	case spinner.TickMsg:
		if f.loading {
			var cmd tea.Cmd
			f.spinner, cmd = f.spinner.Update(msg)
			return f, cmd
		}
		return f, nil

	case tea.KeyMsg:
		// In results mode.
		if f.submitted && !f.loading {
			switch {
			case key.Matches(msg, tui.GlobalKeyMap.Back):
				f.submitted = false
				f.err = ""
				return f, nil
			case key.Matches(msg, tui.ListKeyMap.Refresh):
				return f, f.submitQuery()
			case key.Matches(msg, tui.NavKeyMap.Enter):
				row := f.table.SelectedRow()
				if row != nil {
					return f, pushEventDetail(row)
				}
			case key.Matches(msg, tui.NavKeyMap.Up):
				f.table.MoveCursor(-1)
			case key.Matches(msg, tui.NavKeyMap.Down):
				f.table.MoveCursor(1)
			case key.Matches(msg, tui.NavKeyMap.Top):
				f.table.GoToTop()
			case key.Matches(msg, tui.NavKeyMap.Bottom):
				f.table.GoToBottom()
			}
			return f, nil
		}

		// In form mode.
		if f.focusedField == 2 { // eventType text input has focus
			switch msg.String() {
			case "esc":
				f.eventType.Blur()
				f.focusedField = 0
				return f, nil
			case "enter":
				f.eventType.Blur()
				return f, f.submitQuery()
			case "tab", "up":
				f.eventType.Blur()
				f.focusedField = 1
				return f, nil
			case "shift+tab", "down":
				f.eventType.Blur()
				f.focusedField = 0
				return f, nil
			default:
				var cmd tea.Cmd
				f.eventType, cmd = f.eventType.Update(msg)
				return f, cmd
			}
		}

		switch {
		case key.Matches(msg, tui.GlobalKeyMap.Back):
			return f, func() tea.Msg { return tui.PopScreenMsg{} }

		case key.Matches(msg, tui.NavKeyMap.Enter):
			return f, f.submitQuery()

		case msg.String() == "left", msg.String() == "h":
			f.cycleField(-1)

		case msg.String() == "right", msg.String() == "l":
			f.cycleField(1)

		case key.Matches(msg, tui.NavKeyMap.Up), msg.String() == "k":
			f.moveFocus(-1)

		case key.Matches(msg, tui.NavKeyMap.Down), msg.String() == "j":
			f.moveFocus(1)

		case msg.String() == "tab":
			f.moveFocus(1)

		case msg.String() == "shift+tab":
			f.moveFocus(-1)
		}
	}

	return f, nil
}

// pushEventDetail returns a cmd that pushes an EventDetailScreen for the given row.
func pushEventDetail(row json.RawMessage) tea.Cmd {
	return func() tea.Msg {
		return tui.PushScreenMsg{Screen: NewEventDetailScreen(row)}
	}
}

func (f *InsightsFormScreen) moveFocus(delta int) {
	f.focusedField += delta
	if f.focusedField < 0 {
		f.focusedField = 2
	}
	if f.focusedField > 2 {
		f.focusedField = 0
	}
	if f.focusedField == 2 {
		f.eventType.Focus()
	} else {
		f.eventType.Blur()
	}
}

func (f *InsightsFormScreen) cycleField(delta int) {
	switch f.focusedField {
	case 0: // service
		f.serviceIdx += delta
		if f.serviceIdx < 0 {
			f.serviceIdx = len(api.ValidInsightsServices) - 1
		}
		if f.serviceIdx >= len(api.ValidInsightsServices) {
			f.serviceIdx = 0
		}
	case 1: // time range
		f.timeRangeIdx += delta
		if f.timeRangeIdx < 0 {
			f.timeRangeIdx = len(insightsTimeRangePresets) - 1
		}
		if f.timeRangeIdx >= len(insightsTimeRangePresets) {
			f.timeRangeIdx = 0
		}
	}
}

func (f *InsightsFormScreen) submitQuery() tea.Cmd {
	service := api.ValidInsightsServices[f.serviceIdx]
	timeRange := insightsTimeRangePresets[f.timeRangeIdx]

	startTime, err := api.ParseTimeRange(timeRange)
	if err != nil {
		f.err = fmt.Sprintf("invalid time range: %v", err)
		return nil
	}

	query := api.InsightsQuery{
		Service:   service,
		StartTime: startTime.UTC().Format("2006-01-02T15:04:05Z"),
		Sort:      "-timestamp",
	}

	eventTypeVal := strings.TrimSpace(f.eventType.Value())
	if eventTypeVal != "" {
		query.SearchTermFilter = map[string]any{
			"event_type": eventTypeVal,
		}
	}

	opts := api.InsightsQueryOptions{
		Limit: 100, // Cap results for TUI browsing.
		Sort:  "-timestamp",
	}

	f.submitted = true
	f.loading = true
	f.err = ""
	f.generation = fetch.NextGeneration()
	gen := f.generation

	return tea.Batch(
		f.spinner.Tick,
		f.fetcher.FetchInsightsList(f.entry.Key, query, opts, gen),
	)
}

func (f *InsightsFormScreen) View() string {
	var sb strings.Builder

	sb.WriteString(style.Subtitle.Render("Directory Insights"))
	sb.WriteString("\n\n")

	if f.submitted {
		return f.viewResults(&sb)
	}

	return f.viewForm(&sb)
}

func (f *InsightsFormScreen) viewForm(sb *strings.Builder) string {
	service := api.ValidInsightsServices[f.serviceIdx]
	timeRange := insightsTimeRangePresets[f.timeRangeIdx]

	// Service field.
	serviceLabel := "  Service"
	serviceStyle := style.ResourceName
	if f.focusedField == 0 {
		serviceLabel = "> Service"
		serviceStyle = style.SelectedRow
	}
	sb.WriteString(serviceStyle.Render(serviceLabel))
	sb.WriteString("      ")
	sb.WriteString(style.FilterChip.Render("< " + service + " >"))
	sb.WriteString("\n\n")

	// Time range field.
	timeLabel := "  Time Range"
	timeStyle := style.ResourceName
	if f.focusedField == 1 {
		timeLabel = "> Time Range"
		timeStyle = style.SelectedRow
	}
	sb.WriteString(timeStyle.Render(timeLabel))
	sb.WriteString("  ")
	sb.WriteString(style.FilterChip.Render("< " + timeRange + " >"))
	sb.WriteString("\n\n")

	// Event type field.
	eventLabel := "  Event Type"
	eventStyle := style.ResourceName
	if f.focusedField == 2 {
		eventLabel = "> Event Type"
		eventStyle = style.SelectedRow
	}
	sb.WriteString(eventStyle.Render(eventLabel))
	sb.WriteString("  ")
	f.eventType.Width = f.width/2 - 20
	if f.eventType.Width < 20 {
		f.eventType.Width = 20
	}
	sb.WriteString(f.eventType.View())
	sb.WriteString("\n\n")

	sb.WriteString(style.Help.Render("h/l: change value  j/k: move field  enter: query  esc: back"))
	sb.WriteString("\n")

	if f.err != "" {
		sb.WriteString(style.Error.Render("Error: " + f.err))
		sb.WriteString("\n")
	}

	return sb.String()
}

func (f *InsightsFormScreen) viewResults(sb *strings.Builder) string {
	service := api.ValidInsightsServices[f.serviceIdx]
	timeRange := insightsTimeRangePresets[f.timeRangeIdx]

	sb.WriteString(style.Help.Render(fmt.Sprintf("Service: %s  Time: %s", service, timeRange)))
	sb.WriteString("\n")

	if f.loading {
		sb.WriteString(f.spinner.View())
		sb.WriteString(" Querying events...")
		sb.WriteString("\n")
		return sb.String()
	}

	if f.err != "" {
		sb.WriteString(style.Error.Render("Error: " + f.err))
		sb.WriteString("\n")
		sb.WriteString(style.Help.Render("esc: back to form  r: retry"))
		sb.WriteString("\n")
		return sb.String()
	}

	if len(f.table.Rows) == 0 {
		sb.WriteString(style.DimRow.Render("  No events found"))
		sb.WriteString("\n")
		sb.WriteString(style.Help.Render("esc: back to form"))
		sb.WriteString("\n")
		return sb.String()
	}

	sb.WriteString(style.ResourceVerbs.Render(fmt.Sprintf("(%d events)", len(f.table.Rows))))
	sb.WriteString("\n")
	sb.WriteString(f.table.View())
	sb.WriteString("\n")
	sb.WriteString(style.Help.Render("esc: back to form  r: re-run query  j/k: scroll  enter: detail"))
	sb.WriteString("\n")

	return sb.String()
}
