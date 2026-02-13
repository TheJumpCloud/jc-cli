package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	flags := []string{"output", "verbose", "debug", "quiet", "force", "non-interactive", "no-cache", "no-color", "plan", "org", "api-key"}
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
