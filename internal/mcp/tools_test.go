package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"

	"github.com/klaassen-consulting/jc/internal/config"
)

// --- Test helpers ---

func setupToolTest(t *testing.T) {
	t.Helper()
	keyring.MockInit()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cacheDir := filepath.Join(tmpDir, "cache")
	os.MkdirAll(cacheDir, 0700)

	os.WriteFile(cfgPath, []byte(fmt.Sprintf(`
active_profile: default
profiles:
  default:
    api_key: "test-key-1234"
    org_id: "test-org-id"
cache:
  enabled: true
  ttl: 300
  directory: %s
`, cacheDir)), 0600)
	t.Setenv("JC_CONFIG", cfgPath)
	viper.Reset()
	config.Init()
}

func overrideV1ClientForTest(t *testing.T, serverURL string) {
	t.Helper()
	orig := newV1ClientFunc
	newV1ClientFunc = func() (*api.V1Client, error) {
		return api.NewV1ClientWithKey("test-key-1234"), nil
	}
	// Redirect the test client to the test server.
	newV1ClientFunc = func() (*api.V1Client, error) {
		c := api.NewV1ClientWithKey("test-key-1234")
		c.BaseURL = serverURL + "/api"
		return c, nil
	}
	t.Cleanup(func() { newV1ClientFunc = orig })
}

func overrideV2ClientForTest(t *testing.T, serverURL string) {
	t.Helper()
	orig := newV2ClientFunc
	newV2ClientFunc = func() (*api.V2Client, error) {
		c := api.NewV2ClientWithKey("test-key-1234")
		c.BaseURL = serverURL + "/api/v2"
		return c, nil
	}
	t.Cleanup(func() { newV2ClientFunc = orig })
}

func overrideInsightsClientForTest(t *testing.T, serverURL string) {
	t.Helper()
	orig := newInsightsClientFunc
	newInsightsClientFunc = func() (*api.InsightsClient, error) {
		c := api.NewInsightsClientWithKey("test-key-1234")
		c.BaseURL = serverURL + "/insights/directory/v1"
		return c, nil
	}
	t.Cleanup(func() { newInsightsClientFunc = orig })
}

func connectToolTestServer(t *testing.T, opts Options) *mcp.ClientSession {
	t.Helper()

	if opts.AuditLogPath == "" {
		opts.AuditLogPath = filepath.Join(t.TempDir(), "audit.log")
	}

	server := MustNewServer(opts)
	st, ct := mcp.NewInMemoryTransports()

	ctx := context.Background()
	ss, err := server.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { ss.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })

	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return result
}

func getResultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("expected content in result")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

// startV1Server creates a test HTTP server that handles V1 API endpoints.
func startV1Server(t *testing.T, users, devices, commands []map[string]any) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Handle GET list endpoints.
		switch {
		case path == "/api/systemusers" && r.Method == "GET":
			writeV1List(w, users)
		case path == "/api/systems" && r.Method == "GET":
			writeV1List(w, devices)
		case path == "/api/commands" && r.Method == "GET":
			writeV1List(w, commands)
		case strings.HasPrefix(path, "/api/systemusers/") && r.Method == "GET":
			id := strings.TrimPrefix(path, "/api/systemusers/")
			id = strings.Split(id, "/")[0] // handle sub-resources
			writeV1Get(w, users, id, "_id")
		case strings.HasPrefix(path, "/api/systems/") && r.Method == "GET":
			id := strings.TrimPrefix(path, "/api/systems/")
			id = strings.Split(id, "/")[0]
			writeV1Get(w, devices, id, "_id")
		case strings.HasPrefix(path, "/api/systemusers") && r.Method == "POST":
			// Create user.
			w.WriteHeader(200)
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			body["_id"] = "new-user-id-000000000001"
			json.NewEncoder(w).Encode(body)
		case strings.HasPrefix(path, "/api/systemusers/") && r.Method == "PUT":
			id := strings.TrimPrefix(path, "/api/systemusers/")
			for _, u := range users {
				if u["_id"] == id {
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					for k, v := range body {
						u[k] = v
					}
					json.NewEncoder(w).Encode(u)
					return
				}
			}
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"message": "not found"})
		case strings.HasPrefix(path, "/api/systemusers/") && r.Method == "DELETE":
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]string{})
		case strings.HasPrefix(path, "/api/systems/") && strings.Contains(path, "/command/builtin/") && r.Method == "POST":
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]string{})
		case path == "/api/runcommand" && r.Method == "POST":
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]string{})
		default:
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"message": "not found: " + path})
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func writeV1List(w http.ResponseWriter, items []map[string]any) {
	result := map[string]any{
		"results":    items,
		"totalCount": len(items),
	}
	json.NewEncoder(w).Encode(result)
}

