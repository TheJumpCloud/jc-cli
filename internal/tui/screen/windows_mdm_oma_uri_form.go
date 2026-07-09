package screen

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// WindowsMDMOMAURIFormScreen authors a JumpCloud "Custom MDM
// (OMA-URI)" policy from the browse-screen draft (KLA-462). Unlike
// the Apple form (one payload, one schema, many keys) this is a
// multi-row builder: each drafted CSP setting is one row with one
// value — ENUM settings cycle a pick-list, numeric formats validate
// inline against the DDF range, booleans toggle. Ctrl-S validates the
// whole draft through the same NormalizeAndValidateSettings the CLI
// and MCP use, then previews and creates via the KLA-459 bridge.
type WindowsMDMOMAURIFormScreen struct {
	draft     *windowsMDMDraft
	nameInput textinput.Model
	rows      []windowsOMAURIRow
	focusIdx  int // 0 = name input, 1..len(rows) = rows[i-1]

	stage formStage

	// mode selects between create (POST /policies) and edit
	// (PUT /policies/{editPolicyID}) — set by
	// NewWindowsMDMOMAURIFormScreenForEdit.
	mode         formMode
	editPolicyID string

	preview      viewport.Model
	previewReady bool
	// normalized is the validated, alias-canonicalized settings list
	// the preview showed and createCmd ships. Set by submit().
	normalized []windows_mdm.OMAURISetting

	spinner    spinner.Model
	createErr  string
	policyID   string
	policyName string

	width, height int
}

// windowsOMAURIRowKind picks the editor widget for one draft row.
type windowsOMAURIRowKind int

const (
	windowsRowKindText windowsOMAURIRowKind = iota
	windowsRowKindEnum
	windowsRowKindBool
)

// windowsOMAURIRow is one drafted setting plus its value-editing
// state.
type windowsOMAURIRow struct {
	setting windows_mdm.Setting
	kind    windowsOMAURIRowKind

	// Text-style value (chr / int / float / xml / b64).
	text textinput.Model
	// err is the live inline validation error.
	err string

	// Bool toggle state (Format == "bool").
	boolValue bool

	// ENUM pick-list.
	options     []windows_mdm.EnumValue
	selectedIdx int
}

// windowsMDMCreateMsg carries the POST result back to whichever
// Windows form screen started it (OMA-URI or registry).
type windowsMDMCreateMsg struct {
	policyID   string
	policyName string
	err        error
}

// newV2ClientForWindowsMDM is overridable for tests.
var newV2ClientForWindowsMDM = api.NewV2Client

// NewWindowsMDMOMAURIFormScreen builds the form over the shared
// draft. Row values seed from the setting's default (or first enum
// value), matching `csp template`.
func NewWindowsMDMOMAURIFormScreen(draft *windowsMDMDraft) *WindowsMDMOMAURIFormScreen {
	name := textinput.New()
	name.Placeholder = "e.g. " + suggestWindowsPolicyName(draft)
	name.CharLimit = 128

	rows := make([]windowsOMAURIRow, 0, len(draft.settings))
	for _, setting := range draft.settings {
		rows = append(rows, buildWindowsOMAURIRow(setting))
	}

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner

	s := &WindowsMDMOMAURIFormScreen{
		draft:     draft,
		nameInput: name,
		rows:      rows,
		spinner:   sp,
	}
	s.refocus()
	return s
}

func buildWindowsOMAURIRow(setting windows_mdm.Setting) windowsOMAURIRow {
	r := windowsOMAURIRow{setting: setting}
	seed := windows_mdm.TemplateSetting(setting).Value

	if av := setting.AllowedValues; av != nil && len(av.Enum) > 0 {
		r.kind = windowsRowKindEnum
		r.options = av.Enum
		for i, opt := range av.Enum {
			if opt.Value == seed {
				r.selectedIdx = i
				break
			}
		}
		return r
	}
	if setting.Format == "bool" {
		r.kind = windowsRowKindBool
		r.boolValue = strings.EqualFold(seed, "true")
		return r
	}
	r.kind = windowsRowKindText
	r.text = textinput.New()
	r.text.CharLimit = 512
	r.text.SetValue(seed)
	return r
}

