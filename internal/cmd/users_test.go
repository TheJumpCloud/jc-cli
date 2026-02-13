package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// overrideConfirmReader injects a bufio.Reader for confirmation prompts.
func overrideConfirmReader(t *testing.T, input string) {
	t.Helper()
	orig := confirmReader
	confirmReader = bufio.NewReader(strings.NewReader(input))
	t.Cleanup(func() { confirmReader = orig })
}

// startUsersServer creates a mock JumpCloud server that handles /systemusers endpoints.
// It returns canned user data for list, get, create, update, and delete requests.
func startUsersServer(t *testing.T, users []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// POST /search/systemusers — search endpoint.
		if r.URL.Path == "/search/systemusers" && r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var searchReq map[string]any
			json.Unmarshal(body, &searchReq)

			skip := 0
			limit := 100
			if v, ok := searchReq["skip"].(float64); ok {
				skip = int(v)
			}
			if v, ok := searchReq["limit"].(float64); ok {
				limit = int(v)
			}

			// Extract search term and filter users by matching across fields.
			var matched []map[string]any
			term := ""
			if sf, ok := searchReq["searchFilter"].(map[string]any); ok {
				if st, ok := sf["searchTerm"].(string); ok {
					term = strings.ToLower(st)
				}
			}
			for _, u := range users {
				if term == "" {
					matched = append(matched, u)
					continue
				}
				// Case-insensitive search across username, email, firstname, lastname.
				for _, field := range []string{"username", "email", "firstname", "lastname"} {
					if v, ok := u[field].(string); ok && strings.Contains(strings.ToLower(v), term) {
						matched = append(matched, u)
						break
					}
				}
			}

			total := len(matched)
			end := skip + limit
			if end > total {
				end = total
			}
			var page []map[string]any
			if skip < total {
				page = matched[skip:end]
			}
			if page == nil {
				page = []map[string]any{}
			}

			resp := map[string]any{
				"results":    page,
				"totalCount": total,
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// GET /systemusers — list endpoint.
		if r.URL.Path == "/systemusers" && r.Method == http.MethodGet {
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

		// POST /systemusers — create endpoint.
		if r.URL.Path == "/systemusers" && r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var input map[string]any
			json.Unmarshal(body, &input)

			// Simulate API response — echo back fields with a generated _id.
			input["_id"] = "new123new123new123new123"
			input["activated"] = false
			input["suspended"] = false
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /systemusers/{id}.
		if strings.HasPrefix(r.URL.Path, "/systemusers/") {
			rest := strings.TrimPrefix(r.URL.Path, "/systemusers/")

			// Check for sub-resource endpoints: /{id}/resetmfa, /{id}/expire
			if parts := strings.SplitN(rest, "/", 2); len(parts) == 2 {
				id := parts[0]
				action := parts[1]

				// Find the user first.
				var found bool
				for _, u := range users {
					if u["_id"] == id {
						found = true
						break
					}
				}
				if !found {
					w.WriteHeader(http.StatusNotFound)
					w.Write([]byte(`{"message":"Not Found"}`))
					return
				}

				if r.Method == http.MethodPost {
					switch action {
					case "resetmfa":
						w.WriteHeader(http.StatusOK)
						w.Write([]byte(`{}`))
						return
					case "expire":
						w.WriteHeader(http.StatusOK)
						w.Write([]byte(`{}`))
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
				return
			}

			id := rest

			switch r.Method {
			case http.MethodGet:
				for _, u := range users {
					if u["_id"] == id {
						json.NewEncoder(w).Encode(u)
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return

			case http.MethodPut:
				// Find existing user and merge updates.
				for _, u := range users {
					if u["_id"] == id {
						body, _ := io.ReadAll(r.Body)
						var updates map[string]any
						json.Unmarshal(body, &updates)
						// Merge updates into existing user.
						merged := make(map[string]any)
						for k, v := range u {
							merged[k] = v
						}
						for k, v := range updates {
							merged[k] = v
						}
						json.NewEncoder(w).Encode(merged)
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return

			case http.MethodDelete:
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
			"_id":       "aaa111aaa111aaa111aaa111",
			"username":  "alice",
			"email":     "alice@example.com",
			"firstname": "Alice",
			"lastname":  "Smith",
			"activated": true,
			"suspended": false,
		},
		{
			"_id":       "bbb222bbb222bbb222bbb222",
			"username":  "bob",
			"email":     "bob@example.com",
			"firstname": "Bob",
			"lastname":  "Jones",
			"activated": true,
			"suspended": true,
		},
		{
			"_id":       "ccc333ccc333ccc333ccc333",
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
	if lines[0] != "aaa111aaa111aaa111aaa111" {
		t.Errorf("first ID = %q, want %q", lines[0], "aaa111aaa111aaa111aaa111")
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
			"_id":            "aaa111aaa111aaa111aaa111",
			"username":       "alice",
			"email":          "alice@example.com",
			"firstname":      "Alice",
			"lastname":       "Smith",
			"activated":      true,
			"suspended":      false,
			"password_date":  "2026-01-01",
			"totp_enabled":   true,
			"account_locked": false,
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
	cmd.SetArgs([]string{"users", "get", "aaa111aaa111aaa111aaa111"})

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

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
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
	cmd.SetArgs([]string{"users", "get", "bbb222bbb222bbb222bbb222", "--output", "table"})

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
	cmd.SetArgs([]string{"users", "get", "aaa111aaa111aaa111aaa111", "--output", "human"})

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
	cmd.SetArgs([]string{"users", "get", "aaa111aaa111aaa111aaa111", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if strings.TrimSpace(out.String()) != "aaa111aaa111aaa111aaa111" {
		t.Errorf("--ids output = %q, want %q", strings.TrimSpace(out.String()), "aaa111aaa111aaa111aaa111")
	}
}

func TestUsersGet_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_id":"abc123abc123abc123abc123","username":"test"}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "get", "abc123abc123abc123abc123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systemusers/abc123abc123abc123abc123" {
		t.Errorf("API path = %q, want %q", capturedPath, "/systemusers/abc123abc123abc123abc123")
	}
}

// --- Users Create Tests ---

func TestUsersCreate_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "create", "--username", "jdoe", "--email", "jdoe@acme.com", "--firstname", "John", "--lastname", "Doe"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, out.String())
	}
	if result["username"] != "jdoe" {
		t.Errorf("username = %v, want jdoe", result["username"])
	}
	if result["email"] != "jdoe@acme.com" {
		t.Errorf("email = %v, want jdoe@acme.com", result["email"])
	}
	if result["firstname"] != "John" {
		t.Errorf("firstname = %v, want John", result["firstname"])
	}
	if result["lastname"] != "Doe" {
		t.Errorf("lastname = %v, want Doe", result["lastname"])
	}
	if result["_id"] != "new123new123new123new123" {
		t.Errorf("_id = %v, want new-user-id-123", result["_id"])
	}
}

func TestUsersCreate_RequiredFieldsOnly(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "create", "--username", "minimal", "--email", "min@acme.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, out.String())
	}
	if result["username"] != "minimal" {
		t.Errorf("username = %v, want minimal", result["username"])
	}
	// Optional fields should not be in request body (server echo shouldn't include them).
	if _, ok := result["firstname"]; ok {
		t.Errorf("firstname should not be set when not provided")
	}
}

func TestUsersCreate_MissingUsername(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "create", "--email", "jdoe@acme.com"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --username, got nil")
	}
	if !strings.Contains(err.Error(), "username") {
		t.Errorf("error should mention 'username': %v", err)
	}
}

func TestUsersCreate_MissingEmail(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "create", "--username", "jdoe"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --email, got nil")
	}
	if !strings.Contains(err.Error(), "email") {
		t.Errorf("error should mention 'email': %v", err)
	}
}

func TestUsersCreate_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	var capturedMethod string
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_id":"new123new123new123new123","username":"test","email":"test@example.com"}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "create", "--username", "test", "--email", "test@example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systemusers" {
		t.Errorf("API path = %q, want %q", capturedPath, "/systemusers")
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("HTTP method = %q, want POST", capturedMethod)
	}
	if capturedBody["username"] != "test" {
		t.Errorf("request body username = %v, want test", capturedBody["username"])
	}
	if capturedBody["email"] != "test@example.com" {
		t.Errorf("request body email = %v, want test@example.com", capturedBody["email"])
	}
}

func TestUsersCreate_APIError(t *testing.T) {
	setupUsersTest(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"message":"User already exists"}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "create", "--username", "existing", "--email", "existing@acme.com"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for conflict, got nil")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Errorf("error should mention 409, got: %v", err)
	}
}

// --- Users Update Tests ---

func TestUsersUpdate_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "update", "aaa111aaa111aaa111aaa111", "--department", "Sales", "--jobTitle", "Manager"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, out.String())
	}
	// Existing fields should be preserved.
	if result["username"] != "alice" {
		t.Errorf("username = %v, want alice (should be preserved)", result["username"])
	}
	// Updated fields should have new values.
	if result["department"] != "Sales" {
		t.Errorf("department = %v, want Sales", result["department"])
	}
	if result["jobTitle"] != "Manager" {
		t.Errorf("jobTitle = %v, want Manager", result["jobTitle"])
	}
}

