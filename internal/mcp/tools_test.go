package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

	// Isolate the name-to-ID cache, unless the test seeded one itself. Without
	// this a tool test resolving a name reads the DEVELOPER'S real
	// ~/.cache/jc, so a test asserting its mock's "role-1" instead gets
	// whatever ID that developer last touched a live tenant with. It passes in
	// CI, where the cache is empty, and fails only on a machine that has done
	// real work — the worst way to find out.
	//
	// The guard matters: several tests pre-populate a cache through this same
	// variable, and overwriting it would empty the fixture they depend on.
	if os.Getenv("XDG_CACHE_HOME") == "" {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
	}

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
		case strings.HasPrefix(path, "/api/commands/") && r.Method == "GET":
			id := strings.TrimPrefix(path, "/api/commands/")
			id = strings.Split(id, "/")[0]
			writeV1Get(w, commands, id, "_id")
		case path == "/api/commands" && r.Method == "POST":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			body["_id"] = "new-cmd-id-0000000000001"
			json.NewEncoder(w).Encode(body)
		case strings.HasPrefix(path, "/api/commands/") && r.Method == "PUT":
			id := strings.TrimPrefix(path, "/api/commands/")
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			body["_id"] = id
			json.NewEncoder(w).Encode(body)
		case path == "/api/runcommand" && r.Method == "POST":
			// Mirror the real contract: the command id must arrive under
			// "_id", else 400 "command id is required" (KLA-484).
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if id, _ := body["_id"].(string); id == "" {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"message": "command id is required"})
				return
			}
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
		"ad_translation_rules_list", "ad_translation_rules_recommendations",
		"ad_translation_rules_create", "ad_translation_rules_update",
		"ad_translation_rules_delete", "ad_translation_rules_bulk",
		"ad_translation_rules_preview",
		"devices_settings_get", "devices_settings_signin_set",
		"devices_settings_password_sync_set",

		// password policies (KLA-485)
		"password_policies_list",
		"password_policies_get",
		"password_policies_for_user",
		"password_policies_for_group",
		"password_policies_create",
		"password_policies_update",
		"password_policies_delete",
		"password_policies_set_precedence",

		// workflows (KLA-485)
		"workflows_list",
		"workflows_get",
		"workflows_runs_list",
		"workflows_runs_get",
		"workflows_templates_list",
		"workflows_templates_show",
		"workflows_templates_init",
		"workflows_event_types",
		"workflows_simulate",
		"workflows_health",
		"workflows_lint",
		"workflows_compare_run",
		"workflows_validate",
		"workflows_explain",
		"workflows_create",
		"workflows_update",
		"workflows_delete",
		"workflows_trigger",
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
		// Saved Views
		"saved_views_list", "saved_views_get", "saved_views_create", "saved_views_update", "saved_views_delete",
		// Service Accounts
		"service_accounts_list", "service_accounts_get", "service_accounts_create", "service_accounts_delete", "service_accounts_rotate", "service_accounts_revoke",
		// Roles
		"roles_list", "roles_get", "roles_create", "roles_update", "roles_delete",
		// Notification Channels
		"notification_channels_list", "notification_channels_get", "notification_channels_create", "notification_channels_update", "notification_channels_delete",
		// Alerts
		"alerts_list", "alerts_get", "alerts_stats", "alerts_occurrences", "alerts_notes", "alerts_add_note", "alerts_status", "alerts_delete",
		"alerts_bulk_delete", "alerts_bulk_update",
		// Health-monitoring rules
		"health_rules_list", "health_rules_get", "health_rules_stats", "health_rule_templates_list", "health_rule_templates_get",
		"health_rules_status", "health_rules_create", "health_rules_update", "health_rules_delete",
		// Search (v1 resource-index)
		"search_systems", "search_users", "search_commands", "search_command_results",
		// Reports (read-only)
		"reports_templates_list", "reports_templates_get", "reports_saved_list", "reports_saved_get",
		"reports_custom_list", "reports_custom_get", "reports_builder_list", "reports_builder_get",
		"reports_scheduled_list", "reports_scheduled_get", "reports_scheduled_runs", "reports_scheduled_run_get",
		// Reports writes
		"reports_custom_create", "reports_custom_update", "reports_custom_delete",
		"reports_builder_create", "reports_builder_update", "reports_builder_delete",
		"reports_scheduled_create", "reports_scheduled_update", "reports_scheduled_delete",
		"reports_scheduled_trigger", "reports_export",
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
		// Apple MDM payloads catalog (KLA-452): vendored Apple schema
		// browser + JC Custom-MDM-Profile policy creator. The
		// create_policy tool routes through the step-up auth gate
		// (Execute bool field), same shape as recipe_run / users_delete.
		"apple_mdm_payloads_search",
		"apple_mdm_payloads_show",
		"apple_mdm_payloads_template",
		"apple_mdm_payloads_create_policy",

		// Windows custom MDM policies (KLA-459): OMA-URI + registry
		// passthrough, Execute-gated with preflight validation. Plus
		// the Policy CSP discovery catalog (KLA-460): three read-only
		// tools over Microsoft's fetch-on-demand DDF snapshot.
		"windows_mdm_oma_uri_create_policy",
		"windows_mdm_registry_create_policy",
		"windows_mdm_csp_search",
		"windows_mdm_csp_show",
		"windows_mdm_csp_template",

		// security baseline bundles (KLA-472): list/show/status
		// read-only, apply Execute-gated.
		"bundle_list",
		"bundle_show",
		"bundle_status",
		"bundle_apply",
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
	if len(result.Tools) != 317 {
		t.Errorf("expected 317 tools, got %d", len(result.Tools))
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

// TestMCP_CommandsRunTool_Execute is the #1 regression for the MCP
// surface: the execute path must POST the command id under "_id". The
// fake now 400s on a missing "_id" (mirroring the live endpoint), so a
// regression to the old "command" field fails here.
func TestMCP_CommandsRunTool_Execute(t *testing.T) {
	setupToolTest(t)
	commands := []map[string]any{{"_id": "aabbccddee112233aabbcc01", "name": "Update Agents"}}
	devices := []map[string]any{{"_id": "aabbccddee112233aabbcc02", "hostname": "SERVER-01"}}
	ts := startV1Server(t, nil, devices, commands)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "commands_run", map[string]any{
		"command": "aabbccddee112233aabbcc01",
		"target":  "aabbccddee112233aabbcc02",
		"execute": true,
	})
	if result.IsError {
		t.Fatalf("commands_run execute failed (regression to wrong id field?): %s", getResultText(t, result))
	}
	if !strings.Contains(getResultText(t, result), "triggered") {
		t.Errorf("expected trigger confirmation, got: %s", getResultText(t, result))
	}
}

