package screen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// WindowsMDMPoliciesListScreen lists existing JumpCloud Windows
// custom policies (Custom MDM (OMA-URI) + Custom Registry Keys) —
// the edit counterpart to the CSP browse/create flow (KLA-464),
// mirroring apple_mdm_policies_list.go. Drilling in fetches +
// decodes the policy and opens the matching form pre-populated for
// a PUT.
type WindowsMDMPoliciesListScreen struct {
	all       []windowsPolicyRow
	filtered  []windowsPolicyRow
	cursor    int
	filter    textinput.Model
	filtering bool
	width     int
	height    int

	loading bool
	err     string

	spinner spinner.Model

	drilling      bool
	drillingError string
}

// windowsPolicyRow is one list entry — kept tiny; the values aren't
// decoded until the operator drills in.
type windowsPolicyRow struct {
	ID       string
	Name     string
	Template string
}

// loadWindowsPoliciesMsg carries the filtered policy list back from
// the fetch goroutine.
type loadWindowsPoliciesMsg struct {
	policies []windowsPolicyRow
	err      error
}

// decodeWindowsPolicyMsg carries the drill-in result: the decoded
// policy plus the (possibly nil) catalog used to rehydrate OMA-URI
// rows with enum/range metadata.
type decodeWindowsPolicyMsg struct {
	decoded windows_mdm.DecodedPolicy
	catalog *windows_mdm.Catalog
	err     error
}

// windowsEditCatalogLoader fetches the catalog for edit-row
// rehydration. Overridable for tests. A nil catalog is a soft
// failure — the form falls back to plain text rows rather than
// blocking the edit.
var windowsEditCatalogLoader = func() *windows_mdm.Catalog {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cat, err := windows_mdm.DefaultCatalog(ctx, nil)
	if err != nil {
		return nil
	}
	return cat
}

// NewWindowsMDMPoliciesListScreen builds the screen; the fetch fires
// from Init.
func NewWindowsMDMPoliciesListScreen() *WindowsMDMPoliciesListScreen {
	ti := textinput.New()
	ti.Placeholder = "Type to filter by name or template..."
	ti.CharLimit = 64

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner

	return &WindowsMDMPoliciesListScreen{
		filter:  ti,
		spinner: sp,
		loading: true,
	}
}

func (s *WindowsMDMPoliciesListScreen) Title() string {
	return "Windows MDM custom policies"
}

func (s *WindowsMDMPoliciesListScreen) TextInputActive() bool { return s.filtering }

func (s *WindowsMDMPoliciesListScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.loadCmd())
}