func TestUsersUpdate_SingleField(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "update", "bbb222bbb222bbb222bbb222", "--email", "bob.new@example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, out.String())
	}
	if result["email"] != "bob.new@example.com" {
		t.Errorf("email = %v, want bob.new@example.com", result["email"])
	}
}

func TestUsersUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "update", "aaa111aaa111aaa111aaa111"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no fields specified, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should say 'no fields to update', got: %v", err)
	}
}

func TestUsersUpdate_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "update"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing user ID, got nil")
	}
}

func TestUsersUpdate_NotFound(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "update", "nonexistent", "--department", "Sales"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestUsersUpdate_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	var capturedMethod string
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_id":"abc123abc123abc123abc123","username":"test","department":"Engineering"}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "update", "abc123abc123abc123abc123", "--department", "Engineering"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systemusers/abc123abc123abc123abc123" {
		t.Errorf("API path = %q, want %q", capturedPath, "/systemusers/abc123abc123abc123abc123")
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("HTTP method = %q, want PUT", capturedMethod)
	}
	if capturedBody["department"] != "Engineering" {
		t.Errorf("request body department = %v, want Engineering", capturedBody["department"])
	}
}

func TestUsersUpdate_OnlySendsChangedFields(t *testing.T) {
	setupUsersTest(t)

	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_id":"abc123abc123abc123abc123","username":"test","department":"Sales"}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "update", "abc123abc123abc123abc123", "--department", "Sales"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Only the changed field should be in the body.
	if len(capturedBody) != 1 {
		t.Errorf("body should have 1 field, got %d: %v", len(capturedBody), capturedBody)
	}
	if capturedBody["department"] != "Sales" {
		t.Errorf("body department = %v, want Sales", capturedBody["department"])
	}
}

