package screen

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"go.yaml.in/yaml/v3"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// Authoring screen for `jc tui → Apple MDM payloads → ... → n`. Walks
// the operator through:
//
//  1. Name the policy.
//  2. Edit the values YAML in $EDITOR.
//  3. Validate; show errors and offer re-edit, or preview the
//     emitted .mobileconfig.
//  4. Create the JumpCloud Custom MDM Configuration Profile policy
//     by POSTing through the same primitives as
//     `jc apple-mdm payloads create-policy`.
//
// One screen with sub-stages (vs many screens with deep breadcrumbs)
// keeps the operator's mental model intact through the whole flow.
// Esc at any stage pops back to the detail screen.

// applePolicyAuthorStage tracks where the operator is in the flow.
type applePolicyAuthorStage int

const (
	authorStageName       applePolicyAuthorStage = iota // textinput for policy name
	authorStageEditing                                  // $EDITOR is open
	authorStageValidating                               // parsing + CoerceAndValidate
	authorStageErrors                                   // validation failed
	authorStagePreview                                  // viewport showing emitted XML
	authorStageCreating                                 // POST in flight
	authorStageSuccess                                  // showing new policy id
	authorStageFailed                                   // create POST failed
)

// applePolicyAuthorEditorMsg is sent when $EDITOR exits.
type applePolicyAuthorEditorMsg struct {
	err error
}

// applePolicyAuthorValidateMsg carries the result of parsing +
// validating the edited YAML. Fires from a side goroutine so the TUI
// doesn't block while CoerceAndValidate runs.
type applePolicyAuthorValidateMsg struct {
	typed       map[string]any
	mobileconfg []byte
	err         error
}

// applePolicyAuthorCreateMsg is sent when the POST /policies call
// returns. id and name come from the JC response payload.
type applePolicyAuthorCreateMsg struct {
	policyID   string
	policyName string
	err        error
}

// AppleMDMPayloadsAuthoringScreen is the multi-stage editor +
// preview + create screen. Held state survives across stages because
// each step (skeleton → edited values → validated values →
// emitted plist) builds on the previous.
type AppleMDMPayloadsAuthoringScreen struct {
	payload apple_mdm.Payload

	stage applePolicyAuthorStage

	// Stage 1: name input.
	name textinput.Model

	// Stage 2/3: temp file path holding the YAML scaffold.
	tmpDir  string
	tmpFile string
	editErr string

	// Stage 3 → 4/5: validated values + emitted mobileconfig.
	values       map[string]any
	mobileconfig []byte
	validateErrs []string

	// Stage 5: viewport scrolling the plist.
	preview viewport.Model

	// Stage 6: create-in-flight indicator.
	spinner spinner.Model

	// Stage 7/8: result.
	policyID    string
	policyName  string
	createError string

	width, height int
	ready         bool
}

// newV2ClientForAuthoring is overridable for tests. Default mirrors
// what the rest of the TUI does.
var newV2ClientForAuthoring = api.NewV2Client

// applePolicyAuthorEditor is the editor exec hook. Overridable for
// tests so they can stub the $EDITOR step without actually launching
// a process.
var applePolicyAuthorEditor = func(path string) tea.Cmd {
	editor := resolveAppleAuthorEditor()
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		return func() tea.Msg {
			return applePolicyAuthorEditorMsg{err: fmt.Errorf("no editor resolvable")}
		}
	}
	cmd := exec.Command(fields[0], append(fields[1:], path)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return applePolicyAuthorEditorMsg{err: err}
	})
}

