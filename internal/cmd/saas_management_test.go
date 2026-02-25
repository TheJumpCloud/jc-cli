package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startSaaSServer creates a mock JumpCloud V2 server that handles SaaS Management endpoints:
//   - /saas-management/applications (list/create/get/update/delete)
//   - /saas-management/applications/{id}/accounts (list/get/delete)
//   - /saas-management/applications/{id}/usage (get)
//   - /saas-management/application-licenses (list)
//   - /saas-management/application-catalog/{catalog_app_id} (get)
func startSaaSServer(t *testing.T, apps []map[string]any, accounts []map[string]any, licenses []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /saas-management/applications — list apps (wrapped in {"results": [...], "totalCount": N}).
		if r.URL.Path == "/saas-management/applications" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"results":    apps,
				"totalCount": len(apps),
			})
			return
		}

		// POST /saas-management/applications — create app.
		if r.URL.Path == "/saas-management/applications" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "new123new123new123new123"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// GET /saas-management/application-licenses — list licenses.
		if r.URL.Path == "/saas-management/application-licenses" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(licenses)
			return
		}

		// GET /saas-management/application-catalog/{catalog_app_id} — catalog entry.
		if strings.HasPrefix(r.URL.Path, "/saas-management/application-catalog/") && r.Method == http.MethodGet {
			catalogID := strings.TrimPrefix(r.URL.Path, "/saas-management/application-catalog/")
			json.NewEncoder(w).Encode(map[string]any{
				"id":          catalogID,
				"name":        strings.ToUpper(catalogID[:1]) + catalogID[1:],
				"description": "A test catalog entry",
				"domains":     []string{catalogID + ".com"},
			})
			return
		}

		// Routes under /saas-management/applications/{id}...
		if strings.HasPrefix(r.URL.Path, "/saas-management/applications/") {
			rest := strings.TrimPrefix(r.URL.Path, "/saas-management/applications/")
			parts := strings.SplitN(rest, "/", 2)
			appID := parts[0]

			// Find the app.
			var foundApp map[string]any
			for _, a := range apps {
				if a["id"] == appID {
					foundApp = a
					break
				}
			}

			// No sub-path: /saas-management/applications/{id}
			if len(parts) == 1 {
				if foundApp == nil {
					w.WriteHeader(http.StatusNotFound)
					w.Write([]byte(`{"message":"Not Found"}`))
					return
				}

				switch r.Method {
				case http.MethodGet:
					// Single GET includes name field.
					resp := make(map[string]any)
					for k, v := range foundApp {
						resp[k] = v
					}
					if _, ok := resp["name"]; !ok {
						if catID, ok := resp["catalog_app_id"].(string); ok {
							resp["name"] = strings.ToUpper(catID[:1]) + catID[1:]
						}
					}
					json.NewEncoder(w).Encode(resp)
					return
				case http.MethodPut:
					var input map[string]any
					json.NewDecoder(r.Body).Decode(&input)
					for k, v := range input {
						foundApp[k] = v
					}
					json.NewEncoder(w).Encode(foundApp)
					return
				case http.MethodDelete:
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(foundApp)
					return
				}
			}

			// Sub-path: accounts or usage.
			if len(parts) == 2 {
				subPath := parts[1]

				// /saas-management/applications/{id}/accounts...
				if strings.HasPrefix(subPath, "accounts") {
					acctRest := strings.TrimPrefix(subPath, "accounts")

					// List accounts.
					if acctRest == "" {
						if foundApp == nil {
							w.WriteHeader(http.StatusNotFound)
							w.Write([]byte(`{"message":"Not Found"}`))
							return
						}
						var appAccounts []map[string]any
						for _, a := range accounts {
							if a["parent_app_id"] == appID {
								appAccounts = append(appAccounts, a)
							}
						}
						if appAccounts == nil {
							appAccounts = []map[string]any{}
						}
						json.NewEncoder(w).Encode(appAccounts)
						return
					}

					// Get or delete a specific account.
					if strings.HasPrefix(acctRest, "/") {
						accountID := strings.TrimPrefix(acctRest, "/")
						var foundAccount map[string]any
						for _, a := range accounts {
							if a["id"] == accountID && a["parent_app_id"] == appID {
								foundAccount = a
								break
							}
						}
						if foundAccount == nil {
							w.WriteHeader(http.StatusNotFound)
							w.Write([]byte(`{"message":"Not Found"}`))
							return
						}
						switch r.Method {
						case http.MethodGet:
							json.NewEncoder(w).Encode(foundAccount)
							return
						case http.MethodDelete:
							w.WriteHeader(http.StatusOK)
							json.NewEncoder(w).Encode(foundAccount)
							return
						}
					}
				}

				// /saas-management/applications/{id}/usage
				if strings.HasPrefix(subPath, "usage") {
					json.NewEncoder(w).Encode(map[string]any{
						"results":    []map[string]any{},
						"totalCount": 0,
					})
					return
				}
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleSaaSApps() []map[string]any {
	return []map[string]any{
		{"id": "aabbccddee112233aabb9001", "catalog_app_id": "jumpcloud", "status": "APPROVED", "discovered_at": "2025-01-24T20:58:42.000Z", "access_restriction": "DEFAULT_ACTION"},
		{"id": "aabbccddee112233aabb9002", "catalog_app_id": "slack", "status": "APPROVED", "discovered_at": "2024-10-22T19:02:11.589Z", "access_restriction": "NO_ACTION"},
		{"id": "aabbccddee112233aabb9003", "catalog_app_id": "datadog", "status": "UNAPPROVED", "discovered_at": "2024-08-08T23:01:45.000Z", "access_restriction": "BLOCK"},
	}
}

func sampleSaaSAccounts() []map[string]any {
	return []map[string]any{
		{"id": "aabbccddee112233aabb9011", "email": "alice@example.com", "parent_app_id": "aabbccddee112233aabb9001", "user_id": "aabbccddee112233aabb0001"},
		{"id": "aabbccddee112233aabb9012", "email": "bob@example.com", "parent_app_id": "aabbccddee112233aabb9001", "user_id": "aabbccddee112233aabb0002"},
	}
}

func sampleSaaSLicenses() []map[string]any {
	return []map[string]any{
		{"id": "aabbccddee112233aabb9021", "contract_term": "MONTHLY", "currency": "USD", "renewal_date": "2024-08-09T00:00:00Z"},
		{"id": "aabbccddee112233aabb9022", "contract_term": "ANNUAL", "currency": "EUR", "renewal_date": "2025-01-01T00:00:00Z"},
	}
}

func overrideSaaSConfirmReader(t *testing.T, input string) {
	t.Helper()
	orig := confirmReader
	confirmReader = bufio.NewReader(strings.NewReader(input))
	t.Cleanup(func() { confirmReader = orig })
}

// --- List Tests ---

func TestSaaSList_JSON(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d SaaS apps, want 3", len(result))
	}
}

func TestSaaSList_Alias(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d SaaS apps, want 3", len(result))
	}
}

