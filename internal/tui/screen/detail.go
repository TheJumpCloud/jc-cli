package screen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/component"
	"github.com/klaassen-consulting/jc/internal/tui/fetch"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// DetailScreen shows all fields of a single resource.
type DetailScreen struct {
	entry      tui.ResourceEntry
	id         string
	name       string
	data       json.RawMessage
	viewport   viewport.Model
	spinner    spinner.Model
	fetcher    *fetch.Fetcher
	generation int64
	loading    bool
	allFields  bool
	err        string
	width      int
	height     int
	ready      bool

	// Associations tab state.
	activeTab      int // 0=fields, 1=associations
	assocTargets   []string
	assocTargetIdx int
	assocData      map[string][]json.RawMessage
	assocLoading   bool
	assocErr       string
	assocTable     component.Table
	assocGen       int64
	assocNames     map[string]string // id → resolved name (shared across target types)
}

// NewDetailScreen creates a detail screen for a specific resource.
func NewDetailScreen(entry tui.ResourceEntry, id, name string) *DetailScreen {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = style.Spinner

	var targets []string
	if entry.GraphSourceType != "" {
		targets = tui.ValidAssocTargets[entry.GraphSourceType]
	}

	return &DetailScreen{
		entry:        entry,
		id:           id,
		name:         name,
		spinner:      s,
		fetcher:      fetch.NewFetcher(),
		assocTargets: targets,
		assocData:    make(map[string][]json.RawMessage),
		assocTable:   component.Table{Columns: []string{"type", "id"}},
		assocNames:   make(map[string]string),
	}
}

// SetFetcher allows injecting a custom fetcher (for tests).
func (d *DetailScreen) SetFetcher(f *fetch.Fetcher) {
	d.fetcher = f
}

func (d *DetailScreen) Title() string {
	if d.name != "" {
		return d.name
	}
	return d.id
}

func (d *DetailScreen) Init() tea.Cmd {
	return tea.Batch(d.spinner.Tick, d.fetchDetail())
}

func (d *DetailScreen) fetchDetail() tea.Cmd {
	d.loading = true
	d.err = ""
	d.generation = fetch.NextGeneration()
	gen := d.generation

	switch d.entry.ClientType {
	case tui.ClientV1:
		return d.fetcher.FetchV1Detail(d.entry.Key, d.entry.ListEndpoint, d.id, gen)
	case tui.ClientV2:
		return d.fetcher.FetchV2Detail(d.entry.Key, d.entry.ListEndpoint, d.id, gen)
	default:
		return nil
	}
}

