package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/config"
)

func TestVersionCommand(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.HasPrefix(got, "jc v") {
		t.Errorf("expected version output to start with 'jc v', got %q", got)
	}
}

func TestRootHelp(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "JumpCloud") {
		t.Errorf("expected help to contain 'JumpCloud', got %q", got)
	}
	if !strings.Contains(got, "version") {
		t.Errorf("expected help to list 'version' subcommand, got %q", got)
	}
	if !strings.Contains(got, "completion") {
		t.Errorf("expected help to list 'completion' subcommand, got %q", got)
	}
}

func TestGlobalFlags(t *testing.T) {
	rootCmd := NewRootCmd()

	flags := []string{"output", "table", "verbose", "debug", "quiet", "force", "non-interactive", "no-cache", "no-color", "plan", "org", "api-key", "ids", "fields", "exclude", "all"}
	for _, flag := range flags {
		if rootCmd.PersistentFlags().Lookup(flag) == nil {
			t.Errorf("expected persistent flag %q to be registered", flag)
		}
	}
}

func TestShortFlags(t *testing.T) {
	rootCmd := NewRootCmd()

	shorts := map[string]string{
		"output":  "o",
		"table":   "t",
		"verbose": "v",
		"quiet":   "q",
		"force":   "f",
	}
	for name, short := range shorts {
		f := rootCmd.PersistentFlags().Lookup(name)
		if f == nil {
			t.Errorf("flag %q not found", name)
			continue
		}
		if f.Shorthand != short {
			t.Errorf("flag %q: expected shorthand %q, got %q", name, short, f.Shorthand)
		}
	}
}

func TestVersionShortFlag(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"-V"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.HasPrefix(got, "jc v") {
		t.Errorf("expected -V output to start with 'jc v', got %q", got)
	}
}

func TestVersionFlag(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"--version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.HasPrefix(got, "jc v") {
		t.Errorf("expected --version output to start with 'jc v', got %q", got)
	}
}

func TestCompletionCommand(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			rootCmd := NewRootCmd()
			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetArgs([]string{"completion", shell})

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("completion %s: unexpected error: %v", shell, err)
			}

			if buf.Len() == 0 {
				t.Errorf("completion %s: expected output, got empty", shell)
			}
		})
	}
}

// --- Shell Completions Tests (US-019) ---

func TestCompletionBash_ContainsShellFunctions(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"completion", "bash"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "jc") {
		t.Error("bash completion should reference 'jc' command")
	}
	if !strings.Contains(out, "__jc_handle_go_custom_completion") {
		t.Error("bash completion should contain custom completion handler function")
	}
}

func TestCompletionZsh_ContainsCompdef(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"completion", "zsh"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// Zsh completions should contain compdef directive.
	if !strings.Contains(out, "compdef") {
		t.Error("zsh completion should contain 'compdef' directive")
	}
}

func TestCompletionFish_ContainsComplete(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"completion", "fish"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// Fish completions use 'complete' command for registration.
	if !strings.Contains(out, "complete") {
		t.Error("fish completion should contain 'complete' command")
	}
}

func TestCompletionInvalidShell(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"completion", "powershell"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for unsupported shell 'powershell'")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("expected 'unsupported shell' error, got: %v", err)
	}
}

func TestCompletionMissingArg(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"completion"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no shell argument provided")
	}
}

func TestCompletionIncludesSubcommands(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"completion", "bash"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// Completion scripts should include all registered subcommands.
	for _, sub := range []string{"users", "devices", "auth", "version", "completion"} {
		if !strings.Contains(out, sub) {
			t.Errorf("bash completion should include subcommand %q", sub)
		}
	}
}

func TestCompletionIncludesGlobalFlags(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"completion", "bash"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// Completion scripts should include global flags.
	for _, flag := range []string{"--output", "--verbose", "--debug", "--quiet", "--force"} {
		if !strings.Contains(out, flag) {
			t.Errorf("bash completion should include global flag %q", flag)
		}
	}
}

