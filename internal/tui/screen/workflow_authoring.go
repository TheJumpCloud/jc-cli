package screen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/klaassen-consulting/jc/internal/schema"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
	"github.com/klaassen-consulting/jc/internal/workflow"
)

// Authoring flow for `jc tui → Workflow Templates → <template> → n`.
//
// A workflow is a DSL document with no published schema, so it cannot be
// authored through the generic key/value form the other resources use. What it
// CAN be is filled: every shipped template marks the values only the operator
// can supply with REPLACE_WITH_*, and internal/workflow classifies each one —
// a command, a user group, a device group, a policy, or free text. A
// resolvable marker opens the corresponding list as a picker, so nobody has to
// go and find an ID by hand.
//
// The stages mirror apple_mdm_payloads_authoring.go, which solves the same
// problem for .mobileconfig: one screen with sub-stages rather than a chain of
// screens, so the operator keeps their place through the whole flow.
type wfAuthorStage int

const (
	wfAuthorStageName        wfAuthorStage = iota // name the workflow
	wfAuthorStageFill                             // one row per placeholder
	wfAuthorStagePickerOpen                       // a picker list is on top
	wfAuthorStageFreeText                         // textinput for a free-text marker
	wfAuthorStageEditing                          // $EDITOR is open on the raw DSL
	wfAuthorStageRole                             // choosing the execution role
	wfAuthorStageReview                           // validation + explain + side effects
	wfAuthorStageConfirmRisk                      // extra confirm for side effects
	wfAuthorStageCreating                         // POST in flight
	wfAuthorStageDone
	wfAuthorStageFailed
)

// newV2ClientForWorkflowAuthoring is overridable for tests.
var newV2ClientForWorkflowAuthoring = api.NewV2Client

// wfAuthorEditor is the editor exec hook, overridable so tests need not launch
// a process.
var wfAuthorEditor = func(path string) tea.Cmd {
	editor := resolveWorkflowAuthorEditor()
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		return func() tea.Msg { return wfAuthorEditorMsg{err: fmt.Errorf("no editor resolvable")} }
	}
	cmd := exec.Command(fields[0], append(fields[1:], path)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return wfAuthorEditorMsg{err: err} })
}

