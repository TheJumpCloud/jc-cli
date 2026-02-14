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

func TestMcpServeCmd_SSEHelpText(t *testing.T) {
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
	if !strings.Contains(output, "sse") {
		t.Error("expected help to mention SSE transport")
	}
	if !strings.Contains(output, "Server-Sent Events") {
		t.Error("expected help to describe SSE transport")
	}
	if !strings.Contains(output, "--transport") {
		t.Error("expected help to mention --transport flag")
	}
	if !strings.Contains(output, "--port") {
		t.Error("expected help to mention --port flag")
	}
	if !strings.Contains(output, "--tls-cert") {
		t.Error("expected help to mention --tls-cert flag")
	}
	if !strings.Contains(output, "--tls-key") {
		t.Error("expected help to mention --tls-key flag")
	}
	if !strings.Contains(output, "--cors-origin") {
		t.Error("expected help to mention --cors-origin flag")
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

	tp := mcpCmd.Flags().Lookup("transport")
	if tp == nil {
		t.Fatal("expected transport flag")
	}
	if tp.DefValue != "stdio" {
		t.Errorf("expected transport default 'stdio', got %s", tp.DefValue)
	}

	port := mcpCmd.Flags().Lookup("port")
	if port == nil {
		t.Fatal("expected port flag")
	}
	if port.DefValue != "8080" {
		t.Errorf("expected port default 8080, got %s", port.DefValue)
	}

	cert := mcpCmd.Flags().Lookup("tls-cert")
	if cert == nil {
		t.Fatal("expected tls-cert flag")
	}
	if cert.DefValue != "" {
		t.Errorf("expected tls-cert default empty, got %s", cert.DefValue)
	}

	key := mcpCmd.Flags().Lookup("tls-key")
	if key == nil {
		t.Fatal("expected tls-key flag")
	}
	if key.DefValue != "" {
		t.Errorf("expected tls-key default empty, got %s", key.DefValue)
	}

	cors := mcpCmd.Flags().Lookup("cors-origin")
	if cors == nil {
		t.Fatal("expected cors-origin flag")
	}
	if cors.DefValue != "" {
		t.Errorf("expected cors-origin default empty, got %s", cors.DefValue)
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

func TestMcpServeCmd_SSEExamples(t *testing.T) {
	rootCmd := NewRootCmd()
	mcpCmd, _, err := rootCmd.Find([]string{"mcp", "serve"})
	if err != nil {
		t.Fatalf("find mcp serve: %v", err)
	}

	help := mcpCmd.Long
	if !strings.Contains(help, "jc mcp serve --transport sse") {
		t.Error("expected help to include SSE example")
	}
	if !strings.Contains(help, "sse_port") {
		t.Error("expected help to mention sse_port config key")
	}
}

func TestMcpServeCmd_AllowBlockListDocs(t *testing.T) {
	rootCmd := NewRootCmd()
	mcpCmd, _, err := rootCmd.Find([]string{"mcp", "serve"})
	if err != nil {
		t.Fatalf("find mcp serve: %v", err)
	}

	help := mcpCmd.Long
	if !strings.Contains(help, "allowed_tools") {
		t.Error("expected help to mention allowed_tools config key")
	}
	if !strings.Contains(help, "blocked_tools") {
		t.Error("expected help to mention blocked_tools config key")
	}
	if !strings.Contains(help, "jc mcp tools") {
		t.Error("expected help to mention 'jc mcp tools' command")
	}
}

func TestMcpToolsCmd_Help(t *testing.T) {
	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"mcp", "tools", "--help"})
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "allow") {
		t.Error("expected help to mention allow")
	}
	if !strings.Contains(output, "block") {
		t.Error("expected help to mention block")
	}
}

func TestMcpToolsCmd_ListsTools(t *testing.T) {
	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"mcp", "tools"})
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "jc_ping") {
		t.Error("expected output to include jc_ping")
	}
	if !strings.Contains(output, "users_list") {
		t.Error("expected output to include users_list")
	}
	if !strings.Contains(output, "devices_list") {
		t.Error("expected output to include devices_list")
	}

	// Footer on stderr.
	errOutput := stderr.String()
	if !strings.Contains(errOutput, "tools") {
		t.Error("expected stderr to contain tool count footer")
	}
}

func TestMcpCmd_IncludesToolsSubcommand(t *testing.T) {
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
	if !strings.Contains(output, "tools") {
		t.Error("expected mcp help to mention tools subcommand")
	}
}
