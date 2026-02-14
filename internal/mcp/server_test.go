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

	// Foundation (2) + schema/resources (1) + schema/{resource} (8) + schema/commands (1) + recipes/list (1) + recipes/{name} (11+) = 24+
	if len(result.Resources) < 20 {
		t.Fatalf("expected at least 20 resources, got %d", len(result.Resources))
	}

	// Check for key resources by URI.
	required := []string{
		"jc://server/info",
		"jc://config/profiles",
		"jc://schema/resources",
		"jc://schema/users",
		"jc://schema/devices",
		"jc://schema/groups",
		"jc://schema/commands",
		"jc://recipes/list",
	}
	uris := make(map[string]bool)
	for _, r := range result.Resources {
		uris[r.URI] = true
		if r.MIMEType != "application/json" {
			t.Errorf("resource %s: expected MIME type application/json, got %q", r.URI, r.MIMEType)
		}
	}
	for _, uri := range required {
		if !uris[uri] {
			t.Errorf("expected resource %s to be registered", uri)
		}
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

// --- Schema resource tests ---

func TestMCP_ReadResource_SchemaResources(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://schema/resources",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	if len(result.Contents) == 0 {
		t.Fatal("expected resource content")
	}

	var schemas []map[string]any
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &schemas); err != nil {
		t.Fatalf("parse schema resources: %v", err)
	}

	// Should have entries for all resource types.
	if len(schemas) < 8 {
		t.Errorf("expected at least 8 resource schemas, got %d", len(schemas))
	}

	// Check sorted order — first should be "ad" (alphabetical).
	if schemas[0]["resource"] != "ad" {
		t.Errorf("expected first resource to be 'ad', got %v", schemas[0]["resource"])
	}

	// Find users schema and verify structure.
	var found bool
	for _, s := range schemas {
		if s["resource"] == "users" {
			found = true
			if s["api_version"] != "v1" {
				t.Errorf("expected users api_version 'v1', got %v", s["api_version"])
			}
			verbs, ok := s["verbs"].([]any)
			if !ok || len(verbs) == 0 {
				t.Error("expected users to have verbs")
			}
			fields, ok := s["default_fields"].([]any)
			if !ok || len(fields) == 0 {
				t.Error("expected users to have default_fields")
			}
			if s["id_field"] != "_id" {
				t.Errorf("expected users id_field '_id', got %v", s["id_field"])
			}
			if s["name_field"] != "username" {
				t.Errorf("expected users name_field 'username', got %v", s["name_field"])
			}
		}
	}
	if !found {
		t.Error("expected 'users' schema in resource list")
	}
}

func TestMCP_ReadResource_SchemaUsers(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://schema/users",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &schema); err != nil {
		t.Fatalf("parse users schema: %v", err)
	}

	if schema["resource"] != "users" {
		t.Errorf("expected resource 'users', got %v", schema["resource"])
	}
	if schema["api_version"] != "v1" {
		t.Errorf("expected api_version 'v1', got %v", schema["api_version"])
	}
	if schema["filter_support"] != true {
		t.Errorf("expected filter_support true, got %v", schema["filter_support"])
	}
	if schema["sort_support"] != true {
		t.Errorf("expected sort_support true, got %v", schema["sort_support"])
	}
}

func TestMCP_ReadResource_SchemaDevices(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://schema/devices",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &schema); err != nil {
		t.Fatalf("parse devices schema: %v", err)
	}

	if schema["resource"] != "devices" {
		t.Errorf("expected resource 'devices', got %v", schema["resource"])
	}
	if schema["name_field"] != "hostname" {
		t.Errorf("expected name_field 'hostname', got %v", schema["name_field"])
	}
}

func TestMCP_ReadResource_SchemaGroups(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://schema/groups",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &schema); err != nil {
		t.Fatalf("parse groups schema: %v", err)
	}

	if schema["api_version"] != "v2" {
		t.Errorf("expected api_version 'v2', got %v", schema["api_version"])
	}
	if schema["id_field"] != "id" {
		t.Errorf("expected id_field 'id', got %v", schema["id_field"])
	}
}

func TestMCP_ReadResource_SchemaAllTypes(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	// Verify all schema resource types are readable.
	types := []string{"users", "devices", "groups", "commands", "policies", "apps", "admins", "insights"}
	ctx := context.Background()
	for _, typ := range types {
		t.Run(typ, func(t *testing.T) {
			result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
				URI: "jc://schema/" + typ,
			})
			if err != nil {
				t.Fatalf("ReadResource jc://schema/%s: %v", typ, err)
			}
			if len(result.Contents) == 0 {
				t.Fatal("expected content")
			}
			if result.Contents[0].MIMEType != "application/json" {
				t.Errorf("expected MIME type application/json, got %q", result.Contents[0].MIMEType)
			}
		})
	}
}