// resolveWorkflowAuthorEditor mirrors the recipe and Apple MDM screens:
// VISUAL > EDITOR > vi. Same precedence means no surprise for an admin who has
// already set one of them.
func resolveWorkflowAuthorEditor() string {
	if v := strings.TrimSpace(os.Getenv("VISUAL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("EDITOR")); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

type wfAuthorEditorMsg struct{ err error }

type wfAuthorCreatedMsg struct {
	id   string
	name string
	err  error
}

// wfPlaceholderRow is one marker the operator has to answer.
type wfPlaceholderRow struct {
	Marker string
	Kind   workflow.PlaceholderKind
	// Value is what will be substituted: an ID for a resolved pick, or the
	// literal text for a free-text marker.
	Value string
	// Display is what the operator chose, shown instead of a raw ID.
	Display string
}

// Filled reports whether this row has an answer.
func (r wfPlaceholderRow) Filled() bool { return r.Value != "" }

// WorkflowAuthoringScreen walks the operator from a template to a created
// workflow.
type WorkflowAuthoringScreen struct {
	stage    wfAuthorStage
	template workflow.Template
	spinner  spinner.Model
	input    textinput.Model

	name     string
	rows     []wfPlaceholderRow
	cursor   int
	roleID   string
	roleName string

	// dsl is the working document: the template's, with fills applied and any
	// $EDITOR changes folded in.
	dsl json.RawMessage

	result  workflow.Result
	created string
	err     string

	width, height int
}

func NewWorkflowAuthoringScreen(t workflow.Template) *WorkflowAuthoringScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner

	ti := textinput.New()
	ti.CharLimit = 200
	ti.SetValue(t.Name)

	s := &WorkflowAuthoringScreen{
		stage:    wfAuthorStageName,
		template: t,
		spinner:  sp,
		input:    ti,
		dsl:      t.DSL,
		name:     t.Name,
	}
	s.buildRows()
	return s
}

// buildRows turns the template's markers into rows to answer, sorted so the
// order is stable between runs.
func (s *WorkflowAuthoringScreen) buildRows() {
	d, err := workflow.ParseDSL(s.template.DSL)
	if err != nil {
		s.err = err.Error()
		return
	}
	kinds := d.PlaceholderKinds()
	markers := make([]string, 0, len(kinds))
	for m := range kinds {
		markers = append(markers, m)
	}
	sort.Strings(markers)

	s.rows = s.rows[:0]
	for _, m := range markers {
		s.rows = append(s.rows, wfPlaceholderRow{Marker: m, Kind: kinds[m]})
	}
}

func (s *WorkflowAuthoringScreen) Title() string { return "New workflow" }

func (s *WorkflowAuthoringScreen) TextInputActive() bool {
	return s.stage == wfAuthorStageName || s.stage == wfAuthorStageFreeText
}

func (s *WorkflowAuthoringScreen) Init() tea.Cmd {
	s.input.Focus()
	return tea.Batch(s.spinner.Tick, textinput.Blink)
}

// pickerEntryForKind maps a placeholder kind to the registry entry whose list
// can stand in as a chooser. Free text and kinds with no listable resource
// return false, and fall back to a text prompt.
func pickerEntryForKind(kind string) (tui.ResourceEntry, bool) {
	specs := map[string]struct {
		key, display, endpoint, idField, nameField string
		fields                                     []string
	}{
		workflow.KindCommand: {"commands", "Commands", "/commands", "_id", "name",
			[]string{"_id", "name", "commandType"}},
		workflow.KindUserGroup: {"user-groups", "User Groups", "/usergroups", "id", "name",
			[]string{"id", "name", "type"}},
		workflow.KindDeviceGroup: {"device-groups", "Device Groups", "/systemgroups", "id", "name",
			[]string{"id", "name", "type"}},
		workflow.KindPolicy: {"policies", "Policies", "/policies", "id", "name",
			[]string{"id", "name", "template"}},
		workflow.KindAppleMDM: {"apple-mdm", "Apple MDM", "/applemdms", "id", "name",
			[]string{"id", "name"}},
		workflow.KindWorkflow: {"workflows", "Workflows", "/workflows", "id", "name",
			[]string{"id", "name", "status"}},
	}
	spec, ok := specs[kind]
	if !ok {
		return tui.ResourceEntry{}, false
	}

	client := tui.ClientV2
	if kind == workflow.KindCommand {
		// Commands live on V1; using the V2 client would request
		// /api/v2/commands, which does not exist.
		client = tui.ClientV1
	}
	entry := tui.ResourceEntry{
		Key:          spec.key,
		DisplayName:  spec.display,
		Category:     tui.CategoryWorkflows,
		ClientType:   client,
		ListEndpoint: spec.endpoint,
		Schema: schema.ResourceSchema{
			Resource:      spec.key,
			Verbs:         []string{"list", "get"},
			DefaultFields: spec.fields,
			IDField:       spec.idField,
			NameField:     spec.nameField,
		},
	}
	if kind == workflow.KindWorkflow {
		entry.ResponseKey = "results"
	}
	return entry, true
}

// applyFills produces the working DSL from the template plus every answered
// row. Called before validation and before create, so what is reviewed is
// exactly what is sent.
func (s *WorkflowAuthoringScreen) applyFills() error {
	d, err := workflow.ParseDSL(s.template.DSL)
	if err != nil {
		return err
	}
	values := map[string]string{}
	for _, r := range s.rows {
		if r.Filled() {
			values[r.Marker] = r.Value
		}
	}
	if len(values) == 0 {
		s.dsl = s.template.DSL
		return nil
	}
	filled, err := d.Fill(values)
	if err != nil {
		return err
	}
	s.dsl = filled
	return nil
}

func (s *WorkflowAuthoringScreen) unfilled() []string {
	var out []string
	for _, r := range s.rows {
		if !r.Filled() {
			out = append(out, strings.TrimPrefix(r.Marker, "REPLACE_WITH_"))
		}
	}
	return out
}

// validateCmd runs the same validator the CLI and MCP use.
func (s *WorkflowAuthoringScreen) validate() {
	s.result = workflow.ValidateRaw(s.dsl)
}

func (s *WorkflowAuthoringScreen) createCmd() tea.Cmd {
	dsl, name, roleID := s.dsl, s.name, s.roleID
	description := s.template.Description
	return func() tea.Msg {
		client, err := newV2ClientForWorkflowAuthoring()
		if err != nil {
			return wfAuthorCreatedMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		raw, err := client.Create(ctx, workflow.Endpoint, workflow.CreateBody(workflow.Workflow{
			Name: name, Description: description, DSL: dsl,
			// New workflows are created inactive, as on the CLI: activating is
			// a separate, deliberate step.
			Status: workflow.StatusInactive, ExecutionRoleID: roleID,
		}))
		if err != nil {
			return wfAuthorCreatedMsg{err: err}
		}
		w, perr := workflow.ParseWorkflow(raw)
		if perr != nil {
			return wfAuthorCreatedMsg{err: perr}
		}
		return wfAuthorCreatedMsg{id: w.ID, name: w.Name}
	}
}

// roleEntry is the picker over roles for the execution role.
func roleEntry() tui.ResourceEntry {
	return tui.ResourceEntry{
		Key:          "roles",
		DisplayName:  "Roles",
		Category:     tui.CategoryAccess,
		ClientType:   tui.ClientV2,
		ListEndpoint: "/roles",
		ResponseKey:  "results",
		Schema: schema.ResourceSchema{
			Resource:      "roles",
			Verbs:         []string{"list", "get"},
			DefaultFields: []string{"id", "name", "description"},
			IDField:       resolve.RoleConfig.IDField,
			NameField:     resolve.RoleConfig.NameField,
		},
	}
}

// adminRole reports whether a role name grants broad privilege. A workflow
// runs unattended with its role's permissions, so this is worth saying out
// loud rather than leaving in a field.
func adminRole(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "administrator") || strings.Contains(n, "super admin")
}

func (s *WorkflowAuthoringScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(m)
		return s, cmd

	case wfAuthorEditorMsg:
		s.stage = wfAuthorStageReview
		if m.err != nil {
			s.err = "editor: " + m.err.Error()
			return s, nil
		}
		s.validate()
		return s, nil

	case wfAuthorCreatedMsg:
		if m.err != nil {
			s.stage = wfAuthorStageFailed
			s.err = m.err.Error()
			return s, nil
		}
		s.stage = wfAuthorStageDone
		s.created = m.id
		return s, nil

	case tea.KeyMsg:
		return s.updateKey(m)
	}
	return s, nil
}

func (s *WorkflowAuthoringScreen) updateKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s.stage {
	case wfAuthorStageName:
		switch m.String() {
		case "esc":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "enter":
			name := strings.TrimSpace(s.input.Value())
			if name == "" {
				s.err = "a workflow needs a name"
				return s, nil
			}
			s.name, s.err = name, ""
			s.input.Blur()
			s.stage = wfAuthorStageFill
			// A template with nothing to fill goes straight to the role.
			if len(s.rows) == 0 {
				return s.enterRoleStage()
			}
			return s, nil
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(m)
		return s, cmd

	case wfAuthorStageFreeText:
		switch m.String() {
		case "esc":
			s.input.Blur()
			s.stage = wfAuthorStageFill
			return s, nil
		case "enter":
			v := strings.TrimSpace(s.input.Value())
			if v != "" {
				s.rows[s.cursor].Value = v
				s.rows[s.cursor].Display = v
			}
			s.input.Blur()
			s.stage = wfAuthorStageFill
			return s, nil
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(m)
		return s, cmd

	case wfAuthorStageFill:
		return s.updateFill(m)

	case wfAuthorStageReview:
		switch m.String() {
		case "esc":
			s.stage = wfAuthorStageFill
			return s, nil
		case "e":
			return s.openEditor()
		case "enter":
			if !s.result.OK() {
				return s, nil
			}
			if len(s.result.SideEffects) > 0 {
				s.stage = wfAuthorStageConfirmRisk
				return s, nil
			}
			s.stage = wfAuthorStageCreating
			return s, tea.Batch(s.spinner.Tick, s.createCmd())
		}
		return s, nil

	case wfAuthorStageConfirmRisk:
		switch m.String() {
		case "y":
			s.stage = wfAuthorStageCreating
			return s, tea.Batch(s.spinner.Tick, s.createCmd())
		case "n", "esc":
			s.stage = wfAuthorStageReview
			return s, nil
		}
		return s, nil

	case wfAuthorStageDone, wfAuthorStageFailed:
		if m.String() == "esc" || m.String() == "enter" {
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		}
		return s, nil
	}
	return s, nil
}

func (s *WorkflowAuthoringScreen) updateFill(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case "esc":
		return s, func() tea.Msg { return tui.PopScreenMsg{} }
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
		return s, nil
	case "down", "j":
		if s.cursor < len(s.rows)-1 {
			s.cursor++
		}
		return s, nil
	case "enter":
		return s.answerCurrentRow()
	case "c":
		// Continue to the role once everything is answered.
		if left := s.unfilled(); len(left) > 0 {
			s.err = "still to fill: " + strings.Join(left, ", ")
			return s, nil
		}
		s.err = ""
		return s.enterRoleStage()
	}
	return s, nil
}

// answerCurrentRow opens a picker for a resolvable marker, or a text prompt
// for free text.
func (s *WorkflowAuthoringScreen) answerCurrentRow() (tea.Model, tea.Cmd) {
	if s.cursor >= len(s.rows) {
		return s, nil
	}
	row := s.rows[s.cursor]

	entry, ok := pickerEntryForKind(row.Kind.Kind)
	if !ok {
		s.input.SetValue(row.Value)
		s.input.Placeholder = row.Kind.Describe
		s.input.Focus()
		s.stage = wfAuthorStageFreeText
		return s, textinput.Blink
	}

	idx := s.cursor
	s.stage = wfAuthorStagePickerOpen
	picker := NewListScreen(entry).AsPicker(func(id, name string) tea.Cmd {
		return func() tea.Msg {
			s.rows[idx].Value = id
			s.rows[idx].Display = name
			if name == "" {
				s.rows[idx].Display = id
			}
			s.stage = wfAuthorStageFill
			return tui.PopScreenMsg{}
		}
	})
	return s, func() tea.Msg { return tui.PushScreenMsg{Screen: picker} }
}

// enterRoleStage opens the role picker, then moves to review.
func (s *WorkflowAuthoringScreen) enterRoleStage() (tea.Model, tea.Cmd) {
	if err := s.applyFills(); err != nil {
		s.err = err.Error()
		s.stage = wfAuthorStageFill
		return s, nil
	}
	s.stage = wfAuthorStageRole
	picker := NewListScreen(roleEntry()).AsPicker(func(id, name string) tea.Cmd {
		return func() tea.Msg {
			s.roleID, s.roleName = id, name
			s.validate()
			s.stage = wfAuthorStageReview
			return tui.PopScreenMsg{}
		}
	})
	return s, func() tea.Msg { return tui.PushScreenMsg{Screen: picker} }
}

// openEditor writes the working DSL to a temp file and hands it to $EDITOR,
// for anything the template cannot express.
func (s *WorkflowAuthoringScreen) openEditor() (tea.Model, tea.Cmd) {
	pretty, err := json.MarshalIndent(json.RawMessage(s.dsl), "", "  ")
	if err != nil {
		s.err = err.Error()
		return s, nil
	}
	f, err := os.CreateTemp("", "jc-workflow-*.json")
	if err != nil {
		s.err = err.Error()
		return s, nil
	}
	path := f.Name()
	if _, err := f.Write(pretty); err != nil {
		_ = f.Close()
		s.err = err.Error()
		return s, nil
	}
	_ = f.Close()

	s.stage = wfAuthorStageEditing
	return s, tea.Sequence(
		wfAuthorEditor(path),
		func() tea.Msg {
			edited, err := os.ReadFile(path)
			_ = os.Remove(path)
			if err != nil {
				return wfAuthorEditorMsg{err: err}
			}
			if _, perr := workflow.ParseDSL(edited); perr != nil {
				return wfAuthorEditorMsg{err: fmt.Errorf("edited document is not a valid DSL: %w", perr)}
			}
			s.dsl = edited
			return wfAuthorEditorMsg{}
		},
	)
}

func (s *WorkflowAuthoringScreen) View() string {
	var b strings.Builder

	switch s.stage {
	case wfAuthorStageName:
		fmt.Fprintln(&b, style.SectionHeader.Render("Name this workflow"))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  "+s.input.View())
		if s.err != "" {
			fmt.Fprintln(&b, "\n"+style.Error.Render(s.err))
		}
		fmt.Fprintln(&b, "\n"+style.Subtitle.Render("Enter continue · Esc cancel"))
		return b.String()

	case wfAuthorStageFreeText:
		row := s.rows[s.cursor]
		fmt.Fprintln(&b, style.SectionHeader.Render(strings.TrimPrefix(row.Marker, "REPLACE_WITH_")))
		fmt.Fprintln(&b, "  "+style.Subtitle.Render(row.Kind.Describe))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  "+s.input.View())
		fmt.Fprintln(&b, "\n"+style.Subtitle.Render("Enter accept · Esc back"))
		return b.String()

	case wfAuthorStagePickerOpen, wfAuthorStageRole, wfAuthorStageEditing:
		// A picker or the editor is on top of this screen.
		return ""

	case wfAuthorStageCreating:
		fmt.Fprintln(&b, s.spinner.View()+" Creating workflow...")
		return b.String()

	case wfAuthorStageDone:
		fmt.Fprintln(&b, style.Success.Render("Workflow created."))
		fmt.Fprintf(&b, "\n  %s\n  id %s\n", s.name, s.created)
		fmt.Fprintln(&b, "\n"+style.Subtitle.Render(
			"Created inactive. Activate with `jc workflows update "+s.created+" --status active`."))
		fmt.Fprintln(&b, style.Subtitle.Render("Enter/Esc back"))
		return b.String()

	case wfAuthorStageFailed:
		fmt.Fprintln(&b, style.Error.Render("Create failed"))
		fmt.Fprintln(&b, "\n  "+s.err)
		fmt.Fprintln(&b, "\n"+style.Subtitle.Render("Enter/Esc back"))
		return b.String()

	case wfAuthorStageConfirmRisk:
		fmt.Fprintln(&b, style.Error.Render("This workflow reaches outside JumpCloud"))
		fmt.Fprintln(&b)
		for _, se := range s.result.SideEffects {
			fmt.Fprintf(&b, "  %s — %s\n", se.Task, se.What)
			for _, t := range se.Targets {
				fmt.Fprintf(&b, "      → %s\n", t)
			}
		}
		fmt.Fprintln(&b, "\n"+style.Subtitle.Render("y create anyway · n/Esc back"))
		return b.String()

	case wfAuthorStageReview:
		return s.reviewView()
	}

	return s.fillView()
}

// fillView lists every marker with what it wants and what has been chosen.
func (s *WorkflowAuthoringScreen) fillView() string {
	var b strings.Builder
	fmt.Fprintln(&b, style.Subtitle.Render("From template: "+s.template.Name))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.SectionHeader.Render("Values to supply"))

	if len(s.rows) == 0 {
		fmt.Fprintln(&b, "  "+style.Subtitle.Render("none — this template needs nothing filled in"))
	}
	for i, r := range s.rows {
		val := style.Subtitle.Render("— " + r.Kind.Describe)
		if r.Filled() {
			val = r.Display
			if r.Kind.Resolvable && r.Display != r.Value {
				val += style.Subtitle.Render("  (" + r.Value + ")")
			}
		}
		line := fmt.Sprintf("%-40s %s", strings.TrimPrefix(r.Marker, "REPLACE_WITH_"), val)
		if i == s.cursor {
			fmt.Fprintln(&b, style.SelectedRow.Render("> "+line))
		} else {
			fmt.Fprintln(&b, "  "+line)
		}
	}

	if s.err != "" {
		fmt.Fprintln(&b, "\n"+style.Error.Render(s.err))
	}
	fmt.Fprintln(&b, "\n"+style.Subtitle.Render("Enter choose · c continue · Esc cancel"))
	return b.String()
}