// TestMCP_CommandsCreateTool_WindowsShell: the #3 regression — a windows
// command created without a shell defaults to powershell.
func TestMCP_CommandsCreateTool_WindowsShell(t *testing.T) {
	setupToolTest(t)
	var body map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		json.NewEncoder(w).Encode(map[string]any{"_id": "x"})
	}))
	t.Cleanup(ts.Close)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	// execute:true — commands_create now plans by default, like every other
	// write tool. This test is about what lands in the request body, so it
	// needs the real call.
	result := callTool(t, cs, "commands_create", map[string]any{
		"name": "Win", "command": "Write-Output hi", "command_type": "windows",
		"execute": true,
	})
	if result.IsError {
		t.Fatalf("commands_create failed: %s", getResultText(t, result))
	}
	if body["shell"] != "powershell" {
		t.Errorf("windows create shell = %v, want powershell", body["shell"])
	}
}

// TestMCP_CommandsUpdateTool_PreservesType is the #2 regression for MCP:
// a partial update (command only) must read-modify-write, preserving
// commandType/shell and stripping server-managed keys.
func TestMCP_CommandsUpdateTool_PreservesType(t *testing.T) {
	setupToolTest(t)
	commands := []map[string]any{{
		"_id": "aabbccddee112233aabbcc09", "id": "aabbccddee112233aabbcc09",
		"name": "WinCmd", "command": "Write-Output hi", "commandType": "windows",
		"shell": "powershell", "launchType": "manual", "organization": "org1",
		"commandRunners": []any{}, "systems": []any{},
	}}
	var putBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &putBody)
			w.Write(raw)
			return
		}
		// GET single command (RMW fetch) and name resolution list.
		if strings.HasSuffix(r.URL.Path, "/aabbccddee112233aabbcc09") {
			json.NewEncoder(w).Encode(commands[0])
			return
		}
		writeV1List(w, commands)
	}))
	t.Cleanup(ts.Close)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "commands_update", map[string]any{
		"identifier": "aabbccddee112233aabbcc09",
		"command":    "Write-Output hi2",
		"execute":    true,
	})
	if result.IsError {
		t.Fatalf("commands_update failed: %s", getResultText(t, result))
	}
	if putBody["commandType"] != "windows" {
		t.Errorf("commandType clobbered to %v, want windows (KLA-484 #2)", putBody["commandType"])
	}
	if putBody["shell"] != "powershell" {
		t.Errorf("shell clobbered to %v, want powershell", putBody["shell"])
	}
	for _, k := range []string{"_id", "id", "organization", "commandRunners", "systems"} {
		if _, ok := putBody[k]; ok {
			t.Errorf("server-managed key %q must not be sent back in the PUT", k)
		}
	}
}