func TestCompletionOutputFlagValues(t *testing.T) {
	// Cobra's bash completion uses the __complete binary mechanism at runtime
	// to resolve flag values. Verify this works via Cobra's __complete command.
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"__complete", "--output", ""})

	_ = rootCmd.Execute()
	out := buf.String()
	for _, format := range validOutputFormats {
		if !strings.Contains(out, format) {
			t.Errorf("--output completion should suggest %q, got:\n%s", format, out)
		}
	}
}

func TestCompletionHelp_ShowsInstallInstructions(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"completion", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	for _, expect := range []string{"Bash:", "Zsh:", "Fish:", ".bashrc"} {
		if !strings.Contains(out, expect) {
			t.Errorf("completion help should contain %q, got:\n%s", expect, out)
		}
	}
}

func TestCompletionValidArgs(t *testing.T) {
	rootCmd := NewRootCmd()
	completionCmd, _, _ := rootCmd.Find([]string{"completion"})
	if completionCmd == nil {
		t.Fatal("expected to find 'completion' subcommand")
	}

	expected := []string{"bash", "zsh", "fish"}
	if len(completionCmd.ValidArgs) != len(expected) {
		t.Fatalf("expected %d valid args, got %d", len(expected), len(completionCmd.ValidArgs))
	}
	for i, want := range expected {
		if completionCmd.ValidArgs[i] != want {
			t.Errorf("ValidArgs[%d] = %q, want %q", i, completionCmd.ValidArgs[i], want)
		}
	}
}

func TestOutputFlagCompletionRegistered(t *testing.T) {
	// Verify that the output flag has a completion function by using
	// Cobra's __complete mechanism which invokes registered completions.
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	// Cobra's built-in __complete command triggers completion.
	rootCmd.SetArgs([]string{"__complete", "--output", ""})

	// __complete may return error for incomplete commands, but output should
	// contain the format values.
	_ = rootCmd.Execute()
	out := buf.String()
	for _, format := range validOutputFormats {
		if !strings.Contains(out, format) {
			t.Errorf("output flag completion should suggest %q, got:\n%s", format, out)
		}
	}
}

// --- Global Flags Framework Tests (US-010) ---

// newTestRootWithSub creates a root command with a test subcommand that
// records which Viper keys are set when it executes. This triggers
// PersistentPreRunE which only fires for actual command execution.
func newTestRootWithSub() (*cobra.Command, *bytes.Buffer) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)

	sub := &cobra.Command{
		Use: "testcmd",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Just succeed — the PersistentPreRunE does the validation.
			return nil
		},
	}
	rootCmd.AddCommand(sub)
	return rootCmd, buf
}