// loadCmd lists /policies and keeps the two Windows custom template
// families. Client-side filtering, same rationale as the Apple list:
// the v2 API has no template-name prefix filter and custom-policy
// counts are dozens, not thousands.
func (s *WindowsMDMPoliciesListScreen) loadCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := newV2ClientForWindowsMDM()
		if err != nil {
			return loadWindowsPoliciesMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		result, err := client.ListAll(context.Background(), "/policies", api.V2ListOptions{})
		if err != nil {
			return loadWindowsPoliciesMsg{err: err}
		}
		rows := make([]windowsPolicyRow, 0, len(result.Data))
		for _, raw := range result.Data {
			var p struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Template struct {
					Name string `json:"name"`
				} `json:"template"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				continue
			}
			if p.Template.Name != windows_mdm.TemplateNameOMAURI &&
				p.Template.Name != windows_mdm.TemplateNameRegistry {
				continue
			}
			rows = append(rows, windowsPolicyRow{ID: p.ID, Name: p.Name, Template: p.Template.Name})
		}
		return loadWindowsPoliciesMsg{policies: rows}
	}
}

func (s *WindowsMDMPoliciesListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil

	case spinner.TickMsg:
		if s.loading || s.drilling {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(m)
			return s, cmd
		}
		return s, nil

	case loadWindowsPoliciesMsg:
		s.loading = false
		if m.err != nil {
			s.err = m.err.Error()
			return s, nil
		}
		s.all = m.policies
		s.applyFilter()
		return s, nil

	case decodeWindowsPolicyMsg:
		s.drilling = false
		if m.err != nil {
			s.drillingError = m.err.Error()
			return s, nil
		}
		d, cat := m.decoded, m.catalog
		switch d.Kind {
		case windows_mdm.PolicyKindOMAURI:
			return s, func() tea.Msg {
				return tui.PushScreenMsg{Screen: NewWindowsMDMOMAURIFormScreenForEdit(d, cat)}
			}
		case windows_mdm.PolicyKindRegistry:
			return s, func() tea.Msg {
				return tui.PushScreenMsg{Screen: NewWindowsMDMRegistryFormScreenForEdit(d)}
			}
		}
		s.drillingError = fmt.Sprintf("unknown policy kind %q", d.Kind)
		return s, nil

	case tea.KeyMsg:
		if s.filtering {
			return s.updateFilterMode(m)
		}
		return s.updateBrowseMode(m)
	}
	return s, nil
}

func (s *WindowsMDMPoliciesListScreen) updateFilterMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.filtering = false
		s.filter.Blur()
		s.filter.SetValue("")
		s.applyFilter()
		return s, nil
	case "enter":
		s.filtering = false
		s.filter.Blur()
		return s.openSelected()
	case "up", "ctrl+p":
		if s.cursor > 0 {
			s.cursor--
		}
		return s, nil
	case "down", "ctrl+n":
		if s.cursor < len(s.filtered)-1 {
			s.cursor++
		}
		return s, nil
	}
	var cmd tea.Cmd
	s.filter, cmd = s.filter.Update(msg)
	s.applyFilter()
	return s, cmd
}

func (s *WindowsMDMPoliciesListScreen) updateBrowseMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return s.openSelected()
	case "esc":
		return s, func() tea.Msg { return tui.PopScreenMsg{} }
	case "j", "down":
		if s.cursor < len(s.filtered)-1 {
			s.cursor++
		}
	case "k", "up":
		if s.cursor > 0 {
			s.cursor--
		}
	case "g":
		s.cursor = 0
	case "G":
		s.cursor = len(s.filtered) - 1
		if s.cursor < 0 {
			s.cursor = 0
		}
	case "/":
		if s.err != "" {
			return s, nil
		}
		s.filtering = true
		return s, s.filter.Focus()
	case "r":
		s.loading = true
		s.err = ""
		return s, tea.Batch(s.spinner.Tick, s.loadCmd())
	}
	return s, nil
}

func (s *WindowsMDMPoliciesListScreen) openSelected() (tea.Model, tea.Cmd) {
	if s.cursor < 0 || s.cursor >= len(s.filtered) {
		return s, nil
	}
	// Ignore Enter while a decode is already in flight — repeated
	// presses would stack multiple edit screens (the Apple list's
	// Bugbot PR #54 guard).
	if s.drilling {
		return s, nil
	}
	row := s.filtered[s.cursor]
	s.drilling = true
	s.drillingError = ""
	return s, tea.Batch(s.spinner.Tick, s.decodeCmd(row.ID))
}

// decodeCmd GETs the policy, decodes it, and (for OMA-URI policies)
// loads the catalog for row rehydration — all in one goroutine so the
// screen gets exactly one terminal message per drill-in.
func (s *WindowsMDMPoliciesListScreen) decodeCmd(id string) tea.Cmd {
	return func() tea.Msg {
		client, err := newV2ClientForWindowsMDM()
		if err != nil {
			return decodeWindowsPolicyMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		raw, err := client.Get(context.Background(), "/policies/"+id)
		if err != nil {
			return decodeWindowsPolicyMsg{err: err}
		}
		decoded, err := windows_mdm.DecodeCustomWindowsPolicy(raw)
		if err != nil {
			return decodeWindowsPolicyMsg{err: err}
		}
		var cat *windows_mdm.Catalog
		if decoded.Kind == windows_mdm.PolicyKindOMAURI {
			// Soft dependency: a nil catalog just means text rows
			// instead of enum pick-lists — never block the edit on
			// the snapshot fetch.
			cat = windowsEditCatalogLoader()
		}
		return decodeWindowsPolicyMsg{decoded: decoded, catalog: cat}
	}
}

func (s *WindowsMDMPoliciesListScreen) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(s.filter.Value()))
	if q == "" {
		s.filtered = append([]windowsPolicyRow(nil), s.all...)
		s.clampCursor()
		return
	}
	s.filtered = nil
	for _, p := range s.all {
		hay := strings.ToLower(p.Name + " " + p.Template + " " + p.ID)
		if strings.Contains(hay, q) {
			s.filtered = append(s.filtered, p)
		}
	}
	s.clampCursor()
}

func (s *WindowsMDMPoliciesListScreen) clampCursor() {
	if s.cursor >= len(s.filtered) {
		s.cursor = len(s.filtered) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

func (s *WindowsMDMPoliciesListScreen) View() string {
	if s.loading {
		return fmt.Sprintf("%s Loading policies…", s.spinner.View())
	}
	if s.err != "" {
		return style.Error.Render("Error loading policies: "+s.err) + "\n\n" +
			style.Subtitle.Render("r retry · Esc back")
	}

	var b strings.Builder
	if s.filtering {
		b.WriteString(s.filter.View())
	} else {
		b.WriteString(style.Subtitle.Render(fmt.Sprintf(
			"%d of %d Windows custom policies · / filter · Enter edit · r reload · Esc back",
			len(s.filtered), len(s.all))))
	}
	b.WriteString("\n\n")

	if s.drilling {
		fmt.Fprintf(&b, "%s Decoding policy…\n", s.spinner.View())
		return b.String()
	}
	if s.drillingError != "" {
		fmt.Fprintln(&b, style.Error.Render("Could not open policy: "+s.drillingError))
		fmt.Fprintln(&b)
	}

	const nameW, templateW = 50, 36
	b.WriteString(style.TableHeader.Render(fmt.Sprintf(
		"  %-*s  %-*s  %s", nameW, "NAME", templateW, "TEMPLATE", "ID")))
	b.WriteString("\n")

	if len(s.filtered) == 0 {
		b.WriteString(style.DimRow.Render("  (no Windows custom policies)"))
		return b.String()
	}

	visible := len(s.filtered)
	if s.height > 0 {
		maxRows := s.height - 7
		if maxRows > 0 && maxRows < visible {
			visible = maxRows
		}
	}
	start := 0
	if s.cursor >= visible {
		start = s.cursor - visible + 1
	}
	end := start + visible
	if end > len(s.filtered) {
		end = len(s.filtered)
	}

	for i := start; i < end; i++ {
		p := s.filtered[i]
		row := fmt.Sprintf("%-*s  %-*s  %s",
			nameW, truncateTUI(p.Name, nameW),
			templateW, truncateTUI(p.Template, templateW),
			p.ID)
		if i == s.cursor {
			b.WriteString(style.SelectedRow.Render("> " + row))
		} else {
			b.WriteString(style.NormalRow.Render("  " + row))
		}
		b.WriteString("\n")
	}
	return b.String()
}
