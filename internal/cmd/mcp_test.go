package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestMcpCmd_Help(t *testing.T) {
	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"mcp", "--help"})
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "MCP") {
		t.Error("expected help to mention MCP")
	}
	if !strings.Contains(output, "serve") {
		t.Error("expected help to mention serve subcommand")
	}
}

func TestMcpServeCmd_Help(t *testing.T) {
	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"mcp", "serve", "--help"})
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "stdin/stdout") {
		t.Error("expected help to mention stdin/stdout transport")
	}
	if !strings.Contains(output, "rate-limit") {
		t.Error("expected help to mention rate-limit flag")
	}
	if !strings.Contains(output, "read-only") {
		t.Error("expected help to mention read-only flag")
	}
}

func TestMcpServeCmd_FlagDefaults(t *testing.T) {
	rootCmd := NewRootCmd()
	mcpCmd, _, err := rootCmd.Find([]string{"mcp", "serve"})
	if err != nil {
		t.Fatalf("find mcp serve: %v", err)
	}

	rl := mcpCmd.Flags().Lookup("rate-limit")
	if rl == nil {
		t.Fatal("expected rate-limit flag")
	}
	if rl.DefValue != "60" {
		t.Errorf("expected rate-limit default 60, got %s", rl.DefValue)
	}

	ro := mcpCmd.Flags().Lookup("read-only")
	if ro == nil {
		t.Fatal("expected read-only flag")
	}
	if ro.DefValue != "false" {
		t.Errorf("expected read-only default false, got %s", ro.DefValue)
	}
}

func TestRootCmd_IncludesMcp(t *testing.T) {
	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"--help"})
	rootCmd.Execute()

	output := out.String()
	if !strings.Contains(output, "mcp") {
		t.Error("expected root help to include 'mcp' command")
	}
}

func TestMcpServeCmd_AuditLogPath(t *testing.T) {
	rootCmd := NewRootCmd()
	mcpCmd, _, err := rootCmd.Find([]string{"mcp", "serve"})
	if err != nil {
		t.Fatalf("find mcp serve: %v", err)
	}

	help := mcpCmd.Long
	if !strings.Contains(help, "audit") {
		t.Error("expected serve long help to mention audit logging")
	}
}

func TestMcpCmd_ClaudeDesktopConfig(t *testing.T) {
	rootCmd := NewRootCmd()
	mcpCmd, _, err := rootCmd.Find([]string{"mcp"})
	if err != nil {
		t.Fatalf("find mcp: %v", err)
	}

	if !strings.Contains(mcpCmd.Long, "mcpServers") {
		t.Error("expected mcp help to include Claude Desktop config example")
	}
}