// --- Command manifest resource tests ---

func TestMCP_ReadResource_SchemaCommands(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://schema/commands",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	var manifest map[string]any
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &manifest); err != nil {
		t.Fatalf("parse command manifest: %v", err)
	}

	if manifest["name"] != "jc" {
		t.Errorf("expected name 'jc', got %v", manifest["name"])
	}

	commands, ok := manifest["commands"].([]any)
	if !ok || len(commands) == 0 {
		t.Fatal("expected commands array")
	}

	globalFlags, ok := manifest["global_flags"].([]any)
	if !ok || len(globalFlags) == 0 {
		t.Fatal("expected global_flags array")
	}

	resources, ok := manifest["resources"].([]any)
	if !ok || len(resources) == 0 {
		t.Fatal("expected resources array")
	}

	// Verify at least key commands are present.
	commandPaths := make(map[string]bool)
	for _, c := range commands {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		path, _ := cm["path"].(string)
		commandPaths[path] = true
	}
	for _, required := range []string{"jc auth", "jc users", "jc devices", "jc groups", "jc insights", "jc recipe"} {
		if !commandPaths[required] {
			t.Errorf("expected command %q in manifest", required)
		}
	}
}

func TestMCP_ReadResource_CommandManifestFlags(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://schema/commands",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	var manifest map[string]any
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	globalFlags, ok := manifest["global_flags"].([]any)
	if !ok {
		t.Fatal("expected global_flags array")
	}

	// Verify key global flags are present.
	flagNames := make(map[string]bool)
	for _, f := range globalFlags {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		name, _ := fm["name"].(string)
		flagNames[name] = true
	}
	for _, required := range []string{"output", "verbose", "quiet", "force", "plan", "ids", "fields"} {
		if !flagNames[required] {
			t.Errorf("expected global flag %q in manifest", required)
		}
	}
}

// --- Recipe resource tests ---

func TestMCP_ReadResource_RecipesList(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://recipes/list",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	if len(result.Contents) == 0 {
		t.Fatal("expected resource content")
	}

	var recipes []map[string]any
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &recipes); err != nil {
		t.Fatalf("parse recipes list: %v", err)
	}

	// Should have built-in recipes.
	if len(recipes) == 0 {
		t.Fatal("expected at least one recipe")
	}

	// Verify recipe summary structure.
	first := recipes[0]
	if _, ok := first["name"]; !ok {
		t.Error("expected recipe to have 'name'")
	}
	if _, ok := first["description"]; !ok {
		t.Error("expected recipe to have 'description'")
	}
	if _, ok := first["steps"]; !ok {
		t.Error("expected recipe to have 'steps' count")
	}
}

func TestMCP_ReadResource_RecipeByName(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()

	// First, get the recipe list to find a valid recipe name.
	listResult, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://recipes/list",
	})
	if err != nil {
		t.Fatalf("ReadResource recipes/list: %v", err)
	}

	var recipes []map[string]any
	json.Unmarshal([]byte(listResult.Contents[0].Text), &recipes)
	if len(recipes) == 0 {
		t.Skip("no recipes available")
	}

	recipeName := recipes[0]["name"].(string)

	// Read the individual recipe resource.
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://recipes/" + recipeName,
	})
	if err != nil {
		t.Fatalf("ReadResource recipes/%s: %v", recipeName, err)
	}

	var recipe map[string]any
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &recipe); err != nil {
		t.Fatalf("parse recipe: %v", err)
	}

	if recipe["name"] != recipeName {
		t.Errorf("expected recipe name %q, got %v", recipeName, recipe["name"])
	}
	if _, ok := recipe["steps"]; !ok {
		t.Error("expected recipe to have 'steps'")
	}
	if _, ok := recipe["parameters"]; !ok {
		t.Error("expected recipe to have 'parameters'")
	}
}

func TestMCP_ReadResource_RecipeParameters(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()

	// Read the recipes list and find one with parameters.
	listResult, _ := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "jc://recipes/list",
	})
	var recipes []map[string]any
	json.Unmarshal([]byte(listResult.Contents[0].Text), &recipes)

	// Find a recipe with parameters.
	var recipeName string
	for _, r := range recipes {
		params, ok := r["parameters"].([]any)
		if ok && len(params) > 0 {
			recipeName = r["name"].(string)
			break
		}
	}
	if recipeName == "" {
		t.Skip("no recipe with parameters found")
	}

	// Verify the recipe list shows parameter details.
	for _, r := range recipes {
		if r["name"] != recipeName {
			continue
		}
		params, ok := r["parameters"].([]any)
		if !ok {
			t.Fatal("expected parameters to be an array")
		}
		for _, p := range params {
			pm, ok := p.(map[string]any)
			if !ok {
				t.Fatal("expected parameter to be an object")
			}
			if _, ok := pm["name"]; !ok {
				t.Error("expected parameter to have 'name'")
			}
			if _, ok := pm["type"]; !ok {
				t.Error("expected parameter to have 'type'")
			}
		}
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

// --- Tool allow/block list integration tests ---

func TestMCP_BlockedToolNotRegistered(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{
		BlockedTools: []string{"jc_ping"},
	})

	ctx := context.Background()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range result.Tools {
		if tool.Name == "jc_ping" {
			t.Error("expected jc_ping to be filtered out by block list")
		}
	}
}

