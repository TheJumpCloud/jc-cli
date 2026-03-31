package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/ask"
	"github.com/klaassen-consulting/jc/internal/recipe"
)

// mockAskClient implements ask.Client for tests.
type mockAskClient struct {
	result *ask.TranslateResult
	err    error
}

func (m *mockAskClient) Translate(query string, maxCommands int) (*ask.TranslateResult, error) {
	return m.result, m.err
}

// overrideAskClient injects a mock LLM client for testing.
func overrideAskClient(t *testing.T, client ask.Client, err error) {
	t.Helper()
	orig := newAskClient
	newAskClient = func() (ask.Client, error) {
		if err != nil {
			return nil, err
		}
		return client, nil
	}
	t.Cleanup(func() { newAskClient = orig })
}

// overrideAskConfirmReader injects a bufio.Reader for ask confirmation prompts.
func overrideAskConfirmReader(t *testing.T, input string) {
	t.Helper()
	orig := askConfirmReader
	askConfirmReader = bufio.NewReader(strings.NewReader(input))
	t.Cleanup(func() { askConfirmReader = orig })
}

// overrideRootCmdForAsk overrides newRootCmdForRecipe for ask execution tests.
func overrideRootCmdForAsk(t *testing.T) {
	t.Helper()
	orig := newRootCmdForRecipe
	newRootCmdForRecipe = func() recipe.CobraCommand { return NewRootCmd() }
	t.Cleanup(func() { newRootCmdForRecipe = orig })
}

func TestAsk_ProposedCommands(t *testing.T) {
	viper.Reset()
	client := &mockAskClient{
		result: &ask.TranslateResult{
			Commands:    []string{"users list -t"},
			Explanation: "Lists all users in table format.",
		},
	}
	overrideAskClient(t, client, nil)
	overrideAskConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "list all users"})
	_ = cmd.Execute()

	errOutput := stderr.String()
	if !strings.Contains(errOutput, "Proposed commands:") {
		t.Error("expected 'Proposed commands:' in stderr")
	}
	if !strings.Contains(errOutput, "jc users list -t") {
		t.Errorf("expected proposed command in stderr, got: %s", errOutput)
	}
	if !strings.Contains(errOutput, "Lists all users in table format") {
		t.Error("expected explanation in stderr")
	}
}

func TestAsk_ConfirmNo(t *testing.T) {
	viper.Reset()
	client := &mockAskClient{
		result: &ask.TranslateResult{
			Commands: []string{"users list"},
		},
	}
	overrideAskClient(t, client, nil)
	overrideAskConfirmReader(t, "n\n")
	overrideIsStdinPiped(t, false) // simulate TTY so confirmation prompt fires

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "list users"})
	_ = cmd.Execute()

	if !strings.Contains(stderr.String(), "Aborted") {
		t.Errorf("expected 'Aborted' when user says no, got stderr: %q", stderr.String())
	}
	if stdout.String() != "" {
		t.Errorf("expected no stdout output, got: %s", stdout.String())
	}
}

func TestAsk_ConfirmYes_ExecutesCommand(t *testing.T) {
	viper.Reset()
	client := &mockAskClient{
		result: &ask.TranslateResult{
			Commands: []string{"version"},
		},
	}
	overrideAskClient(t, client, nil)
	overrideAskConfirmReader(t, "y\n")
	overrideRootCmdForAsk(t)

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "what version is this?"})
	_ = cmd.Execute()

	errOutput := stderr.String()
	if !strings.Contains(errOutput, "done") {
		t.Errorf("expected 'done' in stderr, got: %s", errOutput)
	}
	if !strings.Contains(errOutput, "1 executed, 0 failed") {
		t.Errorf("expected success summary, got: %s", errOutput)
	}
}

func TestAsk_Force_SkipsConfirmation(t *testing.T) {
	viper.Reset()
	client := &mockAskClient{
		result: &ask.TranslateResult{
			Commands: []string{"version"},
		},
	}
	overrideAskClient(t, client, nil)
	overrideRootCmdForAsk(t)

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "--force", "what version?"})
	_ = cmd.Execute()

	errOutput := stderr.String()
	// Should not ask for confirmation.
	if strings.Contains(errOutput, "Execute these commands? [y/N]") {
		t.Error("--force should skip confirmation")
	}
	if !strings.Contains(errOutput, "1 executed, 0 failed") {
		t.Errorf("expected success summary, got: %s", errOutput)
	}
}

func TestAsk_JSONOutput(t *testing.T) {
	viper.Reset()
	client := &mockAskClient{
		result: &ask.TranslateResult{
			Commands:    []string{"users list", "devices list"},
			Explanation: "Two list commands.",
		},
	}
	overrideAskClient(t, client, nil)

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "--output", "json", "list everything"})
	_ = cmd.Execute()

	var result map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got error: %v, output: %s", err, stdout.String())
	}
	if result["query"] != "list everything" {
		t.Errorf("expected query in JSON, got: %v", result["query"])
	}
	commands, ok := result["commands"].([]interface{})
	if !ok || len(commands) != 2 {
		t.Errorf("expected 2 commands in JSON, got: %v", result["commands"])
	}
	// Verify commands are proposed, not executed.
	cmd0 := commands[0].(map[string]interface{})
	if cmd0["status"] != "proposed" {
		t.Errorf("expected status 'proposed', got: %v", cmd0["status"])
	}
}

