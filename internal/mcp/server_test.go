package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"

	"github.com/klaassen-consulting/jc/internal/config"
)

func setupTest(t *testing.T) {
	t.Helper()
	keyring.MockInit()

	// Use a temp config to avoid touching real config.
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
active_profile: default
profiles:
  default:
    api_key: "test-key-1234"
    org_id: "test-org-id"
  staging:
    api_key: "staging-key-5678"
    org_id: "staging-org-id"
`), 0600)
	t.Setenv("JC_CONFIG", cfgPath)
	viper.Reset()
	config.Init()
}

// connectTestServer creates a test MCP server and client session.
func connectTestServer(t *testing.T, opts Options) (*Server, *mcp.ClientSession) {
	t.Helper()

	// Override audit log path to temp dir.
	if opts.AuditLogPath == "" {
		opts.AuditLogPath = filepath.Join(t.TempDir(), "audit.log")
	}

	server := NewServer(opts)

	// Create in-memory transport pair.
	st, ct := mcp.NewInMemoryTransports()

	ctx := context.Background()

	// Connect server (must be first).
	ss, err := server.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { ss.Close() })

	// Connect client.
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0",
	}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })

	return server, cs
}

// --- Server initialization tests ---

func TestNewServer_Defaults(t *testing.T) {
	setupTest(t)
	opts := Options{
		AuditLogPath: filepath.Join(t.TempDir(), "audit.log"),
	}
	s := NewServer(opts)
	if s.mcpServer == nil {
		t.Fatal("expected mcpServer to be initialized")
	}
	if s.limiter.maxPerMin != 60 {
		t.Errorf("expected default rate limit 60, got %d", s.limiter.maxPerMin)
	}
	if s.readOnly != false {
		t.Error("expected readOnly to be false by default")
	}
}

func TestNewServer_CustomRateLimit(t *testing.T) {
	setupTest(t)
	opts := Options{
		RateLimit:    120,
		AuditLogPath: filepath.Join(t.TempDir(), "audit.log"),
	}
	s := NewServer(opts)
	if s.limiter.maxPerMin != 120 {
		t.Errorf("expected rate limit 120, got %d", s.limiter.maxPerMin)
	}
}

func TestNewServer_ReadOnly(t *testing.T) {
	setupTest(t)
	opts := Options{
		ReadOnly:     true,
		AuditLogPath: filepath.Join(t.TempDir(), "audit.log"),
	}
	s := NewServer(opts)
	if !s.readOnly {
		t.Error("expected readOnly to be true")
	}
}

// --- MCP protocol tests ---

func TestMCP_Initialize(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	// After connecting, the client should have received the server's initialize response.
	initResult := cs.InitializeResult()
	if initResult == nil {
		t.Fatal("expected InitializeResult to be non-nil")
	}
	if initResult.ServerInfo.Name != "jc" {
		t.Errorf("expected server name 'jc', got %q", initResult.ServerInfo.Name)
	}
	if initResult.Capabilities == nil {
		t.Fatal("expected capabilities to be non-nil")
	}
	if initResult.Capabilities.Tools == nil {
		t.Fatal("expected tools capability")
	}
	if initResult.Capabilities.Resources == nil {
		t.Fatal("expected resources capability")
	}
}

func TestMCP_ListTools(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	if len(result.Tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	// Find the jc_ping tool.
	var foundPing bool
	for _, tool := range result.Tools {
		if tool.Name == "jc_ping" {
			foundPing = true
			if tool.Description == "" {
				t.Error("expected jc_ping to have a description")
			}
		}
	}
	if !foundPing {
		t.Error("expected jc_ping tool to be registered")
	}
}

func TestMCP_CallTool_Ping(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "jc_ping",
	})
	if err != nil {
		t.Fatalf("CallTool jc_ping: %v", err)
	}
	if result.IsError {
		t.Fatal("expected jc_ping to succeed, got error")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content in ping response")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !strings.Contains(tc.Text, "running") {
		t.Errorf("expected ping response to contain 'running', got %q", tc.Text)
	}
}

func TestMCP_CallTool_UnknownTool(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	_, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "nonexistent_tool",
	})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestMCP_ListResources(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}

	if len(result.Resources) < 2 {
		t.Fatalf("expected at least 2 resources, got %d", len(result.Resources))
	}

	var foundServerInfo, foundProfiles bool
	for _, r := range result.Resources {
		switch r.URI {
		case "jc://server/info":
			foundServerInfo = true
			if r.MIMEType != "application/json" {
				t.Errorf("expected MIME type application/json, got %q", r.MIMEType)
			}
		case "jc://config/profiles":
			foundProfiles = true
		}
	}
	if !foundServerInfo {
		t.Error("expected jc://server/info resource")
	}
	if !foundProfiles {
		t.Error("expected jc://config/profiles resource")
	}
}

func TestMCP_ReadResource_ServerInfo(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{RateLimit: 120})

	ctx := context.Background()
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://server/info",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	if len(result.Contents) == 0 {
		t.Fatal("expected resource content")
	}

	var info map[string]any
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &info); err != nil {
		t.Fatalf("parse server info: %v", err)
	}

	if info["name"] != "jc" {
		t.Errorf("expected name 'jc', got %v", info["name"])
	}
	// Check rate_limit matches what we set.
	if rl, ok := info["rate_limit"].(float64); !ok || int(rl) != 120 {
		t.Errorf("expected rate_limit 120, got %v", info["rate_limit"])
	}
	if info["read_only"] != false {
		t.Errorf("expected read_only false, got %v", info["read_only"])
	}
}

func TestMCP_ReadResource_Profiles(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://config/profiles",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	if len(result.Contents) == 0 {
		t.Fatal("expected resource content")
	}

	var profiles map[string]any
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &profiles); err != nil {
		t.Fatalf("parse profiles: %v", err)
	}

	if profiles["active_profile"] != "default" {
		t.Errorf("expected active_profile 'default', got %v", profiles["active_profile"])
	}

	profileList, ok := profiles["profiles"].([]any)
	if !ok {
		t.Fatal("expected profiles to be an array")
	}
	if len(profileList) < 2 {
		t.Errorf("expected at least 2 profiles, got %d", len(profileList))
	}
}

func TestMCP_ReadResource_NotFound(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	_, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://nonexistent/resource",
	})
	if err == nil {
		t.Fatal("expected error for unknown resource")
	}
}

// --- Rate limiter tests ---

func TestRateLimiter_AllowsWithinLimit(t *testing.T) {
	rl := newRateLimiter(5)
	for i := 0; i < 5; i++ {
		if err := rl.allow(); err != nil {
			t.Fatalf("call %d should be allowed: %v", i, err)
		}
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	rl := newRateLimiter(3)
	for i := 0; i < 3; i++ {
		if err := rl.allow(); err != nil {
			t.Fatalf("call %d should be allowed: %v", i, err)
		}
	}
	if err := rl.allow(); err == nil {
		t.Fatal("expected rate limit error on 4th call")
	}
}

func TestRateLimiter_ResetsAfterWindow(t *testing.T) {
	rl := newRateLimiter(2)

	// Override nowFunc for deterministic test.
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	origNow := nowFunc
	nowFunc = func() time.Time { return baseTime }
	defer func() { nowFunc = origNow }()

	// Fill the bucket.
	rl.allow()
	rl.allow()
	if err := rl.allow(); err == nil {
		t.Fatal("expected rate limit")
	}

	// Advance time past the 1-minute window.
	nowFunc = func() time.Time { return baseTime.Add(61 * time.Second) }

	if err := rl.allow(); err != nil {
		t.Fatalf("should be allowed after time window: %v", err)
	}
}

func TestRateLimiter_ErrorMessage(t *testing.T) {
	rl := newRateLimiter(1)
	rl.allow()
	err := rl.allow()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Errorf("expected 'rate limit exceeded', got %q", err.Error())
	}
}

// --- Rate limiting integration test ---

func TestMCP_RateLimiting(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{RateLimit: 2})

	ctx := context.Background()

	// First two calls should succeed.
	for i := 0; i < 2; i++ {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "jc_ping"})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if result.IsError {
			t.Fatalf("call %d: unexpected error: %v", i, result.Content)
		}
	}

	// Third call should get rate limited (returned as tool error, not protocol error).
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "jc_ping"})
	if err != nil {
		t.Fatalf("rate limited call: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected rate limit error on 3rd call")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent in rate limit response")
	}
	if !strings.Contains(tc.Text, "rate limit exceeded") {
		t.Errorf("expected rate limit message, got %q", tc.Text)
	}
}

// --- Audit logging tests ---

func TestAuditLogger_WritesEntries(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	al := newAuditLogger(logPath)
	al.log("test_tool", json.RawMessage(`{"key":"value"}`), true, "")
	al.log("failing_tool", json.RawMessage(`{}`), false, "some error")
	al.close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d", len(lines))
	}

	// Parse first entry.
	var entry1 auditEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry1); err != nil {
		t.Fatalf("parse entry 1: %v", err)
	}
	if entry1.Tool != "test_tool" {
		t.Errorf("expected tool 'test_tool', got %q", entry1.Tool)
	}
	if !entry1.Success {
		t.Error("expected success=true")
	}

	// Parse second entry.
	var entry2 auditEntry
	if err := json.Unmarshal([]byte(lines[1]), &entry2); err != nil {
		t.Fatalf("parse entry 2: %v", err)
	}
	if entry2.Tool != "failing_tool" {
		t.Errorf("expected tool 'failing_tool', got %q", entry2.Tool)
	}
	if entry2.Success {
		t.Error("expected success=false")
	}
	if entry2.Error != "some error" {
		t.Errorf("expected error 'some error', got %q", entry2.Error)
	}
}

func TestAuditLogger_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "subdir", "audit.log")

	al := newAuditLogger(logPath)
	al.log("test", nil, true, "")
	al.close()

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("expected audit log file to be created")
	}
}

func TestMCP_AuditLogIntegration(t *testing.T) {
	setupTest(t)
	auditPath := filepath.Join(t.TempDir(), "audit.log")

	_, cs := connectTestServer(t, Options{AuditLogPath: auditPath})

	ctx := context.Background()
	cs.CallTool(ctx, &mcp.CallToolParams{Name: "jc_ping"})

	// Close the server's audit logger to flush.
	// (The test cleanup will close the connection, triggering server.auditLog.close())
	// Wait briefly for the audit log write.
	time.Sleep(50 * time.Millisecond)

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected audit log entries")
	}
	if !strings.Contains(string(data), "jc_ping") {
		t.Error("expected audit log to contain 'jc_ping'")
	}
	if !strings.Contains(string(data), `"success":true`) {
		t.Error("expected audit log to contain success:true")
	}
}

// --- Helper function tests ---

func TestTextResult(t *testing.T) {
	r := textResult("hello")
	if len(r.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(r.Content))
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", r.Content[0])
	}
	if tc.Text != "hello" {
		t.Errorf("expected 'hello', got %q", tc.Text)
	}
}

func TestErrorResult(t *testing.T) {
	r := errorResult("something failed")
	if !r.IsError {
		t.Error("expected IsError to be true")
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", r.Content[0])
	}
	if tc.Text != "something failed" {
		t.Errorf("expected error text, got %q", tc.Text)
	}
}

func TestJsonResult(t *testing.T) {
	data := map[string]string{"key": "value"}
	r, err := jsonResult(data)
	if err != nil {
		t.Fatalf("jsonResult: %v", err)
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", r.Content[0])
	}
	if !strings.Contains(tc.Text, `"key": "value"`) {
		t.Errorf("expected JSON output, got %q", tc.Text)
	}
}