// suggestWindowsPolicyName derives a readable default from the first
// drafted setting, mirroring the Apple form's suggestPolicyName.
func suggestWindowsPolicyName(draft *windowsMDMDraft) string {
	if len(draft.settings) == 0 {
		return "Windows custom policy"
	}
	first := draft.settings[0]
	name := first.Area + " " + first.Name
	if len(draft.settings) > 1 {
		name += fmt.Sprintf(" +%d", len(draft.settings)-1)
	}
	return name
}

// NewWindowsMDMOMAURIFormScreenForEdit builds the form pre-populated
// from a decoded policy; submit routes to PUT /policies/{id}. Each
// decoded entry is rehydrated through the catalog by URI so enum
// pick-lists and range validation come back; entries whose URI isn't
// in the catalog (standalone CSPs, hand-authored paths) fall back to
// plain text rows — edited verbatim, never dropped. cat may be nil
// (snapshot fetch failed): everything falls back to text rows.
func NewWindowsMDMOMAURIFormScreenForEdit(decoded windows_mdm.DecodedPolicy, cat *windows_mdm.Catalog) *WindowsMDMOMAURIFormScreen {
	// Edit mode gets a private draft: the rows come from the decoded
	// policy, not from the browse screen's pick-flow, and clearing it
	// on success must not disturb any in-flight create draft.
	draft := &windowsMDMDraft{}
	rows := make([]windowsOMAURIRow, 0, len(decoded.Settings))
	for _, entry := range decoded.Settings {
		setting, ok := windows_mdm.Setting{}, false
		if cat != nil {
			setting, ok = cat.ByURI(entry.URI)
		}
		if !ok {
			// Synthesized minimal setting — keeps the row editable
			// with the stored format even without catalog metadata.
			setting = windows_mdm.Setting{
				Area:   "(not in catalog)",
				Name:   entry.URI,
				URI:    entry.URI,
				Format: entry.Format,
				Scope:  "device",
			}
		}
		draft.settings = append(draft.settings, setting)
		row := buildWindowsOMAURIRow(setting)
		// Override the seed with the policy's stored value.
		switch row.kind {
		case windowsRowKindEnum:
			matched := false
			for i, opt := range row.options {
				if opt.Value == entry.Value {
					row.selectedIdx = i
					matched = true
					break
				}
			}
			if !matched {
				// Catalog drift: the stored value isn't among the
				// catalog's enum options. Silently landing on the
				// default option would MUTATE the policy on save —
				// degrade to a text row carrying the stored value
				// verbatim instead, same philosophy as a catalog
				// miss (CodeRabbit PR #68 review).
				row = windowsOMAURIRow{setting: setting, kind: windowsRowKindText}
				row.text = textinput.New()
				row.text.CharLimit = 512
				row.text.SetValue(entry.Value)
			}
		case windowsRowKindBool:
			row.boolValue = strings.EqualFold(entry.Value, "true")
		default:
			row.text.SetValue(entry.Value)
		}
		rows = append(rows, row)
	}

	s := NewWindowsMDMOMAURIFormScreen(draft)
	s.rows = rows
	s.mode = formModeEdit
	s.editPolicyID = decoded.PolicyID
	s.nameInput.SetValue(decoded.PolicyName)
	s.policyName = decoded.PolicyName
	s.refocus()
	return s
}

func (s *WindowsMDMOMAURIFormScreen) Title() string {
	if s.mode == formModeEdit {
		return "Edit: Windows OMA-URI policy"
	}
	return "Author: Windows OMA-URI policy"
}

func (s *WindowsMDMOMAURIFormScreen) TextInputActive() bool {
	if s.stage != mdmFormStageEdit {
		return false
	}
	if s.focusIdx == 0 {
		return true
	}
	idx := s.focusIdx - 1
	return idx >= 0 && idx < len(s.rows) && s.rows[idx].kind == windowsRowKindText
}

func (s *WindowsMDMOMAURIFormScreen) Init() tea.Cmd { return textinput.Blink }

func (s *WindowsMDMOMAURIFormScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		return s.handleCreateFinished(m)

	case tea.KeyMsg:
		return s.handleKey(m)
	}
	return s, nil
}

