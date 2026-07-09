package screen

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// WindowsMDMCSPListScreen browses Microsoft's Policy CSP settings
// catalog (KLA-462) — the TUI face of `jc windows-mdm csp list`.
// Unlike the Apple payloads list (offline, vendored catalog), the
// Windows catalog is fetch-on-demand: the first open may download the
// pinned DDF snapshot from Microsoft, so loading runs async behind a
// spinner (mirroring the policies-list pattern, not the synchronous
// Apple payloads list).
//
// Enter opens the setting detail; from there `a` adds the setting to
// the shared policy draft. `c` opens the draft as an OMA-URI policy
// form once at least one setting is added.
type WindowsMDMCSPListScreen struct {
	all      []windows_mdm.Setting
	filtered []windows_mdm.Setting
	cursor   int

	filter    textinput.Model
	filtering bool

	width  int
	height int

	loading  bool
	err      string
	snapshot string
	spinner  spinner.Model

	// draft accumulates the settings the operator picked across
	// detail screens. Shared by pointer with the detail + form
	// screens — bubbletea screens here are pointer-receiver models,
	// so mutation through the shared pointer is the established
	// cross-screen state channel (same process, single goroutine for
	// Update calls).
	draft *windowsMDMDraft
}

// windowsMDMDraft is the cross-screen policy draft: the settings the
// operator picked while browsing. Kept deliberately tiny.
type windowsMDMDraft struct {
	settings []windows_mdm.Setting
}

// add appends a setting unless it's already drafted (by URI).
func (d *windowsMDMDraft) add(s windows_mdm.Setting) bool {
	for _, existing := range d.settings {
		if existing.URI == s.URI {
			return false
		}
	}
	d.settings = append(d.settings, s)
	return true
}

// loadCatalogMsg carries the parsed catalog (or the fetch/parse error)
// back from the async loader.
type loadCatalogMsg struct {
	settings []windows_mdm.Setting
	snapshot string
	err      error
}

// windowsCSPCatalogLoader is overridable for tests — the default
// fetches/loads the real Microsoft snapshot via DefaultCatalog. The
// context is bounded: an unresponsive endpoint must surface as the
// error/retry state, not hang the fetch goroutine forever after the
// operator Esc'd away (CodeRabbit PR #67 review). Three minutes is
// generous for a ~700KB download + parse even on a slow link.
var windowsCSPCatalogLoader = func() ([]windows_mdm.Setting, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cat, err := windows_mdm.DefaultCatalog(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	return cat.Filter(windows_mdm.FilterOpts{}), cat.Snapshot, nil
}

// NewWindowsMDMCSPListScreen builds the browse screen. The catalog
// load fires from Init.
func NewWindowsMDMCSPListScreen() *WindowsMDMCSPListScreen {
	ti := textinput.New()
	ti.Placeholder = "Type to filter by area, name, URI, or description..."
	ti.CharLimit = 64

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner

	return &WindowsMDMCSPListScreen{
		filter:  ti,
		spinner: sp,
		loading: true,
		draft:   &windowsMDMDraft{},
	}
}

func (s *WindowsMDMCSPListScreen) Title() string { return "Windows MDM — Policy CSP catalog" }

func (s *WindowsMDMCSPListScreen) TextInputActive() bool { return s.filtering }

func (s *WindowsMDMCSPListScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.loadCmd())
}

// loadCmd runs the (possibly downloading) catalog load off the UI
// goroutine. First run on a fresh machine downloads ~700KB from
// Microsoft's pinned URL; subsequent runs are pure local reads.
func (s *WindowsMDMCSPListScreen) loadCmd() tea.Cmd {
	return func() tea.Msg {
		settings, snapshot, err := windowsCSPCatalogLoader()
		return loadCatalogMsg{settings: settings, snapshot: snapshot, err: err}
	}
}

func (s *WindowsMDMCSPListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case loadCatalogMsg:
		s.loading = false
		if m.err != nil {
			s.err = m.err.Error()
			return s, nil
		}
		s.all = m.settings
		s.snapshot = m.snapshot
		s.applyFilter()
		return s, nil

	case tea.KeyMsg:
		if s.loading {
			// Only Esc works while the snapshot fetch runs.
			if m.String() == "esc" {
				return s, func() tea.Msg { return tui.PopScreenMsg{} }
			}
			return s, nil
		}
		if s.filtering {
			return s.updateFilterMode(m)
		}
		return s.updateBrowseMode(m)
	}
	return s, nil
}