// resolveAppleAuthorEditor mirrors recipe_editor.go's resolution:
// VISUAL > EDITOR > vi (or notepad on Windows). Picking the same
// precedence avoids surprise — admins who set $VISUAL for recipes
// get the same editor here.
func resolveAppleAuthorEditor() string {
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

// NewAppleMDMPayloadsAuthoringScreen launches the authoring flow for
// one selected payload. The operator first enters a policy name; the
// rest of the flow advances on its own.
func NewAppleMDMPayloadsAuthoringScreen(p apple_mdm.Payload) *AppleMDMPayloadsAuthoringScreen {
	ti := textinput.New()
	ti.Placeholder = "e.g. " + suggestPolicyName(p)
	ti.CharLimit = 128
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner

	return &AppleMDMPayloadsAuthoringScreen{
		payload: p,
		stage:   authorStageName,
		name:    ti,
		spinner: sp,
	}
}

// suggestPolicyName turns a payloadtype into a default human-readable
// label the operator can customize: "com.apple.security.firewall" →
// "Firewall (MDM)". Falls back to the type when there's no title.
func suggestPolicyName(p apple_mdm.Payload) string {
	if p.Title != "" {
		return p.Title + " (MDM)"
	}
	return p.Type + " (MDM)"
}

func (s *AppleMDMPayloadsAuthoringScreen) Title() string {
	return "Create policy: " + s.payload.Type
}

// TextInputActive is true while the policy-name input has focus so
// the app-level Quit key doesn't intercept typed characters.
func (s *AppleMDMPayloadsAuthoringScreen) TextInputActive() bool {
	return s.stage == authorStageName
}

func (s *AppleMDMPayloadsAuthoringScreen) Init() tea.Cmd {
	return tea.Batch(textinput.Blink)
}

func (s *AppleMDMPayloadsAuthoringScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		if !s.ready {
			s.preview = viewport.New(m.Width, m.Height-4)
			s.ready = true
		} else {
			s.preview.Width = m.Width
			s.preview.Height = m.Height - 4
		}
		return s, nil

	case applePolicyAuthorEditorMsg:
		return s.handleEditorFinished(m)

	case applePolicyAuthorValidateMsg:
		return s.handleValidateFinished(m)

	case applePolicyAuthorCreateMsg:
		return s.handleCreateFinished(m)

	case spinner.TickMsg:
		if s.stage == authorStageCreating {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(m)
			return s, cmd
		}
		return s, nil

	case tea.KeyMsg:
		return s.handleKey(m)
	}
	return s, nil
}

// handleKey routes per-stage keybinds. Esc is universally "go back"
// except during the editor stage (we don't get keys then anyway).
func (s *AppleMDMPayloadsAuthoringScreen) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s.stage {
	case authorStageName:
		switch msg.String() {
		case "esc":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "enter":
			return s.startEditor()
		}
		var cmd tea.Cmd
		s.name, cmd = s.name.Update(msg)
		return s, cmd

	case authorStagePreview:
		switch msg.String() {
		case "esc":
			return s.cleanupAndPop()
		case "c":
			return s.startCreate()
		case "e":
			return s.reopenEditor()
		}
		// Pass through to viewport for j/k/PgUp/PgDn etc.
		var cmd tea.Cmd
		s.preview, cmd = s.preview.Update(msg)
		return s, cmd

	case authorStageErrors:
		switch msg.String() {
		case "esc":
			return s.cleanupAndPop()
		case "e":
			return s.reopenEditor()
		}
		return s, nil

	case authorStageSuccess, authorStageFailed:
		switch msg.String() {
		case "esc", "enter":
			return s.cleanupAndPop()
		}
		return s, nil
	}
	return s, nil
}

func (s *AppleMDMPayloadsAuthoringScreen) startEditor() (tea.Model, tea.Cmd) {
	policyName := strings.TrimSpace(s.name.Value())
	if policyName == "" {
		// Use the suggested default so the operator can just hit Enter.
		policyName = suggestPolicyName(s.payload)
		s.name.SetValue(policyName)
	}
	s.policyName = policyName

	// Write skeleton to a temp dir we can clean up on cancel.
	dir, err := os.MkdirTemp("", "jc-mdm-author-*")
	if err != nil {
		s.editErr = fmt.Sprintf("could not allocate temp dir: %v", err)
		s.stage = authorStageErrors
		s.validateErrs = []string{s.editErr}
		return s, nil
	}
	path := filepath.Join(dir, safeFilename(s.payload.Type)+".yaml")
	scaffold := apple_mdm.EmitValuesSkeleton(s.payload)
	if err := os.WriteFile(path, []byte(scaffold), 0o600); err != nil {
		os.RemoveAll(dir)
		s.editErr = fmt.Sprintf("could not write skeleton: %v", err)
		s.stage = authorStageErrors
		s.validateErrs = []string{s.editErr}
		return s, nil
	}
	s.tmpDir = dir
	s.tmpFile = path
	s.stage = authorStageEditing
	return s, applePolicyAuthorEditor(path)
}