func (s *WindowsMDMOMAURIFormScreen) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s.stage {
	case mdmFormStageEdit:
		return s.handleKeyEdit(msg)
	case mdmFormStagePreview:
		switch msg.String() {
		case "esc":
			s.stage = mdmFormStageEdit
			return s, nil
		case "c":
			return s.startCreate()
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

func (s *WindowsMDMOMAURIFormScreen) handleKeyEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return s, func() tea.Msg { return tui.PopScreenMsg{} }
	case "ctrl+s":
		return s.submit()
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

// removeFocusedRow drops the focused setting from BOTH the form and
// the shared draft, so going back to the browse screen reflects the
// removal.
func (s *WindowsMDMOMAURIFormScreen) removeFocusedRow() (tea.Model, tea.Cmd) {
	idx := s.focusIdx - 1
	if idx < 0 || idx >= len(s.rows) {
		return s, nil
	}
	uri := s.rows[idx].setting.URI
	s.rows = append(s.rows[:idx], s.rows[idx+1:]...)
	for i, drafted := range s.draft.settings {
		if drafted.URI == uri {
			s.draft.settings = append(s.draft.settings[:i], s.draft.settings[i+1:]...)
			break
		}
	}
	if s.focusIdx > len(s.rows) {
		s.focusIdx = len(s.rows)
	}
	s.refocus()
	if len(s.rows) == 0 {
		// Empty form has nothing left to author — pop back to browse.
		return s, func() tea.Msg { return tui.PopScreenMsg{} }
	}
	return s, nil
}

func (s *WindowsMDMOMAURIFormScreen) routeFocused(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if s.focusIdx == 0 {
		var cmd tea.Cmd
		s.nameInput, cmd = s.nameInput.Update(msg)
		return s, cmd
	}
	idx := s.focusIdx - 1
	if idx < 0 || idx >= len(s.rows) {
		return s, nil
	}
	r := &s.rows[idx]
	switch r.kind {
	case windowsRowKindText:
		var cmd tea.Cmd
		r.text, cmd = r.text.Update(msg)
		r.err = validateWindowsRowInline(r)
		return s, cmd
	case windowsRowKindBool:
		switch msg.String() {
		case " ", "space", "enter":
			r.boolValue = !r.boolValue
		}
		return s, nil
	case windowsRowKindEnum:
		switch msg.String() {
		case "left", "h":
			if r.selectedIdx > 0 {
				r.selectedIdx--
			}
		case "right", "l", " ", "space":
			if r.selectedIdx < len(r.options)-1 {
				r.selectedIdx++
			}
		}
		return s, nil
	}
	return s, nil
}

func (s *WindowsMDMOMAURIFormScreen) advanceFocus(delta int) {
	n := len(s.rows) + 1
	s.focusIdx = (s.focusIdx + delta + n) % n
	s.refocus()
}

func (s *WindowsMDMOMAURIFormScreen) refocus() {
	s.nameInput.Blur()
	for i := range s.rows {
		if s.rows[i].kind == windowsRowKindText {
			s.rows[i].text.Blur()
		}
	}
	if s.focusIdx == 0 {
		s.nameInput.Focus()
		return
	}
	idx := s.focusIdx - 1
	if idx >= 0 && idx < len(s.rows) && s.rows[idx].kind == windowsRowKindText {
		s.rows[idx].text.Focus()
	}
}

// rowValue reads the current wire value for a row.
func rowValue(r windowsOMAURIRow) string {
	switch r.kind {
	case windowsRowKindEnum:
		if r.selectedIdx >= 0 && r.selectedIdx < len(r.options) {
			return r.options[r.selectedIdx].Value
		}
		return ""
	case windowsRowKindBool:
		if r.boolValue {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(r.text.Value())
	}
}

// validateWindowsRowInline gives per-keystroke feedback for text rows:
// int/float parse checks plus the DDF Range bound when declared.
func validateWindowsRowInline(r *windowsOMAURIRow) string {
	v := strings.TrimSpace(r.text.Value())
	if v == "" {
		return "" // required-check fires on submit
	}
	switch r.setting.Format {
	case "int":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return "Not a whole number."
		}
		return windowsRangeErr(r.setting, float64(n))
	case "float":
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "Not a number."
		}
		return windowsRangeErr(r.setting, x)
	}
	return ""
}

// ddfRangePattern matches the DDF Range constraint form "[min-max]".
var ddfRangePattern = regexp.MustCompile(`^\[(-?\d+)-(-?\d+)\]$`)

func windowsRangeErr(setting windows_mdm.Setting, v float64) string {
	av := setting.AllowedValues
	if av == nil || av.Type != "Range" {
		return ""
	}
	m := ddfRangePattern.FindStringSubmatch(av.Value)
	if m == nil {
		return ""
	}
	min, _ := strconv.ParseFloat(m[1], 64)
	max, _ := strconv.ParseFloat(m[2], 64)
	if v < min {
		return fmt.Sprintf("Below minimum (%s).", m[1])
	}
	if v > max {
		return fmt.Sprintf("Above maximum (%s).", m[2])
	}
	return ""
}

// submit validates the whole draft through the shared
// NormalizeAndValidateSettings and either surfaces per-row errors or
// moves to the preview stage.
func (s *WindowsMDMOMAURIFormScreen) submit() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(s.nameInput.Value())
	if name == "" {
		name = suggestWindowsPolicyName(s.draft)
		s.nameInput.SetValue(name)
	}
	s.policyName = name

	// Clear stale errors, then re-run BOTH the inline pass and the
	// aggregate validator — same both-passes discipline the Apple form
	// settled on (Bugbot PR #53 re-review).
	for i := range s.rows {
		s.rows[i].err = ""
	}
	hasErr := false
	settings := make([]windows_mdm.OMAURISetting, len(s.rows))
	for i := range s.rows {
		r := &s.rows[i]
		if r.kind == windowsRowKindText {
			r.err = validateWindowsRowInline(r)
			if r.err != "" {
				hasErr = true
			}
		}
		settings[i] = windows_mdm.OMAURISetting{
			URI:    r.setting.URI,
			Format: r.setting.Format,
			Value:  rowValue(*r),
		}
	}

	normalized, err := windows_mdm.NormalizeAndValidateSettings(settings)
	if err != nil {
		// The aggregate error names rows as "setting N: ..." — map
		// each line back onto its row, ACCUMULATING with "; " like the
		// registry form so every problem for a row surfaces in one
		// pass instead of one fix/resubmit cycle each (CodeRabbit
		// PR #67 review). An inline numeric error stays first.
		for _, line := range strings.Split(err.Error(), "\n") {
			line = strings.TrimSpace(line)
			for i := range s.rows {
				prefix := fmt.Sprintf("setting %d:", i+1)
				if strings.HasPrefix(line, prefix) {
					msg := strings.TrimSpace(strings.TrimPrefix(line, prefix))
					if s.rows[i].err != "" {
						s.rows[i].err += "; "
					}
					s.rows[i].err += msg
				}
			}
		}
		hasErr = true
	}
	if hasErr {
		return s, nil
	}

	s.normalized = normalized
	s.preview.SetContent(s.renderPreviewBody())
	s.preview.GotoTop()
	s.stage = mdmFormStagePreview
	return s, nil
}

