package screen

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// WindowsMDMCSPShowScreen renders one Policy CSP setting in full —
// the TUI face of `jc windows-mdm csp show`. `a` adds the setting to
// the shared policy draft (the KLA-462 discover→author loop); ADMX-
// backed settings refuse loudly because their values need ADMX-style
// XML the form deliberately doesn't author in v1.
type WindowsMDMCSPShowScreen struct {
	setting windows_mdm.Setting
	draft   *windowsMDMDraft

	// note is the inline feedback line after an add attempt (added /
	// already drafted / ADMX refusal).
	note      string
	noteIsErr bool

	width, height int
}

func NewWindowsMDMCSPShowScreen(setting windows_mdm.Setting, draft *windowsMDMDraft) *WindowsMDMCSPShowScreen {
	return &WindowsMDMCSPShowScreen{setting: setting, draft: draft}
}

func (s *WindowsMDMCSPShowScreen) Title() string {
	return "CSP setting: " + s.setting.Area + "/" + s.setting.Name
}

func (s *WindowsMDMCSPShowScreen) Init() tea.Cmd { return nil }

func (s *WindowsMDMCSPShowScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "a":
			return s.addToDraft()
		case "c":
			if len(s.draft.settings) == 0 {
				s.note, s.noteIsErr = "Draft is empty — press a to add this setting first", true
				return s, nil
			}
			draft := s.draft
			return s, func() tea.Msg {
				return tui.PushScreenMsg{Screen: NewWindowsMDMOMAURIFormScreen(draft)}
			}
		}
	}
	return s, nil
}

// addToDraft enforces the v1 authoring boundary: ADMX-backed settings
// are browse-only (their values are ADMX-style XML blobs — a freeform
// XML field invites broken policies), and user-scoped settings warn
// because JumpCloud's Custom MDM (OMA-URI) template is device-scoped.
func (s *WindowsMDMCSPShowScreen) addToDraft() (tea.Model, tea.Cmd) {
	if s.setting.ADMXBacked {
		s.note = "ADMX-backed settings are browse-only in the TUI — their values are ADMX-style XML. Use `jc windows-mdm oma-uri create-policy` with a hand-authored xml value if you need this one."
		s.noteIsErr = true
		return s, nil
	}
	if s.setting.RequiresInstance {
		// The draft form edits values, not URIs — an {instance}
		// placeholder could never be substituted in-form and would be
		// refused at create anyway. Route to the CLI where the uri is
		// editable.
		s.note = "This setting's URI contains {instance} (a dynamic node name). The TUI form can't substitute it — use `jc windows-mdm csp template` + edit the uri, then `oma-uri create-policy --settings-file`."
		s.noteIsErr = true
		return s, nil
	}
	if !s.draft.add(s.setting) {
		s.note, s.noteIsErr = "Already in the draft.", false
		return s, nil
	}
	s.note = fmt.Sprintf("Added — draft now has %d setting(s). Press c to author the policy.", len(s.draft.settings))
	s.noteIsErr = false
	if s.setting.Scope == "user" {
		s.note += " Note: this setting is user-scoped; JumpCloud's template is device-scoped and may not apply it."
	}
	return s, nil
}

func (s *WindowsMDMCSPShowScreen) View() string {
	var b strings.Builder
	st := s.setting

	fmt.Fprintln(&b, style.Subtitle.Render(st.Area+"/"+st.Name))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  OMA-URI:  "+st.URI)
	fmt.Fprintln(&b, "  Format:   "+st.Format)
	fmt.Fprintln(&b, "  Scope:    "+st.Scope)
	if st.DefaultValue != "" {
		fmt.Fprintln(&b, "  Default:  "+st.DefaultValue)
	}
	if st.MinOSBuild != "" {
		fmt.Fprintln(&b, "  Min OS:   "+st.MinOSBuild)
	}
	if st.Kind == windows_mdm.KindStandaloneCSP {
		fmt.Fprintln(&b, "  Kind:     standalone CSP")
	}
	if st.RequiresInstance {
		fmt.Fprintln(&b, "  "+style.Error.Render("URI contains {instance} — substitute the real node name in the form before creating"))
	}
	if st.ADMXBacked {
		fmt.Fprintln(&b, "  "+style.Error.Render("ADMX-backed — value must be ADMX-style XML; browse-only here"))
	}
	if st.Deprecated {
		fmt.Fprintln(&b, "  "+style.Error.Render("Deprecated by Microsoft"))
	}
	if st.Description != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  "+wrapTUIText(st.Description, s.width-4))
	}
	if av := st.AllowedValues; av != nil {
		switch {
		case len(av.Enum) > 0:
			fmt.Fprintln(&b)
			fmt.Fprintln(&b, style.SectionHeader.Render(fmt.Sprintf("Allowed values (%s)", av.Type)))
			for _, e := range av.Enum {
				fmt.Fprintf(&b, "    %-8s %s\n", e.Value, e.Description)
			}
		case av.Value != "":
			fmt.Fprintln(&b)
			fmt.Fprintf(&b, "  Allowed values (%s): %s\n", av.Type, av.Value)
		}
	}

	fmt.Fprintln(&b)
	if s.note != "" {
		if s.noteIsErr {
			fmt.Fprintln(&b, style.Error.Render(s.note))
		} else {
			fmt.Fprintln(&b, style.Success.Render(s.note))
		}
		fmt.Fprintln(&b)
	}
	draftNote := ""
	if n := len(s.draft.settings); n > 0 {
		draftNote = fmt.Sprintf(" · draft: %d", n)
	}
	fmt.Fprintln(&b, style.Subtitle.Render("a add to draft · c author draft"+draftNote+" · Esc back"))
	return b.String()
}

// wrapTUIText soft-wraps prose to the given width, preserving words.
// The DDF descriptions run to several hundred characters on one line.
func wrapTUIText(text string, width int) string {
	if width < 20 {
		width = 76
	}
	words := strings.Fields(text)
	var lines []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len() > 0 && cur.Len()+1+len(w) > width {
			lines = append(lines, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return strings.Join(lines, "\n  ")
}
