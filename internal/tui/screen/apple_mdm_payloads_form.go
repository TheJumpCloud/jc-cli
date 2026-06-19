package screen

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// AppleMDMPayloadsFormScreen replaces the $EDITOR flow for scalar
// authoring. Each Apple schema key renders as a typed input — text
// for strings, [x]/[ ] for booleans, numeric input with range
// validation for integers/reals, an h/l-cycle option row for
// rangelist enums. Complex types (dictionary, array) surface as
// disabled rows with a hint to drop into $EDITOR via Ctrl-E.
//
// Scope is deliberately tight (v1, KLA-454 scalar slice): no nested
// form expansion, no array-of-dict authoring, no live `valuetype`
// regex validation. Power-user shapes route through the existing
// editor screen via Ctrl-E.
type AppleMDMPayloadsFormScreen struct {
	payload   apple_mdm.Payload
	nameInput textinput.Model
	fields    []mdmFormField
	focusIdx  int // 0 = name input, 1..len(fields)+1 = fields[i-1]

	stage formStage

	// mode selects between create (POST /policies) and edit
	// (PUT /policies/{editPolicyID}). create is the default for the
	// detail-screen `n` flow; edit is set by
	// NewAppleMDMPayloadsFormScreenForEdit when an existing policy
	// is opened from the custom-MDM policy list screen.
	mode         formMode
	editPolicyID string
	// Edit-mode envelope settings preserved verbatim from the
	// decoded policy. Pre-fix (Bugbot PR #54 review) these were
	// dropped on every edit: redispatch always got rewritten to true,
	// RemovalDisallowed always got dropped from the envelope, and
	// the JumpCloud template family always resolved to darwin even
	// for iOS-family policies. Each was a silent data-loss bug.
	editRedispatch        bool
	editRemovalDisallowed bool
	editOSFamily          string

	mobileconfig []byte
	preview      viewport.Model
	previewReady bool

	spinner    spinner.Model
	createErr  string
	policyID   string
	policyName string

	width, height int
}

// formMode toggles the form between authoring a new policy and
// editing an existing one. Affects only the submit path (POST vs PUT)
// and the rendered "Creating…/Updating…" copy.
type formMode int

const (
	formModeCreate formMode = iota
	formModeEdit
)

type formStage int

const (
	mdmFormStageEdit formStage = iota
	mdmFormStagePreview
	mdmFormStageCreating
	mdmFormStageSuccess
	mdmFormStageFailed
)

// mdmFormFieldKind tells the renderer + Value() collector which underlying
// state to look at. Concrete sum type keeps the per-field logic
// readable without an interface table.
type mdmFormFieldKind int

const (
	mdmFieldKindString mdmFormFieldKind = iota
	mdmFieldKindBool
	mdmFieldKindInteger
	mdmFieldKindReal
	mdmFieldKindRangeList
	mdmFieldKindUnsupported // dictionary, array, date, data — needs editor for v1
)

// mdmFormField is one row of the form. Different kinds use different
// underlying state slots; the renderer + Update branches on Kind.
type mdmFormField struct {
	key  apple_mdm.Key
	kind mdmFormFieldKind

	// String / integer / real inputs share the textinput model.
	text textinput.Model
	// Live validation error for the current text value. Updated on
	// every keystroke so the operator sees range/type failures inline.
	err string

	// Boolean toggle.
	boolValue bool

	// Rangelist enum (strings or string-renderable values).
	options     []string
	selectedIdx int
}

// NewAppleMDMPayloadsFormScreen builds the form for a payload. Field
// initial values default to the schema's declared Default when
// present so the operator can submit minimal-effort policies.
func NewAppleMDMPayloadsFormScreen(p apple_mdm.Payload) *AppleMDMPayloadsFormScreen {
	name := textinput.New()
	name.Placeholder = "e.g. " + suggestPolicyName(p)
	name.CharLimit = 128

	fields := make([]mdmFormField, 0, len(p.Keys))
	for _, k := range p.Keys {
		fields = append(fields, buildMDMFormField(k))
	}

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner

	s := &AppleMDMPayloadsFormScreen{
		payload:   p,
		nameInput: name,
		fields:    fields,
		focusIdx:  0,
		spinner:   sp,
	}
	s.refocus()
	return s
}