func (d *DetailScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		headerHeight := 3
		if !d.ready {
			d.viewport = viewport.New(msg.Width, msg.Height-headerHeight-2)
			d.ready = true
		} else {
			d.viewport.Width = msg.Width
			d.viewport.Height = msg.Height - headerHeight - 2
		}
		if d.data != nil {
			d.viewport.SetContent(d.renderFields())
		}
		return d, nil

	case fetch.DetailResultMsg:
		if msg.Generation != d.generation {
			return d, nil
		}
		d.loading = false
		if msg.Err != nil {
			d.err = msg.Err.Error()
			return d, nil
		}
		d.data = msg.Data
		// Extract name from data if we didn't have it.
		if d.name == "" && d.entry.Schema.NameField != "" {
			d.name = component.ExtractName(msg.Data, d.entry.Schema.NameField)
		}
		if d.ready {
			d.viewport.SetContent(d.renderFields())
		}
		return d, nil

	case spinner.TickMsg:
		if d.loading {
			var cmd tea.Cmd
			d.spinner, cmd = d.spinner.Update(msg)
			return d, cmd
		}
		return d, nil

	case fetch.AssociationsResultMsg:
		if msg.Generation != d.assocGen {
			return d, nil
		}
		d.assocLoading = false
		if msg.Err != nil {
			d.assocErr = msg.Err.Error()
			return d, nil
		}
		d.assocData[msg.TargetType] = msg.Data
		d.assocTable.Rows = msg.Data
		d.assocTable.Cursor = 0
		d.assocTable.Offset = 0
		d.enrichAssocRows()
		return d, d.resolveAssocNames()

	case fetch.AssocNamesResolvedMsg:
		if msg.Generation != d.assocGen {
			return d, nil
		}
		for id, name := range msg.Names {
			d.assocNames[id] = name
		}
		d.enrichAssocRows()
		return d, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, tui.GlobalKeyMap.Back):
			return d, func() tea.Msg { return tui.PopScreenMsg{} }

		case key.Matches(msg, tui.DetailKeyMap.Copy):
			_ = clipboardWriteFunc(d.id)
			return d, func() tea.Msg { return tui.FlashMsg{Text: "Copied: " + d.id} }

		case key.Matches(msg, tui.DetailKeyMap.Tab):
			if len(d.assocTargets) > 0 {
				d.activeTab = 1 - d.activeTab
				if d.activeTab == 1 {
					return d, d.fetchAssocIfNeeded()
				}
			}

		case key.Matches(msg, tui.DetailKeyMap.AllFields):
			d.allFields = !d.allFields
			if d.data != nil && d.ready {
				d.viewport.SetContent(d.renderFields())
			}

		case key.Matches(msg, tui.DetailKeyMap.Refresh):
			if d.activeTab == 1 {
				return d, d.fetchAssoc()
			}
			return d, d.fetchDetail()

		default:
			if d.activeTab == 1 {
				return d, d.handleAssocKeys(msg)
			}
			var cmd tea.Cmd
			d.viewport, cmd = d.viewport.Update(msg)
			return d, cmd
		}
	}

	return d, nil
}

func (d *DetailScreen) renderFields() string {
	if d.data == nil {
		return ""
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(d.data, &obj); err != nil {
		return style.Error.Render("Failed to parse resource data")
	}

	// Determine which fields to show.
	var fieldNames []string
	if d.allFields {
		for k := range obj {
			fieldNames = append(fieldNames, k)
		}
		sort.Strings(fieldNames)
	} else {
		// Show default fields first, then remaining fields.
		seen := make(map[string]bool)
		for _, f := range d.entry.Schema.DefaultFields {
			if _, ok := obj[f]; ok {
				fieldNames = append(fieldNames, f)
				seen[f] = true
			}
		}
		// Add remaining fields.
		var remaining []string
		for k := range obj {
			if !seen[k] {
				remaining = append(remaining, k)
			}
		}
		sort.Strings(remaining)
		fieldNames = append(fieldNames, remaining...)
	}

	var sb strings.Builder
	maxKeyLen := 0
	for _, k := range fieldNames {
		if len(k) > maxKeyLen {
			maxKeyLen = len(k)
		}
	}

	for _, k := range fieldNames {
		v := obj[k]
		keyStr := style.FieldKey.Render(fmt.Sprintf("%-*s", maxKeyLen, k))
		val := formatDetailValue(v)
		sb.WriteString(keyStr + "  " + style.FieldValue.Render(val) + "\n")
	}

	return sb.String()
}

// formatDetailValue formats a JSON value for the detail view.
func formatDetailValue(v json.RawMessage) string {
	if len(v) == 0 || string(v) == "null" {
		return "-"
	}

	// Try string.
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return s
	}

	// Try bool.
	var b bool
	if err := json.Unmarshal(v, &b); err == nil {
		if b {
			return "true"
		}
		return "false"
	}

	// Try number.
	var n json.Number
	if err := json.Unmarshal(v, &n); err == nil {
		return n.String()
	}

	// Complex value — pretty print.
	var pretty json.RawMessage
	if err := json.Unmarshal(v, &pretty); err == nil {
		formatted, err := json.MarshalIndent(pretty, "  ", "  ")
		if err == nil {
			return string(formatted)
		}
	}

	return string(v)
}

