package screen

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// newV2ClientForPatchPolicies is overridable for tests.
var newV2ClientForPatchPolicies = api.NewV2Client

// patchPolicyTemplates pins the OS-update template family → OS label.
// JumpCloud patch management is entirely policy-template-based; these
// 12 names were verified on the live tenant during the KLA-481 recon
// (2026-07-16). A JumpCloud template rename makes the pinned-constant
// test fail rather than silently emptying the screen.
var patchPolicyTemplates = map[string]string{
	"system_updates_windows":                          "Windows",
	"system_updates_osp_windows":                      "Windows",
	"system_update_darwin":                            "macOS",
	"automatic_macOS_updates_darwin":                  "macOS",
	"automatic_macOS_updates_darwin_ddm":              "macOS",
	"software_update_enforcement_specific_darwin_ddm": "macOS",
	"software_update_preferences_darwin":              "macOS",
	"delay_software_updates_darwin":                   "macOS",
	"automatic_ios_updates_ios_ddm":                   "iOS",
	"software_update_enforcement_specific_ios_ddm":    "iOS",
	"system_update_ubuntu_linux":                      "Linux",
	"system_update_policy_android":                    "Android",
}

// patchOSOrder fixes the group display order.
var patchOSOrder = []string{"macOS", "iOS", "Windows", "Linux", "Android"}

// PatchPoliciesListScreen lists the tenant's OS-update policies
// grouped by platform (KLA-481) — the TUI face of JumpCloud patch
// management, which has no dedicated API: it IS these policies.
type PatchPoliciesListScreen struct {
	rows    []patchPolicyRow
	cursor  int
	loading bool
	err     string
	spinner spinner.Model

	width, height int
}

// patchPolicyRow is one OS-update policy.
type patchPolicyRow struct {
	ID       string
	Name     string
	Template string
	OS       string
}

// loadPatchPoliciesMsg carries the filtered list.
type loadPatchPoliciesMsg struct {
	rows []patchPolicyRow
	err  error
}

func NewPatchPoliciesListScreen() *PatchPoliciesListScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner
	return &PatchPoliciesListScreen{spinner: sp, loading: true}
}

func (s *PatchPoliciesListScreen) Title() string { return "Patch Management" }

func (s *PatchPoliciesListScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.loadCmd())
}

// loadCmd lists /policies and keeps the OS-update template family.
// Client-side filtering — same rationale as the MDM policy lists: no
// template-name filter server-side, and policy counts are small.
func (s *PatchPoliciesListScreen) loadCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := newV2ClientForPatchPolicies()
		if err != nil {
			return loadPatchPoliciesMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := client.ListAll(ctx, "/policies", api.V2ListOptions{})
		if err != nil {
			return loadPatchPoliciesMsg{err: err}
		}
		var rows []patchPolicyRow
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
			os, ok := patchPolicyTemplates[p.Template.Name]
			if !ok {
				continue
			}
			rows = append(rows, patchPolicyRow{ID: p.ID, Name: p.Name, Template: p.Template.Name, OS: os})
		}
		// Group by OS (fixed order), name within group.
		osRank := map[string]int{}
		for i, os := range patchOSOrder {
			osRank[os] = i
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].OS != rows[j].OS {
				return osRank[rows[i].OS] < osRank[rows[j].OS]
			}
			return rows[i].Name < rows[j].Name
		})
		return loadPatchPoliciesMsg{rows: rows}
	}
}

func (s *PatchPoliciesListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil
	case spinner.TickMsg:
		if s.loading {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(m)
			return s, cmd
		}
		return s, nil
	case loadPatchPoliciesMsg:
		s.loading = false
		if m.err != nil {
			s.err = m.err.Error()
			return s, nil
		}
		s.rows = m.rows
		return s, nil
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "r":
			if !s.loading {
				s.loading, s.err = true, ""
				return s, tea.Batch(s.spinner.Tick, s.loadCmd())
			}
		case "up", "k":
			if s.cursor > 0 {
				s.cursor--
			}
		case "down", "j":
			if s.cursor < len(s.rows)-1 {
				s.cursor++
			}
		case "enter":
			if s.cursor < len(s.rows) {
				row := s.rows[s.cursor]
				return s, func() tea.Msg {
					return tui.PushScreenMsg{Screen: NewPatchPolicyDetailScreen(row)}
				}
			}
		}
	}
	return s, nil
}