// reviewView shows what the workflow will do before it is created.
func (s *WorkflowAuthoringScreen) reviewView() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", style.SectionHeader.Render("Review — "+s.name))

	fmt.Fprintf(&b, "  runs as role: %s\n", s.roleName)
	if adminRole(s.roleName) {
		fmt.Fprintln(&b, "  "+style.Error.Render(
			"an administrator role is a broad standing grant to an unattended workflow"))
	}
	if s.result.TriggerType != "" {
		fmt.Fprintf(&b, "  trigger: %s\n", s.result.TriggerType)
	}
	fmt.Fprintln(&b, "  status: inactive (activate deliberately after review)")

	if d, err := workflow.ParseDSL(s.dsl); err == nil {
		fmt.Fprintln(&b, "\n"+style.SectionHeader.Render("Steps"))
		for _, t := range d.Tasks() {
			indent := "  "
			if t.Depth > 0 {
				indent = "      "
			}
			fmt.Fprintf(&b, "%s%s\n", indent, t.Describe())
		}
	}

	if errs := s.result.Errors(); len(errs) > 0 {
		fmt.Fprintln(&b, "\n"+style.Error.Render(fmt.Sprintf("%d problem(s) — cannot create", len(errs))))
		for _, f := range errs {
			fmt.Fprintf(&b, "  %s: %s\n", f.Path, f.Message)
			if f.Hint != "" {
				fmt.Fprintf(&b, "      %s\n", style.Subtitle.Render(f.Hint))
			}
		}
	}
	if len(s.result.SideEffects) > 0 {
		fmt.Fprintln(&b, "\n"+style.Error.Render("Reaches outside JumpCloud"))
		for _, se := range s.result.SideEffects {
			fmt.Fprintf(&b, "  %s — %s\n", se.Task, se.What)
		}
	}
	if s.err != "" {
		fmt.Fprintln(&b, "\n"+style.Error.Render(s.err))
	}

	footer := "e edit raw DSL · Esc back"
	if s.result.OK() {
		footer = "Enter create · " + footer
	}
	fmt.Fprintln(&b, "\n"+style.Subtitle.Render(footer))
	return b.String()
}
