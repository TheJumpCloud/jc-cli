package screen

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

func TestAuthoring_NameInput_EnterStartsEditor(t *testing.T) {
	// Stub the editor exec so the test doesn't actually launch anything.
	// We capture the path the editor would have been pointed at.
	var editorPath string
	prev := applePolicyAuthorEditor
	t.Cleanup(func() { applePolicyAuthorEditor = prev })
	applePolicyAuthorEditor = func(path string) tea.Cmd {
		editorPath = path
		return func() tea.Msg { return applePolicyAuthorEditorMsg{err: nil} }
	}

	p := apple_mdm.Payload{
		Type: "com.example.test",
		Keys: []apple_mdm.Key{
			{Name: "RequiredStr", Type: "string", Presence: "required"},
		},
	}
	s := NewAppleMDMPayloadsAuthoringScreen(p)
	// Set a name + press Enter.
	s.name.SetValue("My Test Policy")
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected editor cmd")
	}
	// Editor cmd is built lazily — execute it.
	_ = cmd()
	if s.stage != authorStageEditing {
		t.Errorf("stage = %d, want authorStageEditing", s.stage)
	}
	if editorPath == "" {
		t.Fatal("editor not called")
	}
	if !strings.HasSuffix(editorPath, ".yaml") {
		t.Errorf("editor path = %q, expected .yaml suffix", editorPath)
	}
	// Skeleton should be on disk at the recorded path.
	data, err := os.ReadFile(editorPath)
	if err != nil {
		t.Fatalf("reading skeleton: %v", err)
	}
	if !strings.Contains(string(data), "RequiredStr:") {
		t.Errorf("skeleton missing required key:\n%s", data)
	}
	// Clean up the tmp dir the screen created.
	if s.tmpDir != "" {
		_ = os.RemoveAll(s.tmpDir)
	}
}

func TestAuthoring_EmptyName_UsesSuggested(t *testing.T) {
	prev := applePolicyAuthorEditor
	t.Cleanup(func() { applePolicyAuthorEditor = prev })
	applePolicyAuthorEditor = func(string) tea.Cmd {
		return func() tea.Msg { return applePolicyAuthorEditorMsg{} }
	}
	p := apple_mdm.Payload{Type: "com.apple.security.firewall", Title: "Firewall"}
	s := NewAppleMDMPayloadsAuthoringScreen(p)
	// Don't type anything; press Enter immediately.
	_, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.policyName == "" {
		t.Error("policyName should fall back to suggested when input is empty")
	}
	if !strings.Contains(s.policyName, "Firewall") {
		t.Errorf("suggested name should include title, got %q", s.policyName)
	}
	if s.tmpDir != "" {
		_ = os.RemoveAll(s.tmpDir)
	}
}

func TestAuthoring_EditorErrorRoutesToErrorsStage(t *testing.T) {
	prev := applePolicyAuthorEditor
	t.Cleanup(func() { applePolicyAuthorEditor = prev })
	applePolicyAuthorEditor = func(string) tea.Cmd {
		return func() tea.Msg {
			return applePolicyAuthorEditorMsg{err: os.ErrPermission}
		}
	}
	p := apple_mdm.Payload{Type: "com.example.test"}
	s := NewAppleMDMPayloadsAuthoringScreen(p)
	s.name.SetValue("test")
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd() // executes editor
	s.Update(msg)
	if s.stage != authorStageErrors {
		t.Errorf("editor error should route to errors stage, got %d", s.stage)
	}
	if len(s.validateErrs) == 0 {
		t.Error("expected an error to surface in validateErrs")
	}
	if s.tmpDir != "" {
		_ = os.RemoveAll(s.tmpDir)
	}
}

func TestAuthoring_RoundTripFromSkeletonToPreview(t *testing.T) {
	// End-to-end: skeleton written → operator "edits" by replacing
	// the file with valid content → editor returns success → screen
	// validates + emits → preview stage entered.
	prev := applePolicyAuthorEditor
	t.Cleanup(func() { applePolicyAuthorEditor = prev })

	var capturedPath string
	applePolicyAuthorEditor = func(path string) tea.Cmd {
		capturedPath = path
		// Simulate the operator writing a complete values file.
		os.WriteFile(path, []byte("RequiredKey: hello\n"), 0o600)
		return func() tea.Msg { return applePolicyAuthorEditorMsg{} }
	}

	p := apple_mdm.Payload{
		Type:  "com.example.test",
		Title: "Test",
		Keys: []apple_mdm.Key{
			{Name: "RequiredKey", Type: "string", Presence: "required"},
		},
	}
	s := NewAppleMDMPayloadsAuthoringScreen(p)
	s.name.SetValue("My Test")

	// Enter → starts editor (stage transitions to editing).
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Run the editor cmd, which now overwrites the file + emits the
	// finished message.
	editorDone := cmd()
	s.Update(editorDone)
	if s.stage != authorStageValidating {
		t.Errorf("after editor returns, stage = %d, want authorStageValidating", s.stage)
	}
	// Validation is its own cmd we have to invoke. We re-issue it via
	// the editor-finished handler's tea.Cmd.
	_, validateCmd := s.Update(applePolicyAuthorEditorMsg{})
	if validateCmd != nil {
		validateMsg := validateCmd()
		s.Update(validateMsg)
	}

	if s.stage != authorStagePreview {
		t.Errorf("expected preview stage after validation, got %d (errors=%v)", s.stage, s.validateErrs)
	}
	if len(s.mobileconfig) == 0 {
		t.Error("expected non-empty mobileconfig in preview")
	}
	if s.tmpDir != "" {
		_ = os.RemoveAll(s.tmpDir)
	}
	_ = capturedPath
}

func TestAuthoring_InvalidYAMLRoutesToErrors(t *testing.T) {
	prev := applePolicyAuthorEditor
	t.Cleanup(func() { applePolicyAuthorEditor = prev })
	applePolicyAuthorEditor = func(path string) tea.Cmd {
		// Simulate an edit that produces malformed YAML.
		os.WriteFile(path, []byte("bogus: : :\n"), 0o600)
		return func() tea.Msg { return applePolicyAuthorEditorMsg{} }
	}

	p := apple_mdm.Payload{Type: "com.example.test", Keys: []apple_mdm.Key{{Name: "X", Type: "string"}}}
	s := NewAppleMDMPayloadsAuthoringScreen(p)
	s.name.SetValue("X")

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	editorDone := cmd()
	s.Update(editorDone)
	if s.stage != authorStageValidating {
		t.Fatalf("stage = %d, want validating", s.stage)
	}
	_, vCmd := s.Update(applePolicyAuthorEditorMsg{})
	if vCmd != nil {
		s.Update(vCmd())
	}
	if s.stage != authorStageErrors {
		t.Errorf("invalid YAML should route to errors stage, got %d", s.stage)
	}
	if s.tmpDir != "" {
		_ = os.RemoveAll(s.tmpDir)
	}
}

func TestSafeFilename_StripsParens(t *testing.T) {
	if got := safeFilename("com.apple.MCX(FileVault2)"); got != "com.apple.MCX_FileVault2_" {
		t.Errorf("got %q", got)
	}
}