// TestMCP_CommandsUpdateTool_ConvertToWindowsDefaultsShell guards the
// Bugbot finding on PR #93 for the MCP surface: converting a command to
// windows via command_type (no shell) fills the powershell default.
func TestMCP_CommandsUpdateTool_ConvertToWindowsDefaultsShell(t *testing.T) {
	setupToolTest(t)
	commands := []map[string]any{{
		"_id": "aabbccddee112233aabbcc0a", "id": "aabbccddee112233aabbcc0a",
		"name": "L", "command": "ls", "commandType": "linux", "shell": "",
	}}
	var putBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &putBody)
			w.Write(raw)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/aabbccddee112233aabbcc0a") {
			json.NewEncoder(w).Encode(commands[0])
			return
		}
		writeV1List(w, commands)
	}))
	t.Cleanup(ts.Close)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "commands_update", map[string]any{
		"identifier": "aabbccddee112233aabbcc0a", "command_type": "windows", "execute": true,
	})
	if result.IsError {
		t.Fatalf("commands_update failed: %s", getResultText(t, result))
	}
	if putBody["commandType"] != "windows" {
		t.Errorf("commandType = %v, want windows", putBody["commandType"])
	}
	if putBody["shell"] != "powershell" {
		t.Errorf("shell = %v, want powershell (default on convert-to-windows)", putBody["shell"])
	}
}