func TestTableShortFlag(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	rootCmd, _ := newTestRootWithSub()
	rootCmd.SetArgs([]string{"-t", "testcmd"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := viper.GetString("defaults.output"); got != "table" {
		t.Errorf("defaults.output = %q, want %q (-t should set output to table)", got, "table")
	}
}

func TestTableLongFlag(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	rootCmd, _ := newTestRootWithSub()
	rootCmd.SetArgs([]string{"--table", "testcmd"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := viper.GetString("defaults.output"); got != "table" {
		t.Errorf("defaults.output = %q, want %q (--table should set output to table)", got, "table")
	}
}

func TestOutputFormatValidation_Valid(t *testing.T) {
	for _, format := range validOutputFormats {
		t.Run(format, func(t *testing.T) {
			viper.Reset()
			defer viper.Reset()

			rootCmd, _ := newTestRootWithSub()
			rootCmd.SetArgs([]string{"--output", format, "testcmd"})

			if err := rootCmd.Execute(); err != nil {
				t.Errorf("valid format %q should not error, got: %v", format, err)
			}
		})
	}
}

func TestOutputFormatValidation_Invalid(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	rootCmd, _ := newTestRootWithSub()
	rootCmd.SetArgs([]string{"--output", "xml", "testcmd"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid output format 'xml'")
	}
	if !strings.Contains(err.Error(), "unknown output format") {
		t.Errorf("expected 'unknown output format' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "json, table, csv, human, yaml, ndjson") {
		t.Errorf("expected valid formats listed in error, got: %v", err)
	}
}

func TestFieldsAndExcludeMutuallyExclusive(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	rootCmd, _ := newTestRootWithSub()
	rootCmd.SetArgs([]string{"--fields", "username", "--exclude", "email", "testcmd"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when both --fields and --exclude are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should mention mutual exclusivity, got: %v", err)
	}
}

func TestUnknownFlagSuggestion(t *testing.T) {
	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"--verbos"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown flag --verbos")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "Did you mean") {
		t.Errorf("expected suggestion in error, got: %v", errMsg)
	}
	if !strings.Contains(errMsg, "--verbose") {
		t.Errorf("expected --verbose suggestion, got: %v", errMsg)
	}
}

func TestUnknownFlagNoSuggestion(t *testing.T) {
	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"--zzzzzzz"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown flag --zzzzzzz")
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "Did you mean") {
		t.Errorf("should not suggest anything for --zzzzzzz, got: %v", errMsg)
	}
}

func TestFlagsViperBinding(t *testing.T) {
	// Verify that each persistent flag is correctly bound to a Viper key.
	tests := []struct {
		flag     string
		viperKey string
		args     []string
		expected string
	}{
		{"output", "defaults.output", []string{"--output", "table", "--help"}, "table"},
		{"verbose", "verbose", []string{"--verbose", "--help"}, ""},
		{"debug", "debug", []string{"--debug", "--help"}, ""},
		{"quiet", "quiet", []string{"--quiet", "--help"}, ""},
		{"force", "force", []string{"--force", "--help"}, ""},
		{"org", "org", []string{"--org", "myorg", "--help"}, "myorg"},
		{"fields", "fields", []string{"--fields", "username,email", "--help"}, "username,email"},
		{"exclude", "exclude", []string{"--exclude", "password_date", "--help"}, "password_date"},
		{"all", "all", []string{"--all", "--help"}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.flag, func(t *testing.T) {
			viper.Reset()
			defer viper.Reset()

			rootCmd := NewRootCmd()
			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetArgs(tc.args)

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.expected != "" {
				if got := viper.GetString(tc.viperKey); got != tc.expected {
					t.Errorf("viper key %q = %q, want %q", tc.viperKey, got, tc.expected)
				}
			} else {
				// Boolean flags: just check they're set to true.
				if !viper.GetBool(tc.viperKey) {
					t.Errorf("viper key %q should be true when flag is set", tc.viperKey)
				}
			}
		})
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"verbose", "verbos", 1},
		{"debug", "debuf", 1},
		{"output", "outpu", 1},
		{"force", "fource", 1},
		{"abc", "xyz", 3},
	}
	for _, tc := range tests {
		if got := levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestFlagsInheritedBySubcommands(t *testing.T) {
	rootCmd := NewRootCmd()

	// auth is a subcommand — its inherited flags should include all persistent flags.
	authCmd, _, _ := rootCmd.Find([]string{"auth"})
	if authCmd == nil {
		t.Fatal("expected to find 'auth' subcommand")
	}

	// Check that persistent flags are inherited.
	inherited := authCmd.InheritedFlags()
	for _, name := range []string{"output", "verbose", "debug", "quiet", "force", "plan", "org", "fields", "exclude", "all"} {
		if inherited.Lookup(name) == nil {
			t.Errorf("auth command should inherit persistent flag %q", name)
		}
	}
}

// --- Alias Expansion Tests (US-054) ---

func TestExpandAliases_NoAlias(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	args := []string{"users", "list"}
	expanded, warning := expandAliases(args)
	if warning != "" {
		t.Errorf("unexpected warning: %s", warning)
	}
	if len(expanded) != 2 || expanded[0] != "users" || expanded[1] != "list" {
		t.Errorf("expected unchanged args, got: %v", expanded)
	}
}

func TestExpandAliases_MatchesAlias(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	viper.Set("aliases", map[string]interface{}{
		"inactive": "users list --filter 'suspended=true' -t",
	})

	args := []string{"inactive"}
	expanded, warning := expandAliases(args)
	if warning != "" {
		t.Errorf("unexpected warning: %s", warning)
	}
	// Should expand to: users list --filter 'suspended=true' -t
	if len(expanded) != 5 {
		t.Fatalf("expected 5 tokens, got %d: %v", len(expanded), expanded)
	}
	if expanded[0] != "users" || expanded[1] != "list" {
		t.Errorf("expected 'users list' prefix, got: %v", expanded)
	}
}

func TestExpandAliases_WithTrailingArgs(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	viper.Set("aliases", map[string]interface{}{
		"suspended": "users list --filter 'suspended=true'",
	})

	// User types: jc suspended -t --limit 10
	args := []string{"suspended", "-t", "--limit", "10"}
	expanded, warning := expandAliases(args)
	if warning != "" {
		t.Errorf("unexpected warning: %s", warning)
	}
	// Should be: users list --filter 'suspended=true' -t --limit 10
	if len(expanded) != 7 {
		t.Fatalf("expected 7 tokens, got %d: %v", len(expanded), expanded)
	}
	if expanded[0] != "users" {
		t.Errorf("expected 'users' first, got: %s", expanded[0])
	}
	// Trailing args preserved.
	if expanded[4] != "-t" || expanded[5] != "--limit" || expanded[6] != "10" {
		t.Errorf("trailing args not preserved: %v", expanded[4:])
	}
}

func TestExpandAliases_BuiltinConflict(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	viper.Set("aliases", map[string]interface{}{
		"users": "devices list",
	})

	args := []string{"users", "list"}
	expanded, warning := expandAliases(args)
	// Built-in takes precedence — args unchanged.
	if len(expanded) != 2 || expanded[0] != "users" || expanded[1] != "list" {
		t.Errorf("built-in conflict should leave args unchanged, got: %v", expanded)
	}
	if !strings.Contains(warning, "conflicts with built-in") {
		t.Errorf("expected conflict warning, got: %s", warning)
	}
}

func TestExpandAliases_EmptyArgs(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	args := []string{}
	expanded, warning := expandAliases(args)
	if warning != "" {
		t.Errorf("unexpected warning: %s", warning)
	}
	if len(expanded) != 0 {
		t.Errorf("expected empty args, got: %v", expanded)
	}
}

func TestExpandAliases_NoAliasConfigured(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	args := []string{"nonexistent"}
	expanded, warning := expandAliases(args)
	if warning != "" {
		t.Errorf("unexpected warning: %s", warning)
	}
	if len(expanded) != 1 || expanded[0] != "nonexistent" {
		t.Errorf("expected unchanged args, got: %v", expanded)
	}
}

func TestExpandAliases_WithGlobalFlagsBeforeAlias(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	viper.Set("aliases", map[string]interface{}{
		"inactive": "users list --filter 'suspended=true'",
	})

	// User types: jc --verbose inactive
	args := []string{"--verbose", "inactive"}
	expanded, warning := expandAliases(args)
	if warning != "" {
		t.Errorf("unexpected warning: %s", warning)
	}
	// Should be: --verbose users list --filter 'suspended=true'
	if len(expanded) != 5 {
		t.Fatalf("expected 5 tokens, got %d: %v", len(expanded), expanded)
	}
	if expanded[0] != "--verbose" {
		t.Errorf("global flag should be preserved, got: %s", expanded[0])
	}
	if expanded[1] != "users" {
		t.Errorf("alias expansion should follow flag, got: %s", expanded[1])
	}
}

func TestAlias_IntegrationWithCommand(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	// Set up an alias that maps to a known command.
	viper.Set("aliases", map[string]interface{}{
		"ver": "version",
	})

	// Test the full flow: alias expansion → Cobra execution.
	args := []string{"ver"}
	expanded, _ := expandAliases(args)

	rootCmd := NewRootCmd()
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs(expanded)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(stdout.String(), "jc v") {
		t.Errorf("alias 'ver' should expand to 'version' command, got: %s", stdout.String())
	}
}

func TestAlias_ConfigSetAndExpand(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	// Set alias via config.
	if err := config.SetConfigValue("aliases.v", "version"); err != nil {
		t.Fatalf("SetConfigValue() error: %v", err)
	}

	// Verify expansion works.
	args := []string{"v"}
	expanded, warning := expandAliases(args)
	if warning != "" {
		t.Errorf("unexpected warning: %s", warning)
	}
	if len(expanded) != 1 || expanded[0] != "version" {
		t.Errorf("expected expanded to ['version'], got: %v", expanded)
	}
}

func TestAlias_VisibleInConfigView(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`aliases:
  inactive: "users list -t"
`), 0600)

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	// Run config view and check alias is visible.
	rootCmd := NewRootCmd()
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"config", "view"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stdout.String(), "inactive") {
		t.Error("config view should show aliases")
	}
	if !strings.Contains(stdout.String(), "users list -t") {
		t.Error("config view should show alias expansion")
	}
}

func TestAlias_ConfigSetHelp(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	rootCmd := NewRootCmd()
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"config", "set", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	helpText := stdout.String()
	if !strings.Contains(helpText, "aliases.inactive") {
		t.Error("config set help should show alias example")
	}
}

func TestPlanFlagIsPersistent(t *testing.T) {
	rootCmd := NewRootCmd()
	flag := rootCmd.PersistentFlags().Lookup("plan")
	if flag == nil {
		t.Fatal("expected --plan to be a persistent flag")
	}
}

// --- Pipe Detection Tests (US-056) ---

func TestNoColorFlagRegistered(t *testing.T) {
	rootCmd := NewRootCmd()
	flag := rootCmd.PersistentFlags().Lookup("no-color")
	if flag == nil {
		t.Fatal("expected --no-color to be a persistent flag")
	}
}

func TestNoColorFlagDisablesColor(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("NO_COLOR", "")
	t.Setenv("JC_NO_COLOR", "")

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	rootCmd, _ := newTestRootWithSub()
	rootCmd.SetArgs([]string{"--no-color", "testcmd"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !config.NoColor() {
		t.Error("config.NoColor() should be true when --no-color flag is set")
	}
}

func TestNoColorEnvVarWorks(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("NO_COLOR", "1")

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	if !config.NoColor() {
		t.Error("config.NoColor() should be true when NO_COLOR env var is set")
	}
}

func TestJCNoColorEnvVarWorks(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("NO_COLOR", "")
	t.Setenv("JC_NO_COLOR", "1")

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	if !config.NoColor() {
		t.Error("config.NoColor() should be true when JC_NO_COLOR env var is set")
	}
}

// --- Priority Chain Tests (US-003) ---

// TestPriorityChain_FlagOverridesEnvOverridesConfig verifies the full
// priority chain: flags > env vars > config file > built-in defaults.
func TestPriorityChain_FlagOverridesEnvOverridesConfig(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	// Config file says csv.
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("defaults:\n  output: csv\n"), 0600)

	// Env var says table.
	t.Setenv("JC_OUTPUT", "table")

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)

	// Flag says yaml — this should win over everything.
	rootCmd.SetArgs([]string{"--output", "yaml", "--help"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After flag parsing, the Viper value should reflect the flag.
	if got := viper.GetString("defaults.output"); got != "yaml" {
		t.Errorf("defaults.output = %q, want %q (flag should override env and config)", got, "yaml")
	}
}

func TestPriorityChain_EnvOverridesConfig(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	// Config file says csv.
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("defaults:\n  output: csv\n"), 0600)

	// Env var says table.
	t.Setenv("JC_OUTPUT", "table")

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)

	// Run with --help (no --output flag) to trigger flag parsing without the output flag.
	rootCmd.SetArgs([]string{"--help"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without flag, env should win over config.
	if got := viper.GetString("defaults.output"); got != "table" {
		t.Errorf("defaults.output = %q, want %q (env should override config)", got, "table")
	}
}

func TestPriorityChain_ConfigOverridesDefaults(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_OUTPUT", "")

	// Config file says csv (not the default "json").
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("defaults:\n  output: csv\n"), 0600)

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"--help"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Config should win over built-in defaults.
	if got := viper.GetString("defaults.output"); got != "csv" {
		t.Errorf("defaults.output = %q, want %q (config should override default)", got, "csv")
	}
}