func TestMCP_BlockedToolCallFails(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{
		BlockedTools: []string{"jc_ping"},
	})

	ctx := context.Background()
	_, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "jc_ping"})
	if err == nil {
		t.Fatal("expected error calling blocked tool")
	}
}

func TestMCP_AllowListFiltersTools(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{
		AllowedTools: []string{"jc_ping", "users_list"},
	})

	ctx := context.Background()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}

	if !names["jc_ping"] {
		t.Error("expected jc_ping to be allowed")
	}
	if !names["users_list"] {
		t.Error("expected users_list to be allowed")
	}
	if names["devices_list"] {
		t.Error("expected devices_list to be filtered out (not in allow list)")
	}
	if names["users_delete"] {
		t.Error("expected users_delete to be filtered out (not in allow list)")
	}

	// Only 2 tools should be registered.
	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result.Tools))
	}
}

func TestMCP_WildcardAllowPattern(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{
		AllowedTools: []string{"users_*", "jc_ping"},
	})

	ctx := context.Background()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}

	// All users tools should be present.
	if !names["users_list"] {
		t.Error("expected users_list allowed by wildcard")
	}
	if !names["users_get"] {
		t.Error("expected users_get allowed by wildcard")
	}
	if !names["users_delete"] {
		t.Error("expected users_delete allowed by wildcard")
	}
	if !names["jc_ping"] {
		t.Error("expected jc_ping allowed explicitly")
	}
	// Other tools should be filtered.
	if names["devices_list"] {
		t.Error("expected devices_list filtered out")
	}
}

func TestMCP_WildcardBlockPattern(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{
		BlockedTools: []string{"devices_*"},
	})

	ctx := context.Background()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}

	// Users tools should still be present.
	if !names["users_list"] {
		t.Error("expected users_list allowed")
	}
	// All device tools should be blocked.
	if names["devices_list"] {
		t.Error("expected devices_list blocked by wildcard")
	}
	if names["devices_erase"] {
		t.Error("expected devices_erase blocked by wildcard")
	}
}

func TestMCP_BlockPrecedenceOverAllow(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{
		AllowedTools: []string{"users_*"},
		BlockedTools: []string{"users_delete"},
	})

	ctx := context.Background()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}

	if !names["users_list"] {
		t.Error("expected users_list allowed")
	}
	if names["users_delete"] {
		t.Error("expected users_delete blocked (block takes precedence over allow)")
	}
}

func TestMCP_ReadOnlyEquivalentToBlockMutations(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{ReadOnly: true})

	ctx := context.Background()

	// Mutation tools should return read-only error.
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "users_create",
		Arguments: map[string]any{"username": "test", "email": "test@test.com"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for mutation in read-only mode")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !strings.Contains(tc.Text, "read-only") {
		t.Errorf("expected read-only error, got %q", tc.Text)
	}
}

func TestMCP_ListToolNames(t *testing.T) {
	setupTest(t)
	server := NewServer(Options{
		AuditLogPath: filepath.Join(t.TempDir(), "audit.log"),
	})

	names := server.ListToolNames()
	if len(names) == 0 {
		t.Fatal("expected at least one tool name")
	}

	// Verify sorted order.
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("tool names not sorted: %q before %q", names[i-1], names[i])
		}
	}

	// Verify key tools present.
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	for _, expected := range []string{"jc_ping", "users_list", "users_get", "devices_list", "groups_list"} {
		if !nameSet[expected] {
			t.Errorf("expected tool %q in list", expected)
		}
	}
}

func TestMCP_ListToolNames_WithFilter(t *testing.T) {
	setupTest(t)
	server := NewServer(Options{
		AuditLogPath: filepath.Join(t.TempDir(), "audit.log"),
		AllowedTools: []string{"jc_ping"},
	})

	names := server.ListToolNames()
	if len(names) != 1 {
		t.Fatalf("expected 1 tool, got %d: %v", len(names), names)
	}
	if names[0] != "jc_ping" {
		t.Errorf("expected jc_ping, got %q", names[0])
	}
}