func writeV1Get(w http.ResponseWriter, items []map[string]any, id, idField string) {
	for _, item := range items {
		if fmt.Sprint(item[idField]) == id {
			json.NewEncoder(w).Encode(item)
			return
		}
	}
	w.WriteHeader(404)
	json.NewEncoder(w).Encode(map[string]string{"message": "not found"})
}

// startV2Server creates a test HTTP server that handles V2 API endpoints.
func startV2Server(t *testing.T, userGroups, deviceGroups []map[string]any) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case path == "/api/v2/usergroups" && r.Method == "GET":
			json.NewEncoder(w).Encode(userGroups)
		case path == "/api/v2/systemgroups" && r.Method == "GET":
			json.NewEncoder(w).Encode(deviceGroups)
		case path == "/api/v2/policies" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{})
		case strings.HasPrefix(path, "/api/v2/usergroups/") && strings.HasSuffix(path, "/members") && r.Method == "POST":
			w.WriteHeader(204)
		case strings.HasPrefix(path, "/api/v2/systemgroups/") && strings.HasSuffix(path, "/membership") && r.Method == "POST":
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"message": "not found: " + path})
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

// startCombinedServer creates a test server handling both V1 and V2 endpoints.
func startCombinedServer(t *testing.T, users, devices []map[string]any, userGroups, deviceGroups []map[string]any) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		// V1 endpoints
		case path == "/api/systemusers" && r.Method == "GET":
			writeV1List(w, users)
		case path == "/api/systems" && r.Method == "GET":
			writeV1List(w, devices)
		case strings.HasPrefix(path, "/api/systemusers/") && r.Method == "GET":
			id := strings.TrimPrefix(path, "/api/systemusers/")
			writeV1Get(w, users, id, "_id")
		case strings.HasPrefix(path, "/api/systems/") && r.Method == "GET":
			id := strings.TrimPrefix(path, "/api/systems/")
			id = strings.Split(id, "/")[0]
			writeV1Get(w, devices, id, "_id")

		// V2 endpoints
		case path == "/api/v2/usergroups" && r.Method == "GET":
			json.NewEncoder(w).Encode(userGroups)
		case path == "/api/v2/systemgroups" && r.Method == "GET":
			json.NewEncoder(w).Encode(deviceGroups)
		case strings.HasPrefix(path, "/api/v2/usergroups/") && strings.HasSuffix(path, "/members") && r.Method == "POST":
			w.WriteHeader(204)
		case strings.HasPrefix(path, "/api/v2/systemgroups/") && strings.HasSuffix(path, "/membership") && r.Method == "POST":
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"message": "not found: " + path})
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

// --- Tool registration tests ---