func buildMDMFormField(k apple_mdm.Key) mdmFormField {
	f := mdmFormField{key: k}

	switch k.Type {
	case "string":
		if len(k.RangeList) > 0 {
			f.kind = mdmFieldKindRangeList
			f.options = make([]string, 0, len(k.RangeList))
			for _, v := range k.RangeList {
				f.options = append(f.options, fmt.Sprintf("%v", v))
			}
			if d, ok := k.Default.(string); ok {
				for i, opt := range f.options {
					if opt == d {
						f.selectedIdx = i
						break
					}
				}
			}
			return f
		}
		f.kind = mdmFieldKindString
		f.text = textinput.New()
		f.text.CharLimit = 256
		if d, ok := k.Default.(string); ok {
			f.text.SetValue(d)
		}
		return f

	case "boolean":
		f.kind = mdmFieldKindBool
		if d, ok := k.Default.(bool); ok {
			f.boolValue = d
		}
		return f

	case "integer":
		f.kind = mdmFieldKindInteger
		f.text = textinput.New()
		f.text.CharLimit = 20
		switch d := k.Default.(type) {
		case int:
			f.text.SetValue(strconv.Itoa(d))
		case int64:
			f.text.SetValue(strconv.FormatInt(d, 10))
		case float64:
			f.text.SetValue(strconv.FormatFloat(d, 'f', -1, 64))
		}
		return f

	case "real":
		f.kind = mdmFieldKindReal
		f.text = textinput.New()
		f.text.CharLimit = 32
		switch d := k.Default.(type) {
		case float64:
			f.text.SetValue(strconv.FormatFloat(d, 'f', -1, 64))
		case int:
			f.text.SetValue(strconv.Itoa(d))
		}
		return f

	default:
		// dictionary, array, date, data — unsupported in v1 scope.
		// The row renders disabled; submitting still works if the key
		// is optional, and Ctrl-E drops to the $EDITOR flow for the
		// power-user case.
		f.kind = mdmFieldKindUnsupported
		return f
	}
}

func (s *AppleMDMPayloadsFormScreen) Title() string {
	return "Author: " + s.payload.Type
}

// TextInputActive is true when any text-style field has focus, so the
// app-level Quit shortcut doesn't intercept typed characters.
func (s *AppleMDMPayloadsFormScreen) TextInputActive() bool {
	if s.stage != mdmFormStageEdit {
		return false
	}
	if s.focusIdx == 0 {
		return true
	}
	idx := s.focusIdx - 1
	if idx < 0 || idx >= len(s.fields) {
		return false
	}
	switch s.fields[idx].kind {
	case mdmFieldKindString, mdmFieldKindInteger, mdmFieldKindReal:
		return true
	}
	return false
}

func (s *AppleMDMPayloadsFormScreen) Init() tea.Cmd { return textinput.Blink }

func (s *AppleMDMPayloadsFormScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case applePolicyAuthorCreateMsg:
		return s.handleCreateFinished(m)

	case tea.KeyMsg:
		return s.handleKey(m)
	}
	return s, nil
}

func (s *AppleMDMPayloadsFormScreen) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s.stage {
	case mdmFormStageEdit:
		return s.handleKeyEdit(msg)
	case mdmFormStagePreview:
		return s.handleKeyPreview(msg)
	case mdmFormStageSuccess, mdmFormStageFailed:
		switch msg.String() {
		case "esc", "enter":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		}
	}
	return s, nil
}

func (s *AppleMDMPayloadsFormScreen) handleKeyEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return s, func() tea.Msg { return tui.PopScreenMsg{} }
	case "ctrl+e":
		// Switch to $EDITOR-based authoring for the same payload.
		// The current form state is dropped — the editor scaffold is
		// schema-derived rather than form-derived, which keeps the
		// crossover simple but means the operator restarts naming.
		payload := s.payload
		return s, func() tea.Msg {
			return tui.ReplaceScreenMsg{Screen: NewAppleMDMPayloadsAuthoringScreen(payload)}
		}
	case "ctrl+s":
		return s.submit()
	case "tab", "down":
		s.advanceFocus(1)
		return s, nil
	case "shift+tab", "up":
		s.advanceFocus(-1)
		return s, nil
	}
	return s.routeFocused(msg)
}