// reopenEditor takes the operator back to $EDITOR with whatever they
// last had. tmpFile persists between edits so they don't lose progress.
func (s *AppleMDMPayloadsAuthoringScreen) reopenEditor() (tea.Model, tea.Cmd) {
	if s.tmpFile == "" {
		return s, nil
	}
	s.stage = authorStageEditing
	return s, applePolicyAuthorEditor(s.tmpFile)
}

func (s *AppleMDMPayloadsAuthoringScreen) handleEditorFinished(msg applePolicyAuthorEditorMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		s.editErr = msg.err.Error()
		s.stage = authorStageErrors
		s.validateErrs = []string{"editor exited with error: " + msg.err.Error()}
		return s, nil
	}
	s.stage = authorStageValidating
	return s, s.validateCmd()
}

func (s *AppleMDMPayloadsAuthoringScreen) validateCmd() tea.Cmd {
	path := s.tmpFile
	payload := s.payload
	return func() tea.Msg {
		raw, err := os.ReadFile(path)
		if err != nil {
			return applePolicyAuthorValidateMsg{err: fmt.Errorf("reading edited file: %w", err)}
		}
		var parsed map[string]any
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			return applePolicyAuthorValidateMsg{err: fmt.Errorf("parsing YAML: %w", err)}
		}
		if parsed == nil {
			parsed = map[string]any{}
		}
		typed, err := apple_mdm.CoerceAndValidate(payload, parsed)
		if err != nil {
			return applePolicyAuthorValidateMsg{err: err}
		}
		var buf bytes.Buffer
		err = apple_mdm.EmitMobileconfig(&buf,
			apple_mdm.EnvelopeOpts{},
			[]apple_mdm.PayloadInstance{{Schema: payload, Values: typed}})
		if err != nil {
			return applePolicyAuthorValidateMsg{err: fmt.Errorf("emitting plist: %w", err)}
		}
		return applePolicyAuthorValidateMsg{
			typed:       typed,
			mobileconfg: buf.Bytes(),
		}
	}
}

func (s *AppleMDMPayloadsAuthoringScreen) handleValidateFinished(msg applePolicyAuthorValidateMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		s.validateErrs = splitErrLines(msg.err.Error())
		s.stage = authorStageErrors
		return s, nil
	}
	s.values = msg.typed
	s.mobileconfig = msg.mobileconfg
	s.preview.SetContent(string(s.mobileconfig))
	s.preview.GotoTop()
	s.stage = authorStagePreview
	return s, nil
}

func (s *AppleMDMPayloadsAuthoringScreen) startCreate() (tea.Model, tea.Cmd) {
	s.stage = authorStageCreating
	return s, tea.Batch(s.spinner.Tick, s.createCmd())
}

func (s *AppleMDMPayloadsAuthoringScreen) createCmd() tea.Cmd {
	payload := s.payload
	mobileconfig := s.mobileconfig
	policyName := s.policyName
	return func() tea.Msg {
		client, err := newV2ClientForAuthoring()
		if err != nil {
			return applePolicyAuthorCreateMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		ctx := context.Background()
		tmpl, err := apple_mdm.ResolveCustomMDMTemplate(ctx, client, apple_mdm.OSFamilyDarwin)
		if err != nil {
			return applePolicyAuthorCreateMsg{err: fmt.Errorf("resolving template: %w", err)}
		}
		body := apple_mdm.BuildCustomMDMPolicyBody(policyName, tmpl, mobileconfig, true /* redispatch */)
		raw, err := client.Create(ctx, "/policies", body)
		if err != nil {
			return applePolicyAuthorCreateMsg{err: err}
		}
		// The response is JSON; extract id + name without dragging
		// the full struct.
		id, name := extractPolicyIDName(raw)
		_ = payload // payload retained for future per-payload logging
		return applePolicyAuthorCreateMsg{policyID: id, policyName: name}
	}
}

func (s *AppleMDMPayloadsAuthoringScreen) handleCreateFinished(msg applePolicyAuthorCreateMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		s.createError = msg.err.Error()
		s.stage = authorStageFailed
		return s, nil
	}
	s.policyID = msg.policyID
	s.policyName = msg.policyName
	s.stage = authorStageSuccess
	return s, nil
}

