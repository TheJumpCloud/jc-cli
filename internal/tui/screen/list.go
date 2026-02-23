package screen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/component"
	"github.com/klaassen-consulting/jc/internal/tui/fetch"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// clipboardWriteFunc is the function used to write to clipboard.
// Overridden in tests to avoid real clipboard access.
var clipboardWriteFunc = clipboard.WriteAll

// ListScreen displays a filterable, sortable list of resources.
type ListScreen struct {
	entry      tui.ResourceEntry
	table      component.Table
	filterBar  component.FilterBar
	spinner    spinner.Model
	fetcher    *fetch.Fetcher
	generation int64
	loading    bool
	err        string
	allFields  bool
	totalCount int
	width      int
	height     int
	exporting  bool
}

// NewListScreen creates a list screen for the given resource.
func NewListScreen(entry tui.ResourceEntry) *ListScreen {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = style.Spinner

	columns := entry.Schema.DefaultFields
	if len(columns) == 0 {
		columns = []string{"id"}
	}

	return &ListScreen{
		entry:     entry,
		table:     component.Table{Columns: columns},
		filterBar: component.NewFilterBar(),
		spinner:   s,
		fetcher:   fetch.NewFetcher(),
	}
}

// SetFetcher allows injecting a custom fetcher (for tests).
func (l *ListScreen) SetFetcher(f *fetch.Fetcher) {
	l.fetcher = f
}

func (l *ListScreen) Title() string { return l.entry.DisplayName }

// TextInputActive reports whether the list screen has active text input.
func (l *ListScreen) TextInputActive() bool {
	return l.filterBar.Focused()
}

func (l *ListScreen) Init() tea.Cmd {
	return tea.Batch(l.spinner.Tick, l.fetchData())
}

func (l *ListScreen) fetchData() tea.Cmd {
	l.loading = true
	l.err = ""
	l.generation = fetch.NextGeneration()
	gen := l.generation

	var filters []filter.Expression
	var search string
	if fb := l.filterBar.Filters(); len(fb) > 0 {
		filters = fb
	}
	search = l.filterBar.SearchTerm()

	// Use POST search endpoint for resources that support it (users, devices).
	if search != "" && l.entry.SearchEndpoint != "" && l.entry.ClientType == tui.ClientV1 {
		return l.fetcher.FetchV1Search(
			l.entry.Key, l.entry.SearchEndpoint, search,
			l.entry.SearchFields, l.sortString(), filters, gen,
		)
	}

	switch l.entry.ClientType {
	case tui.ClientV1:
		opts := api.ListOptions{
			Sort: l.sortString(),
		}
		if len(filters) > 0 {
			opts.Filter = filter.ToV1Queries(filters)
		}
		if search != "" {
			opts.Search = search
		}
		return l.fetcher.FetchV1List(l.entry.Key, l.entry.ListEndpoint, opts, gen)

	case tui.ClientV2:
		opts := api.V2ListOptions{
			Sort: l.sortString(),
		}
		if len(filters) > 0 {
			opts.Filter = filter.ToV2Queries(filters)
		}
		if search != "" {
			opts.Search = search
		}
		return l.fetcher.FetchV2List(l.entry.Key, l.entry.ListEndpoint, opts, gen)

	default:
		l.loading = false
		l.err = "This resource type is not supported for browsing in the TUI"
		return nil
	}
}

func (l *ListScreen) sortString() string {
	if l.table.SortField == "" {
		return ""
	}
	if l.table.SortDesc {
		return "-" + l.table.SortField
	}
	return l.table.SortField
}

