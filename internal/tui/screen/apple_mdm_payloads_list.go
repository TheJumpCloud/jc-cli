package screen

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// applePayloadCatalogLoader is overridable for tests. Defaults to the
// real embedded catalog from internal/apple_mdm/catalog.go.
var applePayloadCatalogLoader = func() (*apple_mdm.Catalog, error) {
	return apple_mdm.Default()
}

// AppleMDMPayloadsListScreen browses the vendored Apple Configuration
// Profile catalog. Mirrors RecipeListScreen's shape (filter input +
// cursor + keybindings) so the UX is consistent across offline-data
// browsers.
type AppleMDMPayloadsListScreen struct {
	all       []apple_mdm.Payload
	filtered  []apple_mdm.Payload
	cursor    int
	filter    textinput.Model
	filtering bool
	width     int
	height    int
	err       string
	loaded    bool
	release   string
}

// NewAppleMDMPayloadsListScreen builds a fresh list screen. The
// catalog load happens in Init so tests can inject a stub via
// applePayloadCatalogLoader.
func NewAppleMDMPayloadsListScreen() *AppleMDMPayloadsListScreen {
	ti := textinput.New()
	ti.Placeholder = "Type to filter payloads (type, title, description)..."
	ti.CharLimit = 64
	return &AppleMDMPayloadsListScreen{filter: ti}
}

func (s *AppleMDMPayloadsListScreen) Title() string { return "Apple MDM payloads" }

// TextInputActive lets the app-level GlobalKeyMap suppress single-key
// shortcuts (q to quit) while the filter input has focus. Mirrors the
// RecipeListScreen pattern; see the comment there for why this isn't
// just `return s.filtering` in screens with confirmation prompts.
func (s *AppleMDMPayloadsListScreen) TextInputActive() bool { return s.filtering }

func (s *AppleMDMPayloadsListScreen) Init() tea.Cmd {
	s.loadCatalog()
	return nil
}

func (s *AppleMDMPayloadsListScreen) loadCatalog() {
	cat, err := applePayloadCatalogLoader()
	if err != nil {
		s.err = err.Error()
		return
	}
	s.all = cat.All()
	s.release = cat.Release
	s.applyFilter()
	s.loaded = true
}

func (s *AppleMDMPayloadsListScreen) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(s.filter.Value()))
	// Always allocate a fresh slice for s.filtered. Pre-fix the
	// empty-query branch aliased s.filtered to s.all, and the next
	// non-empty filter's `s.filtered[:0]` + append silently
	// overwrote s.all's contents — the operator saw a corrupt
	// catalog after the first filter cycle (Bugbot PR #52 review).
	if q == "" {
		s.filtered = append([]apple_mdm.Payload(nil), s.all...)
		s.clampCursor()
		return
	}
	s.filtered = nil
	for _, p := range s.all {
		hay := strings.ToLower(p.Type + " " + p.Title + " " + p.Description)
		if strings.Contains(hay, q) {
			s.filtered = append(s.filtered, p)
		}
	}
	s.clampCursor()
}

func (s *AppleMDMPayloadsListScreen) clampCursor() {
	if s.cursor >= len(s.filtered) {
		s.cursor = len(s.filtered) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

func (s *AppleMDMPayloadsListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil
	case tea.KeyMsg:
		if s.filtering {
			return s.updateFilterMode(m)
		}
		return s.updateBrowseMode(m)
	}
	return s, nil
}

func (s *AppleMDMPayloadsListScreen) updateFilterMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (s *AppleMDMPayloadsListScreen) updateBrowseMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		s.clampCursor()
	case "/":
		s.filtering = true
		return s, s.filter.Focus()
	case "r":
		s.loadCatalog()
		return s, func() tea.Msg { return tui.FlashMsg{Text: "Catalog reloaded"} }
	}
	return s, nil
}

func (s *AppleMDMPayloadsListScreen) openSelected() (tea.Model, tea.Cmd) {
	if s.cursor < 0 || s.cursor >= len(s.filtered) {
		return s, nil
	}
	p := s.filtered[s.cursor]
	return s, func() tea.Msg {
		return tui.PushScreenMsg{Screen: NewAppleMDMPayloadsShowScreen(p)}
	}
}

// View renders the catalog as a header + filter line + aligned table.
// We hand-roll the table (no shared table component renders today —
// see Recon report) so column widths can adapt to the terminal width.
func (s *AppleMDMPayloadsListScreen) View() string {
	if !s.loaded && s.err == "" {
		return "Loading Apple MDM catalog..."
	}
	if s.err != "" {
		return style.Error.Render("Error loading catalog: " + s.err)
	}

	var b strings.Builder

	// Filter bar or count line
	if s.filtering {
		b.WriteString(s.filter.View())
	} else {
		b.WriteString(style.Subtitle.Render(fmt.Sprintf(
			"%d of %d payloads · release %s · press / to filter, Enter to drill in, Esc to go back",
			len(s.filtered), len(s.all), s.release)))
	}
	b.WriteString("\n\n")

	// Column widths sized to leave room for PLATFORMS at the right.
	// Hardcoded for now; a future improvement is to use s.width and
	// truncate Title more aggressively on narrow terminals.
	const typeW, titleW = 48, 30

	b.WriteString(style.TableHeader.Render(fmt.Sprintf(
		"  %-*s  %-*s  %s", typeW, "TYPE", titleW, "TITLE", "PLATFORMS")))
	b.WriteString("\n")

	if len(s.filtered) == 0 {
		b.WriteString(style.DimRow.Render("  (no payloads match)"))
		return b.String()
	}

	// Cap the visible window to the terminal height minus the header
	// rows so we don't write past the screen. Bubbletea wraps but the
	// visual result is jarring; truncate is cleaner.
	visible := len(s.filtered)
	if s.height > 0 {
		// Header consumes ~5 rows (subtitle + blank + table header + ...).
		maxRows := s.height - 6
		if maxRows > 0 && maxRows < visible {
			visible = maxRows
		}
	}
	// Scroll so the cursor stays in view.
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
		title := p.Title
		if title == "" {
			title = "—"
		}
		platforms := strings.Join(availablePlatformsForList(p.SupportedOS), ",")
		row := fmt.Sprintf("%-*s  %-*s  %s",
			typeW, truncateTUI(p.Type, typeW),
			titleW, truncateTUI(title, titleW),
			platforms)
		if i == s.cursor {
			b.WriteString(style.SelectedRow.Render("> " + row))
		} else {
			b.WriteString(style.NormalRow.Render("  " + row))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// applePlatforms is the display order for the PLATFORMS column. Matches
// the CLI's `apple-mdm payloads list` table for consistency across
// surfaces. Stripped to the values that actually appear in Apple's
// vendored schemas (Release-v26.4).
var appleListPlatforms = []string{"iOS", "macOS", "tvOS", "visionOS", "watchOS"}

func availablePlatformsForList(s apple_mdm.SupportedOS) []string {
	out := make([]string, 0, len(appleListPlatforms))
	for _, plat := range appleListPlatforms {
		if sup, ok := s[plat]; ok && sup.Available() {
			out = append(out, plat)
		}
	}
	return out
}

// truncateTUI clips a string to width n, replacing the trailing
// characters with "…". Single-character ellipsis (vs "...") keeps the
// table column widths predictable.
func truncateTUI(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