func TestMCP_ListTools_AllRegistered(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	expectedTools := []string{
		"jc_ping",
		// Access Requests
		"access_requests_list", "access_requests_get", "access_requests_create", "access_requests_update", "access_requests_revoke",
		// Users
		"users_list", "users_get", "users_create", "users_update", "users_delete",
		"users_lock", "users_unlock", "users_reset_mfa", "users_reset_password",
		"users_search", "users_ssh_keys_list", "users_ssh_keys_add", "users_ssh_keys_delete",
		// Devices
		"devices_list", "devices_get", "devices_update", "devices_delete", "devices_search",
		"devices_lock", "devices_restart", "devices_erase", "devices_fde_key",
		// Groups
		"groups_list", "groups_add_member", "groups_remove_member",
		"groups_user_list", "groups_user_get", "groups_user_create", "groups_user_update", "groups_user_delete",
		"groups_device_list", "groups_device_get", "groups_device_create", "groups_device_update", "groups_device_delete",
		// Commands
		"commands_list", "commands_get", "commands_create", "commands_update", "commands_delete",
		"commands_run", "commands_results", "commands_trigger",
		// Policies
		"policies_list", "policies_get", "policies_create", "policies_update", "policies_delete", "policies_results",
		// Auth Policies
		"auth_policies_list", "auth_policies_get", "auth_policies_create", "auth_policies_update", "auth_policies_delete",
		"auth_policies_enable", "auth_policies_disable", "auth_policies_simulate", "auth_policies_blast_radius",
		// IP Lists
		"iplists_list", "iplists_get", "iplists_create", "iplists_update", "iplists_delete",
		// Identity Providers
		"identity_providers_list", "identity_providers_get", "identity_providers_create", "identity_providers_update", "identity_providers_delete",
		// Insights
		"insights_query", "insights_count", "insights_distinct",
		// Apps
		"apps_list", "apps_get", "apps_create", "apps_update", "apps_delete",
		// Graph
		"graph_traverse", "graph_bind", "graph_unbind",
		// Admins
		"admins_list", "admins_get", "admins_create", "admins_update", "admins_delete",
		// Org
		"org_list", "org_get", "org_settings", "org_update",
		// Software
		"software_list", "software_get", "software_create", "software_update", "software_delete",
		"software_statuses", "software_associations", "software_reclaim_license",
		// Assets (devices, accessories, locations)
		"assets_devices_list", "assets_devices_get", "assets_devices_create", "assets_devices_update", "assets_devices_delete",
		"assets_accessories_list", "assets_accessories_get", "assets_accessories_create", "assets_accessories_update", "assets_accessories_delete",
		"assets_locations_list", "assets_locations_get", "assets_locations_create", "assets_locations_update", "assets_locations_delete",
		// G Suite
		"gsuite_list", "gsuite_get", "gsuite_translation_rules", "gsuite_import_users",
		// Office 365
		"office365_list", "office365_get", "office365_translation_rules", "office365_import_users",
		// Duo
		"duo_list", "duo_get", "duo_create", "duo_delete",
		"duo_apps", "duo_app_get", "duo_app_create", "duo_app_delete",
		// LDAP
		"ldap_list", "ldap_get", "ldap_create", "ldap_update", "ldap_delete",
		"ldap_samba_domains_list", "ldap_samba_domain_get", "ldap_samba_domain_create", "ldap_samba_domain_update", "ldap_samba_domain_delete",
		// AD
		"ad_list", "ad_get", "ad_create", "ad_update", "ad_delete",
		// System Insights
		"system_insights_list_table", "system_insights_tables",
		// RADIUS
		"radius_list", "radius_get", "radius_create", "radius_update", "radius_delete",
		// Policy Templates
		"policy_templates_list", "policy_templates_get",
		// Apple MDM
		"apple_mdm_list", "apple_mdm_get", "apple_mdm_create", "apple_mdm_update", "apple_mdm_delete",
		"apple_mdm_enrollment_profiles", "apple_mdm_devices",
		// Policy Groups
		"policy_groups_list", "policy_groups_get", "policy_groups_create", "policy_groups_update", "policy_groups_delete",
		// User States
		"user_states_list", "user_states_get", "user_states_create", "user_states_delete",
		// SaaS Management
		"saas_management_list", "saas_management_get", "saas_management_create", "saas_management_update", "saas_management_delete",
		"saas_management_accounts", "saas_management_account_get", "saas_management_account_delete",
		"saas_management_usage", "saas_management_licenses", "saas_management_catalog_get",
		// Custom Emails
		"custom_emails_templates", "custom_emails_get", "custom_emails_create", "custom_emails_update", "custom_emails_delete",
		// App Templates
		"app_templates_list", "app_templates_get",
		// Utility
		"recipe_run", "plan", "explain",
		// MCP Apps
		"dashboard_view",
		"insights_view",
		"user_view",
		"device_view",
		"compliance_view",
		"recipe_runner_view",
		// Recipe catalog tool (paired with recipe_run + recipe_runner_view).
		"recipe_list",
	}

	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}

	for _, name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("expected tool %q to be registered", name)
		}
	}

	// Verify exact count — update when adding/removing tools.
	if len(result.Tools) != 201 {
		t.Errorf("expected 201 tools, got %d", len(result.Tools))
	}
}

func TestMCP_ToolDescriptions(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range result.Tools {
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
	}
}

// --- Users tools tests ---

func TestMCP_UsersListTool(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice", "email": "alice@test.com"},
		{"_id": "aabbccddee112233aabbcc02", "username": "bob", "email": "bob@test.com"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_list", nil)

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "alice") {
		t.Errorf("expected alice in result, got: %s", text)
	}
	if !strings.Contains(text, "bob") {
		t.Errorf("expected bob in result, got: %s", text)
	}
	// Verify data structure.
	var res map[string]any
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if res["total"] != float64(2) {
		t.Errorf("expected total 2, got %v", res["total"])
	}
}

func TestMCP_UsersGetTool(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice", "email": "alice@test.com"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_get", map[string]any{"identifier": "aabbccddee112233aabbcc01"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "alice") {
		t.Errorf("expected alice in result, got: %s", text)
	}
}