// routeFocused dispatches a key to the currently-focused input. Each
// field kind has its own handling. Name input is focusIdx==0.
func (s *AppleMDMPayloadsFormScreen) routeFocused(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if s.focusIdx == 0 {
		var cmd tea.Cmd
		s.nameInput, cmd = s.nameInput.Update(msg)
		return s, cmd
	}
	idx := s.focusIdx - 1
	if idx < 0 || idx >= len(s.fields) {
		return s, nil
	}
	f := &s.fields[idx]
	switch f.kind {
	case mdmFieldKindString:
		var cmd tea.Cmd
		f.text, cmd = f.text.Update(msg)
		return s, cmd
	case mdmFieldKindInteger, mdmFieldKindReal:
		var cmd tea.Cmd
		f.text, cmd = f.text.Update(msg)
		f.err = validateMDMFieldNumeric(f)
		return s, cmd
	case mdmFieldKindBool:
		switch msg.String() {
		case " ", "space", "enter":
			f.boolValue = !f.boolValue
		}
		return s, nil
	case mdmFieldKindRangeList:
		switch msg.String() {
		case "left", "h":
			if f.selectedIdx > 0 {
				f.selectedIdx--
			}
		case "right", "l", " ", "space":
			if f.selectedIdx < len(f.options)-1 {
				f.selectedIdx++
			}
		}
		return s, nil
	}
	return s, nil
}

// advanceFocus moves between the name input and the form fields,
// skipping unsupported rows (they're disabled and have no editable
// state). Wraps around the ends.
func (s *AppleMDMPayloadsFormScreen) advanceFocus(delta int) {
	n := len(s.fields) + 1 // +1 for the name input
	for i := 0; i < n; i++ {
		s.focusIdx = (s.focusIdx + delta + n) % n
		if s.focusIdx == 0 {
			s.refocus()
			return
		}
		if s.fields[s.focusIdx-1].kind != mdmFieldKindUnsupported {
			s.refocus()
			return
		}
	}
}

// refocus updates the textinput focus state to match focusIdx. Only
// one text input is focused at a time so the cursor blink is sane.
func (s *AppleMDMPayloadsFormScreen) refocus() {
	s.nameInput.Blur()
	for i := range s.fields {
		if s.fields[i].kind == mdmFieldKindString ||
			s.fields[i].kind == mdmFieldKindInteger ||
			s.fields[i].kind == mdmFieldKindReal {
			s.fields[i].text.Blur()
		}
	}
	if s.focusIdx == 0 {
		s.nameInput.Focus()
		return
	}
	idx := s.focusIdx - 1
	if idx < 0 || idx >= len(s.fields) {
		return
	}
	switch s.fields[idx].kind {
	case mdmFieldKindString, mdmFieldKindInteger, mdmFieldKindReal:
		s.fields[idx].text.Focus()
	}
}

// submit collects values and runs CoerceAndValidate. Validation
// failures surface inline as per-field errors; the screen stays on
// the edit stage with the broken fields highlighted. Otherwise emit
// the mobileconfig and transition to preview.
func (s *AppleMDMPayloadsFormScreen) submit() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(s.nameInput.Value())
	if name == "" {
		name = suggestPolicyName(s.payload)
		s.nameInput.SetValue(name)
	}
	s.policyName = name

	// Clear stale inline errors from any prior submit attempt before
	// re-validating. Without this, Esc-from-preview back to the form
	// can leave old errors visible on fields that have since become
	// valid, suggesting the policy is still broken (Bugbot PR #53
	// review).
	for i := range s.fields {
		s.fields[i].err = ""
	}

	// Run BOTH validation passes regardless of which one finds
	// problems first. Pre-fix (Bugbot PR #53 re-review) the numeric
	// pass returned early on any range error, so CoerceAndValidate
	// never ran — the operator only saw the numeric problem while
	// other invalid values silently blocked submission. The form
	// path is fast enough that surfacing all errors at once is
	// strictly a UX win.
	hasFieldErr := false
	for i := range s.fields {
		f := &s.fields[i]
		if f.kind == mdmFieldKindInteger || f.kind == mdmFieldKindReal {
			f.err = validateMDMFieldNumeric(f)
			if f.err != "" {
				hasFieldErr = true
			}
		}
	}

	values := s.collectValues()
	typed, err := apple_mdm.CoerceAndValidate(s.payload, values)
	if err != nil {
		msg := err.Error()
		for i := range s.fields {
			// Don't overwrite a more specific numeric error with the
			// catch-all schema error.
			if s.fields[i].err != "" {
				continue
			}
			if strings.Contains(msg, "key "+strconv.Quote(s.fields[i].key.Name)) {
				s.fields[i].err = "Invalid value (see schema)"
			}
		}
		hasFieldErr = true
	}
	if hasFieldErr {
		return s, nil
	}

	// Preserve the envelope's PayloadRemovalDisallowed in edit mode.
	// Pre-fix (Bugbot PR #54 review) the rebuilt mobileconfig omitted
	// the flag and JC silently downgraded the policy's removal lock.
	envelopeOpts := apple_mdm.EnvelopeOpts{DisplayName: s.policyName}
	if s.mode == formModeEdit {
		envelopeOpts.RemovalDisallowed = s.editRemovalDisallowed
	}
	var buf bytes.Buffer
	if err := apple_mdm.EmitMobileconfig(&buf, envelopeOpts,
		[]apple_mdm.PayloadInstance{{
			Schema:      s.payload,
			Values:      typed,
			DisplayName: s.policyName,
		}}); err != nil {
		s.createErr = err.Error()
		s.stage = mdmFormStageFailed
		return s, nil
	}
	s.mobileconfig = buf.Bytes()
	s.preview.SetContent(string(s.mobileconfig))
	s.preview.GotoTop()
	s.stage = mdmFormStagePreview
	return s, nil
}

