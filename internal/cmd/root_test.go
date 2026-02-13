package cmd

import (
	"bytes"
	"strings"
	"testing"
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