func TestMCP_UsersCreateTool(t *testing.T) {
	setupToolTest(t)

	ts := startV1Server(t, nil, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_create", map[string]any{
		"username": "newuser",
		"email":    "newuser@test.com",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "newuser") {
		t.Errorf("expected newuser in result, got: %s", text)
	}
}

func TestMCP_UsersCreateTool_WithDepartment(t *testing.T) {
	setupToolTest(t)

	ts := startV1Server(t, nil, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_create", map[string]any{
		"username":   "newuser",
		"email":      "newuser@test.com",
		"department": "Engineering",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "Engineering") {
		t.Errorf("expected Engineering in result, got: %s", text)
	}
}

func TestMCP_UsersDeleteTool_PlanFirst(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	// Without execute=true, should return plan.
	result := callTool(t, cs, "users_delete", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	var plan map[string]any
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	if plan["plan"] != true {
		t.Error("expected plan=true")
	}
	if plan["action"] != "delete" {
		t.Errorf("expected action=delete, got %v", plan["action"])
	}
	if !strings.Contains(plan["message"].(string), "execute=true") {
		t.Errorf("expected plan message to mention execute=true, got: %s", plan["message"])
	}
}

func TestMCP_UsersDeleteTool_Execute(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_delete", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		"execute":    true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "deleted successfully") {
		t.Errorf("expected success message, got: %s", text)
	}
}

func TestMCP_UsersUpdateTool_PlanFirst(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice", "email": "old@test.com"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_update", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		"email":      "new@test.com",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	var plan map[string]any
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	if plan["plan"] != true {
		t.Error("expected plan=true")
	}
	if plan["action"] != "update" {
		t.Errorf("expected action=update, got %v", plan["action"])
	}
}

func TestMCP_UsersUpdateTool_Execute(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice", "email": "old@test.com"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_update", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		"email":      "new@test.com",
		"execute":    true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "new@test.com") {
		t.Errorf("expected updated email in result, got: %s", text)
	}
}

func TestMCP_UsersUpdateTool_NoFields(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_update", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
	})

	if !result.IsError {
		t.Fatal("expected error for no fields")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "no fields to update") {
		t.Errorf("expected 'no fields to update' error, got: %s", text)
	}
}

func TestMCP_UsersLockTool_PlanFirst(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_lock", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "plan") {
		t.Errorf("expected plan result, got: %s", text)
	}
}

func TestMCP_UsersLockTool_Execute(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_lock", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		"execute":    true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "locked successfully") {
		t.Errorf("expected lock success message, got: %s", text)
	}
}

func TestMCP_UsersUnlockTool_Execute(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_unlock", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		"execute":    true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "unlocked successfully") {
		t.Errorf("expected unlock success message, got: %s", text)
	}
}

func TestMCP_UsersResetMFATool_PlanFirst(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_reset_mfa", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "plan") {
		t.Errorf("expected plan result, got: %s", text)
	}
}

func TestMCP_UsersResetPasswordTool_Execute(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/expire") && r.Method == "POST" {
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]string{})
			return
		}
		// Handle list for resolution.
		if r.URL.Path == "/api/systemusers" {
			writeV1List(w, users)
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(ts.Close)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_reset_password", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		"execute":    true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "Password reset email sent") {
		t.Errorf("expected password reset message, got: %s", text)
	}
}

// --- Devices tools tests ---

func TestMCP_DevicesListTool(t *testing.T) {
	setupToolTest(t)

	devices := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "hostname": "JDOE-MBP", "os": "Mac OS X"},
		{"_id": "aabbccddee112233aabbcc02", "hostname": "SERVER-01", "os": "Ubuntu"},
	}
	ts := startV1Server(t, nil, devices, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "devices_list", nil)

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "JDOE-MBP") {
		t.Errorf("expected JDOE-MBP in result, got: %s", text)
	}
}

func TestMCP_DevicesGetTool(t *testing.T) {
	setupToolTest(t)

	devices := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "hostname": "JDOE-MBP", "os": "Mac OS X"},
	}
	ts := startV1Server(t, nil, devices, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "devices_get", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "JDOE-MBP") {
		t.Errorf("expected JDOE-MBP in result, got: %s", text)
	}
}