// TestMCP_CommandsUpdateTool_PlanShowsDefaultedShell guards the second
// Bugbot finding on PR #93: the execute=false plan preview must reflect
// the powershell default that the execute path would persist when
// converting to windows, not just the user-supplied fields.
func TestMCP_CommandsUpdateTool_PlanShowsDefaultedShell(t *testing.T) {
	setupToolTest(t)
	commands := []map[string]any{{
		"_id": "aabbccddee112233aabbcc0b", "id": "aabbccddee112233aabbcc0b",
		"name": "L", "command": "ls", "commandType": "linux", "shell": "",
	}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/aabbccddee112233aabbcc0b") {
			json.NewEncoder(w).Encode(commands[0])
			return
		}
		writeV1List(w, commands)
	}))
	t.Cleanup(ts.Close)
	overrideV1ClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	// execute omitted → plan preview only.
	result := callTool(t, cs, "commands_update", map[string]any{
		"identifier": "aabbccddee112233aabbcc0b", "command_type": "windows",
	})
	if result.IsError {
		t.Fatalf("commands_update plan failed: %s", getResultText(t, result))
	}
	var plan map[string]any
	json.Unmarshal([]byte(getResultText(t, result)), &plan)
	if plan["plan"] != true {
		t.Fatalf("expected a plan preview, got: %v", plan)
	}
	effects, _ := plan["effects"].(map[string]any)
	if effects["shell"] != "powershell" {
		t.Errorf("plan effects shell = %v, want powershell (preview must match execute)", effects["shell"])
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
	// `returned` is the honest field: how many came back in this page.
	// `total` is deliberately ABSENT here — the policies endpoint reports no
	// grand total, and the envelope used to invent one by echoing len(data),
	// which told a paging consumer "that is all of them" without knowing.
	if res["returned"] != float64(0) {
		t.Errorf("expected returned 0, got %v", res["returned"])
	}
	if _, present := res["total"]; present {
		t.Errorf("total must be absent when the API reports none, got %v", res["total"])
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
	// The MCP SDK validates required fields; since go-sdk 1.6 the
	// failure comes back as a TOOL RESULT error (isError + message)
	// rather than a protocol-level error — better for agents, which
	// see the validation detail as tool output they can react to.
	ctx := context.Background()
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "users_create",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("validation failures should be tool results, not protocol errors: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected isError result for missing required fields")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "required") && !strings.Contains(text, "missing") {
		t.Errorf("expected required/missing detail in the result, got: %s", text)
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

// Every mutating tool must take an `execute` argument and return a plan
// without it.
//
// Four shipped without one — apple_mdm_create, groups_user_create,
// user_states_create and users_ssh_keys_add — while 93 others had it. They
// acted immediately on call. apple_mdm_create provisions an MDM certificate
// and users_ssh_keys_add grants access, so "the caller can just be careful"
// was never the right answer.
//
// This asserts the SHAPE across the whole surface rather than naming the four,
// so the next write tool cannot ship without the guard.
func TestMCP_MutatingToolsRequireAnExecuteArgument(t *testing.T) {
	cs := connectToolTestServer(t, Options{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	// Name prefixes/suffixes that denote a write. Kept explicit rather than
	// inferred: a tool named "search" that writes would be a worse problem
	// than this test failing to catch it.
	writeish := func(name string) bool {
		// Matched as a trailing verb, not a substring: "_set" inside
		// "devices_settings_get" is a read, and treating it as a write would
		// put false positives in a list whose whole value is being trusted.
		for _, verb := range []string{
			"create", "update", "delete", "add", "remove", "reset",
			"lock", "unlock", "bind", "unbind", "apply", "trigger",
			"activate", "deactivate", "erase", "import",
		} {
			if strings.HasSuffix(name, "_"+verb) {
				return true
			}
		}
		// A few writes name the object last.
		for _, prefixed := range []string{"import_users", "erase_device"} {
			if strings.HasSuffix(name, prefixed) {
				return true
			}
		}
		return false
	}

	var missing []string
	for _, tool := range res.Tools {
		if !writeish(tool.Name) {
			continue
		}
		raw, merr := json.Marshal(tool.InputSchema)
		if merr != nil {
			continue
		}
		var sch struct {
			Properties map[string]any `json:"properties"`
		}
		if json.Unmarshal(raw, &sch) != nil {
			continue
		}
		if _, ok := sch.Properties["execute"]; !ok {
			missing = append(missing, tool.Name)
		}
	}

	// Tools that legitimately have no execute guard, each with a reason.
	// Adding a name here is a decision, not a convenience.
	// KNOWN DEBT, deliberately listed rather than silently tolerated.
	//
	// 58 write tools take an `execute` argument and return a plan without it.
	// These do not, and act on call. The inconsistency is itself the hazard: a
	// caller cannot predict from a tool's name which kind it is, so "check
	// before calling" is not advice anyone can follow.
	//
	// Guarding them is a behaviour change for every existing caller — a call
	// that creates a user today would start returning a plan — so it is a
	// product decision, not a test fixture one. Until that decision is made
	// the list stays here, visible and countable, and the assertion below
	// stops the set GROWING.
	//
	// Four were fixed when they were reported: apple_mdm_create (it provisions
	// an MDM certificate), users_ssh_keys_add (an SSH key grants access),
	// groups_user_create and user_states_create.
	// Every mutating tool is now guarded. The list that used to sit here — 23
	// tools that acted on call while 58 planned by default — is empty, and the
	// two entries left are not writes.
	allowed := map[string]string{
		// Recipe running is itself a dry-run-capable dispatcher.
		"recipe_run": "takes its own dry_run argument",
		// Both are READS the name heuristic mistook for writes: each lists the
		// users available to import, and imports nothing.
		"gsuite_import_users":    "reads: lists importable users",
		"office365_import_users": "reads: lists importable users",
	}
	var unexplained []string
	for _, name := range missing {
		if _, ok := allowed[name]; !ok {
			unexplained = append(unexplained, name)
		}
	}
	sort.Strings(unexplained)
	if len(unexplained) > 0 {
		t.Errorf("mutating tools with no `execute` guard — they act on call:\n  %s",
			strings.Join(unexplained, "\n  "))
	}
}

// The guard must be HONOURED, not merely declared.
//
// An earlier version of the test above asserted only that an `execute`
// argument existed in the schema. Deleting the plan branch from a handler left
// the argument in place, so the tool acted on call and the test still passed —
// it was checking the doorbell, not the door.
//
// This calls each newly guarded tool WITHOUT execute and asserts two things:
// the response is a plan, and no write reached the server.
func TestMCP_ExecuteGuardsAreHonouredNotJustDeclared(t *testing.T) {
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			writes = append(writes, r.Method+" "+r.URL.Path)
		}
		switch {
		// Resolver paths, so a name can be turned into an id.
		case strings.Contains(r.URL.Path, "/search/systemusers"),
			r.URL.Path == "/api/systemusers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{"_id": "u1", "username": "probe",
					"email": "p@example.com"}}, "totalCount": 1})
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)

	overrideV1ClientForTest(t, srv.URL)
	overrideV2ClientForTest(t, srv.URL)
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"groups_user_create", map[string]any{"name": "probe-group"}},
		{"apple_mdm_create", map[string]any{"name": "probe-mdm"}},
		{"users_ssh_keys_add", map[string]any{
			"user": "probe", "name": "k", "public_key": "ssh-rsa AAAA"}},
		{"user_states_create", map[string]any{
			"user": "probe", "state": "SUSPENDED", "start_date": "2026-12-01"}},

		// The rest of the surface, guarded in the same pass. These are the
		// ones where acting on call is worst: an administrator has console
		// access, and a command trigger runs scripts on real devices.
		{"users_create", map[string]any{"username": "probe2", "email": "p2@example.com"}},
		{"admins_create", map[string]any{"email": "admin@example.com"}},
		{"iplists_create", map[string]any{"name": "probe-list", "ips": []any{"10.0.0.1"}}},
		{"policy_groups_create", map[string]any{"name": "probe-pg"}},
	} {
		writes = nil
		out := getResultText(t, callTool(t, cs, c.tool, c.args))

		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Errorf("%s: expected a plan document, got: %s", c.tool, out)
			continue
		}
		if m["plan"] != true {
			t.Errorf("%s: called without execute and did not return a plan: %s", c.tool, out)
		}
		// The part that matters: nothing was actually done.
		for _, wr := range writes {
			if strings.HasPrefix(wr, "POST") || strings.HasPrefix(wr, "PUT") ||
				strings.HasPrefix(wr, "DELETE") {
				// The resolver may POST to a search endpoint; that is a read.
				if strings.Contains(wr, "/search/") {
					continue
				}
				t.Errorf("%s: called without execute but issued %s", c.tool, wr)
			}
		}
	}
}