// --- Get Tests ---

func TestSaaSGet_ByID(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "get", "aabbccddee112233aabb9001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["catalog_app_id"] != "jumpcloud" {
		t.Errorf("catalog_app_id = %q, want 'jumpcloud'", result["catalog_app_id"])
	}
}

func TestSaaSGet_ByCatalogAppID(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "get", "jumpcloud"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb9001" {
		t.Errorf("id = %q, want 'aabbccddee112233aabb9001'", result["id"])
	}
}

func TestSaaSGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "get", "aabbccddee112233aabb9999"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found SaaS app, got nil")
	}
}

// --- Create Tests ---

func TestSaaSCreate(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "create", "--catalog-app-id", "notion"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["catalog_app_id"] != "notion" {
		t.Errorf("catalog_app_id = %q, want 'notion'", result["catalog_app_id"])
	}
	if result["id"] != "new123new123new123new123" {
		t.Errorf("id = %q, want 'new123new123new123new123'", result["id"])
	}
}

func TestSaaSCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "create", "--catalog-app-id", "notion", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
	if exitErr.Code != 10 {
		t.Errorf("exit code = %d, want 10", exitErr.Code)
	}
}

// --- Update Tests ---

func TestSaaSUpdate(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "update", "aabbccddee112233aabb9001", "--status", "IGNORED"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["status"] != "IGNORED" {
		t.Errorf("status = %q, want 'IGNORED'", result["status"])
	}
}

func TestSaaSUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "update", "aabbccddee112233aabb9001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for empty update, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error = %q, want 'no fields to update'", err.Error())
	}
}

// --- Delete Tests ---

func TestSaaSDelete(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "delete", "aabbccddee112233aabb9001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestSaaSDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "delete", "aabbccddee112233aabb9001", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestSaaSDelete_Cancelled(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)
	overrideSaaSConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	var buf bytes.Buffer
	errBuf := &bytes.Buffer{}
	cmd.SetOut(&buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"saas-management", "delete", "aabbccddee112233aabb9001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(errBuf.String(), "Cancelled") {
		t.Errorf("stderr should contain 'Cancelled', got: %s", errBuf.String())
	}
}

// --- Account Tests ---

func TestSaaSAccounts(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	accounts := sampleSaaSAccounts()
	ts := startSaaSServer(t, apps, accounts, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "accounts", "aabbccddee112233aabb9001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d accounts, want 2", len(result))
	}
}

func TestSaaSAccountGet(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	accounts := sampleSaaSAccounts()
	ts := startSaaSServer(t, apps, accounts, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "account-get", "aabbccddee112233aabb9001", "--account-id", "aabbccddee112233aabb9011"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["email"] != "alice@example.com" {
		t.Errorf("email = %q, want 'alice@example.com'", result["email"])
	}
}

func TestSaaSAccountDelete(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	accounts := sampleSaaSAccounts()
	ts := startSaaSServer(t, apps, accounts, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "account-delete", "aabbccddee112233aabb9001", "--account-id", "aabbccddee112233aabb9011", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

// --- Usage Tests ---

func TestSaaSUsage(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSaaSApps()
	ts := startSaaSServer(t, apps, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "usage", "aabbccddee112233aabb9001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 0 {
		t.Errorf("got %d usage items, want 0", len(result))
	}
}

// --- License Tests ---

func TestSaaSLicenses(t *testing.T) {
	setupUsersTest(t)
	licenses := sampleSaaSLicenses()
	ts := startSaaSServer(t, nil, nil, licenses)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "licenses"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d licenses, want 2", len(result))
	}
}

// --- Catalog Tests ---

func TestSaaSCatalogGet(t *testing.T) {
	setupUsersTest(t)
	ts := startSaaSServer(t, nil, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "catalog-get", "jumpcloud"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "jumpcloud" {
		t.Errorf("id = %q, want 'jumpcloud'", result["id"])
	}
}

// --- Help Tests ---

func TestSaaSManagement_Help(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"saas-management", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, sub := range []string{"list", "get", "create", "update", "delete", "accounts", "account-get", "account-delete", "usage", "licenses", "catalog-get"} {
		if !strings.Contains(out, sub) {
			t.Errorf("help output missing subcommand %q", sub)
		}
	}
}

// Note: errorAs is defined in cli_error.go and available to tests.
