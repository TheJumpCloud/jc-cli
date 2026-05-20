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

	addrFlag := mcpCmd.Flags().Lookup("addr")
	if addrFlag == nil {
		t.Fatal("expected addr flag")
	}
	if addrFlag.DefValue != "" {
		t.Errorf("expected addr default empty, got %s", addrFlag.DefValue)
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

	stepUp := mcpCmd.Flags().Lookup("require-step-up")
	if stepUp == nil {
		t.Fatal("expected require-step-up flag")
	}
	if stepUp.DefValue != "false" {
		t.Errorf("expected require-step-up default false, got %s", stepUp.DefValue)
	}

	signDest := mcpCmd.Flags().Lookup("sign-destructive")
	if signDest == nil {
		t.Fatal("expected sign-destructive flag")
	}
	if signDest.DefValue != "false" {
		t.Errorf("expected sign-destructive default false, got %s", signDest.DefValue)
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

func TestResolveSSEAddr_DefaultLoopback(t *testing.T) {
	got := resolveSSEAddr("", 8080)
	if got != "127.0.0.1:8080" {
		t.Errorf("expected 127.0.0.1:8080, got %s", got)
	}

	got = resolveSSEAddr("", 9090)
	if got != "127.0.0.1:9090" {
		t.Errorf("expected 127.0.0.1:9090, got %s", got)
	}
}

func TestResolveSSEAddr_ExplicitAddr(t *testing.T) {
	got := resolveSSEAddr("0.0.0.0:3000", 8080)
	if got != "0.0.0.0:3000" {
		t.Errorf("expected 0.0.0.0:3000, got %s", got)
	}

	got = resolveSSEAddr("192.168.1.5:4000", 8080)
	if got != "192.168.1.5:4000" {
		t.Errorf("expected 192.168.1.5:4000, got %s", got)
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

func TestApplyProfileRole_NoRolePassesThrough(t *testing.T) {
	cases := []struct {
		flagChanged bool
		readOnly    bool
	}{
		{flagChanged: false, readOnly: false},
		{flagChanged: false, readOnly: true},
		{flagChanged: true, readOnly: false},
		{flagChanged: true, readOnly: true},
	}
	for _, c := range cases {
		got, warn, err := applyProfileRole("default", false, c.flagChanged, c.readOnly)
		if err != nil {
			t.Errorf("unexpected error for %+v: %v", c, err)
		}
		if warn != "" {
			t.Errorf("unexpected warning for %+v: %q", c, warn)
		}
		if got != c.readOnly {
			t.Errorf("for %+v: got readOnly=%v, want %v", c, got, c.readOnly)
		}
	}
}

func TestApplyProfileRole_ReadOnlyProfileForcesTrue(t *testing.T) {
	got, warn, err := applyProfileRole("reporting", true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Errorf("readOnly = false, want true (read-only profile, no flag)")
	}
	if !strings.Contains(warn, "reporting") {
		t.Errorf("warning missing profile name: %q", warn)
	}
	if !strings.Contains(warn, "read-only") {
		t.Errorf("warning missing read-only mention: %q", warn)
	}
}

func TestApplyProfileRole_ReadOnlyProfileSilentWhenFlagAgrees(t *testing.T) {
	got, warn, err := applyProfileRole("reporting", true, true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Errorf("readOnly = false, want true")
	}
	if warn != "" {
		t.Errorf("expected no warning when operator and profile agree, got %q", warn)
	}
}

func TestApplyProfileRole_ReadOnlyProfileRejectsExplicitFalse(t *testing.T) {
	got, warn, err := applyProfileRole("reporting", true, true, false)
	if err == nil {
		t.Fatal("expected error when --read-only=false is passed against a read-only profile")
	}
	if !strings.Contains(err.Error(), "reporting") {
		t.Errorf("error missing profile name: %v", err)
	}
	if !strings.Contains(err.Error(), "incompatible") {
		t.Errorf("error should call out the incompatibility: %v", err)
	}
	if got {
		t.Errorf("on error, readOnly should not be coerced to true; got %v", got)
	}
	if warn != "" {
		t.Errorf("on error, warning should be empty; got %q", warn)
	}
}