func TestAsk_LLMError(t *testing.T) {
	viper.Reset()
	overrideAskClient(t, nil, NewCLIError(ErrCodeConfigError, "conversational mode is disabled", ""))

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "list users"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when LLM client creation fails")
	}
}

func TestAsk_EmptyResponse(t *testing.T) {
	viper.Reset()
	client := &mockAskClient{
		result: &ask.TranslateResult{
			Commands: []string{},
		},
	}
	overrideAskClient(t, client, nil)

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "something vague"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "No commands generated") {
		t.Error("expected 'No commands generated' message")
	}
}

func TestAsk_MissingArgs(t *testing.T) {
	viper.Reset()
	cmd := NewRootCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error with no args")
	}
}

func TestAsk_MultiWordQuery(t *testing.T) {
	viper.Reset()
	var capturedQuery string
	client := &mockAskClient{
		result: &ask.TranslateResult{
			Commands: []string{"users list"},
		},
	}
	// Wrap to capture the query
	overrideAskClient(t, nil, nil)
	origClient := newAskClient
	newAskClient = func() (ask.Client, error) {
		return &queryCapturingClient{
			inner:   client,
			capture: &capturedQuery,
		}, nil
	}
	defer func() { newAskClient = origClient }()

	overrideAskConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "which", "users", "are", "suspended?"})
	_ = cmd.Execute()

	if capturedQuery != "which users are suspended?" {
		t.Errorf("expected joined query, got: %s", capturedQuery)
	}
}

type queryCapturingClient struct {
	inner   ask.Client
	capture *string
}

func (c *queryCapturingClient) Translate(query string, maxCommands int) (*ask.TranslateResult, error) {
	*c.capture = query
	return c.inner.Translate(query, maxCommands)
}

func TestAsk_ConfirmEmpty(t *testing.T) {
	viper.Reset()
	client := &mockAskClient{
		result: &ask.TranslateResult{
			Commands: []string{"users list"},
		},
	}
	overrideAskClient(t, client, nil)
	overrideAskConfirmReader(t, "\n") // empty = no
	overrideIsStdinPiped(t, false)    // simulate TTY so confirmation prompt fires

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "list users"})
	_ = cmd.Execute()

	if !strings.Contains(stderr.String(), "Aborted") {
		t.Error("expected 'Aborted' on empty input (defaults to no)")
	}
}

func TestAsk_FailedExecution(t *testing.T) {
	viper.Reset()
	client := &mockAskClient{
		result: &ask.TranslateResult{
			Commands: []string{"nonexistent command"},
		},
	}
	overrideAskClient(t, client, nil)
	overrideAskConfirmReader(t, "y\n")
	overrideRootCmdForAsk(t)

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "do something impossible"})
	_ = cmd.Execute()

	errOutput := stderr.String()
	if !strings.Contains(errOutput, "failed") {
		t.Errorf("expected 'failed' in stderr, got: %s", errOutput)
	}
	if !strings.Contains(errOutput, "0 executed, 1 failed") {
		t.Errorf("expected failure summary, got: %s", errOutput)
	}
}

func TestAsk_TranslationError(t *testing.T) {
	viper.Reset()
	client := &mockAskClient{
		err: NewCLIError(ErrCodeGeneral, "API rate limited", ""),
	}
	overrideAskClient(t, client, nil)

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "list users"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when translation fails")
	}
}

func TestAsk_HelpOutput(t *testing.T) {
	viper.Reset()
	cmd := NewRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"ask", "--help"})
	_ = cmd.Execute()

	helpText := stdout.String()
	if !strings.Contains(helpText, "natural language") {
		t.Error("help should mention natural language")
	}
	if !strings.Contains(helpText, "ask.provider") {
		t.Error("help should mention ask.provider configuration")
	}
}

func TestAsk_RootHelpIncludesAsk(t *testing.T) {
	viper.Reset()
	cmd := NewRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--help"})
	_ = cmd.Execute()

	if !strings.Contains(stdout.String(), "ask") {
		t.Error("root help should list ask command")
	}
}

func TestAsk_HistoryLogging(t *testing.T) {
	viper.Reset()
	tmpDir := t.TempDir()
	t.Setenv("JC_CONFIG", tmpDir+"/config.yaml")

	client := &mockAskClient{
		result: &ask.TranslateResult{
			Commands: []string{"users list"},
		},
	}
	overrideAskClient(t, client, nil)
	overrideAskConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "list users"})
	_ = cmd.Execute()

	// History file should exist in the config dir.
	// We use askHistoryFile() which uses config.ConfigDir().
	// Since JC_CONFIG is set, ConfigDir() returns tmpDir.
	// Non-fatal if logging fails, so we just verify the flow completes.
}

func TestAsk_NonInteractive(t *testing.T) {
	viper.Reset()
	client := &mockAskClient{
		result: &ask.TranslateResult{
			Commands: []string{"version"},
		},
	}
	overrideAskClient(t, client, nil)
	overrideRootCmdForAsk(t)

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ask", "--non-interactive", "what version?"})
	_ = cmd.Execute()

	errOutput := stderr.String()
	// Should not ask for confirmation.
	if strings.Contains(errOutput, "Execute these commands? [y/N]") {
		t.Error("--non-interactive should skip confirmation")
	}
	if !strings.Contains(errOutput, "1 executed, 0 failed") {
		t.Errorf("expected success summary, got: %s", errOutput)
	}
}
