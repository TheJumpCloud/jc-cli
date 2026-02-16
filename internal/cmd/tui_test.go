package cmd

import (
	"strings"
	"testing"
)

func TestTUICmd_HelpContainsDescription(t *testing.T) {
	cmd := newTUICmd()
	if !strings.Contains(cmd.Long, "interactive terminal UI") {
		t.Error("TUI command Long description should mention 'interactive terminal UI'")
	}
}

func TestTUICmd_Use(t *testing.T) {
	cmd := newTUICmd()
	if cmd.Use != "tui" {
		t.Errorf("Use = %q, want 'tui'", cmd.Use)
	}
}

func TestTUICmd_InBuiltinCommands(t *testing.T) {
	if !builtinCommands["tui"] {
		t.Error("'tui' should be in builtinCommands map")
	}
}
