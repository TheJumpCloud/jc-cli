package screen

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/bundle"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// BundleStatusScreen is the drift dashboard: bundle.Status rendered
// per-unit with value-level diffs — the screen the KLA-468 plan
// deferred TUI work for.
type BundleStatusScreen struct {
	bundle  *bundle.Bundle
	spinner spinner.Model

	loading bool
	report  *bundle.StatusReport
	err     string

	width, height int
}

// bundleStatusMsg carries the drift report back from the fetch.
type bundleStatusMsg struct {
	report *bundle.StatusReport
	err    error
}

func NewBundleStatusScreen(b *bundle.Bundle) *BundleStatusScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner
	return &BundleStatusScreen{bundle: b, spinner: sp, loading: true}
}

func (s *BundleStatusScreen) Title() string { return "Drift: " + s.bundle.Name }

func (s *BundleStatusScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.statusCmd())
}

func (s *BundleStatusScreen) statusCmd() tea.Cmd {
	b := s.bundle
	return func() tea.Msg {
		client, err := newV2ClientForBundles()
		if err != nil {
			return bundleStatusMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		cat, err := apple_mdm.Default()
		if err != nil {
			return bundleStatusMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		report, err := bundle.Status(ctx, client, b, cat)
		return bundleStatusMsg{report: report, err: err}
	}
}

func (s *BundleStatusScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case bundleStatusMsg:
		s.loading = false
		s.report = m.report
		if m.err != nil {
			s.err = m.err.Error()
		}
		return s, nil
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "r":
			if !s.loading {
				s.loading, s.err, s.report = true, "", nil
				return s, tea.Batch(s.spinner.Tick, s.statusCmd())
			}
		}
	}
	return s, nil
}

func (s *BundleStatusScreen) View() string {
	var b strings.Builder
	switch {
	case s.loading:
		fmt.Fprintln(&b, s.spinner.View()+" Comparing tenant state against the bundle definition...")

	case s.err != "":
		fmt.Fprintln(&b, style.Error.Render("Error: "))
		fmt.Fprintln(&b, "  "+wrapTUIText(s.err, s.width-4))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("r retry · Esc back"))

	default:
		r := s.report
		matched := "provenance marker"
		if !r.MatchedByMarker {
			matched = "group name (marker missing)"
		}
		fmt.Fprintln(&b, style.Subtitle.Render(fmt.Sprintf(
			"Policy group %q (matched by %s)", r.PolicyGroupName, matched)))
		fmt.Fprintln(&b)
		for _, u := range r.Units {
			var stateLabel string
			switch u.State {
			case bundle.StateInSync:
				stateLabel = style.Success.Render(fmt.Sprintf("%-9s", u.State))
			case bundle.StateDrifted, bundle.StateMissing:
				stateLabel = style.Error.Render(fmt.Sprintf("%-9s", u.State))
			default:
				stateLabel = fmt.Sprintf("%-9s", u.State)
			}
			fmt.Fprintf(&b, "  %s %s\n", stateLabel, u.PolicyName)
			for _, d := range u.Diffs {
				fmt.Fprintln(&b, "            ↳ "+wrapTUIText(d, s.width-14))
			}
		}
		for _, o := range r.Orphans {
			fmt.Fprintf(&b, "  %s %s (in the policy group but not in the bundle)\n",
				style.Error.Render(fmt.Sprintf("%-9s", "orphan")), o)
		}
		fmt.Fprintln(&b)
		if r.InSync {
			fmt.Fprintln(&b, style.Success.Render(fmt.Sprintf("In sync (%d units).", len(r.Units))))
		} else {
			fmt.Fprintln(&b, style.Error.Render("Drift detected."))
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("r refresh · Esc back"))
	}
	return b.String()
}