func (l *ListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width = msg.Width
		l.height = msg.Height
		l.table.Width = msg.Width
		l.table.Height = msg.Height - 8 // Reserve for header, filter, statusbar
		l.filterBar.Width = msg.Width
		return l, nil

	case fetch.ListResultMsg:
		if msg.Generation != l.generation {
			return l, nil // Stale response.
		}
		l.loading = false
		if msg.Err != nil {
			l.err = msg.Err.Error()
			return l, nil
		}
		data := msg.Data
		if l.entry.FlattenFunc != nil {
			data = l.entry.FlattenFunc(data)
		}
		// Client-side sort for resources without server-side sort support.
		if !l.entry.Schema.SortSupport && l.table.SortField != "" {
			data = sortLocally(data, l.table.SortField, l.table.SortDesc)
		}
		l.table.Rows = data
		l.totalCount = msg.TotalCount
		l.table.Cursor = 0
		l.table.Offset = 0

		// Derive columns from data when schema has no default fields
		// (e.g. system insights tables with varying schemas).
		if len(l.entry.Schema.DefaultFields) == 0 && len(msg.Data) > 0 {
			if cols := component.ExtractColumnNames(msg.Data[0]); len(cols) > 0 {
				l.table.Columns = cols
			}
		}
		return l, nil

	case component.FilterChangedMsg:
		return l, l.fetchData()

	case tui.RefreshListMsg:
		l.fetcher.Cache.InvalidateResource(l.entry.Key)
		return l, l.fetchData()

	case spinner.TickMsg:
		if l.loading {
			var cmd tea.Cmd
			l.spinner, cmd = l.spinner.Update(msg)
			return l, cmd
		}
		return l, nil

	case tea.KeyMsg:
		// When filter bar has focus, delegate to it.
		if l.filterBar.Focused() {
			var cmd tea.Cmd
			l.filterBar, cmd = l.filterBar.Update(msg)
			return l, cmd
		}

		// Export mode: intercept format keys or cancel.
		if l.exporting {
			return l, l.handleExportKey(msg)
		}

		switch {
		case key.Matches(msg, tui.GlobalKeyMap.Back):
			// If filters active, clear them first.
			if len(l.filterBar.Filters()) > 0 || l.filterBar.SearchTerm() != "" {
				l.filterBar.ClearFilters()
				return l, l.fetchData()
			}
			return l, func() tea.Msg { return tui.PopScreenMsg{} }

		case key.Matches(msg, tui.NavKeyMap.Up):
			l.table.MoveCursor(-1)
		case key.Matches(msg, tui.NavKeyMap.Down):
			l.table.MoveCursor(1)
		case key.Matches(msg, tui.NavKeyMap.Top):
			l.table.GoToTop()
		case key.Matches(msg, tui.NavKeyMap.Bottom):
			l.table.GoToBottom()

		case key.Matches(msg, tui.NavKeyMap.Enter):
			return l, l.openDetail()

		case key.Matches(msg, tui.ListKeyMap.Filter):
			l.filterBar.Focus()
			return l, nil

		case key.Matches(msg, tui.ListKeyMap.Sort):
			l.cycleSort()
			if !l.entry.Schema.SortSupport {
				l.table.Rows = sortLocally(l.table.Rows, l.table.SortField, l.table.SortDesc)
				return l, nil
			}
			return l, l.fetchData()

		case key.Matches(msg, tui.ListKeyMap.SortDir):
			l.table.SortDesc = !l.table.SortDesc
			if !l.entry.Schema.SortSupport {
				l.table.Rows = sortLocally(l.table.Rows, l.table.SortField, l.table.SortDesc)
				return l, nil
			}
			return l, l.fetchData()

		case key.Matches(msg, tui.ListKeyMap.Refresh):
			l.fetcher.Cache.InvalidateResource(l.entry.Key)
			return l, l.fetchData()

		case key.Matches(msg, tui.ListKeyMap.AllFields):
			l.toggleAllFields()

		case key.Matches(msg, tui.ListKeyMap.Copy):
			return l, l.copySelectedID()

		case key.Matches(msg, tui.ListKeyMap.Export):
			if len(l.table.Rows) > 0 {
				l.exporting = true
			}

		case msg.String() == "n":
			if hasVerb(l.entry.Schema.Verbs, "create") {
				form := NewFormScreen(l.entry, "create", nil)
				form.SetFetcher(l.fetcher)
				return l, func() tea.Msg {
					return tui.PushScreenMsg{Screen: form}
				}
			}
		}
	}

	return l, nil
}

func (l *ListScreen) openDetail() tea.Cmd {
	row := l.table.SelectedRow()
	if row == nil {
		return nil
	}

	// Pivot navigation: redirect Enter to a different resource's detail screen.
	if l.entry.PivotField != "" && l.entry.PivotTargetKey != "" {
		pivotID := component.ExtractID(row, l.entry.PivotField)
		if pivotID == "" {
			return nil
		}
		targetEntry, ok := tui.RegistryByKey()[l.entry.PivotTargetKey]
		if !ok {
			return nil
		}
		return func() tea.Msg {
			return tui.PushScreenMsg{
				Screen: NewDetailScreen(targetEntry, pivotID, ""),
			}
		}
	}

	id := component.ExtractID(row, l.entry.Schema.IDField)
	if id == "" {
		return nil
	}

	name := component.ExtractName(row, l.entry.Schema.NameField)

	return func() tea.Msg {
		return tui.PushScreenMsg{
			Screen: NewDetailScreen(l.entry, id, name),
		}
	}
}