func (s *PatchPoliciesListScreen) View() string {
	var b strings.Builder
	switch {
	case s.loading:
		fmt.Fprintln(&b, s.spinner.View()+" Loading OS-update policies...")
	case s.err != "":
		fmt.Fprintln(&b, style.Error.Render("Error: "+s.err))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("r retry · Esc back"))
	case len(s.rows) == 0:
		fmt.Fprintln(&b, style.Subtitle.Render("No OS-update policies on this tenant."))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  Create them in the Admin Portal (Policy Management) or with")
		fmt.Fprintln(&b, "  `jc policies create` using an OS-update template — e.g.")
		fmt.Fprintln(&b, "  system_updates_windows, automatic_macOS_updates_darwin_ddm.")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("r refresh · Esc back"))
	default:
		fmt.Fprintln(&b, style.Subtitle.Render(fmt.Sprintf("%d OS-update policies", len(s.rows))))
		fmt.Fprintln(&b)
		var lines []string
		focusLine := 0
		lastOS := ""
		for i, r := range s.rows {
			if r.OS != lastOS {
				lines = append(lines, style.SectionHeader.Render(r.OS))
				lastOS = r.OS
			}
			line := fmt.Sprintf("%-40s %s", truncTUI(r.Name, 40), r.Template)
			if i == s.cursor {
				lines = append(lines, style.SelectedRow.Render("> "+line))
				focusLine = len(lines) - 1
			} else {
				lines = append(lines, "  "+line)
			}
		}
		fmt.Fprintln(&b, renderWindowed(lines, focusLine, s.height, 4))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("Enter detail · r refresh · Esc back"))
	}
	return b.String()
}

// PatchPolicyDetailScreen fetches one policy and renders its
// configured values generically — patch templates carry flat
// configFields (no base64 payloads to decode).
type PatchPolicyDetailScreen struct {
	row     patchPolicyRow
	loading bool
	err     string
	values  []patchPolicyValue
	spinner spinner.Model

	width, height int
}

type patchPolicyValue struct {
	Name  string
	Value string
}

// loadPatchPolicyDetailMsg carries the decoded values.
type loadPatchPolicyDetailMsg struct {
	values []patchPolicyValue
	err    error
}

func NewPatchPolicyDetailScreen(row patchPolicyRow) *PatchPolicyDetailScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner
	return &PatchPolicyDetailScreen{row: row, spinner: sp, loading: true}
}

func (s *PatchPolicyDetailScreen) Title() string { return "Patch policy: " + s.row.Name }

func (s *PatchPolicyDetailScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.loadCmd())
}

func (s *PatchPolicyDetailScreen) loadCmd() tea.Cmd {
	id := s.row.ID
	return func() tea.Msg {
		client, err := newV2ClientForPatchPolicies()
		if err != nil {
			return loadPatchPolicyDetailMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		raw, err := client.Get(ctx, "/policies/"+id)
		if err != nil {
			return loadPatchPolicyDetailMsg{err: err}
		}
		var p struct {
			Values []struct {
				ConfigFieldName string `json:"configFieldName"`
				Value           any    `json:"value"`
			} `json:"values"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return loadPatchPolicyDetailMsg{err: err}
		}
		values := make([]patchPolicyValue, 0, len(p.Values))
		for _, v := range p.Values {
			rendered := fmt.Sprintf("%v", v.Value)
			if data, err := json.Marshal(v.Value); err == nil && (strings.HasPrefix(string(data), "{") || strings.HasPrefix(string(data), "[")) {
				rendered = string(data)
			}
			values = append(values, patchPolicyValue{Name: v.ConfigFieldName, Value: rendered})
		}
		return loadPatchPolicyDetailMsg{values: values}
	}
}

func (s *PatchPolicyDetailScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil
	case spinner.TickMsg:
		if s.loading {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(m)
			return s, cmd
		}
		return s, nil
	case loadPatchPolicyDetailMsg:
		s.loading = false
		s.values = m.values
		if m.err != nil {
			s.err = m.err.Error()
		}
		return s, nil
	case tea.KeyMsg:
		if m.String() == "esc" {
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		}
	}
	return s, nil
}

func (s *PatchPolicyDetailScreen) View() string {
	var b strings.Builder
	fmt.Fprintln(&b, style.Subtitle.Render(fmt.Sprintf("%s — %s (%s)", s.row.Name, s.row.OS, s.row.Template)))
	fmt.Fprintln(&b)
	switch {
	case s.loading:
		fmt.Fprintln(&b, s.spinner.View()+" Loading policy values...")
	case s.err != "":
		fmt.Fprintln(&b, style.Error.Render("Error: "+s.err))
	case len(s.values) == 0:
		fmt.Fprintln(&b, "  No configured values (template defaults apply).")
	default:
		fmt.Fprintln(&b, style.SectionHeader.Render("Configured values"))
		for _, v := range s.values {
			val := v.Value
			if len(val) > 100 {
				val = val[:100] + "…"
			}
			fmt.Fprintf(&b, "  %-42s %s\n", v.Name, val)
		}
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.Subtitle.Render("Esc back"))
	return b.String()
}