// renderPreviewBody shows what will ship: the policy name and every
// normalized {uri, format, value} triple — the plan-mode analog.
func (s *WindowsMDMOMAURIFormScreen) renderPreviewBody() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Policy: %s\n", s.policyName)
	fmt.Fprintf(&b, "Template: %s (resolved on create)\n", windows_mdm.TemplateNameOMAURI)
	fmt.Fprintf(&b, "OMA-URI settings (%d):\n\n", len(s.normalized))
	for _, setting := range s.normalized {
		fmt.Fprintf(&b, "  %s\n    = %s (%s)\n", setting.URI, setting.Value, setting.Format)
	}
	return b.String()
}

func (s *WindowsMDMOMAURIFormScreen) startCreate() (tea.Model, tea.Cmd) {
	s.stage = mdmFormStageCreating
	return s, tea.Batch(s.spinner.Tick, s.createCmd())
}

func (s *WindowsMDMOMAURIFormScreen) createCmd() tea.Cmd {
	policyName := s.policyName
	settings := s.normalized
	mode := s.mode
	editID := s.editPolicyID
	return func() tea.Msg {
		client, err := newV2ClientForWindowsMDM()
		if err != nil {
			return windowsMDMCreateMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		ctx := context.Background()
		tmpl, err := windows_mdm.ResolveOMAURITemplate(ctx, client)
		if err != nil {
			return windowsMDMCreateMsg{err: fmt.Errorf("resolving template: %w", err)}
		}
		body := windows_mdm.BuildOMAURIPolicyBody(policyName, tmpl, settings)
		var raw []byte
		if mode == formModeEdit {
			raw, err = client.Update(ctx, "/policies/"+editID, body)
		} else {
			raw, err = client.Create(ctx, "/policies", body)
		}
		if err != nil {
			return windowsMDMCreateMsg{err: err}
		}
		id, name := extractPolicyIDName(raw)
		return windowsMDMCreateMsg{policyID: id, policyName: name}
	}
}

func (s *WindowsMDMOMAURIFormScreen) handleCreateFinished(msg windowsMDMCreateMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		s.createErr = msg.err.Error()
		s.stage = mdmFormStageFailed
		return s, nil
	}
	s.policyID = msg.policyID
	if msg.policyName != "" {
		s.policyName = msg.policyName
	}
	s.stage = mdmFormStageSuccess
	// The draft shipped; clear it so the browse screen starts fresh.
	s.draft.settings = nil
	return s, nil
}

