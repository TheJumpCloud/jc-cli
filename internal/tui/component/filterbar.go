package component

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// FilterChangedMsg is emitted when filters change.
type FilterChangedMsg struct {
	Filters    []filter.Expression
	SearchTerm string
}

// FilterBar provides a text input for filter expressions and search.
type FilterBar struct {
	input   textinput.Model
	filters []filter.Expression
	search  string
	focused bool
	err     string
	Width   int
}

// NewFilterBar creates a new filter bar component.
func NewFilterBar() FilterBar {
	ti := textinput.New()
	ti.Placeholder = "Filter (field:value or field=value) or search (~term)..."
	ti.CharLimit = 256
	return FilterBar{input: ti}
}

// Focused returns whether the filter bar has focus.
func (f *FilterBar) Focused() bool {
	return f.focused
}

// Focus gives the filter bar input focus.
func (f *FilterBar) Focus() {
	f.focused = true
	f.input.Focus()
}

// Blur removes focus from the filter bar.
func (f *FilterBar) Blur() {
	f.focused = false
	f.input.Blur()
}

// Filters returns the current active filters.
func (f *FilterBar) Filters() []filter.Expression {
	return f.filters
}

// SearchTerm returns the current search term.
func (f *FilterBar) SearchTerm() string {
	return f.search
}

// ClearFilters removes all active filters and search.
func (f *FilterBar) ClearFilters() {
	f.filters = nil
	f.search = ""
	f.err = ""
	f.input.SetValue("")
}

// Update handles input events.
func (f *FilterBar) Update(msg tea.Msg) (FilterBar, tea.Cmd) {
	if !f.focused {
		return *f, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			return f.applyInput()
		case "esc":
			f.Blur()
			return *f, nil
		}
	}

	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	f.err = ""
	return *f, cmd
}

// applyInput parses and applies the current input value.
func (f *FilterBar) applyInput() (FilterBar, tea.Cmd) {
	val := strings.TrimSpace(f.input.Value())
	if val == "" {
		return *f, nil
	}

	// Search prefix.
	if strings.HasPrefix(val, "~") {
		f.search = strings.TrimSpace(val[1:])
		f.input.SetValue("")
		f.err = ""
		return *f, func() tea.Msg {
			return FilterChangedMsg{Filters: f.filters, SearchTerm: f.search}
		}
	}

	// Parse as filter expression.
	expr, err := filter.Parse(val)
	if err != nil {
		f.err = err.Error()
		return *f, nil
	}

	f.filters = append(f.filters, expr)
	f.input.SetValue("")
	f.err = ""

	filters := make([]filter.Expression, len(f.filters))
	copy(filters, f.filters)
	return *f, func() tea.Msg {
		return FilterChangedMsg{Filters: filters, SearchTerm: f.search}
	}
}

// View renders the filter bar.
func (f *FilterBar) View() string {
	var sb strings.Builder

	// Active filter chips.
	if len(f.filters) > 0 || f.search != "" {
		var chips []string
		for _, expr := range f.filters {
			chip := style.FilterChip.Render(expr.Field + ":" + expr.Operator + ":" + expr.Value)
			chips = append(chips, chip)
		}
		if f.search != "" {
			chip := style.FilterChip.Render("~" + f.search)
			chips = append(chips, chip)
		}
		sb.WriteString(strings.Join(chips, " "))
		sb.WriteString("\n")
	}

	if f.focused {
		f.input.Width = f.Width - 4
		sb.WriteString(style.FilterInput.Render(f.input.View()))
		if f.err != "" {
			sb.WriteString("\n")
			sb.WriteString(style.Error.Render(f.err))
		}
	}

	return sb.String()
}