// `total` must mean one thing, everywhere it appears.
//
// It used to mean two: 52 call sites passed len(data) — "how many I am handing
// you" — and 13 passed the API's count — "how many exist". Two catalog tools
// meant a third thing, the size of the whole corpus. Same key, no error when
// read wrong, so a consumer paging on it looped correctly against users_list
// and stopped early against groups_*: the tenant has at least 8 user groups
// while groups_user_list(limit:2) reported total:2.
//
// The rule now: `returned` is this page, always. `total` is how many exist,
// and is ABSENT when unknown. `catalog_size` is a corpus, and never called
// total.
func TestMCP_ListEnvelopeTotalMeansOneThing(t *testing.T) {
	// A page whose grand total the API reported.
	known := 42
	res, _, err := listEnvelope([]json.RawMessage{[]byte(`{"id":"a"}`)}, &known)
	if err != nil {
		t.Fatal(err)
	}
	m := decodeEnvelope(t, res)
	if m["returned"] != float64(1) {
		t.Errorf("returned = %v, want 1 (the length of this page)", m["returned"])
	}
	if m["total"] != float64(42) {
		t.Errorf("total = %v, want the API's count", m["total"])
	}

	// A page whose grand total is unknown: `total` must be absent, not echoed
	// back as the page length.
	res, _, err = listEnvelope([]json.RawMessage{[]byte(`{"id":"a"}`), []byte(`{"id":"b"}`)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m = decodeEnvelope(t, res)
	if m["returned"] != float64(2) {
		t.Errorf("returned = %v, want 2", m["returned"])
	}
	if v, present := m["total"]; present {
		t.Errorf("total must be absent when unknown, got %v — echoing the page "+
			"length tells a paging caller 'that is all of them' without knowing", v)
	}

	// An empty page still reports honestly rather than omitting the field.
	res, _, err = listEnvelope(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m = decodeEnvelope(t, res)
	if m["returned"] != float64(0) {
		t.Errorf("returned = %v on an empty page, want 0", m["returned"])
	}
	if _, ok := m["data"].([]any); !ok {
		t.Errorf("data must be an empty array, not null: %v", m["data"])
	}
}

// No list tool may report a self-referential total again. The helper that made
// that possible is gone; this asserts nobody reintroduces the shape by hand.
func TestMCP_NoToolEchoesPageLengthAsTotal(t *testing.T) {
	srcs, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range srcs {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		body, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		// rawListResult(x, len(x)) — the exact shape that made total a lie.
		if m := regexp.MustCompile(`rawListResult\(\s*(\w[\w.]*)\s*,\s*len\(\s*(\w[\w.]*)\s*\)`).
			FindAllStringSubmatch(string(body), -1); m != nil {
			for _, hit := range m {
				if hit[1] == hit[2] {
					t.Errorf("%s: rawListResult(%s, len(%s)) reports the page length as a "+
						"grand total — use rawListPage(%s)", f, hit[1], hit[2], hit[1])
				}
			}
		}
	}
}

func decodeEnvelope(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(getResultText(t, res)), &m); err != nil {
		t.Fatalf("envelope did not parse: %v", err)
	}
	return m
}

// A tool nobody can find is a tool that does not exist.
//
// An enumeration found eleven tools unreachable through tool_search —
// users_create, devices_list, apple_mdm_get among them — while natural
// phrasings returned neighbours instead. devices_get surfaced only when
// queried with its own description almost verbatim.
//
// The cause was description length: those tools carried the shortest text on
// the surface, several under 40 characters and saying nothing beyond their own
// name ("Create a new JumpCloud user."). Search has nothing to match on but
// the words present, so a description that restates the identifier competes
// badly against one that mentions what the thing is called in practice.
//
// The bar here is deliberately low. It is a floor against descriptions that
// carry no information, NOT a target: length is a proxy for "does this contain
// the words someone would actually type", and padding one to clear the
// threshold would defeat the point entirely.
func TestMCP_ToolDescriptionsCarryMoreThanTheirOwnName(t *testing.T) {
	const floor = 60

	srcs, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	pat := regexp.MustCompile(`addTypedTool\(s, "(\w+)", "((?:[^"\\]|\\.)*)"`)

	type finding struct {
		name string
		n    int
	}
	var thin []finding
	for _, f := range srcs {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		body, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		for _, m := range pat.FindAllStringSubmatch(string(body), -1) {
			if len(m[2]) < floor {
				thin = append(thin, finding{m[1], len(m[2])})
			}
		}
	}

	// KNOWN DEBT. These predate the floor and each needs a rewrite by someone
	// who knows what the tool actually does — padding one to clear a threshold
	// would defeat the point, and a confident-sounding wrong description is
	// worse for an agent than a terse true one. Listed so the debt is
	// countable and so the set cannot GROW.
	//
	// 36 were rewritten when the discoverability problem was reported: the
	// eleven named as unreachable, plus every description under 45 characters.
	predating := map[string]string{
		"access_requests_get":           "pre-existing: 55 chars",
		"ad_create":                     "pre-existing: 52 chars",
		"admins_get":                    "pre-existing: 52 chars",
		"alerts_get":                    "pre-existing: 50 chars",
		"app_templates_get":             "pre-existing: 50 chars",
		"apple_mdm_devices":             "pre-existing: 52 chars",
		"apple_mdm_enrollment_profiles": "pre-existing: 56 chars",
		"apps_get":                      "pre-existing: 53 chars",
		"auth_policies_get":             "pre-existing: 59 chars",
		"commands_get":                  "pre-existing: 45 chars",
		"custom_emails_get":             "pre-existing: 57 chars",
		"duo_get":                       "pre-existing: 49 chars",
		"groups_device_get":             "pre-existing: 59 chars",
		"groups_user_get":               "pre-existing: 48 chars",
		"gsuite_get":                    "pre-existing: 57 chars",
		"gsuite_import_users":           "pre-existing: 49 chars",
		"gsuite_list":                   "pre-existing: 59 chars",
		"gsuite_translation_rules":      "pre-existing: 49 chars",
		"health_rules_stats":            "pre-existing: 48 chars",
		"identity_providers_get":        "pre-existing: 55 chars",
		"iplists_get":                   "pre-existing: 45 chars",
		"ldap_get":                      "pre-existing: 49 chars",
		"ldap_samba_domain_get":         "pre-existing: 56 chars",
		"ldap_samba_domains_list":       "pre-existing: 47 chars",
		"notification_channels_get":     "pre-existing: 58 chars",
		"office365_import_users":        "pre-existing: 53 chars",
		"office365_translation_rules":   "pre-existing: 53 chars",
		"org_settings":                  "pre-existing: 52 chars",
		"password_policies_for_group":   "pre-existing: 58 chars",
		"policies_create":               "pre-existing: 46 chars",
		"saas_management_accounts":      "pre-existing: 58 chars",
		"saas_management_catalog_get":   "pre-existing: 55 chars",
		"saas_management_create":        "pre-existing: 51 chars",
		"saas_management_licenses":      "pre-existing: 56 chars",
		"saas_management_usage":         "pre-existing: 59 chars",
		"saved_views_get":               "pre-existing: 48 chars",
		"service_accounts_get":          "pre-existing: 53 chars",
		"software_associations":         "pre-existing: 54 chars",
		"software_get":                  "pre-existing: 50 chars",
	}

	sort.Slice(thin, func(i, j int) bool { return thin[i].n < thin[j].n })
	for _, f := range thin {
		if _, known := predating[f.name]; known {
			continue
		}
		t.Errorf("%s: %d-char description — too thin to be found by search. "+
			"Say what it is called in practice (an admin is not a user; a system is a machine, "+
			"computer or laptop), and what it does NOT do.", f.name, f.n)
	}
}

// A description must not promise an argument the tool does not accept.
//
// users_create said "optional firstname, lastname, department, job title,
// employee id and attributes" and accepted only the first three. All six fields
// are real on the user OBJECT, which is how the error happened: a rewrite aimed
// at discoverability described the DOMAIN rather than the INTERFACE. A caller
// writes code against a parameter that is silently ignored.
//
// This is the last_ran bug inverted. There a description named an output field
// that did not exist; here it named input fields that did not. Same class,
// opposite direction, same silent failure.
//
// The check reads enumerated field lists out of the prose — the "requires X and
// Y" / "optional A, B and C" shapes — and asserts each names a real argument.
func TestMCP_DescriptionsDoNotPromiseAbsentArguments(t *testing.T) {
	body, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)

	// json keys per input struct.
	structs := map[string]map[string]bool{}
	for _, m := range regexp.MustCompile(`(?s)type (\w+) struct \{(.*?)\n\}`).FindAllStringSubmatch(src, -1) {
		keys := map[string]bool{}
		for _, k := range regexp.MustCompile(`json:"(\w+)`).FindAllStringSubmatch(m[2], -1) {
			keys[strings.ToLower(k[1])] = true
		}
		structs[m[1]] = keys
	}

	// Phrases that name a field in prose, mapped to the argument they imply.
	// Deliberately a short curated list: a general parser over English would
	// produce false positives, and a check nobody trusts gets ignored.
	implies := map[string][]string{
		"job title":           {"job_title", "jobtitle"},
		"employee id":         {"employee_id", "employeeid", "employeeidentifier"},
		"employee identifier": {"employee_id", "employeeidentifier"},
		"custom attributes":   {"attributes"},
	}

	toolPat := regexp.MustCompile(`addTypedTool\(s, "(\w+)", "((?:[^"\\]|\\.)*)",\s*\n\s*func\(ctx context\.Context, req \*mcp\.CallToolRequest, args (\w+)\)`)
	for _, m := range toolPat.FindAllStringSubmatch(src, -1) {
		tool, desc, st := m[1], strings.ToLower(m[2]), m[3]
		args := structs[st]

		// Only the part of the description that enumerates inputs. Prose
		// elsewhere may legitimately mention a field as output.
		enumerated := ""
		for _, lead := range []string{"requires ", "optional "} {
			if i := strings.Index(desc, lead); i >= 0 {
				seg := desc[i:]
				if j := strings.Index(seg, "."); j > 0 {
					seg = seg[:j]
				}
				enumerated += " " + seg
			}
		}
		if enumerated == "" {
			continue
		}

		for phrase, names := range implies {
			if !strings.Contains(enumerated, phrase) {
				continue
			}
			ok := false
			for _, n := range names {
				if args[n] {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s names %q where it enumerates its inputs, but accepts none of %v — "+
					"a caller writes against a parameter that is silently ignored",
					tool, phrase, names)
			}
		}
	}
}

// Sibling tools must not describe themselves almost identically.
//
// A verification pass found two tools unreachable by natural phrasing even
// though their text was correct. users_delete says "remove a leaver" verbatim
// and still lost "remove a user who left" to groups_remove_member; apple_mdm_list
// lost to apple_mdm_create. Neither is a missing-words problem — it is a
// DISCRIMINATION problem.
//
// The mechanism was visible in the text. apple_mdm_list and apple_mdm_create
// shared about twenty tokens of identical boilerplate, so the one word that
// distinguished them competed against twenty that did not. Pairs doing opposite
// things were worse: users_lock and users_unlock differed by a single character
// in otherwise identical sentences.
//
// Note this pulls against TestMCP_DescriptionsDoNotNameOtherTools, deliberately:
// the easy way to make two tools read differently is to have each name the
// other, and that turned out to move queries between them. The difference has
// to come from describing each tool's own effect instead. That constraint is
// the useful part — it forces a real description rather than a signpost.
//
// This is a floor on how alike two tools in one family may read. It is a proxy
// — search ranking is not vocabulary overlap — but the pairs it catches are
// genuinely ones a caller could be handed the wrong half of, and the fix that
// clears it (say what the tool does NOT do, and name its counterpart) is worth
// making regardless.
func TestMCP_SiblingToolsAreDistinguishable(t *testing.T) {
	body, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatal(err)
	}
	pat := regexp.MustCompile(`addTypedTool\(s, "(\w+)", "((?:[^"\\]|\\.)*)"`)
	type tool struct{ name, desc string }
	var tools []tool
	for _, m := range pat.FindAllStringSubmatch(string(body), -1) {
		tools = append(tools, tool{m[1], m[2]})
	}

	words := func(d string) map[string]bool {
		out := map[string]bool{}
		for _, w := range strings.Fields(strings.ToLower(d)) {
			w = strings.Trim(w, ".,—:;()`\"")
			if len(w) > 3 {
				out[w] = true
			}
		}
		return out
	}

	// Pairs that do OPPOSITE things are the ones worth failing on: being handed
	// the wrong half is a mistake, not an inconvenience.
	opposites := [][2]string{
		{"users_lock", "users_unlock"},
		{"custom_emails_create", "custom_emails_delete"},
		{"auth_policies_delete", "auth_policies_disable"},
		{"apple_mdm_list", "apple_mdm_create"},
		{"groups_remove_member", "users_delete"},
	}
	byName := map[string]string{}
	for _, tl := range tools {
		byName[tl.name] = tl.desc
	}

	const ceiling = 0.60
	for _, pair := range opposites {
		a, b := byName[pair[0]], byName[pair[1]]
		if a == "" || b == "" {
			t.Errorf("pair %v: a tool is missing", pair)
			continue
		}
		wa, wb := words(a), words(b)
		shared := 0
		for w := range wa {
			if wb[w] {
				shared++
			}
		}
		// Containment, not Jaccard: what fraction of the SHORTER description
		// is also in the longer one. Jaccard lets a stub sibling pass simply
		// because its counterpart is verbose — which is precisely the case
		// here, where one tool was rewritten richly and the other left as a
		// one-liner. Containment asks the question that matters: is this tool
		// wholly describable in its sibling's words?
		smaller := len(wa)
		if len(wb) < smaller {
			smaller = len(wb)
		}
		if smaller == 0 {
			continue
		}
		if j := float64(shared) / float64(smaller); j > ceiling {
			t.Errorf("%s and %s do opposite things, but %.0f%% of the shorter description's "+
				"words appear in the other — describe each tool's own distinct effect. Note the "+
				"constraint these two tests impose together: you may NOT create the difference "+
				"by naming the counterpart, because that moves its queries onto this tool "+
				"(see TestMCP_DescriptionsDoNotNameOtherTools). The difference has to come "+
				"from what the tool itself does.",
				pair[0], pair[1], j*100)
		}
	}
}

