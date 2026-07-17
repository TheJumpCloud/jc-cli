package screen

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/bundle"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// newV2ClientForBundles is overridable for tests (windows_mdm TUI
// precedent) — shared by the apply and status screens.
var newV2ClientForBundles = api.NewV2Client

// BundlesListScreen lists security baseline bundles (KLA-477) — the
// TUI face of `jc bundle list`. Loading is local (embedded builtins +
// ~/.config/jc/bundles/), so unlike the API-backed lists there is no
// spinner: LoadAll runs synchronously in Init's first Update.
type BundlesListScreen struct {
	bundles []*bundle.Bundle
	cursor  int
	err     string
	width   int
	height  int
}

func NewBundlesListScreen() *BundlesListScreen {
	s := &BundlesListScreen{}
	bundles, err := bundle.LoadAll()
	if err != nil {
		s.err = err.Error()
	}
	s.bundles = bundles
	return s
}

func (s *BundlesListScreen) Title() string { return "Security baseline bundles" }

func (s *BundlesListScreen) Init() tea.Cmd { return nil }

func (s *BundlesListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "up", "k":
			if s.cursor > 0 {
				s.cursor--
			}
		case "down", "j":
			if s.cursor < len(s.bundles)-1 {
				s.cursor++
			}
		case "enter":
			if s.cursor < len(s.bundles) {
				b := s.bundles[s.cursor]
				return s, func() tea.Msg {
					return tui.PushScreenMsg{Screen: NewBundleDetailScreen(b)}
				}
			}
		}
	}
	return s, nil
}

func (s *BundlesListScreen) View() string {
	var b strings.Builder
	if s.err != "" {
		fmt.Fprintln(&b, style.Error.Render("Error: "+s.err))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("Esc back"))
		return b.String()
	}

	fmt.Fprintln(&b, style.Subtitle.Render(fmt.Sprintf("%d bundles (builtin + ~/.config/jc/bundles/)", len(s.bundles))))
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "  %-20s %-12s %-9s %-16s %s\n", "NAME", "VERSION", "ORIGIN", "PLATFORMS", "POLICIES")
	var lines []string
	for i, bd := range s.bundles {
		line := fmt.Sprintf("%-20s %-12s %-9s %-16s %d",
			bd.Name, bd.Version, bd.Source.Origin, strings.Join(bd.Platforms(), ", "), len(bd.Policies))
		if i == s.cursor {
			lines = append(lines, style.SelectedRow.Render("> "+line))
		} else {
			lines = append(lines, "  "+line)
		}
	}
	fmt.Fprintln(&b, renderWindowed(lines, s.cursor, s.height, 5))

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.Subtitle.Render("Enter detail · Esc back"))
	return b.String()
}