func TestMCP_DevicesLockTool_PlanFirst(t *testing.T) {
	setupToolTest(t)

	devices := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "hostname": "JDOE-MBP"},
	}
	ts := startV1Server(t, nil, devices, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "devices_lock", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	var plan map[string]any
	json.Unmarshal([]byte(text), &plan)
	if plan["plan"] != true {
		t.Error("expected plan=true")
	}
	if plan["action"] != "lock" {
		t.Errorf("expected action=lock, got %v", plan["action"])
	}
}

func TestMCP_DevicesEraseTool_PlanFirst(t *testing.T) {
	setupToolTest(t)

	devices := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "hostname": "JDOE-MBP"},
	}
	ts := startV1Server(t, nil, devices, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "devices_erase", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	var plan map[string]any
	json.Unmarshal([]byte(text), &plan)
	if plan["action"] != "erase" {
		t.Errorf("expected action=erase, got %v", plan["action"])
	}
}

func TestMCP_DevicesEraseTool_Execute(t *testing.T) {
	setupToolTest(t)

	devices := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "hostname": "JDOE-MBP"},
	}
	ts := startV1Server(t, nil, devices, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "devices_erase", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		"execute":    true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "erase command sent successfully") {
		t.Errorf("expected erase success message, got: %s", text)
	}
}

// --- Groups tools tests ---

func TestMCP_GroupsListTool(t *testing.T) {
	setupToolTest(t)

	userGroups := []map[string]any{
		{"id": "ug01", "name": "Engineering", "type": "user_group"},
	}
	deviceGroups := []map[string]any{
		{"id": "sg01", "name": "macOS Fleet", "type": "system_group"},
	}
	ts := startV2Server(t, userGroups, deviceGroups)
	overrideV2ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "groups_list", nil)

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "Engineering") {
		t.Errorf("expected Engineering in result, got: %s", text)
	}
	if !strings.Contains(text, "macOS Fleet") {
		t.Errorf("expected macOS Fleet in result, got: %s", text)
	}
}

func TestMCP_GroupsAddMemberTool_PlanFirst(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	userGroups := []map[string]any{
		{"id": "aabbccddee112233aabbcc11", "name": "Engineering"},
	}

	ts := startCombinedServer(t, users, nil, userGroups, nil)
	overrideV1ClientForTest(t, ts.URL)
	overrideV2ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "groups_add_member", map[string]any{
		"group":       "aabbccddee112233aabbcc11",
		"member":      "aabbccddee112233aabbcc01",
		"member_type": "user",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	var plan map[string]any
	json.Unmarshal([]byte(text), &plan)
	if plan["plan"] != true {
		t.Error("expected plan=true for add-member without execute")
	}
}

func TestMCP_GroupsAddMemberTool_Execute(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	userGroups := []map[string]any{
		{"id": "aabbccddee112233aabbcc11", "name": "Engineering"},
	}

	ts := startCombinedServer(t, users, nil, userGroups, nil)
	overrideV1ClientForTest(t, ts.URL)
	overrideV2ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "groups_add_member", map[string]any{
		"group":       "aabbccddee112233aabbcc11",
		"member":      "aabbccddee112233aabbcc01",
		"member_type": "user",
		"execute":     true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "added to") {
		t.Errorf("expected 'added to' message, got: %s", text)
	}
}

func TestMCP_GroupsMembershipTool_InvalidType(t *testing.T) {
	setupToolTest(t)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "groups_add_member", map[string]any{
		"group":       "somegroup",
		"member":      "somemember",
		"member_type": "invalid",
	})

	if !result.IsError {
		t.Fatal("expected error for invalid member type")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "must be 'user' or 'device'") {
		t.Errorf("expected member type error, got: %s", text)
	}
}

// --- Insights tools tests ---

func TestMCP_InsightsQueryTool(t *testing.T) {
	setupToolTest(t)

	// Override InsightsNowFunc for deterministic time.
	origNow := api.InsightsNowFunc
	api.InsightsNowFunc = func() time.Time {
		return time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { api.InsightsNowFunc = origNow })

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/insights/directory/v1/events" && r.Method == "POST" {
			events := []map[string]any{
				{"event_type": "sso_auth", "timestamp": "2026-02-13T10:00:00Z"},
			}
			json.NewEncoder(w).Encode(events)
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(ts.Close)
	overrideInsightsClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "insights_query", map[string]any{
		"service": "sso",
		"last":    "24h",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "sso_auth") {
		t.Errorf("expected sso_auth event in result, got: %s", text)
	}
}