// collectValues turns the field state into the map[string]any the
// validator expects. Empty strings and unsupported fields are
// skipped — the validator's required-key check then catches any
// required fields the operator left blank.
func (s *AppleMDMPayloadsFormScreen) collectValues() map[string]any {
	out := make(map[string]any, len(s.fields))
	for _, f := range s.fields {
		switch f.kind {
		case mdmFieldKindString:
			v := strings.TrimSpace(f.text.Value())
			if v == "" {
				continue
			}
			out[f.key.Name] = v
		case mdmFieldKindBool:
			out[f.key.Name] = f.boolValue
		case mdmFieldKindInteger:
			v := strings.TrimSpace(f.text.Value())
			if v == "" {
				continue
			}
			if n, err := strconv.Atoi(v); err == nil {
				out[f.key.Name] = n
			}
		case mdmFieldKindReal:
			v := strings.TrimSpace(f.text.Value())
			if v == "" {
				continue
			}
			if x, err := strconv.ParseFloat(v, 64); err == nil {
				out[f.key.Name] = x
			}
		case mdmFieldKindRangeList:
			if f.selectedIdx >= 0 && f.selectedIdx < len(f.options) {
				out[f.key.Name] = f.options[f.selectedIdx]
			}
		}
	}
	return out
}

// validateMDMFieldNumeric returns a short inline error for numeric
// fields that fail parse or range constraints. Used as the inline
// validation source so the operator sees feedback per keystroke.
func validateMDMFieldNumeric(f *mdmFormField) string {
	v := strings.TrimSpace(f.text.Value())
	if v == "" {
		// Empty is OK at field level; required-check fires later.
		return ""
	}
	if f.kind == mdmFieldKindInteger {
		n, err := strconv.Atoi(v)
		if err != nil {
			return "Not a whole number."
		}
		return mdmNumericRangeErr(f.key, float64(n))
	}
	if f.kind == mdmFieldKindReal {
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "Not a number."
		}
		return mdmNumericRangeErr(f.key, x)
	}
	return ""
}

func mdmNumericRangeErr(k apple_mdm.Key, v float64) string {
	if k.Range == nil {
		return ""
	}
	if min, ok := toFloatMDMForm(k.Range.Min); ok && v < min {
		return fmt.Sprintf("Below minimum (%v).", k.Range.Min)
	}
	if max, ok := toFloatMDMForm(k.Range.Max); ok && v > max {
		return fmt.Sprintf("Above maximum (%v).", k.Range.Max)
	}
	return ""
}

func toFloatMDMForm(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// ── Preview / create stages ────────────────────────────────────────

func (s *AppleMDMPayloadsFormScreen) handleKeyPreview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
}

func (s *AppleMDMPayloadsFormScreen) startCreate() (tea.Model, tea.Cmd) {
	s.stage = mdmFormStageCreating
	return s, tea.Batch(s.spinner.Tick, s.createCmd())
}

