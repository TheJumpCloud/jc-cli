package screen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// customMDMTemplateFamilyPrefix is the JumpCloud template-name
// prefix we filter on. Matches `custom_mdm_profile_darwin`,
// `custom_mdm_profile_iphone`, etc. — i.e. every Custom MDM
// Configuration Profile family JumpCloud might add.
const customMDMTemplateFamilyPrefix = "custom_mdm_profile_"

// AppleMDMPoliciesListScreen lists existing JumpCloud policies whose
// template is in the Custom MDM Configuration Profile family. Drilling
// in fetches + decodes the policy and either opens the edit form
// (single-payload) or surfaces the multi-payload guard.
type AppleMDMPoliciesListScreen struct {
	all       []policyRow
	filtered  []policyRow
	cursor    int
	filter    textinput.Model
	filtering bool
	width     int
	height    int

	loading bool
	err     string

	spinner spinner.Model

	// Drill-in stage shows a spinner while the GET + decode runs in
	// a side goroutine. Errors surface inline; on success the
	// authoring screen pushes via PushScreenMsg.
	drilling      bool
	drillingError string
}

// policyRow is one entry in the list. Keep it tiny — the list call
// returns dozens of policies on a typical tenant and we don't decode
// the inner plist until the operator drills in.
type policyRow struct {
	ID       string
	Name     string
	Template string
}

// loadPoliciesMsg carries the list of custom-MDM policies back to the
// screen after the fetch goroutine returns.
type loadPoliciesMsg struct {
	policies []policyRow
	err      error
}

// decodePolicyMsg carries the result of GET+decode on a single
// policy. nil err means we successfully resolved the schema and the
// caller can push the edit form.
type decodePolicyMsg struct {
	decoded apple_mdm.DecodedPolicy
	err     error
}

// newV2ClientForMDMPolicies is overridable for tests. Mirrors the
// pattern used by the authoring screen.
var newV2ClientForMDMPolicies = api.NewV2Client

// NewAppleMDMPoliciesListScreen builds the screen. Initial fetch
// fires from Init so the visible delay is the first user-perceptible
// hint that this is API-backed (vs the offline payloads catalog).
func NewAppleMDMPoliciesListScreen() *AppleMDMPoliciesListScreen {
	ti := textinput.New()
	ti.Placeholder = "Type to filter by name or template..."
	ti.CharLimit = 64

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner

	return &AppleMDMPoliciesListScreen{
		filter:  ti,
		spinner: sp,
		loading: true,
	}
}

func (s *AppleMDMPoliciesListScreen) Title() string {
	return "Apple MDM custom MDM policies"
}

func (s *AppleMDMPoliciesListScreen) TextInputActive() bool { return s.filtering }

func (s *AppleMDMPoliciesListScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.loadCmd())
}

// loadCmd issues GET /policies and filters by template-name prefix.
// JumpCloud doesn't accept a string-prefix filter against template.name
// via the v2 query API, so we list everything and prefix-filter client-
// side. Custom MDM policy counts are typically dozens, not thousands —
// this is cheap.
func (s *AppleMDMPoliciesListScreen) loadCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := newV2ClientForMDMPolicies()
		if err != nil {
			return loadPoliciesMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		result, err := client.ListAll(context.Background(), "/policies", api.V2ListOptions{})
		if err != nil {
			return loadPoliciesMsg{err: err}
		}
		rows := make([]policyRow, 0, len(result.Data))
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
			if !strings.HasPrefix(p.Template.Name, customMDMTemplateFamilyPrefix) {
				continue
			}
			rows = append(rows, policyRow{
				ID: p.ID, Name: p.Name, Template: p.Template.Name,
			})
		}
		return loadPoliciesMsg{policies: rows}
	}
}

