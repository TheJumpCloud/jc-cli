package screen

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/bundle"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// bundleApplyStage tracks the apply flow's state machine.
type bundleApplyStage int

const (
	bundleApplyStageGroup    bundleApplyStage = iota // optional device-group input
	bundleApplyStagePlanning                         // BuildApplyPlan running (read-only)
	bundleApplyStagePlan                             // plan shown, awaiting y/esc
	bundleApplyStageApplying                         // Execute running
	bundleApplyStageDone                             // result or failure shown
)

// BundleApplyScreen drives apply as staged flow: optional device
// group → plan preview (BuildApplyPlan, read-only — validation,
// template resolution, conflict pre-flight all happen here) → explicit
// y confirm → Execute → result. Reuses the exact orchestration the
// CLI and MCP run; nothing is re-implemented.
type BundleApplyScreen struct {
	bundle *bundle.Bundle
	stage  bundleApplyStage

	groupInput textinput.Model
	spinner    spinner.Model

	plan   *bundle.ApplyPlan
	result *bundle.ApplyResult
	err    string

	width, height int
}

// bundlePlanMsg carries BuildApplyPlan's outcome.
type bundlePlanMsg struct {
	plan *bundle.ApplyPlan
	err  error
}

// bundleApplyDoneMsg carries Execute's outcome. result may be non-nil
// even on error (partial failure: everything already created).
type bundleApplyDoneMsg struct {
	result *bundle.ApplyResult
	err    error
}

func NewBundleApplyScreen(b *bundle.Bundle) *BundleApplyScreen {
	ti := textinput.New()
	ti.Placeholder = "Device group name or ID (empty = create policies + group only)"
	ti.CharLimit = 128
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner

	return &BundleApplyScreen{bundle: b, groupInput: ti, spinner: sp}
}

func (s *BundleApplyScreen) Title() string { return "Apply bundle: " + s.bundle.Name }

func (s *BundleApplyScreen) TextInputActive() bool { return s.stage == bundleApplyStageGroup }

func (s *BundleApplyScreen) Init() tea.Cmd { return textinput.Blink }

// planCmd resolves the (optional) device group and builds the apply
// plan — all read-only tenant calls.
func (s *BundleApplyScreen) planCmd(group string) tea.Cmd {
	b := s.bundle
	return func() tea.Msg {
		client, err := newV2ClientForBundles()
		if err != nil {
			return bundlePlanMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		cat, err := apple_mdm.Default()
		if err != nil {
			return bundlePlanMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		opts := bundle.ApplyOptions{}
		if group != "" {
			id, err := resolve.NewV2Resolver(client).Resolve(ctx, group, resolve.DeviceGroupConfig)
			if err != nil {
				return bundlePlanMsg{err: fmt.Errorf("resolving device group %q: %w", group, err)}
			}
			opts.DeviceGroupID, opts.DeviceGroupName = id, group
		}
		p, err := bundle.BuildApplyPlan(ctx, client, b, cat, opts)
		if err != nil {
			return bundlePlanMsg{err: err}
		}
		return bundlePlanMsg{plan: p}
	}
}

func (s *BundleApplyScreen) executeCmd() tea.Cmd {
	p := s.plan
	return func() tea.Msg {
		client, err := newV2ClientForBundles()
		if err != nil {
			return bundleApplyDoneMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		result, err := p.Execute(ctx, client)
		return bundleApplyDoneMsg{result: result, err: err}
	}
}

func (s *BundleApplyScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil

	case spinner.TickMsg:
		if s.stage == bundleApplyStagePlanning || s.stage == bundleApplyStageApplying {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(m)
			return s, cmd
		}
		return s, nil

	case bundlePlanMsg:
		if m.err != nil {
			s.stage = bundleApplyStageDone
			s.err = m.err.Error()
			return s, nil
		}
		s.plan = m.plan
		s.stage = bundleApplyStagePlan
		return s, nil

	case bundleApplyDoneMsg:
		s.stage = bundleApplyStageDone
		s.result = m.result
		if m.err != nil {
			s.err = m.err.Error()
		}
		return s, nil

	case tea.KeyMsg:
		switch s.stage {
		case bundleApplyStageGroup:
			switch m.String() {
			case "esc":
				return s, func() tea.Msg { return tui.PopScreenMsg{} }
			case "enter":
				s.stage = bundleApplyStagePlanning
				return s, tea.Batch(s.spinner.Tick, s.planCmd(strings.TrimSpace(s.groupInput.Value())))
			default:
				var cmd tea.Cmd
				s.groupInput, cmd = s.groupInput.Update(m)
				return s, cmd
			}
		case bundleApplyStagePlan:
			switch m.String() {
			case "esc":
				return s, func() tea.Msg { return tui.PopScreenMsg{} }
			case "y":
				s.stage = bundleApplyStageApplying
				return s, tea.Batch(s.spinner.Tick, s.executeCmd())
			}
		case bundleApplyStageDone:
			if m.String() == "esc" || m.String() == "enter" {
				return s, func() tea.Msg { return tui.PopScreenMsg{} }
			}
		}
	}
	return s, nil
}

func (s *BundleApplyScreen) View() string {
	var b strings.Builder
	switch s.stage {
	case bundleApplyStageGroup:
		fmt.Fprintln(&b, style.Subtitle.Render(fmt.Sprintf(
			"Apply %s v%s (%d policy units)", s.bundle.Name, s.bundle.Version, len(s.bundle.Policies))))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  Bind to a device group (optional):")
		fmt.Fprintln(&b, "  "+s.groupInput.View())
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("Enter preview plan · Esc cancel"))

	case bundleApplyStagePlanning:
		fmt.Fprintln(&b, s.spinner.View()+" Building apply plan (validating, resolving templates, checking for name conflicts)...")

	case bundleApplyStagePlan:
		fmt.Fprintln(&b, style.SectionHeader.Render(fmt.Sprintf("Plan: %d steps — nothing created yet", len(s.plan.Steps))))
		fmt.Fprintln(&b)
		for _, st := range s.plan.Steps {
			fmt.Fprintf(&b, "  %-13s %s — %s\n", st.Kind, st.Name, st.Detail)
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("y apply · Esc cancel"))

	case bundleApplyStageApplying:
		fmt.Fprintln(&b, s.spinner.View()+" Applying (create-only; a failure stops and reports, nothing is rolled back)...")

	case bundleApplyStageDone:
		if s.err != "" {
			fmt.Fprintln(&b, style.Error.Render("Apply failed:"))
			fmt.Fprintln(&b, "  "+wrapTUIText(s.err, s.width-4))
		} else {
			fmt.Fprintln(&b, style.Success.Render(fmt.Sprintf(
				"Applied %s v%s: %d objects created", s.bundle.Name, s.bundle.Version, len(s.result.Created))))
			fmt.Fprintln(&b)
			for _, c := range s.result.Created {
				fmt.Fprintf(&b, "  %-13s %-50s %s\n", c.Kind, c.Name, c.ID)
			}
			if s.result.Bound {
				fmt.Fprintln(&b)
				fmt.Fprintln(&b, "  Device group bound to the policy group.")
			}
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("Enter/Esc back"))
	}
	return b.String()
}
