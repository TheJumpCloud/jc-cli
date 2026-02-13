package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/config"
)

// overrideV1Client redirects V1 API calls to the given test server.
// Returns a cleanup function that restores the original factory.
func overrideV1Client(t *testing.T, serverURL string) {
	t.Helper()
	orig := newV1Client
	newV1Client = func() (*api.V1Client, error) {
		c := api.NewV1ClientWithKey("test-key-1234")
		c.BaseURL = serverURL
		return c, nil
	}
	t.Cleanup(func() { newV1Client = orig })
}

// startUsersServer creates a mock JumpCloud server that handles /systemusers endpoints.
// It returns canned user data for list and get requests.
func startUsersServer(t *testing.T, users []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /systemusers — list endpoint.
		if r.URL.Path == "/systemusers" {
			skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit == 0 {
				limit = 100
			}

			// Slice the users based on skip/limit.
			end := skip + limit
			if end > len(users) {
				end = len(users)
			}
			var page []map[string]any
			if skip < len(users) {
				page = users[skip:end]
			}
			if page == nil {
				page = []map[string]any{}
			}

			resp := map[string]any{
				"results":    page,
				"totalCount": len(users),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// GET /systemusers/{id} — get endpoint.
		if strings.HasPrefix(r.URL.Path, "/systemusers/") {
			id := strings.TrimPrefix(r.URL.Path, "/systemusers/")
			for _, u := range users {
				if u["_id"] == id {
					json.NewEncoder(w).Encode(u)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

// setupUsersTest sets up a clean Viper state with a test config for user command tests.
func setupUsersTest(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	viper.Reset()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_ORG_ID", "")
	t.Setenv("JC_PROFILE", "")
	t.Setenv("JC_OUTPUT", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("active_profile: default\nprofiles:\n  default:\n    api_key: test-key-1234\n"), 0600)

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}
}

// sampleUsers returns a set of test user records.
func sampleUsers() []map[string]any {
	return []map[string]any{
		{
			"_id":       "aaa111",
			"username":  "alice",
			"email":     "alice@example.com",
			"firstname": "Alice",
			"lastname":  "Smith",
			"activated": true,
			"suspended": false,
		},
		{
			"_id":       "bbb222",
			"username":  "bob",
			"email":     "bob@example.com",
			"firstname": "Bob",
			"lastname":  "Jones",
			"activated": true,
			"suspended": true,
		},
		{
			"_id":       "ccc333",
			"username":  "charlie",
			"email":     "charlie@example.com",
			"firstname": "Charlie",
			"lastname":  "Brown",
			"activated": false,
			"suspended": false,
		},
	}
}

// --- Users List Tests ---

func TestUsersList_JSON(t *testing.T) {
	setupUsersTest(t)
	users := sampleUsers()
	ts := startUsersServer(t, users)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Output should be a JSON array.
	var result []map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, out.String())
	}
	if len(result) != 3 {
		t.Errorf("got %d users, want 3", len(result))
	}
}

func TestUsersList_Table(t *testing.T) {
	setupUsersTest(t)
	users := sampleUsers()
	ts := startUsersServer(t, users)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	// Table should have headers matching default fields.
	if !strings.Contains(output, "USERNAME") {
		t.Errorf("table output missing USERNAME header:\n%s", output)
	}
	if !strings.Contains(output, "EMAIL") {
		t.Errorf("table output missing EMAIL header:\n%s", output)
	}
	if !strings.Contains(output, "alice") {
		t.Errorf("table output missing user 'alice':\n%s", output)
	}
}

func TestUsersList_TableShorthand(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list", "-t"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "USERNAME") {
		t.Errorf("-t flag did not produce table output:\n%s", out.String())
	}
}

func TestUsersList_Limit(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("got %d users, want 2 (limit)", len(result))
	}
}

func TestUsersList_Sort(t *testing.T) {
	setupUsersTest(t)

	var capturedSort string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSort = r.URL.Query().Get("sort")
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"results":    []map[string]any{},
			"totalCount": 0,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list", "--sort", "username"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedSort != "username" {
		t.Errorf("sort param = %q, want %q", capturedSort, "username")
	}
}

func TestUsersList_SortDescending(t *testing.T) {
	setupUsersTest(t)

	var capturedSort string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSort = r.URL.Query().Get("sort")
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"results":    []map[string]any{},
			"totalCount": 0,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list", "--sort", "-created"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedSort != "-created" {
		t.Errorf("sort param = %q, want %q", capturedSort, "-created")
	}
}

func TestUsersList_EmptyResult(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, []map[string]any{})
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if strings.TrimSpace(out.String()) != "[]" {
		t.Errorf("expected empty JSON array, got: %q", out.String())
	}
}

func TestUsersList_IDs(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d IDs, want 3: %v", len(lines), lines)
	}
	if lines[0] != "aaa111" {
		t.Errorf("first ID = %q, want %q", lines[0], "aaa111")
	}
}

func TestUsersList_Quiet(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list", "--quiet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if out.String() != "" {
		t.Errorf("expected empty output with --quiet, got: %q", out.String())
	}
}