func TestMCP_InsightsCountTool(t *testing.T) {
	setupToolTest(t)

	origNow := api.InsightsNowFunc
	api.InsightsNowFunc = func() time.Time {
		return time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { api.InsightsNowFunc = origNow })

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/insights/directory/v1/events/count" && r.Method == "POST" {
			json.NewEncoder(w).Encode(map[string]int{"count": 42})
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(ts.Close)
	overrideInsightsClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "insights_count", map[string]any{
		"service": "sso",
		"last":    "7d",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "42") {
		t.Errorf("expected count 42 in result, got: %s", text)
	}
}

func TestMCP_InsightsQueryTool_MissingTimeRange(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "insights_query", map[string]any{
		"service": "sso",
	})

	if !result.IsError {
		t.Fatal("expected error for missing time range")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "last or start is required") {
		t.Errorf("expected time range error, got: %s", text)
	}
}

// --- Commands tools tests ---

func TestMCP_CommandsListTool(t *testing.T) {
	setupToolTest(t)

	commands := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "name": "Update Agents", "commandType": "linux"},
	}
	ts := startV1Server(t, nil, nil, commands)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "commands_list", nil)

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "Update Agents") {
		t.Errorf("expected 'Update Agents' in result, got: %s", text)
	}
}

func TestMCP_CommandsRunTool_PlanFirst(t *testing.T) {
	setupToolTest(t)

	commands := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "name": "Update Agents"},
	}
	devices := []map[string]any{
		{"_id": "aabbccddee112233aabbcc02", "hostname": "SERVER-01"},
	}
	ts := startV1Server(t, nil, devices, commands)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "commands_run", map[string]any{
		"command": "aabbccddee112233aabbcc01",
		"target":  "aabbccddee112233aabbcc02",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	var plan map[string]any
	json.Unmarshal([]byte(text), &plan)
	if plan["plan"] != true {
		t.Error("expected plan=true for commands_run without execute")
	}
}

// --- Policies tools tests ---

func TestMCP_PoliciesListTool(t *testing.T) {
	setupToolTest(t)

	ts := startV2Server(t, nil, nil)
	overrideV2ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "policies_list", nil)

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	var res map[string]any
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if res["total"] != float64(0) {
		t.Errorf("expected total 0, got %v", res["total"])
	}
}

// --- Read-only mode tests ---

func TestMCP_ReadOnlyMode_BlocksMutations(t *testing.T) {
	setupToolTest(t)

	cs := connectToolTestServer(t, Options{ReadOnly: true})

	mutationTools := []struct {
		name string
		args map[string]any
	}{
		{"users_create", map[string]any{"username": "test", "email": "test@test.com"}},
		{"users_update", map[string]any{"identifier": "test", "email": "new@test.com"}},
		{"users_delete", map[string]any{"identifier": "test", "execute": true}},
		{"users_lock", map[string]any{"identifier": "test", "execute": true}},
		{"users_unlock", map[string]any{"identifier": "test", "execute": true}},
		{"users_reset_mfa", map[string]any{"identifier": "test", "execute": true}},
		{"users_reset_password", map[string]any{"identifier": "test", "execute": true}},
		{"devices_lock", map[string]any{"identifier": "test", "execute": true}},
		{"devices_restart", map[string]any{"identifier": "test", "execute": true}},
		{"devices_erase", map[string]any{"identifier": "test", "execute": true}},
		{"groups_add_member", map[string]any{"group": "g", "member": "m", "member_type": "user", "execute": true}},
		{"groups_remove_member", map[string]any{"group": "g", "member": "m", "member_type": "user", "execute": true}},
		{"commands_run", map[string]any{"command": "cmd", "target": "dev", "execute": true}},
	}

	for _, mt := range mutationTools {
		result := callTool(t, cs, mt.name, mt.args)
		if !result.IsError {
			t.Errorf("%s: expected error in read-only mode, got success", mt.name)
			continue
		}
		text := getResultText(t, result)
		if !strings.Contains(text, "read-only") {
			t.Errorf("%s: expected 'read-only' error, got: %s", mt.name, text)
		}
	}
}

func TestMCP_ReadOnlyMode_AllowsReads(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	v2ts := startV2Server(t, nil, nil)
	overrideV2ClientForTest(t, v2ts.URL)

	cs := connectToolTestServer(t, Options{ReadOnly: true})

	// Read operations should work.
	result := callTool(t, cs, "users_list", nil)
	if result.IsError {
		t.Fatalf("users_list should work in read-only mode: %s", getResultText(t, result))
	}
}

// --- Plan and explain tools tests ---