func (l *ListScreen) cycleSort() {
	sortFields := l.entry.Schema.SortFields
	if len(sortFields) == 0 {
		return
	}

	current := l.table.SortField
	idx := -1
	for i, f := range sortFields {
		if f == current {
			idx = i
			break
		}
	}

	next := (idx + 1) % len(sortFields)
	l.table.SortField = sortFields[next]
	l.table.SortDesc = false
}

// sortLocally sorts rows by a field value using string comparison.
func sortLocally(rows []json.RawMessage, field string, desc bool) []json.RawMessage {
	if len(rows) == 0 || field == "" {
		return rows
	}
	sorted := make([]json.RawMessage, len(rows))
	copy(sorted, rows)

	sort.SliceStable(sorted, func(i, j int) bool {
		vi := extractStringField(sorted[i], field)
		vj := extractStringField(sorted[j], field)
		if desc {
			return vi > vj
		}
		return vi < vj
	})
	return sorted
}

// extractStringField extracts a top-level field value as a lowercase string for sorting.
func extractStringField(raw json.RawMessage, field string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	v, ok := obj[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		// Non-string value — use raw representation.
		return strings.ToLower(strings.Trim(string(v), `"`))
	}
	return strings.ToLower(s)
}

func (l *ListScreen) copySelectedID() tea.Cmd {
	row := l.table.SelectedRow()
	if row == nil {
		return nil
	}

	// Use the schema ID field, falling back to pivot field for pivot-based screens.
	idField := l.entry.Schema.IDField
	if idField == "" {
		idField = l.entry.PivotField
	}
	if idField == "" {
		return nil
	}

	id := component.ExtractID(row, idField)
	if id == "" {
		return nil
	}

	_ = clipboardWriteFunc(id)
	return func() tea.Msg { return tui.FlashMsg{Text: "Copied: " + id} }
}

func (l *ListScreen) handleExportKey(msg tea.KeyMsg) tea.Cmd {
	l.exporting = false
	switch msg.String() {
	case "j":
		flash, err := exportListToClipboard(l.table.Rows, l.table.Columns)
		if err != nil {
			return func() tea.Msg { return tui.FlashMsg{Text: "Export error: " + err.Error()} }
		}
		return func() tea.Msg { return tui.FlashMsg{Text: flash} }
	case "c":
		flash, err := exportListToFile(l.table.Rows, l.table.Columns, output.FormatCSV, l.entry.Key, "csv")
		if err != nil {
			return func() tea.Msg { return tui.FlashMsg{Text: "Export error: " + err.Error()} }
		}
		return func() tea.Msg { return tui.FlashMsg{Text: flash} }
	case "J":
		flash, err := exportListToFile(l.table.Rows, l.table.Columns, output.FormatJSON, l.entry.Key, "json")
		if err != nil {
			return func() tea.Msg { return tui.FlashMsg{Text: "Export error: " + err.Error()} }
		}
		return func() tea.Msg { return tui.FlashMsg{Text: flash} }
	}
	return nil
}

func (l *ListScreen) toggleAllFields() {
	l.allFields = !l.allFields
	if l.allFields {
		// Show all fields from schema.
		cols := make([]string, len(l.entry.Schema.Fields))
		for i, f := range l.entry.Schema.Fields {
			cols[i] = f.Name
		}
		l.table.Columns = cols
	} else {
		l.table.Columns = l.entry.Schema.DefaultFields
	}
}

func (l *ListScreen) View() string {
	var sb strings.Builder

	header := style.Subtitle.Render(l.entry.DisplayName)
	if l.totalCount > 0 {
		count := component.FormatCount(len(l.table.Rows), l.totalCount)
		header += style.ResourceVerbs.Render(fmt.Sprintf(" (%s items)", count))
	}
	sb.WriteString(header)
	sb.WriteString("\n")

	// Filter bar.
	filterView := l.filterBar.View()
	if filterView != "" {
		sb.WriteString(filterView)
		sb.WriteString("\n")
	}

	if l.loading {
		sb.WriteString(l.spinner.View())
		sb.WriteString(" Loading...")
		sb.WriteString("\n")
		return sb.String()
	}

	if l.err != "" {
		sb.WriteString(style.Error.Render("Error: " + l.err))
		sb.WriteString("\n")
		return sb.String()
	}

	if len(l.table.Rows) == 0 {
		sb.WriteString(style.DimRow.Render("  No results"))
		sb.WriteString("\n")
		return sb.String()
	}

	sb.WriteString(l.table.View())
	sb.WriteString("\n")

	if l.exporting {
		sb.WriteString(style.Help.Render("Export: [j]son clipboard  [c]sv file  [J]son file  [esc] cancel"))
		sb.WriteString("\n")
	}

	return sb.String()
}