// --- Users Delete Tests ---

func TestUsersDelete_WithForce(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "delete", "aaa111aaa111aaa111aaa111", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "alice deleted successfully") {
		t.Errorf("output should confirm deletion: %q", out.String())
	}
}

func TestUsersDelete_WithConfirmYes(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideConfirmReader(t, "y\n")

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"users", "delete", "bbb222bbb222bbb222bbb222"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should show the confirmation prompt.
	if !strings.Contains(errOut.String(), "Delete user bob") {
		t.Errorf("stderr should show confirmation prompt, got: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "bob@example.com") {
		t.Errorf("confirmation should show email, got: %q", errOut.String())
	}
	// Should confirm deletion.
	if !strings.Contains(out.String(), "bob deleted successfully") {
		t.Errorf("output should confirm deletion: %q", out.String())
	}
}

func TestUsersDelete_WithConfirmNo(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"users", "delete", "aaa111aaa111aaa111aaa111"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should be cancelled, no deletion message.
	if strings.Contains(out.String(), "deleted") {
		t.Errorf("should not have deleted, got: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "Cancelled") {
		t.Errorf("stderr should show 'Cancelled', got: %q", errOut.String())
	}
}

func TestUsersDelete_NotFound(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "delete", "nonexistent", "--force"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestUsersDelete_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "delete"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument, got nil")
	}
}

func TestUsersDelete_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedDeletePath string
	var capturedDeleteMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/systemusers/abc123abc123abc123abc123" {
			w.Write([]byte(`{"_id":"abc123abc123abc123abc123","username":"testuser","email":"test@example.com"}`))
			return
		}
		if r.Method == http.MethodDelete {
			capturedDeletePath = r.URL.Path
			capturedDeleteMethod = r.Method
			w.Write([]byte(`{"_id":"abc123abc123abc123abc123","username":"testuser"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "delete", "abc123abc123abc123abc123", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedDeletePath != "/systemusers/abc123abc123abc123abc123" {
		t.Errorf("DELETE path = %q, want %q", capturedDeletePath, "/systemusers/abc123abc123abc123abc123")
	}
	if capturedDeleteMethod != http.MethodDelete {
		t.Errorf("HTTP method = %q, want DELETE", capturedDeleteMethod)
	}
}

func TestUsersDelete_ConfirmEmptyInput(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideConfirmReader(t, "\n") // Just hitting enter

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"users", "delete", "aaa111aaa111aaa111aaa111"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Default is N — should be cancelled.
	if strings.Contains(out.String(), "deleted") {
		t.Errorf("empty input should cancel delete, got: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "Cancelled") {
		t.Errorf("stderr should show 'Cancelled', got: %q", errOut.String())
	}
}

// --- Users Search Tests ---

func TestUsersSearch_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "search", "alice"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, out.String())
	}
	if len(result) != 1 {
		t.Errorf("got %d results, want 1 (only alice matches)", len(result))
	}
	if len(result) > 0 && result[0]["username"] != "alice" {
		t.Errorf("username = %v, want alice", result[0]["username"])
	}
}

func TestUsersSearch_MultipleMatches(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	// "example.com" appears in all users' emails.
	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "search", "example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("got %d results, want 3 (all users match 'example.com')", len(result))
	}
}

func TestUsersSearch_NoResults(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "search", "zzznomatch"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if strings.TrimSpace(out.String()) != "[]" {
		t.Errorf("expected empty JSON array, got: %q", out.String())
	}
}

func TestUsersSearch_CaseInsensitive(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "search", "ALICE"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("got %d results, want 1 (case-insensitive match for ALICE)", len(result))
	}
}

func TestUsersSearch_WithLimit(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "search", "example.com", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("got %d results, want 1 (limit=1)", len(result))
	}
}

func TestUsersSearch_TableOutput(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "search", "bob", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "USERNAME") {
		t.Errorf("table output missing USERNAME header:\n%s", output)
	}
	if !strings.Contains(output, "bob") {
		t.Errorf("table output missing user 'bob':\n%s", output)
	}
}

func TestUsersSearch_IDsOutput(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "search", "bob", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Errorf("got %d IDs, want 1: %v", len(lines), lines)
	}
	if lines[0] != "bbb222bbb222bbb222bbb222" {
		t.Errorf("ID = %q, want %q", lines[0], "bbb222bbb222bbb222bbb222")
	}
}

func TestUsersSearch_Footer(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"users", "search", "example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errOut.String()
	if !strings.Contains(footer, "3 items") {
		t.Errorf("footer missing count info: %q", footer)
	}
}

func TestUsersSearch_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "search"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing search term, got nil")
	}
}

func TestUsersSearch_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	var capturedMethod string
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
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
	cmd.SetArgs([]string{"users", "search", "findme"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/search/systemusers" {
		t.Errorf("API path = %q, want %q", capturedPath, "/search/systemusers")
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("HTTP method = %q, want POST", capturedMethod)
	}

	// Verify search filter is in the request body.
	sf, ok := capturedBody["searchFilter"].(map[string]any)
	if !ok {
		t.Fatalf("searchFilter missing from request body: %v", capturedBody)
	}
	if sf["searchTerm"] != "findme" {
		t.Errorf("searchTerm = %v, want findme", sf["searchTerm"])
	}
	fields, ok := sf["fields"].([]any)
	if !ok || len(fields) == 0 {
		t.Fatalf("fields missing from searchFilter: %v", sf)
	}
	expectedFields := map[string]bool{"username": true, "email": true, "firstname": true, "lastname": true}
	for _, f := range fields {
		if !expectedFields[f.(string)] {
			t.Errorf("unexpected search field: %v", f)
		}
	}
}

func TestUsersSearch_APIError(t *testing.T) {
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
	cmd.SetArgs([]string{"users", "search", "test"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
}

func TestUsersSearch_FooterWithLimit(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"users", "search", "example.com", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errOut.String()
	if !strings.Contains(footer, "2 of 3 items") {
		t.Errorf("footer should show '2 of 3 items', got: %q", footer)
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
	if !strings.Contains(help, "search") {
		t.Errorf("users help should mention 'search' subcommand:\n%s", help)
	}
	if !strings.Contains(help, "create") {
		t.Errorf("users help should mention 'create' subcommand:\n%s", help)
	}
	if !strings.Contains(help, "update") {
		t.Errorf("users help should mention 'update' subcommand:\n%s", help)
	}
	if !strings.Contains(help, "delete") {
		t.Errorf("users help should mention 'delete' subcommand:\n%s", help)
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

func TestUsersSearchCmd_Help(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "search", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "--limit") {
		t.Errorf("search help should mention --limit flag:\n%s", help)
	}
	if !strings.Contains(help, "search") {
		t.Errorf("search help should mention 'search':\n%s", help)
	}
}

func TestUsersCreateCmd_Help(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "create", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "--username") {
		t.Errorf("create help should mention --username flag:\n%s", help)
	}
	if !strings.Contains(help, "--email") {
		t.Errorf("create help should mention --email flag:\n%s", help)
	}
}

func TestUsersUpdateCmd_Help(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "update", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "--department") {
		t.Errorf("update help should mention --department flag:\n%s", help)
	}
	if !strings.Contains(help, "--jobTitle") {
		t.Errorf("update help should mention --jobTitle flag:\n%s", help)
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
			"_id":       fmt.Sprintf("aaaaaaaaaaaaaaaaaa00%04x", i),
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

// --- Users Lock Tests ---

func TestUsersLock_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"users", "lock", "aaa111aaa111aaa111aaa111"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "alice locked successfully") {
		t.Errorf("output should confirm lock: %q", out.String())
	}
}

func TestUsersLock_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	var capturedMethod string
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"_id":"abc123abc123abc123abc123","username":"testuser","email":"test@example.com","account_locked":false}`))
			return
		}
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Write([]byte(`{"_id":"abc123abc123abc123abc123","username":"testuser","account_locked":true}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "lock", "abc123abc123abc123abc123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systemusers/abc123abc123abc123abc123" {
		t.Errorf("API path = %q, want %q", capturedPath, "/systemusers/abc123abc123abc123abc123")
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("HTTP method = %q, want PUT", capturedMethod)
	}
	if capturedBody["account_locked"] != true {
		t.Errorf("body account_locked = %v, want true", capturedBody["account_locked"])
	}
}

func TestUsersLock_NotFound(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "lock", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
}

func TestUsersLock_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "lock"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument, got nil")
	}
}

// --- Users Unlock Tests ---

func TestUsersUnlock_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "unlock", "bbb222bbb222bbb222bbb222"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "bob unlocked successfully") {
		t.Errorf("output should confirm unlock: %q", out.String())
	}
}

func TestUsersUnlock_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	var capturedMethod string
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"_id":"abc123abc123abc123abc123","username":"testuser","email":"test@example.com","account_locked":true}`))
			return
		}
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Write([]byte(`{"_id":"abc123abc123abc123abc123","username":"testuser","account_locked":false}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "unlock", "abc123abc123abc123abc123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systemusers/abc123abc123abc123abc123" {
		t.Errorf("API path = %q, want %q", capturedPath, "/systemusers/abc123abc123abc123abc123")
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("HTTP method = %q, want PUT", capturedMethod)
	}
	if capturedBody["account_locked"] != false {
		t.Errorf("body account_locked = %v, want false", capturedBody["account_locked"])
	}
}

func TestUsersUnlock_NotFound(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "unlock", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
}

func TestUsersUnlock_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "unlock"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument, got nil")
	}
}

// --- Users Reset-MFA Tests ---

func TestUsersResetMFA_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "reset-mfa", "aaa111aaa111aaa111aaa111"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "alice") {
		t.Errorf("output should mention username 'alice': %q", output)
	}
	if !strings.Contains(output, "MFA reset successfully") {
		t.Errorf("output should confirm MFA reset: %q", output)
	}
}

func TestUsersResetMFA_ShowsReEnrollmentWarning(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"users", "reset-mfa", "aaa111aaa111aaa111aaa111"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Warning about re-enrollment should appear on stderr.
	combined := out.String() + errOut.String()
	if !strings.Contains(strings.ToLower(combined), "re-enroll") {
		t.Errorf("should warn about MFA re-enrollment, got stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestUsersResetMFA_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	var capturedMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"_id":"abc123abc123abc123abc123","username":"testuser","email":"test@example.com"}`))
			return
		}
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "reset-mfa", "abc123abc123abc123abc123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systemusers/abc123abc123abc123abc123/resetmfa" {
		t.Errorf("API path = %q, want %q", capturedPath, "/systemusers/abc123abc123abc123abc123/resetmfa")
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("HTTP method = %q, want POST", capturedMethod)
	}
}