func (d *DetailScreen) handleAssocKeys(msg tea.KeyMsg) tea.Cmd {
	switch {
	case msg.String() == "h", msg.String() == "left":
		return d.cycleTarget(-1)
	case msg.String() == "l", msg.String() == "right":
		return d.cycleTarget(1)
	case key.Matches(msg, tui.NavKeyMap.Up):
		d.assocTable.MoveCursor(-1)
	case key.Matches(msg, tui.NavKeyMap.Down):
		d.assocTable.MoveCursor(1)
	case key.Matches(msg, tui.NavKeyMap.Top):
		d.assocTable.GoToTop()
	case key.Matches(msg, tui.NavKeyMap.Bottom):
		d.assocTable.GoToBottom()
	case key.Matches(msg, tui.NavKeyMap.Enter):
		return d.openAssocDetail()
	}
	return nil
}

// openAssocDetail navigates from an association row to that resource's detail screen.
func (d *DetailScreen) openAssocDetail() tea.Cmd {
	row := d.assocTable.SelectedRow()
	if row == nil {
		return nil
	}

	assocID := component.ExtractID(row, "id")
	if assocID == "" {
		return nil
	}

	assocType := component.ExtractName(row, "type")
	registryKey := tui.RegistryKeyForGraphType(assocType)
	if registryKey == "" {
		return nil
	}

	targetEntry, ok := tui.RegistryByKey()[registryKey]
	if !ok {
		return nil
	}

	return func() tea.Msg {
		return tui.PushScreenMsg{
			Screen: NewDetailScreen(targetEntry, assocID, ""),
		}
	}
}

// resolveAssocNames builds name-resolution requests for association rows with unknown names.
func (d *DetailScreen) resolveAssocNames() tea.Cmd {
	rows := d.assocTable.Rows
	if len(rows) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var reqs []fetch.AssocNameReq
	registry := tui.RegistryByKey()

	for _, row := range rows {
		id := component.ExtractID(row, "id")
		typ := component.ExtractName(row, "type")
		if id == "" || typ == "" || seen[id] || d.assocNames[id] != "" {
			continue
		}
		seen[id] = true

		regKey := tui.RegistryKeyForGraphType(typ)
		entry, ok := registry[regKey]
		if !ok || entry.Schema.NameField == "" {
			continue
		}

		reqs = append(reqs, fetch.AssocNameReq{
			ID:        id,
			V1:        entry.ClientType == tui.ClientV1,
			Endpoint:  entry.ListEndpoint,
			NameField: entry.Schema.NameField,
		})
	}

	if len(reqs) == 0 {
		return nil
	}
	return d.fetcher.ResolveAssocNames(reqs, d.assocGen)
}

// enrichAssocRows injects resolved names into the current association table rows.
func (d *DetailScreen) enrichAssocRows() {
	hasName := false
	enriched := make([]json.RawMessage, len(d.assocTable.Rows))
	for i, row := range d.assocTable.Rows {
		id := component.ExtractID(row, "id")
		name, ok := d.assocNames[id]
		if !ok || name == "" {
			enriched[i] = row
			continue
		}
		hasName = true
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(row, &obj); err != nil {
			enriched[i] = row
			continue
		}
		nameBytes, _ := json.Marshal(name)
		obj["name"] = nameBytes
		enriched[i], _ = json.Marshal(obj)
	}
	d.assocTable.Rows = enriched
	if hasName {
		d.assocTable.Columns = []string{"type", "name", "id"}
	} else {
		d.assocTable.Columns = []string{"type", "id"}
	}
}

func (d *DetailScreen) cycleTarget(delta int) tea.Cmd {
	if len(d.assocTargets) == 0 {
		return nil
	}
	d.assocTargetIdx += delta
	if d.assocTargetIdx < 0 {
		d.assocTargetIdx = len(d.assocTargets) - 1
	}
	if d.assocTargetIdx >= len(d.assocTargets) {
		d.assocTargetIdx = 0
	}
	return d.fetchAssocIfNeeded()
}

func (d *DetailScreen) fetchAssocIfNeeded() tea.Cmd {
	if len(d.assocTargets) == 0 {
		return nil
	}
	target := d.assocTargets[d.assocTargetIdx]
	if data, ok := d.assocData[target]; ok {
		d.assocTable.Rows = data
		d.assocTable.Cursor = 0
		d.assocTable.Offset = 0
		d.assocErr = ""
		d.enrichAssocRows()
		return nil
	}
	return d.fetchAssoc()
}

