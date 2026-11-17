package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/spf13/viper"
)

// startAppsServer creates a mock JumpCloud server that handles V1 /applications endpoints
// and V2 /applications/{id}/associations endpoints.
func startAppsServer(t *testing.T, apps []map[string]any, associations map[string]map[string][]map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// V1: GET /applications — list endpoint.
		if r.URL.Path == "/applications" && r.Method == http.MethodGet {
			resp := map[string]any{
				"results":    apps,
				"totalCount": len(apps),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// V1: POST /applications — create endpoint. Mirrors the live contract:
		// name must be a catalog connector key, the SSO type follows from it,
		// and a top-level ssoType is ignored.
		if r.URL.Path == "/applications" && r.Method == http.MethodPost {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			ssoType, ok := mockConnectors[fmt.Sprint(body["name"])]
			if !ok {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"status":400,"error":"Could not find the service provider."}`))
				return
			}
			delete(body, "ssoType")
			body["sso"] = map[string]any{"type": ssoType}
			body["_id"] = "aabbccddee112233aabb9001"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(body)
			return
		}

		// Routes under /applications/{id}.
		if strings.HasPrefix(r.URL.Path, "/applications/") {
			rest := strings.TrimPrefix(r.URL.Path, "/applications/")
			parts := strings.SplitN(rest, "/", 2)
			id := parts[0]

			// V2: GET /applications/{id}/associations?targets=<type>
			if len(parts) == 2 && parts[1] == "associations" {
				targets := r.URL.Query().Get("targets")
				if associations != nil {
					if appAssoc, ok := associations[id]; ok {
						if items, ok := appAssoc[targets]; ok {
							json.NewEncoder(w).Encode(items)
							return
						}
					}
				}
				// Return empty array for unknown associations.
				json.NewEncoder(w).Encode([]map[string]any{})
				return
			}

			// V1: GET /applications/{id} — get endpoint.
			if r.Method == http.MethodGet {
				for _, a := range apps {
					if a["_id"] == id {
						json.NewEncoder(w).Encode(a)
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}

			// V1: PUT /applications/{id} — update endpoint.
			if r.Method == http.MethodPut {
				for _, a := range apps {
					if a["_id"] == id {
						var body map[string]any
						json.NewDecoder(r.Body).Decode(&body)
						for k, v := range body {
							a[k] = v
						}
						json.NewEncoder(w).Encode(a)
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}

			// V1: DELETE /applications/{id} — delete endpoint.
			if r.Method == http.MethodDelete {
				for _, a := range apps {
					if a["_id"] == id {
						json.NewEncoder(w).Encode(a)
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

// mockConnectors maps the catalog connector keys the mock accepts on create to
// the SSO type JumpCloud derives from each.
var mockConnectors = map[string]string{"bookmark": "bookmark", "aws-sso": "saml", "slack": "oidc"}

func sampleApps() []map[string]any {
	return []map[string]any{
		{
			"_id":          "aabbccddee112233aabb2001",
			"name":         "AWS SSO",
			"displayLabel": "Amazon Web Services",
			"sso":          map[string]any{"type": "saml"},
			"active":       true,
			"organization": "org001org001org001org001",
		},
		{
			"_id":          "aabbccddee112233aabb2002",
			"name":         "Slack",
			"displayLabel": "Slack Workspace",
			"sso":          map[string]any{"type": "oidc"},
			"active":       true,
			"organization": "org001org001org001org001",
		},
		{
			"_id":          "aabbccddee112233aabb2003",
			"name":         "GitHub Enterprise",
			"displayLabel": "GitHub",
			"sso":          map[string]any{"type": "saml"},
			"active":       false,
			"organization": "org001org001org001org001",
		},
	}
}

func sampleAppAssociations() map[string]map[string][]map[string]any {
	return map[string]map[string][]map[string]any{
		"aabbccddee112233aabb2001": {
			"user_group": {
				{
					"to":         map[string]any{"id": "ug001ug001ug001ug001ug01", "type": "user_group"},
					"attributes": map[string]any{},
				},
				{
					"to":         map[string]any{"id": "ug002ug002ug002ug002ug02", "type": "user_group"},
					"attributes": map[string]any{},
				},
			},
			"system_group": {
				{
					"to":         map[string]any{"id": "sg001sg001sg001sg001sg01", "type": "system_group"},
					"attributes": map[string]any{},
				},
			},
		},
	}
}

// setupAppsTest creates a combined V1+V2 mock server and overrides both clients.
func setupAppsTest(t *testing.T, apps []map[string]any, associations map[string]map[string][]map[string]any) *httptest.Server {
	t.Helper()
	ts := startAppsServer(t, apps, associations)
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)
	return ts
}

// --- List Tests ---

func TestAppsList_JSON(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d apps, want 3", len(result))
	}
}

func TestAppsList_Table(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "AWS SSO") {
		t.Errorf("table output should contain 'AWS SSO', got:\n%s", out)
	}
}

func TestAppsList_CSV(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--output", "csv"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "AWS SSO") {
		t.Errorf("CSV output should contain 'AWS SSO', got:\n%s", out)
	}
}

func TestAppsList_IDs(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d ID lines, want 3", len(lines))
	}
	if lines[0] != "aabbccddee112233aabb2001" {
		t.Errorf("first ID = %q, want aabbccddee112233aabb2001", lines[0])
	}
}

func TestAppsList_Quiet(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--quiet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("quiet output should be empty, got: %q", buf.String())
	}
}

func TestAppsList_Footer(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"apps", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errBuf.String()
	if !strings.Contains(footer, "3 items") {
		t.Errorf("footer should contain '3 items', got: %q", footer)
	}
}

func TestAppsList_Empty(t *testing.T) {
	setupUsersTest(t)
	ts := setupAppsTest(t, []map[string]any{}, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 0 {
		t.Errorf("got %d apps, want 0", len(result))
	}
}

func TestAppsList_Filter(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--filter", "name=bookmark"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
}

// The API rejects every filter on the SSO type, so --sso-type filters the
// fetched list on sso.type locally.
func TestAppsList_SSOTypeFiltersLocally(t *testing.T) {
	setupUsersTest(t)
	var gotQuery string
	apps := sampleApps()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]any{"results": apps, "totalCount": len(apps)})
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--sso-type", "SAML"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if strings.Contains(gotQuery, "filter") {
		t.Errorf("--sso-type must not be sent as a server filter, query = %q", gotQuery)
	}
	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if len(result) != 2 {
		t.Fatalf("got %d apps, want the 2 saml apps", len(result))
	}
	for _, a := range result {
		if a["sso"].(map[string]any)["type"] != "saml" {
			t.Errorf("non-saml app in result: %v", a)
		}
	}
}

func TestAppsList_SSOTypeWithLimitRejected(t *testing.T) {
	setupUsersTest(t)
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--sso-type", "saml", "--limit", "5"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--limit") {
		t.Fatalf("expected a --limit error, got %v", err)
	}
}

func TestAppsList_Sort(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--sort", "-name"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
}

func TestAppsList_InvalidFilter(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--filter", "badfilter"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid filter, got nil")
	}
}

func TestAppsList_Limit(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) > 2 {
		t.Errorf("got %d apps, want at most 2", len(result))
	}
}

// --- Get Tests ---

func TestAppsGet_ByID(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	assoc := sampleAppAssociations()
	ts := setupAppsTest(t, apps, assoc)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "get", "aabbccddee112233aabb2001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "AWS SSO" {
		t.Errorf("name = %q, want 'AWS SSO'", result["name"])
	}
}

func TestAppsGet_ByName(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	assoc := sampleAppAssociations()
	ts := setupAppsTest(t, apps, assoc)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "get", "AWS SSO"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["_id"] != "aabbccddee112233aabb2001" {
		t.Errorf("_id = %q, want 'aabbccddee112233aabb2001'", result["_id"])
	}
}

func TestAppsGet_IncludesAssociations(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	assoc := sampleAppAssociations()
	ts := setupAppsTest(t, apps, assoc)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "get", "aabbccddee112233aabb2001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	// Check user group associations.
	ug, ok := result["associatedUserGroups"].([]any)
	if !ok {
		t.Fatalf("associatedUserGroups missing or wrong type, got: %T", result["associatedUserGroups"])
	}
	if len(ug) != 2 {
		t.Errorf("got %d user group associations, want 2", len(ug))
	}

	// Check device group associations.
	dg, ok := result["associatedDeviceGroups"].([]any)
	if !ok {
		t.Fatalf("associatedDeviceGroups missing or wrong type, got: %T", result["associatedDeviceGroups"])
	}
	if len(dg) != 1 {
		t.Errorf("got %d device group associations, want 1", len(dg))
	}
}

func TestAppsGet_NoAssociations(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	// No associations provided — should return empty arrays.
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "get", "aabbccddee112233aabb2002"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	// Should still have the association fields, just empty.
	ug, ok := result["associatedUserGroups"].([]any)
	if !ok {
		t.Fatalf("associatedUserGroups missing or wrong type")
	}
	if len(ug) != 0 {
		t.Errorf("got %d user group associations, want 0", len(ug))
	}

	dg, ok := result["associatedDeviceGroups"].([]any)
	if !ok {
		t.Fatalf("associatedDeviceGroups missing or wrong type")
	}
	if len(dg) != 0 {
		t.Errorf("got %d device group associations, want 0", len(dg))
	}
}

func TestAppsGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "get", "NonExistentApp"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found app, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestAppsGet_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "get"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing arg, got nil")
	}
}

// --- Help Tests ---

func TestAppsHelp_Subcommands(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, sub := range []string{"list", "get", "create", "update", "delete"} {
		if !strings.Contains(out, sub) {
			t.Errorf("help should contain subcommand %q, got:\n%s", sub, out)
		}
	}
}

func TestAppsHelp_ListFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, flag := range []string{"--filter", "--sort", "--limit"} {
		if !strings.Contains(out, flag) {
			t.Errorf("list help should contain flag %q, got:\n%s", flag, out)
		}
	}
}

func TestAppsHelp_RootIncludesApps(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "apps") {
		t.Errorf("root help should contain 'apps', got:\n%s", out)
	}
}

// --- Create Tests ---

func TestAppsCreate(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	var posted map[string]any
	inner := startAppsServer(t, apps, nil).Config.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &posted)
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		inner.ServeHTTP(w, r)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	errBuf := &bytes.Buffer{}
	cmd.SetOut(&buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"apps", "create", "--name", "bookmark", "--display-label", "Team Wiki",
		"--sso-url", "https://wiki.example.com", "--sso-type", "saml"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdout must be clean JSON: %v\nOutput: %s", err, buf.String())
	}
	if posted["name"] != "bookmark" || posted["displayLabel"] != "Team Wiki" || posted["ssoUrl"] != "https://wiki.example.com" {
		t.Errorf("posted body = %v", posted)
	}
	if _, sent := posted["ssoType"]; sent {
		t.Errorf("ssoType must not be sent (JumpCloud ignores it): %v", posted)
	}
	if !strings.Contains(errBuf.String(), "--sso-type is ignored") {
		t.Errorf("expected a warning about --sso-type on stderr, got %q", errBuf.String())
	}
	if result["_id"] != "aabbccddee112233aabb9001" {
		t.Errorf("_id = %v, want 'aabbccddee112233aabb9001'", result["_id"])
	}
}

func TestAppsCreate_UnknownConnectorExplained(t *testing.T) {
	setupUsersTest(t)
	ts := setupAppsTest(t, sampleApps(), nil)
	defer ts.Close()

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "create", "--name", "Team Wiki"})

	err := cmd.Execute()
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected a CLIError, got %v", err)
	}
	if !strings.Contains(cliErr.Suggestion, "catalog connector key") || !strings.Contains(cliErr.Suggestion, "--display-label") {
		t.Errorf("suggestion should explain the connector key, got %q", cliErr.Suggestion)
	}
}

func TestAppsCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	errBuf := &bytes.Buffer{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"apps", "create", "--name", "bookmark", "--display-label", "Team Wiki", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.Code != plan.ExitCodePlan {
		t.Errorf("exit code = %d, want %d", exitErr.Code, plan.ExitCodePlan)
	}
	if !strings.Contains(errBuf.String(), "create") || !strings.Contains(errBuf.String(), "Team Wiki") {
		t.Errorf("plan should mention 'create' and the label, got: %s", errBuf.String())
	}
}

func TestAppsCreate_MissingName(t *testing.T) {
	setupUsersTest(t)
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "create", "--display-label", "x"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --name")
	}
}

// --- Update Tests ---

func TestAppsUpdate(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "update", "aabbccddee112233aabb2001", "--display-label", "Updated App"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["displayLabel"] != "Updated App" {
		t.Errorf("displayLabel = %v, want 'Updated App'", result["displayLabel"])
	}
}

// name is the catalog connector key: JumpCloud refuses or silently ignores a
// change, so jc refuses before sending anything.
func TestAppsUpdate_NameRefusedWithoutRequest(t *testing.T) {
	setupUsersTest(t)
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "update", "aabbccddee112233aabb2001", "--name", "slack"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--display-label") {
		t.Fatalf("expected a refusal pointing at --display-label, got %v", err)
	}
	if requests != 0 {
		t.Errorf("made %d requests, want 0", requests)
	}
}

func TestAppsUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "update", "aabbccddee112233aabb2001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields to update")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should mention 'no fields to update', got: %v", err)
	}
}

func TestAppsUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	errBuf := &bytes.Buffer{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"apps", "update", "aabbccddee112233aabb2001", "--display-label", "Updated App", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.Code != plan.ExitCodePlan {
		t.Errorf("exit code = %d, want %d", exitErr.Code, plan.ExitCodePlan)
	}
}

// --- Delete Tests ---

func TestAppsDelete_Force(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "delete", "aabbccddee112233aabb2001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(buf.String(), "AWS SSO") {
		t.Errorf("output should mention app name: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "deleted successfully") {
		t.Errorf("output should confirm deletion: %s", buf.String())
	}
}

func TestAppsDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	errBuf := &bytes.Buffer{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"apps", "delete", "aabbccddee112233aabb2001", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.Code != plan.ExitCodePlan {
		t.Errorf("exit code = %d, want %d", exitErr.Code, plan.ExitCodePlan)
	}
	if !strings.Contains(errBuf.String(), "delete") {
		t.Errorf("plan should mention 'delete', got: %s", errBuf.String())
	}
}

func TestAppsDelete_NotFound(t *testing.T) {
	setupUsersTest(t)
	viper.Set("cache.directory", t.TempDir())
	apps := sampleApps()
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "delete", "NonExistentApp", "--force"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not found app")
	}
}

func TestAppsDelete_MissingArg(t *testing.T) {
	setupUsersTest(t)
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "delete"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}