func TestUsersResetMFA_NotFound(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "reset-mfa", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
}

func TestUsersResetMFA_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "reset-mfa"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument, got nil")
	}
}

// --- Users Reset-Password Tests ---

func TestUsersResetPassword_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "reset-password", "bbb222bbb222bbb222bbb222"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "bob") {
		t.Errorf("output should mention username 'bob': %q", output)
	}
	if !strings.Contains(output, "password reset") {
		t.Errorf("output should confirm password reset: %q", output)
	}
}

func TestUsersResetPassword_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	var capturedMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"_id":"abc123abc123abc123abc123","username":"testuser","email":"test@example.com"}`))
			return
		}
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "reset-password", "abc123abc123abc123abc123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systemusers/abc123abc123abc123abc123/expire" {
		t.Errorf("API path = %q, want %q", capturedPath, "/systemusers/abc123abc123abc123abc123/expire")
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("HTTP method = %q, want POST", capturedMethod)
	}
}

func TestUsersResetPassword_NotFound(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "reset-password", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
}

func TestUsersResetPassword_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "reset-password"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument, got nil")
	}
}

// --- Users Help Tests for New Commands ---

func TestUsersCmd_Help_IncludesNewCommands(t *testing.T) {
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
	for _, sub := range []string{"lock", "unlock", "reset-mfa", "reset-password"} {
		if !strings.Contains(help, sub) {
			t.Errorf("users help should mention '%s' subcommand:\n%s", sub, help)
		}
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
