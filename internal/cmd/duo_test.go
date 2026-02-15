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

// startDuoServer creates a mock JumpCloud V2 server that handles /duo/accounts and
// /duo/accounts/{id}/applications endpoints.
func startDuoServer(t *testing.T, accounts []map[string]any, apps []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /duo/accounts — list accounts.
		if r.URL.Path == "/duo/accounts" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(accounts)
			return
		}

		// POST /duo/accounts — create account.
		if r.URL.Path == "/duo/accounts" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "new123new123new123new123"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /duo/accounts/{id}...
		if strings.HasPrefix(r.URL.Path, "/duo/accounts/") {
			rest := strings.TrimPrefix(r.URL.Path, "/duo/accounts/")
			parts := strings.SplitN(rest, "/", 2)
			accountID := parts[0]

			// Find the account.
			var foundAccount map[string]any
			for _, a := range accounts {
				if a["id"] == accountID {
					foundAccount = a
					break
				}
			}

			// No sub-path: /duo/accounts/{id}
			if len(parts) == 1 {
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

			// Sub-path: /duo/accounts/{id}/applications...
			if len(parts) == 2 && strings.HasPrefix(parts[1], "applications") {
				appRest := strings.TrimPrefix(parts[1], "applications")

				// /duo/accounts/{id}/applications — list or create apps.
				if appRest == "" {
					if foundAccount == nil {
						w.WriteHeader(http.StatusNotFound)
						w.Write([]byte(`{"message":"Not Found"}`))
						return
					}

					if r.Method == http.MethodGet {
						// Filter apps belonging to this account.
						var accountApps []map[string]any
						for _, app := range apps {
							if app["accountId"] == accountID {
								accountApps = append(accountApps, app)
							}
						}
						if accountApps == nil {
							accountApps = []map[string]any{}
						}
						json.NewEncoder(w).Encode(accountApps)
						return
					}

					if r.Method == http.MethodPost {
						var input map[string]any
						json.NewDecoder(r.Body).Decode(&input)
						input["id"] = "new456new456new456new456"
						input["accountId"] = accountID
						w.WriteHeader(http.StatusCreated)
						json.NewEncoder(w).Encode(input)
						return
					}
				}

				// /duo/accounts/{id}/applications/{appId} — get or delete a specific app.
				if strings.HasPrefix(appRest, "/") {
					appID := strings.TrimPrefix(appRest, "/")

					var foundApp map[string]any
					for _, app := range apps {
						if app["id"] == appID && app["accountId"] == accountID {
							foundApp = app
							break
						}
					}

					if foundApp == nil {
						w.WriteHeader(http.StatusNotFound)
						w.Write([]byte(`{"message":"Not Found"}`))
						return
					}

					switch r.Method {
					case http.MethodGet:
						json.NewEncoder(w).Encode(foundApp)
						return
					case http.MethodDelete:
						w.WriteHeader(http.StatusOK)
						json.NewEncoder(w).Encode(foundApp)
						return
					}
				}
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleDuoAccounts() []map[string]any {
	return []map[string]any{
		{"id": "aabbccddee112233aabb8001", "name": "Duo Primary"},
		{"id": "aabbccddee112233aabb8002", "name": "Duo Secondary"},
	}
}

func sampleDuoApps() []map[string]any {
	return []map[string]any{
		{"id": "aabbccddee112233aabb8011", "name": "Admin Panel", "apiHost": "api-abc123.duosecurity.com", "accountId": "aabbccddee112233aabb8001"},
		{"id": "aabbccddee112233aabb8012", "name": "VPN Access", "apiHost": "api-def456.duosecurity.com", "accountId": "aabbccddee112233aabb8001"},
	}
}

// overrideDuoConfirmReader injects a bufio.Reader for Duo confirmation prompts.
func overrideDuoConfirmReader(t *testing.T, input string) {
	t.Helper()
	orig := confirmReader
	confirmReader = bufio.NewReader(strings.NewReader(input))
	t.Cleanup(func() { confirmReader = orig })
}

// --- List Tests ---

func TestDuoList_JSON(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	ts := startDuoServer(t, accounts, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d Duo accounts, want 2", len(result))
	}
}

// --- Get Tests ---

func TestDuoGet(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	ts := startDuoServer(t, accounts, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "get", "aabbccddee112233aabb8001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "Duo Primary" {
		t.Errorf("name = %q, want 'Duo Primary'", result["name"])
	}
}

func TestDuoGet_ByName(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	ts := startDuoServer(t, accounts, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "get", "Duo Primary"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb8001" {
		t.Errorf("id = %q, want 'aabbccddee112233aabb8001'", result["id"])
	}
}

func TestDuoGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	ts := startDuoServer(t, accounts, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "get", "aabbccddee112233aabb9999"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found Duo account, got nil")
	}
}

// --- Create Tests ---

func TestDuoCreate(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	ts := startDuoServer(t, accounts, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "create", "--name", "New Duo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "New Duo" {
		t.Errorf("name = %q, want 'New Duo'", result["name"])
	}
	if result["id"] != "new123new123new123new123" {
		t.Errorf("id = %q, want 'new123new123new123new123'", result["id"])
	}
}

func TestDuoCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	ts := startDuoServer(t, accounts, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "create", "--name", "New Duo", "--plan"})

	err := cmd.Execute()
	// Plan mode returns ExitError with plan exit code.
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

// --- Delete Tests ---

func TestDuoDelete(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	ts := startDuoServer(t, accounts, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "delete", "aabbccddee112233aabb8001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestDuoDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	ts := startDuoServer(t, accounts, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "delete", "aabbccddee112233aabb8001", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

// --- Application Tests ---

func TestDuoApps(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	apps := sampleDuoApps()
	ts := startDuoServer(t, accounts, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "apps", "aabbccddee112233aabb8001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d Duo apps, want 2", len(result))
	}
}

func TestDuoAppGet(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	apps := sampleDuoApps()
	ts := startDuoServer(t, accounts, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "app-get", "aabbccddee112233aabb8001", "--app-id", "aabbccddee112233aabb8011"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "Admin Panel" {
		t.Errorf("name = %q, want 'Admin Panel'", result["name"])
	}
	if result["apiHost"] != "api-abc123.duosecurity.com" {
		t.Errorf("apiHost = %q, want 'api-abc123.duosecurity.com'", result["apiHost"])
	}
}

func TestDuoAppCreate(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	apps := sampleDuoApps()
	ts := startDuoServer(t, accounts, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "app-create", "aabbccddee112233aabb8001", "--name", "New App", "--api-host", "api-new.duosecurity.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "New App" {
		t.Errorf("name = %q, want 'New App'", result["name"])
	}
	if result["apiHost"] != "api-new.duosecurity.com" {
		t.Errorf("apiHost = %q, want 'api-new.duosecurity.com'", result["apiHost"])
	}
	if result["id"] != "new456new456new456new456" {
		t.Errorf("id = %q, want 'new456new456new456new456'", result["id"])
	}
}

func TestDuoAppDelete(t *testing.T) {
	setupUsersTest(t)
	accounts := sampleDuoAccounts()
	apps := sampleDuoApps()
	ts := startDuoServer(t, accounts, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"duo", "app-delete", "aabbccddee112233aabb8001", "--app-id", "aabbccddee112233aabb8011", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}
