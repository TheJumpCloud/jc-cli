package screen

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// WindowsMDMRegistryFormScreen authors a JumpCloud "Advanced: Custom
// Registry Keys" policy (KLA-462) — a free-form row editor (no
// catalog behind it; registry keys are operator knowledge). Each row
// is location / value name / type / data; the type cycles through
// windows_mdm.RegistryRegTypes(). Ctrl-S validates every row through
// the same NormalizeAndValidateKeys the CLI and MCP use (hive-prefix
// rejection, 32-bit DWORD range, length limits), then previews and
// creates via the KLA-459 bridge.
type WindowsMDMRegistryFormScreen struct {
	nameInput textinput.Model
	rows      []windowsRegistryRow

	// focusIdx: 0 = policy name; then rows are 3 inputs + 1 cycle
	// each, addressed as 1 + row*4 + sub (sub: 0 location, 1 value
	// name, 2 type, 3 data).
	focusIdx int

	stage formStage

	preview      viewport.Model
	previewReady bool
	normalized   []windows_mdm.RegistryKey

	spinner    spinner.Model
	createErr  string
	policyID   string
	policyName string

	width, height int
}

// windowsRegistryRow is one registry key row: three text inputs and a
// type cycle.
type windowsRegistryRow struct {
	location textinput.Model
	name     textinput.Model
	typeIdx  int
	data     textinput.Model
	err      string
}

func newWindowsRegistryRow() windowsRegistryRow {
	loc := textinput.New()
	loc.Placeholder = `SOFTWARE\Policies\...`
	loc.CharLimit = 255
	name := textinput.New()
	name.Placeholder = "ValueName"
	name.CharLimit = 99
	data := textinput.New()
	data.Placeholder = "data"
	data.CharLimit = 512
	return windowsRegistryRow{location: loc, name: name, data: data}
}

// NewWindowsMDMRegistryFormScreen starts with one empty row.
func NewWindowsMDMRegistryFormScreen() *WindowsMDMRegistryFormScreen {
	name := textinput.New()
	name.Placeholder = "e.g. Disable Autorun"
	name.CharLimit = 128

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner

	s := &WindowsMDMRegistryFormScreen{
		nameInput: name,
		rows:      []windowsRegistryRow{newWindowsRegistryRow()},
		spinner:   sp,
	}
	s.refocus()
	return s
}

func (s *WindowsMDMRegistryFormScreen) Title() string { return "Author: Windows registry policy" }

// slotCount is the total number of focusable slots.
func (s *WindowsMDMRegistryFormScreen) slotCount() int { return 1 + len(s.rows)*4 }

// slotFor decodes focusIdx into (row, sub); row == -1 means the name
// input.
func (s *WindowsMDMRegistryFormScreen) slotFor(focusIdx int) (row, sub int) {
	if focusIdx <= 0 {
		return -1, 0
	}
	return (focusIdx - 1) / 4, (focusIdx - 1) % 4
}

func (s *WindowsMDMRegistryFormScreen) TextInputActive() bool {
	if s.stage != mdmFormStageEdit {
		return false
	}
	row, sub := s.slotFor(s.focusIdx)
	if row == -1 {
		return true
	}
	return sub != 2 // every sub-slot except the type cycle is a text input
}

func (s *WindowsMDMRegistryFormScreen) Init() tea.Cmd { return textinput.Blink }

func (s *WindowsMDMRegistryFormScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		if !s.previewReady {
			s.preview = viewport.New(m.Width, m.Height-4)
			s.previewReady = true
		} else {
			s.preview.Width = m.Width
			s.preview.Height = m.Height - 4
		}
		return s, nil

	case spinner.TickMsg:
		if s.stage == mdmFormStageCreating {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(m)
			return s, cmd
		}
		return s, nil

	case windowsMDMCreateMsg:
		if m.err != nil {
			s.createErr = m.err.Error()
			s.stage = mdmFormStageFailed
			return s, nil
		}
		s.policyID = m.policyID
		if m.policyName != "" {
			s.policyName = m.policyName
		}
		s.stage = mdmFormStageSuccess
		return s, nil

	case tea.KeyMsg:
		return s.handleKey(m)
	}
	return s, nil
}

func (s *WindowsMDMRegistryFormScreen) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s.stage {
	case mdmFormStageEdit:
		return s.handleKeyEdit(msg)
	case mdmFormStagePreview:
		switch msg.String() {
		case "esc":
			s.stage = mdmFormStageEdit
			return s, nil
		case "c":
			s.stage = mdmFormStageCreating
			return s, tea.Batch(s.spinner.Tick, s.createCmd())
		}
		var cmd tea.Cmd
		s.preview, cmd = s.preview.Update(msg)
		return s, cmd
	case mdmFormStageSuccess, mdmFormStageFailed:
		switch msg.String() {
		case "esc", "enter":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		}
	}
	return s, nil
}