func TestUsersList_CSV(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list", "--output", "csv"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 { // header + 3 rows
		t.Errorf("got %d CSV lines, want 4 (header + 3 rows)", len(lines))
	}
	// Header should contain default fields.
	if !strings.Contains(lines[0], "username") {
		t.Errorf("CSV header missing 'username': %s", lines[0])
	}
}

func TestUsersList_Footer(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"users", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errOut.String()
	if !strings.Contains(footer, "3 items") {
		t.Errorf("footer missing count info: %q", footer)
	}
}

func TestUsersList_FooterWithLimit(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"users", "list", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errOut.String()
	if !strings.Contains(footer, "2 of 3 items") {
		t.Errorf("footer should show '2 of 3 items', got: %q", footer)
	}
}

func TestUsersList_DefaultFields(t *testing.T) {
	setupUsersTest(t)

	// User with extra fields that should not appear in default table output.
	users := []map[string]any{
		{
			"_id":             "aaa111",
			"username":        "alice",
			"email":           "alice@example.com",
			"firstname":       "Alice",
			"lastname":        "Smith",
			"activated":       true,
			"suspended":       false,
			"password_date":   "2026-01-01",
			"totp_enabled":    true,
			"account_locked":  false,
		},
	}
	ts := startUsersServer(t, users)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	tableOut := out.String()
	if !strings.Contains(tableOut, "USERNAME") {
		t.Errorf("table missing USERNAME column")
	}
	// password_date and totp_enabled are not in default fields — they should not appear as headers.
	if strings.Contains(tableOut, "PASSWORD_DATE") {
		t.Errorf("table should not show PASSWORD_DATE in default fields")
	}
	if strings.Contains(tableOut, "TOTP_ENABLED") {
		t.Errorf("table should not show TOTP_ENABLED in default fields")
	}
}

func TestUsersList_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"results":    []map[string]any{},
			"totalCount": 0,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systemusers" {
		t.Errorf("API path = %q, want %q", capturedPath, "/systemusers")
	}
}

// --- Users Get Tests ---

func TestUsersGet_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "get", "aaa111"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var user map[string]any
	if err := json.Unmarshal(out.Bytes(), &user); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, out.String())
	}
	if user["username"] != "alice" {
		t.Errorf("username = %v, want alice", user["username"])
	}
}

func TestUsersGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "get", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}

	if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "Not Found") {
		t.Errorf("error should mention 404 or Not Found, got: %v", err)
	}
}

func TestUsersGet_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "get"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument, got nil")
	}
}

func TestUsersGet_TableOutput(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "get", "bbb222", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "bob") {
		t.Errorf("table output should contain 'bob':\n%s", out.String())
	}
}

func TestUsersGet_HumanOutput(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "get", "aaa111", "--output", "human"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Human output shows key: value pairs.
	if !strings.Contains(out.String(), "username:") {
		t.Errorf("human output missing 'username:' label:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "alice") {
		t.Errorf("human output missing value 'alice':\n%s", out.String())
	}
}

func TestUsersGet_IDs(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "get", "aaa111", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if strings.TrimSpace(out.String()) != "aaa111" {
		t.Errorf("--ids output = %q, want %q", strings.TrimSpace(out.String()), "aaa111")
	}
}

func TestUsersGet_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_id":"abc123","username":"test"}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "get", "abc123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systemusers/abc123" {
		t.Errorf("API path = %q, want %q", capturedPath, "/systemusers/abc123")
	}
}

// --- Help Output Tests ---

func TestUsersCmd_Help(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "list") {
		t.Errorf("users help should mention 'list' subcommand:\n%s", help)
	}
	if !strings.Contains(help, "get") {
		t.Errorf("users help should mention 'get' subcommand:\n%s", help)
	}
}

func TestUsersListCmd_Help(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "--limit") {
		t.Errorf("list help should mention --limit flag:\n%s", help)
	}
	if !strings.Contains(help, "--sort") {
		t.Errorf("list help should mention --sort flag:\n%s", help)
	}
}

// --- Auth Error Tests ---

func TestUsersList_AuthError(t *testing.T) {
	setupUsersTest(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
}

// --- Pagination Tests ---

func TestUsersList_Pagination(t *testing.T) {
	setupUsersTest(t)

	// Create 15 users, serve in pages of 5.
	users := make([]map[string]any, 15)
	for i := range users {
		users[i] = map[string]any{
			"_id":       fmt.Sprintf("id-%02d", i),
			"username":  fmt.Sprintf("user-%02d", i),
			"email":     fmt.Sprintf("user%02d@example.com", i),
			"firstname": fmt.Sprintf("First%02d", i),
			"lastname":  fmt.Sprintf("Last%02d", i),
			"activated": true,
			"suspended": false,
		}
	}

	ts := startUsersServer(t, users)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 15 {
		t.Errorf("got %d users, want 15", len(result))
	}
}

// Verify that the users command appears in root help.
func TestRootHelp_IncludesUsers(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "users") {
		t.Errorf("root help should list 'users' command:\n%s", out.String())
	}
}
