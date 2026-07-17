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
	scroll int
	width  int
	height int
}

// bodyLines builds every line below the pinned title (description,
// source, units) so the View can window them and keep the footer
// visible on long bundles.
func (s *BundleDetailScreen) bodyLines() []string {
	bd := s.bundle
	var lines []string
	if bd.Description != "" {
		lines = append(lines, "", "  "+wrapTUIText(bd.Description, s.width-4))
	}
	lines = append(lines, "", style.SectionHeader.Render("Source"))
	if bd.Source.Attribution != "" {
		lines = append(lines, "  "+wrapTUIText(bd.Source.Attribution, s.width-4))
	}
	if bd.Source.License != "" {
		lines = append(lines, "  License: "+bd.Source.License)
	}
	if bd.Source.URL != "" {
		lines = append(lines, "  "+bd.Source.URL)
	}
	lines = append(lines, "", style.SectionHeader.Render(fmt.Sprintf("Policy units (%d)", len(bd.Policies))))
	for i := range bd.Policies {
		u := &bd.Policies[i]
		lines = append(lines, "  "+unitSummary(u))
		if u.Description != "" {
			lines = append(lines, "    "+style.Subtitle.Render(u.Description))
		}
	}
	return lines
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
		case "up", "k":
			s.scroll = clampScroll(s.scroll-1, len(s.bodyLines()))
		case "down", "j":
			s.scroll = clampScroll(s.scroll+1, len(s.bodyLines()))
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
	// chrome: title + footer blank + footer = 3
	fmt.Fprintln(&b, renderWindowed(s.bodyLines(), s.scroll, s.height, 3))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.Subtitle.Render("↑/↓ scroll · a apply · s status (drift) · Esc back"))
	return b.String()
}