func (s *WindowsMDMRegistryFormScreen) handleKeyEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return s, func() tea.Msg { return tui.PopScreenMsg{} }
	case "ctrl+s":
		return s.submit()
	case "ctrl+n":
		s.rows = append(s.rows, newWindowsRegistryRow())
		// Jump focus to the new row's location field.
		s.focusIdx = 1 + (len(s.rows)-1)*4
		s.refocus()
		return s, nil
	case "ctrl+d":
		return s.removeFocusedRow()
	case "tab", "down":
		s.advanceFocus(1)
		return s, nil
	case "shift+tab", "up":
		s.advanceFocus(-1)
		return s, nil
	}
	return s.routeFocused(msg)
}

func (s *WindowsMDMRegistryFormScreen) removeFocusedRow() (tea.Model, tea.Cmd) {
	row, _ := s.slotFor(s.focusIdx)
	if row < 0 || row >= len(s.rows) || len(s.rows) == 1 {
		// Keep at least one row — an empty policy can't be authored,
		// and popping the last row would strand focus.
		return s, nil
	}
	s.rows = append(s.rows[:row], s.rows[row+1:]...)
	if s.focusIdx >= s.slotCount() {
		s.focusIdx = s.slotCount() - 1
	}
	s.refocus()
	return s, nil
}

func (s *WindowsMDMRegistryFormScreen) advanceFocus(delta int) {
	n := s.slotCount()
	s.focusIdx = (s.focusIdx + delta + n) % n
	s.refocus()
}

func (s *WindowsMDMRegistryFormScreen) refocus() {
	s.nameInput.Blur()
	for i := range s.rows {
		s.rows[i].location.Blur()
		s.rows[i].name.Blur()
		s.rows[i].data.Blur()
	}
	row, sub := s.slotFor(s.focusIdx)
	if row == -1 {
		s.nameInput.Focus()
		return
	}
	if row >= len(s.rows) {
		return
	}
	switch sub {
	case 0:
		s.rows[row].location.Focus()
	case 1:
		s.rows[row].name.Focus()
	case 3:
		s.rows[row].data.Focus()
	}
}

func (s *WindowsMDMRegistryFormScreen) routeFocused(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	row, sub := s.slotFor(s.focusIdx)
	if row == -1 {
		var cmd tea.Cmd
		s.nameInput, cmd = s.nameInput.Update(msg)
		return s, cmd
	}
	if row >= len(s.rows) {
		return s, nil
	}
	r := &s.rows[row]
	var cmd tea.Cmd
	switch sub {
	case 0:
		r.location, cmd = r.location.Update(msg)
	case 1:
		r.name, cmd = r.name.Update(msg)
	case 2:
		types := windows_mdm.RegistryRegTypes()
		switch msg.String() {
		case "left", "h":
			if r.typeIdx > 0 {
				r.typeIdx--
			}
		case "right", "l", " ", "space":
			if r.typeIdx < len(types)-1 {
				r.typeIdx++
			}
		}
	case 3:
		r.data, cmd = r.data.Update(msg)
	}
	return s, cmd
}

func (s *WindowsMDMRegistryFormScreen) collectKeys() []windows_mdm.RegistryKey {
	types := windows_mdm.RegistryRegTypes()
	keys := make([]windows_mdm.RegistryKey, len(s.rows))
	for i, r := range s.rows {
		keys[i] = windows_mdm.RegistryKey{
			Location:  strings.TrimSpace(r.location.Value()),
			ValueName: strings.TrimSpace(r.name.Value()),
			RegType:   types[r.typeIdx],
			Data:      strings.TrimSpace(r.data.Value()),
		}
	}
	return keys
}

func (s *WindowsMDMRegistryFormScreen) submit() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(s.nameInput.Value())
	if name == "" {
		name = "Windows registry policy"
		s.nameInput.SetValue(name)
	}
	s.policyName = name

	for i := range s.rows {
		s.rows[i].err = ""
	}
	normalized, err := windows_mdm.NormalizeAndValidateKeys(s.collectKeys())
	if err != nil {
		// Aggregate error lines name rows as "key N: ..." — map back.
		for _, line := range strings.Split(err.Error(), "\n") {
			line = strings.TrimSpace(line)
			for i := range s.rows {
				prefix := fmt.Sprintf("key %d:", i+1)
				if strings.HasPrefix(line, prefix) {
					if s.rows[i].err != "" {
						s.rows[i].err += "; "
					}
					s.rows[i].err += strings.TrimSpace(strings.TrimPrefix(line, prefix))
				}
			}
		}
		return s, nil
	}

	s.normalized = normalized
	var b strings.Builder
	fmt.Fprintf(&b, "Policy: %s\n", s.policyName)
	fmt.Fprintf(&b, "Template: %s (resolved on create)\n", windows_mdm.TemplateNameRegistry)
	fmt.Fprintf(&b, "Registry keys (%d, all under HKLM):\n\n", len(normalized))
	for _, k := range normalized {
		fmt.Fprintf(&b, "  %s\\%s\n    = %s (%s)\n", k.Location, k.ValueName, k.Data, k.RegType)
	}
	s.preview.SetContent(b.String())
	s.preview.GotoTop()
	s.stage = mdmFormStagePreview
	return s, nil
}