// cleanupAndPop removes the temp dir then pops the screen.
func (s *AppleMDMPayloadsAuthoringScreen) cleanupAndPop() (tea.Model, tea.Cmd) {
	if s.tmpDir != "" {
		_ = os.RemoveAll(s.tmpDir)
		s.tmpDir = ""
	}
	return s, func() tea.Msg { return tui.PopScreenMsg{} }
}

func (s *AppleMDMPayloadsAuthoringScreen) View() string {
	switch s.stage {
	case authorStageName:
		var b strings.Builder
		fmt.Fprintln(&b, style.Subtitle.Render("Name your JumpCloud policy"))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "Payload: "+style.ResourceName.Render(s.payload.Type))
		if s.payload.Title != "" {
			fmt.Fprintln(&b, "Title:   "+s.payload.Title)
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, s.name.View())
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("Enter to continue · Esc to cancel"))
		return b.String()

	case authorStageEditing, authorStageValidating:
		// $EDITOR has taken the terminal; nothing meaningful to draw.
		// Return a brief placeholder for the validation tick (it'll
		// flash through fast).
		return "Validating…"

	case authorStageErrors:
		var b strings.Builder
		fmt.Fprintln(&b, style.Error.Render("Validation failed:"))
		fmt.Fprintln(&b)
		for _, line := range s.validateErrs {
			fmt.Fprintln(&b, "  "+line)
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("e to re-edit · Esc to cancel"))
		return b.String()

	case authorStagePreview:
		hint := style.Subtitle.Render(
			fmt.Sprintf("Preview (%d bytes) · c to create policy · e to re-edit · Esc to cancel · j/k scroll",
				len(s.mobileconfig)))
		return s.preview.View() + "\n" + hint

	case authorStageCreating:
		return fmt.Sprintf("%s Creating JumpCloud policy %q…", s.spinner.View(), s.policyName)

	case authorStageSuccess:
		var b strings.Builder
		fmt.Fprintln(&b, style.Success.Render("Policy created."))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  ID:   "+s.policyID)
		fmt.Fprintln(&b, "  Name: "+s.policyName)
		fmt.Fprintln(&b, "  Type: "+s.payload.Type)
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("Esc / Enter to dismiss"))
		return b.String()

	case authorStageFailed:
		var b strings.Builder
		fmt.Fprintln(&b, style.Error.Render("Failed to create policy:"))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  "+s.createError)
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("Esc to dismiss"))
		return b.String()
	}
	return ""
}

// safeFilename strips characters that would surprise an editor's path
// handling — Apple payload types can contain parens (com.apple.MCX(FileVault2))
// which shells and some editors treat as syntax.
func safeFilename(s string) string {
	r := strings.NewReplacer(
		"(", "_",
		")", "_",
		" ", "_",
		"/", "_",
	)
	return r.Replace(s)
}

// splitErrLines turns CoerceAndValidate's "header: \n  - a\n  - b"
// shape into a slice the View can render as bullet points.
func splitErrLines(s string) []string {
	out := []string{}
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// extractPolicyIDName pulls .id and .name out of the create response.
// Kept tolerant: missing fields yield "" rather than an error, because
// the create itself succeeded — only the display is degraded.
func extractPolicyIDName(raw []byte) (string, string) {
	var resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := yaml.Unmarshal(raw, &resp); err != nil { // YAML parses JSON cleanly
		return "", ""
	}
	return resp.ID, resp.Name
}