// ── Rendering ──────────────────────────────────────────────────────

func (s *WindowsMDMOMAURIFormScreen) View() string {
	switch s.stage {
	case mdmFormStageEdit:
		return s.viewEdit()
	case mdmFormStagePreview:
		hint := style.Subtitle.Render("Preview · c to create policy · Esc back to form · j/k scroll")
		return s.preview.View() + "\n" + hint
	case mdmFormStageCreating:
		verb := "Creating"
		if s.mode == formModeEdit {
			verb = "Updating"
		}
		return fmt.Sprintf("%s %s JumpCloud policy %q…", s.spinner.View(), verb, s.policyName)
	case mdmFormStageSuccess:
		var b strings.Builder
		verb := "Policy created."
		if s.mode == formModeEdit {
			verb = "Policy updated."
		}
		fmt.Fprintln(&b, style.Success.Render(verb))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  ID:   "+s.policyID)
		fmt.Fprintln(&b, "  Name: "+s.policyName)
		fmt.Fprintln(&b, "  Kind: Custom MDM (OMA-URI), device-scoped")
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

func (s *WindowsMDMOMAURIFormScreen) viewEdit() string {
	var b strings.Builder
	fmt.Fprintln(&b, style.Subtitle.Render(fmt.Sprintf(
		"Custom MDM (OMA-URI) policy — %d setting(s)", len(s.rows))))
	fmt.Fprintln(&b)
	fmt.Fprint(&b, mdmFocusMarker(s.focusIdx == 0), " Policy name: ", s.nameInput.View(), "\n\n")

	for i := range s.rows {
		s.renderRow(&b, i)
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.Subtitle.Render(
		"Tab/↑↓ focus · Space toggle/cycle · Ctrl-D remove row · Ctrl-S submit · Esc cancel"))
	return b.String()
}

func (s *WindowsMDMOMAURIFormScreen) renderRow(b *strings.Builder, idx int) {
	r := s.rows[idx]
	focused := s.focusIdx == idx+1
	marker := mdmFocusMarker(focused)

	label := truncateTUI(r.setting.Area+"/"+r.setting.Name, 44)
	fmt.Fprintf(b, "%s %-44s  %s", marker, label, renderWindowsRowValue(r, focused))
	if r.setting.Scope == "user" {
		fmt.Fprintf(b, "  %s", style.Error.Render("(user-scoped)"))
	}
	if r.err != "" {
		fmt.Fprintf(b, "  %s", style.Error.Render(r.err))
	}
	fmt.Fprintln(b)
}

func renderWindowsRowValue(r windowsOMAURIRow, focused bool) string {
	switch r.kind {
	case windowsRowKindText:
		if focused {
			return r.text.View()
		}
		v := r.text.Value()
		if v == "" {
			return "(empty)"
		}
		return v
	case windowsRowKindBool:
		if r.boolValue {
			return "[true]"
		}
		return "[false]"
	case windowsRowKindEnum:
		var parts []string
		for i, opt := range r.options {
			if i == r.selectedIdx {
				parts = append(parts, "["+opt.Value+"]")
			} else {
				parts = append(parts, opt.Value)
			}
		}
		line := strings.Join(parts, "  ")
		if r.selectedIdx >= 0 && r.selectedIdx < len(r.options) && r.options[r.selectedIdx].Description != "" {
			line += "  " + style.Subtitle.Render("— "+truncateTUI(r.options[r.selectedIdx].Description, 48))
		}
		return line
	}
	return ""
}