func TestMCP_PlanTool(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result := callTool(t, cs, "plan", map[string]any{
		"command": "users delete jdoe",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	var plan map[string]any
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	if plan["command"] != "users delete jdoe" {
		t.Errorf("expected command in plan, got: %v", plan["command"])
	}
	if plan["description"] == nil || plan["description"] == "" {
		t.Error("expected description in plan result")
	}
}

func TestMCP_PlanTool_EmptyCommand(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result := callTool(t, cs, "plan", map[string]any{
		"command": "",
	})

	if !result.IsError {
		t.Fatal("expected error for empty command")
	}
}

func TestMCP_ExplainTool(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result := callTool(t, cs, "explain", map[string]any{
		"command": "devices erase JDOE-MBP",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "DESTRUCTIVE") {
		t.Errorf("expected DESTRUCTIVE warning for erase, got: %s", text)
	}
}

func TestMCP_ExplainTool_UnknownCommand(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result := callTool(t, cs, "explain", map[string]any{
		"command": "foobar baz",
	})

	if result.IsError {
		t.Fatal("explain should not error for unknown commands")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "foobar baz") {
		t.Errorf("expected command echo for unknown command, got: %s", text)
	}
}

// --- Helper function tests ---

func TestDescribeCommand(t *testing.T) {
	tests := []struct {
		parts    []string
		contains string
	}{
		{[]string{"users", "list"}, "List all JumpCloud"},
		{[]string{"users", "delete"}, "IRREVERSIBLE"},
		{[]string{"devices", "erase"}, "DESTRUCTIVE"},
		{[]string{"groups", "add-member"}, "Add a user or device"},
		{[]string{"insights", "query"}, "Directory Insights"},
		{[]string{"commands", "run"}, "Trigger a command"},
		{[]string{"policies", "list"}, "policies"},
		{[]string{"recipe", "run"}, "recipe"},
		{[]string{"users"}, "Manage JumpCloud users"},
		{[]string{"unknown"}, "unknown"},
		{nil, "Empty command"},
	}

	for _, tt := range tests {
		desc := describeCommand(tt.parts)
		if !strings.Contains(desc, tt.contains) {
			t.Errorf("describeCommand(%v) = %q, expected to contain %q", tt.parts, desc, tt.contains)
		}
	}
}

func TestBuildV1ListOptions_WithFilter(t *testing.T) {
	args := listInput{
		Limit:  10,
		Sort:   "-created",
		Filter: []string{"os=Mac OS X"},
	}
	opts, err := buildV1ListOptions(args)
	if err != nil {
		t.Fatalf("buildV1ListOptions: %v", err)
	}
	if opts.Limit != 10 {
		t.Errorf("expected limit 10, got %d", opts.Limit)
	}
	if opts.Sort != "-created" {
		t.Errorf("expected sort -created, got %q", opts.Sort)
	}
	if len(opts.Filter) == 0 {
		t.Fatal("expected filters")
	}
}

func TestBuildV1ListOptions_InvalidFilter(t *testing.T) {
	args := listInput{
		Filter: []string{"invalid filter no operator"},
	}
	_, err := buildV1ListOptions(args)
	if err == nil {
		t.Fatal("expected error for invalid filter")
	}
}

func TestBuildV2ListOptions_WithFilter(t *testing.T) {
	args := listInput{
		Limit:  5,
		Filter: []string{"name=Engineering"},
	}
	opts, err := buildV2ListOptions(args)
	if err != nil {
		t.Fatalf("buildV2ListOptions: %v", err)
	}
	if opts.Limit != 5 {
		t.Errorf("expected limit 5, got %d", opts.Limit)
	}
	if len(opts.Filter) == 0 {
		t.Fatal("expected filters")
	}
}

func TestResolveTimeRange_Last(t *testing.T) {
	origNow := api.InsightsNowFunc
	api.InsightsNowFunc = func() time.Time {
		return time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	}
	defer func() { api.InsightsNowFunc = origNow }()

	start, end, err := resolveTimeRange("24h", "", "")
	if err != nil {
		t.Fatalf("resolveTimeRange: %v", err)
	}
	if start == "" || end == "" {
		t.Fatal("expected non-empty start and end")
	}
	if end != "2026-02-13T12:00:00Z" {
		t.Errorf("expected end = now, got %q", end)
	}
}

func TestResolveTimeRange_MutuallyExclusive(t *testing.T) {
	_, _, err := resolveTimeRange("24h", "2026-02-01", "")
	if err == nil {
		t.Fatal("expected error for last + start")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutually exclusive error, got: %v", err)
	}
}

func TestResolveTimeRange_NoRange(t *testing.T) {
	_, _, err := resolveTimeRange("", "", "")
	if err == nil {
		t.Fatal("expected error for missing time range")
	}
}

func TestPlanResult_Structure(t *testing.T) {
	result, _, _ := planResult("delete", "user", "alice", "id123", nil)
	text := getResultText(t, result)

	var plan map[string]any
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	if plan["plan"] != true {
		t.Error("expected plan=true")
	}
	if plan["action"] != "delete" {
		t.Errorf("expected action=delete, got %v", plan["action"])
	}
	if plan["resource"] != "user" {
		t.Errorf("expected resource=user, got %v", plan["resource"])
	}
	if plan["target"] != "alice" {
		t.Errorf("expected target=alice, got %v", plan["target"])
	}
	if plan["resolved_id"] != "id123" {
		t.Errorf("expected resolved_id=id123, got %v", plan["resolved_id"])
	}
}

func TestRawListResult_NilData(t *testing.T) {
	result, _, _ := rawListResult(nil, 0)
	text := getResultText(t, result)

	var res map[string]any
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	// nil data should be serialized as empty array.
	data, ok := res["data"].([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", res["data"])
	}
	if len(data) != 0 {
		t.Errorf("expected empty data array, got %d items", len(data))
	}
}

// --- Batch 5: MCP Tool Input Validation Edge Cases ---

func TestMCP_UsersGet_EmptyIdentifier(t *testing.T) {
	setupToolTest(t)

	// Start a server that returns empty results (resolve will fail to find anything).
	users := []map[string]any{}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "users_get", map[string]any{"identifier": ""})

	// Empty identifier should produce an error (resolve finds no match).
	if !result.IsError {
		text := getResultText(t, result)
		t.Fatalf("expected error for empty identifier, got success: %s", text)
	}
}

func TestMCP_UsersCreate_MissingFields(t *testing.T) {
	setupToolTest(t)

	ts := startV1Server(t, nil, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})

	// username and email are required (no omitempty in struct tag).
	// MCP SDK validates required fields and returns an error at the transport level.
	ctx := context.Background()
	_, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "users_create",
		Arguments: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected SDK validation error for missing required fields")
	}
	if !strings.Contains(err.Error(), "required") && !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected error about required/missing fields, got: %v", err)
	}
}