func (s *AppleMDMPayloadsFormScreen) createCmd() tea.Cmd {
	mobileconfig := s.mobileconfig
	policyName := s.policyName
	mode := s.mode
	editID := s.editPolicyID
	// In edit mode the OS family rides from the decoded policy's
	// template (pre-fix this always used darwin and an iOS edit got
	// silently reassigned to the macOS template). In create mode v1
	// only supports darwin; iOS support is tracked as KLA-450.
	osFamily := apple_mdm.OSFamilyDarwin
	redispatch := true
	if mode == formModeEdit {
		osFamily = s.editOSFamily
		redispatch = s.editRedispatch
	}
	return func() tea.Msg {
		client, err := newV2ClientForAuthoring()
		if err != nil {
			return applePolicyAuthorCreateMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		ctx := context.Background()
		tmpl, err := apple_mdm.ResolveCustomMDMTemplate(ctx, client, osFamily)
		if err != nil {
			return applePolicyAuthorCreateMsg{err: fmt.Errorf("resolving template: %w", err)}
		}
		body := apple_mdm.BuildCustomMDMPolicyBody(policyName, tmpl, mobileconfig, redispatch)
		var raw []byte
		if mode == formModeEdit {
			// PUT /policies/{id} with the same body shape JumpCloud
			// produces server-side. Empirically the v2 client's
			// Update method handles PUT and returns the updated
			// resource JSON, matching the create path.
			raw, err = client.Update(ctx, "/policies/"+editID, body)
		} else {
			raw, err = client.Create(ctx, "/policies", body)
		}
		if err != nil {
			return applePolicyAuthorCreateMsg{err: err}
		}
		id, name := extractPolicyIDName(raw)
		return applePolicyAuthorCreateMsg{policyID: id, policyName: name}
	}
}

// NewAppleMDMPayloadsFormScreenForEdit returns a form screen
// pre-populated from a DecodedPolicy. On submit the screen runs
// PUT /policies/{policyID} instead of POST /policies — the same form
// UX, just routed to the update endpoint.
//
// Pre-population walks Schema.Keys and looks up each field's current
// value in decoded.Values. Types are coerced through fmt.Sprintf for
// numeric fields because howett.net/plist decodes integers as uint64
// and the form holds them as textinput strings.
func NewAppleMDMPayloadsFormScreenForEdit(decoded apple_mdm.DecodedPolicy) *AppleMDMPayloadsFormScreen {
	s := NewAppleMDMPayloadsFormScreen(decoded.Schema)
	s.mode = formModeEdit
	s.editPolicyID = decoded.PolicyID
	s.editRedispatch = decoded.Redispatch
	s.editRemovalDisallowed = decoded.RemovalDisallowed
	s.editOSFamily = apple_mdm.OSFamilyFromTemplateName(decoded.TemplateName)
	if s.editOSFamily == "" {
		// Fall back to darwin only if the policy's template was
		// unparseable — saves the operator a hard failure on weird
		// data while still defaulting to the most common case.
		s.editOSFamily = apple_mdm.OSFamilyDarwin
	}
	s.nameInput.SetValue(decoded.PolicyName)
	s.policyName = decoded.PolicyName

	for i := range s.fields {
		f := &s.fields[i]
		v, ok := decoded.Values[f.key.Name]
		if !ok {
			continue
		}
		switch f.kind {
		case mdmFieldKindString:
			if str, ok := v.(string); ok {
				f.text.SetValue(str)
			}
		case mdmFieldKindBool:
			if b, ok := v.(bool); ok {
				f.boolValue = b
			}
		case mdmFieldKindInteger, mdmFieldKindReal:
			f.text.SetValue(fmt.Sprintf("%v", v))
		case mdmFieldKindRangeList:
			target := fmt.Sprintf("%v", v)
			for j, opt := range f.options {
				if opt == target {
					f.selectedIdx = j
					break
				}
			}
		}
	}
	return s
}

func (s *AppleMDMPayloadsFormScreen) handleCreateFinished(msg applePolicyAuthorCreateMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		s.createErr = msg.err.Error()
		s.stage = mdmFormStageFailed
		return s, nil
	}
	s.policyID = msg.policyID
	s.policyName = msg.policyName
	s.stage = mdmFormStageSuccess
	return s, nil
}

// ── Rendering ──────────────────────────────────────────────────────