// A tool's description must not name another tool.
//
// This reverses advice I gave and shipped, on evidence that it backfired.
//
// Making siblings distinguishable, I wrote cross-references like "see
// users_delete to remove the person, groups_remove_member to revoke one group's
// access". Accurate, useful to a human — and it moved the REFERENCED tool's
// queries onto the REFERRING one. users_lock then ranked first for both "remove
// a user who left" and "revoke someone's access to one group", with the tool
// that should have won absent from the results entirely.
//
// The index cannot tell a description of the tool from a description of its
// alternatives. Worse, the bare identifier is enough on its own: users_unlock
// saying "the exact inverse of users_lock" made it outrank users_lock on "lock
// a user", because unlock's text contained that literal token and lock's did
// not repeat its own name.
//
// So the rule is the strict one: describe what THIS tool does, including what
// it does not do, without naming another tool. Pointing a caller onward is
// still worth doing — it belongs in a result or an error message, where it does
// not enter the index.
func TestMCP_DescriptionsDoNotNameOtherTools(t *testing.T) {
	body, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	pat := regexp.MustCompile(`addTypedTool\(s, "(\w+)", "((?:[^"\\]|\\.)*)"`)

	type entry struct{ name, desc string }
	var tools []entry
	names := map[string]bool{}
	for _, m := range pat.FindAllStringSubmatch(src, -1) {
		tools = append(tools, entry{m[1], m[2]})
		names[m[1]] = true
	}
	if len(tools) < 100 {
		t.Fatalf("only %d tools parsed — the pattern probably broke", len(tools))
	}

	// Tools whose whole purpose is to point at another, where naming it is the
	// content rather than a cross-reference.
	exempt := map[string]bool{
		"recipe_run": true, // dispatches to other operations by name
		"jc_ping":    true,
		"jc_whoami":  true,
	}

	for _, tl := range tools {
		if exempt[tl.name] {
			continue
		}
		for other := range names {
			if other == tl.name || len(other) < 8 {
				continue
			}
			if strings.Contains(tl.desc, other) {
				t.Errorf("%s names %s in its description — the index cannot tell a description "+
					"of this tool from one of its alternative, and the reference moves %s's "+
					"queries onto %s. Say what this tool does instead; put the pointer in a "+
					"result or error message.", tl.name, other, other, tl.name)
			}
		}
	}
}