func (s *WindowsMDMCSPListScreen) updateFilterMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (s *WindowsMDMCSPListScreen) updateBrowseMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		// The error view renders instead of the list; entering filter
		// mode there would type into a hidden input (CodeRabbit PR #67
		// review).
		if s.err != "" {
			return s, nil
		}
		s.filtering = true
		return s, s.filter.Focus()
	case "c":
		return s.openDraft()
	case "r":
		s.loading = true
		s.err = ""
		return s, tea.Batch(s.spinner.Tick, s.loadCmd())
	}
	return s, nil
}

func (s *WindowsMDMCSPListScreen) openSelected() (tea.Model, tea.Cmd) {
	if s.cursor < 0 || s.cursor >= len(s.filtered) {
		return s, nil
	}
	setting := s.filtered[s.cursor]
	draft := s.draft
	return s, func() tea.Msg {
		return tui.PushScreenMsg{Screen: NewWindowsMDMCSPShowScreen(setting, draft)}
	}
}

// openDraft pushes the OMA-URI policy form over the current draft.
// With an empty draft it flashes a hint instead — an empty form has
// nothing to author.
func (s *WindowsMDMCSPListScreen) openDraft() (tea.Model, tea.Cmd) {
	if len(s.draft.settings) == 0 {
		return s, func() tea.Msg {
			return tui.FlashMsg{Text: "Draft is empty — open a setting and press a to add it"}
		}
	}
	draft := s.draft
	return s, func() tea.Msg {
		return tui.PushScreenMsg{Screen: NewWindowsMDMOMAURIFormScreen(draft)}
	}
}

func (s *WindowsMDMCSPListScreen) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(s.filter.Value()))
	if q == "" {
		// Fresh slice, never an alias of s.all — same regression guard
		// as the Apple list.
		s.filtered = append([]windows_mdm.Setting(nil), s.all...)
		s.clampCursor()
		return
	}
	s.filtered = nil
	for _, item := range s.all {
		hay := strings.ToLower(item.Area + "/" + item.Name + " " + item.URI + " " + item.Description)
		if strings.Contains(hay, q) {
			s.filtered = append(s.filtered, item)
		}
	}
	s.clampCursor()
}

func (s *WindowsMDMCSPListScreen) clampCursor() {
	if s.cursor >= len(s.filtered) {
		s.cursor = len(s.filtered) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

func (s *WindowsMDMCSPListScreen) View() string {
	if s.loading {
		return fmt.Sprintf("%s Loading Policy CSP catalog… (first run downloads Microsoft's %s snapshot, ~700KB)",
			s.spinner.View(), windows_mdm.SnapshotName)
	}
	if s.err != "" {
		return style.Error.Render("Error loading catalog: "+s.err) + "\n\n" +
			style.Subtitle.Render("r retry · Esc back")
	}

	var b strings.Builder
	if s.filtering {
		b.WriteString(s.filter.View())
	} else {
		draftNote := ""
		if n := len(s.draft.settings); n > 0 {
			draftNote = fmt.Sprintf(" · draft: %d (c to author)", n)
		}
		b.WriteString(style.Subtitle.Render(fmt.Sprintf(
			"%d of %d settings (%s)%s · / filter · Enter detail · Esc back",
			len(s.filtered), len(s.all), s.snapshot, draftNote)))
	}
	b.WriteString("\n\n")

	const settingW, formatW, scopeW = 52, 6, 6
	b.WriteString(style.TableHeader.Render(fmt.Sprintf(
		"  %-*s  %-*s  %-*s  %-4s  %s", settingW, "SETTING", formatW, "FORMAT", scopeW, "SCOPE", "ADMX", "DESCRIPTION")))
	b.WriteString("\n")

	if len(s.filtered) == 0 {
		b.WriteString(style.DimRow.Render("  (no settings match)"))
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

	descW := s.width - settingW - formatW - scopeW - 20
	if descW < 10 {
		descW = 10
	}
	for i := start; i < end; i++ {
		item := s.filtered[i]
		admx := "no"
		if item.ADMXBacked {
			admx = "yes"
		}
		row := fmt.Sprintf("%-*s  %-*s  %-*s  %-4s  %s",
			settingW, truncateTUI(item.Area+"/"+item.Name, settingW),
			formatW, item.Format,
			scopeW, item.Scope,
			admx,
			truncateTUI(strings.ReplaceAll(item.Description, "\n", " "), descW))
		if i == s.cursor {
			b.WriteString(style.SelectedRow.Render("> " + row))
		} else {
			b.WriteString(style.NormalRow.Render("  " + row))
		}
		b.WriteString("\n")
	}
	return b.String()
}