func (s *AppleMDMPoliciesListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case loadPoliciesMsg:
		s.loading = false
		if m.err != nil {
			s.err = m.err.Error()
			return s, nil
		}
		s.all = m.policies
		s.applyFilter()
		return s, nil

	case decodePolicyMsg:
		s.drilling = false
		if m.err != nil {
			s.drillingError = m.err.Error()
			return s, nil
		}
		// Multi-payload bundles route to the guard screen; single
		// payloads route to the edit form.
		if m.decoded.IsMulti {
			d := m.decoded
			return s, func() tea.Msg {
				return tui.PushScreenMsg{Screen: NewAppleMDMMultiPayloadGuardScreen(d)}
			}
		}
		if m.decoded.Schema.Type == "" {
			s.drillingError = fmt.Sprintf(
				"policy uses payloadtype %q which isn't in the embedded catalog (release %s). "+
					"Drop to the Admin Portal to edit.",
				m.decoded.PayloadType, apple_mdm.SchemaRelease)
			return s, nil
		}
		d := m.decoded
		return s, func() tea.Msg {
			return tui.PushScreenMsg{Screen: NewAppleMDMPayloadsFormScreenForEdit(d)}
		}

	case tea.KeyMsg:
		if s.filtering {
			return s.updateFilterMode(m)
		}
		return s.updateBrowseMode(m)
	}
	return s, nil
}

func (s *AppleMDMPoliciesListScreen) updateFilterMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (s *AppleMDMPoliciesListScreen) updateBrowseMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		s.filtering = true
		return s, s.filter.Focus()
	case "r":
		s.loading = true
		return s, tea.Batch(s.spinner.Tick, s.loadCmd())
	}
	return s, nil
}

func (s *AppleMDMPoliciesListScreen) openSelected() (tea.Model, tea.Cmd) {
	if s.cursor < 0 || s.cursor >= len(s.filtered) {
		return s, nil
	}
	row := s.filtered[s.cursor]
	s.drilling = true
	s.drillingError = ""
	return s, tea.Batch(s.spinner.Tick, s.decodeCmd(row.ID))
}

// decodeCmd does GET /policies/{id} and runs the decoder. Bundling
// these as one command keeps the screen state simple — we get one
// terminal message per drill-in.
func (s *AppleMDMPoliciesListScreen) decodeCmd(id string) tea.Cmd {
	return func() tea.Msg {
		client, err := newV2ClientForMDMPolicies()
		if err != nil {
			return decodePolicyMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		raw, err := client.Get(context.Background(), "/policies/"+id)
		if err != nil {
			return decodePolicyMsg{err: err}
		}
		decoded, err := apple_mdm.DecodeCustomMDMPolicy(raw)
		if err != nil {
			return decodePolicyMsg{err: err}
		}
		return decodePolicyMsg{decoded: decoded}
	}
}

func (s *AppleMDMPoliciesListScreen) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(s.filter.Value()))
	if q == "" {
		s.filtered = append([]policyRow(nil), s.all...)
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

func (s *AppleMDMPoliciesListScreen) clampCursor() {
	if s.cursor >= len(s.filtered) {
		s.cursor = len(s.filtered) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

func (s *AppleMDMPoliciesListScreen) View() string {
	if s.loading {
		return fmt.Sprintf("%s Loading policies…", s.spinner.View())
	}
	if s.err != "" {
		return style.Error.Render("Error loading policies: " + s.err)
	}

	var b strings.Builder
	if s.filtering {
		b.WriteString(s.filter.View())
	} else {
		b.WriteString(style.Subtitle.Render(fmt.Sprintf(
			"%d of %d Custom MDM policies · / filter · Enter edit · r reload · Esc back",
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

	const nameW, templateW = 50, 28
	b.WriteString(style.TableHeader.Render(fmt.Sprintf(
		"  %-*s  %-*s  %s", nameW, "NAME", templateW, "TEMPLATE", "ID")))
	b.WriteString("\n")

	if len(s.filtered) == 0 {
		b.WriteString(style.DimRow.Render("  (no Custom MDM policies)"))
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
