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
}

// NewDetailScreen creates a detail screen for a specific resource.
func NewDetailScreen(entry tui.ResourceEntry, id, name string) *DetailScreen {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = style.Spinner

	return &DetailScreen{
		entry:   entry,
		id:      id,
		name:    name,
		spinner: s,
		fetcher: fetch.NewFetcher(),
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

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, tui.GlobalKeyMap.Back):
			return d, func() tea.Msg { return tui.PopScreenMsg{} }

		case key.Matches(msg, tui.DetailKeyMap.AllFields):
			d.allFields = !d.allFields
			if d.data != nil && d.ready {
				d.viewport.SetContent(d.renderFields())
			}

		case key.Matches(msg, tui.DetailKeyMap.Refresh):
			return d, d.fetchDetail()

		default:
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

	if d.ready {
		sb.WriteString(d.viewport.View())
	} else {
		sb.WriteString(d.renderFields())
	}

	return sb.String()
}