func (d *DetailScreen) fetchAssoc() tea.Cmd {
	if len(d.assocTargets) == 0 || d.entry.GraphSourceType == "" {
		return nil
	}
	target := d.assocTargets[d.assocTargetIdx]
	graphEP := tui.GraphEndpoint(d.entry.GraphSourceType)
	if graphEP == "" {
		return nil
	}
	d.assocLoading = true
	d.assocErr = ""
	d.assocGen = fetch.NextGeneration()
	gen := d.assocGen

	// Group members use dedicated membership endpoints, not graph associations.
	memberTarget := tui.MembershipTarget(d.entry.GraphSourceType)
	if memberTarget != "" && target == memberTarget {
		memberEP := tui.MembershipEndpoint(d.entry.GraphSourceType)
		return tea.Batch(
			d.spinner.Tick,
			d.fetcher.FetchMembership(d.entry.Key, memberEP, d.id, memberTarget, gen),
		)
	}

	return tea.Batch(
		d.spinner.Tick,
		d.fetcher.FetchAssociations(d.entry.Key, graphEP, d.id, target, gen),
	)
}

func (d *DetailScreen) View() string {
	var sb strings.Builder

	title := d.entry.DisplayName
	if d.name != "" {
		title += " / " + d.name
	} else {
		title += " / " + d.id
	}
	sb.WriteString(style.Subtitle.Render(title))
	sb.WriteString("\n\n")

	if d.loading {
		sb.WriteString(d.spinner.View())
		sb.WriteString(" Loading...")
		sb.WriteString("\n")
		return sb.String()
	}

	if d.err != "" {
		sb.WriteString(style.Error.Render("Error: " + d.err))
		sb.WriteString("\n")
		return sb.String()
	}

	if d.data == nil {
		sb.WriteString(style.DimRow.Render("  No data"))
		sb.WriteString("\n")
		return sb.String()
	}

	// Show tab header if associations are available.
	if len(d.assocTargets) > 0 {
		fieldsTab := " Fields "
		assocTab := " Associations "
		if d.activeTab == 0 {
			fieldsTab = style.BreadcrumbActive.Render("[Fields]")
			assocTab = style.Breadcrumb.Render(" Associations ")
		} else {
			fieldsTab = style.Breadcrumb.Render(" Fields ")
			assocTab = style.BreadcrumbActive.Render("[Associations]")
		}
		sb.WriteString(fieldsTab + "  " + assocTab)
		sb.WriteString("\n\n")
	}

	if d.activeTab == 0 {
		if d.ready {
			sb.WriteString(d.viewport.View())
		} else {
			sb.WriteString(d.renderFields())
		}
	} else {
		sb.WriteString(d.renderAssociations())
	}

	return sb.String()
}

func (d *DetailScreen) renderAssociations() string {
	var sb strings.Builder

	// Target type selector.
	for i, target := range d.assocTargets {
		label := target
		if i == d.assocTargetIdx {
			label = style.FilterChip.Render(label)
		} else {
			label = style.DimRow.Render(label)
		}
		sb.WriteString(label + "  ")
	}
	sb.WriteString("\n")
	sb.WriteString(style.Help.Render("h/l: change target type"))
	sb.WriteString("\n\n")

	if d.assocLoading {
		sb.WriteString(d.spinner.View())
		sb.WriteString(" Loading associations...")
		sb.WriteString("\n")
		return sb.String()
	}

	if d.assocErr != "" {
		sb.WriteString(style.Error.Render("Error: " + d.assocErr))
		sb.WriteString("\n")
		return sb.String()
	}

	if len(d.assocTable.Rows) == 0 {
		sb.WriteString(style.DimRow.Render("  No associations"))
		sb.WriteString("\n")
		return sb.String()
	}

	d.assocTable.Width = d.width
	d.assocTable.Height = d.height - 14
	sb.WriteString(d.assocTable.View())
	sb.WriteString("\n")

	return sb.String()
}