func (s *AppleMDMPayloadsFormScreen) View() string {
	switch s.stage {
	case mdmFormStageEdit:
		return s.viewEdit()
	case mdmFormStagePreview:
		hint := style.Subtitle.Render(
			fmt.Sprintf("Preview (%d bytes) · c to create policy · Esc back to form · j/k scroll",
				len(s.mobileconfig)))
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
		fmt.Fprintln(&b, "  Type: "+s.payload.Type)
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

func (s *AppleMDMPayloadsFormScreen) viewEdit() string {
	var b strings.Builder
	fmt.Fprintln(&b, style.Subtitle.Render("Payload: "+s.payload.Type))
	if s.payload.Title != "" {
		fmt.Fprintln(&b, "         "+s.payload.Title)
	}
	fmt.Fprintln(&b)
	fmt.Fprint(&b, mdmFocusMarker(s.focusIdx == 0), "Policy name: ", s.nameInput.View(), "\n\n")

	// Group fields by required / optional / unsupported.
	required := s.mdmFieldIndices(func(f mdmFormField) bool {
		return strings.ToLower(f.key.Presence) == "required" && f.kind != mdmFieldKindUnsupported
	})
	optional := s.mdmFieldIndices(func(f mdmFormField) bool {
		pres := strings.ToLower(f.key.Presence)
		return (pres == "optional" || pres == "") && f.kind != mdmFieldKindUnsupported
	})
	unsupported := s.mdmFieldIndices(func(f mdmFormField) bool { return f.kind == mdmFieldKindUnsupported })

	if len(required) > 0 {
		fmt.Fprintln(&b, style.SectionHeader.Render("Required"))
		for _, idx := range required {
			s.renderMDMFieldRow(&b, idx, true)
		}
		fmt.Fprintln(&b)
	}
	if len(optional) > 0 {
		fmt.Fprintln(&b, style.SectionHeader.Render("Optional"))
		for _, idx := range optional {
			s.renderMDMFieldRow(&b, idx, false)
		}
		fmt.Fprintln(&b)
	}
	if len(unsupported) > 0 {
		fmt.Fprintln(&b, style.SectionHeader.Render("Complex types — drop to $EDITOR (Ctrl-E) to set"))
		for _, idx := range unsupported {
			// Required-presence on a complex-type key must still
			// surface the `*` marker so the operator sees Ctrl-E is
			// not optional for this payload. Pre-fix the unsupported
			// section dropped the required marker entirely and the
			// operator was misled into thinking they could submit
			// without dropping to the editor (Bugbot PR #53 review).
			required := strings.ToLower(s.fields[idx].key.Presence) == "required"
			s.renderMDMFieldRow(&b, idx, required)
		}
		fmt.Fprintln(&b)
	}

	if len(required)+len(optional) == 0 {
		fmt.Fprintln(&b, style.Subtitle.Render(
			"No scalar fields in this payload. Press Ctrl-E to use the $EDITOR flow."))
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, style.Subtitle.Render(
		"Tab/↑↓ focus · Space toggle/cycle · Ctrl-S submit · Ctrl-E editor · Esc cancel"))
	return b.String()
}

// mdmFieldIndices selects indices into s.fields whose entry matches pred.
// Used to bucket the form into required / optional / unsupported.
func (s *AppleMDMPayloadsFormScreen) mdmFieldIndices(pred func(mdmFormField) bool) []int {
	var out []int
	for i, f := range s.fields {
		if pred(f) {
			out = append(out, i)
		}
	}
	return out
}

func (s *AppleMDMPayloadsFormScreen) renderMDMFieldRow(b *strings.Builder, idx int, required bool) {
	f := s.fields[idx]
	focused := s.focusIdx == idx+1
	marker := mdmFocusMarker(focused)
	star := " "
	if required {
		star = style.Error.Render("*")
	}

	label := fmt.Sprintf("%-32s", truncateTUI(f.key.Name, 32))
	value := renderMDMFormFieldValue(f, focused)

	fmt.Fprintf(b, "%s%s %s  %s", marker, star, label, value)
	if f.err != "" {
		fmt.Fprintf(b, "  %s", style.Error.Render(f.err))
	}
	fmt.Fprintln(b)
}

func renderMDMFormFieldValue(f mdmFormField, focused bool) string {
	switch f.kind {
	case mdmFieldKindString, mdmFieldKindInteger, mdmFieldKindReal:
		if focused {
			return f.text.View()
		}
		v := f.text.Value()
		if v == "" {
			v = "(empty)"
		}
		return v
	case mdmFieldKindBool:
		if f.boolValue {
			return "[x]"
		}
		return "[ ]"
	case mdmFieldKindRangeList:
		var parts []string
		for i, opt := range f.options {
			if i == f.selectedIdx {
				parts = append(parts, "["+opt+"]")
			} else {
				parts = append(parts, opt)
			}
		}
		return strings.Join(parts, "  ")
	case mdmFieldKindUnsupported:
		return style.Subtitle.Render(fmt.Sprintf("(%s — complex type, set via $EDITOR)", f.key.Type))
	}
	return ""
}

func mdmFocusMarker(focused bool) string {
	if focused {
		return "▸ "
	}
	return "  "
}