func TestMCP_UsersDelete_PlanFirst_ReturnsPreview(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})

	// Without execute=true, should return plan (not actually delete).
	result := callTool(t, cs, "users_delete", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		"execute":    false,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	var plan map[string]any
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	if plan["plan"] != true {
		t.Error("expected plan=true for execute=false")
	}
	if plan["action"] != "delete" {
		t.Errorf("expected action=delete, got %v", plan["action"])
	}
}

func TestMCP_DevicesUpdate_EmptyBody(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	// Provide identifier but no update fields → should get "no fields to update" error.
	result := callTool(t, cs, "devices_update", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
	})

	if !result.IsError {
		t.Fatal("expected error for devices_update with no fields")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "no fields to update") {
		t.Errorf("expected 'no fields to update' error, got: %s", text)
	}
}

func TestMCP_InsightsQuery_InvalidService(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result := callTool(t, cs, "insights_query", map[string]any{
		"service": "nonexistent_bogus_service",
		"last":    "24h",
	})

	if !result.IsError {
		t.Fatal("expected error for invalid insights service")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "invalid service") {
		t.Errorf("expected 'invalid service' error, got: %s", text)
	}
}

func TestMCP_GraphBind_MissingFields(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	// graph_bind requires "from" and "to" in "type:identifier" format.
	// Empty strings should fail parseGraphFrom validation.
	result := callTool(t, cs, "graph_bind", map[string]any{
		"from":    "",
		"to":      "",
		"execute": true,
	})

	if !result.IsError {
		t.Fatal("expected error for graph_bind with empty from/to")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "invalid from format") && !strings.Contains(text, "invalid") {
		t.Errorf("expected validation error for empty from/to, got: %s", text)
	}
}

func TestMCP_ReadOnlyMode_BlocksCreate(t *testing.T) {
	setupToolTest(t)

	cs := connectToolTestServer(t, Options{ReadOnly: true})

	result := callTool(t, cs, "users_create", map[string]any{
		"username": "newuser",
		"email":    "new@test.com",
	})

	if !result.IsError {
		t.Fatal("expected error for create in read-only mode")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "read-only") {
		t.Errorf("expected 'read-only' error, got: %s", text)
	}
}