func (s *WindowsMDMRegistryFormScreen) createCmd() tea.Cmd {
	policyName := s.policyName
	keys := s.normalized
	return func() tea.Msg {
		client, err := newV2ClientForWindowsMDM()
		if err != nil {
			return windowsMDMCreateMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		ctx := context.Background()
		tmpl, err := windows_mdm.ResolveRegistryTemplate(ctx, client)
		if err != nil {
			return windowsMDMCreateMsg{err: fmt.Errorf("resolving template: %w", err)}
		}
		body := windows_mdm.BuildRegistryPolicyBody(policyName, tmpl, keys)
		raw, err := client.Create(ctx, "/policies", body)
		if err != nil {
			return windowsMDMCreateMsg{err: err}
		}
		id, name := extractPolicyIDName(raw)
		return windowsMDMCreateMsg{policyID: id, policyName: name}
	}
}

// ── Rendering ──────────────────────────────────────────────────────

func (s *WindowsMDMRegistryFormScreen) View() string {
	switch s.stage {
	case mdmFormStageEdit:
		return s.viewEdit()
	case mdmFormStagePreview:
		hint := style.Subtitle.Render("Preview · c to create policy · Esc back to form · j/k scroll")
		return s.preview.View() + "\n" + hint
	case mdmFormStageCreating:
		return fmt.Sprintf("%s Creating JumpCloud policy %q…", s.spinner.View(), s.policyName)
	case mdmFormStageSuccess:
		var b strings.Builder
		fmt.Fprintln(&b, style.Success.Render("Policy created."))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  ID:   "+s.policyID)
		fmt.Fprintln(&b, "  Name: "+s.policyName)
		fmt.Fprintln(&b, "  Kind: Custom Registry Keys (HKLM), device-scoped")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("Esc / Enter to dismiss"))
		return b.String()
	case mdmFormStageFailed:
		var b strings.Builder
		fmt.Fprintln(&b, style.Error.Render("Failed:"))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  "+s.createErr)
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("Esc to dismiss"))
		return b.String()
	}
	return ""
}

func (s *WindowsMDMRegistryFormScreen) viewEdit() string {
	types := windows_mdm.RegistryRegTypes()
	var b strings.Builder
	fmt.Fprintln(&b, style.Subtitle.Render(fmt.Sprintf(
		"Custom Registry Keys policy — %d row(s), all under HKEY_LOCAL_MACHINE (hive implied)", len(s.rows))))
	fmt.Fprintln(&b)
	fmt.Fprint(&b, mdmFocusMarker(s.focusIdx == 0), " Policy name: ", s.nameInput.View(), "\n\n")

	for i, r := range s.rows {
		base := 1 + i*4
		fmt.Fprintln(&b, style.SectionHeader.Render(fmt.Sprintf("Key %d", i+1)))
		fmt.Fprintf(&b, "%s Location:   %s\n", mdmFocusMarker(s.focusIdx == base), s.renderTextSlot(r.location, s.focusIdx == base))
		fmt.Fprintf(&b, "%s Value name: %s\n", mdmFocusMarker(s.focusIdx == base+1), s.renderTextSlot(r.name, s.focusIdx == base+1))

		var parts []string
		for j, t := range types {
			if j == r.typeIdx {
				parts = append(parts, "["+t+"]")
			} else {
				parts = append(parts, t)
			}
		}
		fmt.Fprintf(&b, "%s Type:       %s\n", mdmFocusMarker(s.focusIdx == base+2), strings.Join(parts, "  "))
		fmt.Fprintf(&b, "%s Data:       %s\n", mdmFocusMarker(s.focusIdx == base+3), s.renderTextSlot(r.data, s.focusIdx == base+3))
		if r.err != "" {
			fmt.Fprintln(&b, "  "+style.Error.Render(r.err))
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, style.Subtitle.Render(
		"Tab/↑↓ focus · Space cycle type · Ctrl-N add key · Ctrl-D remove key · Ctrl-S submit · Esc cancel"))
	return b.String()
}

func (s *WindowsMDMRegistryFormScreen) renderTextSlot(ti textinput.Model, focused bool) string {
	if focused {
		return ti.View()
	}
	v := ti.Value()
	if v == "" {
		return style.DimRow.Render("(empty)")
	}
	return v
}
