package screen

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/bundle"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// BundleDetailScreen shows one bundle in full — metadata, the
// source/licensing attribution (first-class, per the KLA-468
// licensing gate), and every policy unit. `a` starts the apply flow,
// `s` opens the drift dashboard.
type BundleDetailScreen struct {
	bundle *bundle.Bundle
	width  int
	height int
}

func NewBundleDetailScreen(b *bundle.Bundle) *BundleDetailScreen {
	return &BundleDetailScreen{bundle: b}
}

func (s *BundleDetailScreen) Title() string { return "Bundle: " + s.bundle.Name }

func (s *BundleDetailScreen) Init() tea.Cmd { return nil }

func (s *BundleDetailScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "a":
			b := s.bundle
			return s, func() tea.Msg {
				return tui.PushScreenMsg{Screen: NewBundleApplyScreen(b)}
			}
		case "s":
			b := s.bundle
			return s, func() tea.Msg {
				return tui.PushScreenMsg{Screen: NewBundleStatusScreen(b)}
			}
		}
	}
	return s, nil
}

// unitSummary is one line per policy unit: kind + payload/setting/key
// count, plus the unit description (STIG rule IDs etc.) when present.
func unitSummary(u *bundle.PolicyUnit) string {
	var detail string
	switch u.Type {
	case bundle.UnitAppleProfile:
		detail = fmt.Sprintf("%s profile, %d payload(s)", u.OS, len(u.Profile.Payloads))
	case bundle.UnitWindowsOMAURI:
		detail = fmt.Sprintf("%d OMA-URI setting(s)", len(u.Settings))
	case bundle.UnitWindowsRegistry:
		detail = fmt.Sprintf("%d registry key(s)", len(u.Keys))
	}
	return fmt.Sprintf("%-45s %s", u.Name, detail)
}

func (s *BundleDetailScreen) View() string {
	var b strings.Builder
	bd := s.bundle

	fmt.Fprintln(&b, style.Subtitle.Render(fmt.Sprintf("%s v%s (%s)", bd.Name, bd.Version, bd.Source.Origin)))
	if bd.Description != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  "+wrapTUIText(bd.Description, s.width-4))
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.SectionHeader.Render("Source"))
	if bd.Source.Attribution != "" {
		fmt.Fprintln(&b, "  "+wrapTUIText(bd.Source.Attribution, s.width-4))
	}
	if bd.Source.License != "" {
		fmt.Fprintln(&b, "  License: "+bd.Source.License)
	}
	if bd.Source.URL != "" {
		fmt.Fprintln(&b, "  "+bd.Source.URL)
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.SectionHeader.Render(fmt.Sprintf("Policy units (%d)", len(bd.Policies))))
	for i := range bd.Policies {
		u := &bd.Policies[i]
		fmt.Fprintln(&b, "  "+unitSummary(u))
		if u.Description != "" {
			fmt.Fprintln(&b, "    "+style.Subtitle.Render(u.Description))
		}
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.Subtitle.Render("a apply · s status (drift) · Esc back"))
	return b.String()
}
