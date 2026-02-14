package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/spf13/viper"
)

// startAdminsServer creates a mock JumpCloud V1 server that handles /users endpoint.
// V1 responses are wrapped: {"results": [...], "totalCount": N}.
func startAdminsServer(t *testing.T, admins []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /users — list endpoint (V1-style).
		if r.URL.Path == "/users" && r.Method == http.MethodGet {
			skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit == 0 {
				limit = 100
			}
			end := skip + limit
			if end > len(admins) {
				end = len(admins)
			}
			var page []map[string]any
			if skip < len(admins) {
				page = admins[skip:end]
			}
			if page == nil {
				page = []map[string]any{}
			}
			json.NewEncoder(w).Encode(map[string]any{
				"results":    page,
				"totalCount": len(admins),
			})
			return
		}

		// POST /users — create admin.
		if r.URL.Path == "/users" && r.Method == http.MethodPost {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			body["_id"] = "newadminnewadminnewadm01"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(body)
			return
		}

		// Routes under /users/{id}.
		if strings.HasPrefix(r.URL.Path, "/users/") {
			id := strings.TrimPrefix(r.URL.Path, "/users/")
			switch r.Method {
			case http.MethodGet:
				for _, a := range admins {
					if a["_id"] == id {
						json.NewEncoder(w).Encode(a)
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			case http.MethodPut:
				for _, a := range admins {
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
			case http.MethodDelete:
				for _, a := range admins {
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

func sampleAdmins() []map[string]any {
	return []map[string]any{
		{
			"_id":               "aabbccddee112233aabb2001",
			"email":             "admin@acme.com",
			"roleName":          "Administrator",
			"enableMultiFactor": true,
			"firstname":         "Alice",
			"lastname":          "Admin",
		},
		{
			"_id":               "aabbccddee112233aabb2002",
			"email":             "manager@acme.com",
			"roleName":          "Manager",
			"enableMultiFactor": false,
			"firstname":         "Bob",
			"lastname":          "Manager",
		},
		{
			"_id":               "aabbccddee112233aabb2003",
			"email":             "readonly@acme.com",
			"roleName":          "Read Only",
			"enableMultiFactor": true,
			"firstname":         "Carol",
			"lastname":          "Reader",
		},
	}
}

// --- List Tests ---

func TestAdminsList_JSON(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d admins, want 3", len(result))
	}
}

func TestAdminsList_DefaultFields(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	// Default fields should include _id, email, roleName, enableMultiFactor.
	first := result[0]
	if _, ok := first["email"]; !ok {
		t.Error("default fields should include 'email'")
	}
	if _, ok := first["roleName"]; !ok {
		t.Error("default fields should include 'roleName'")
	}
	if _, ok := first["enableMultiFactor"]; !ok {
		t.Error("default fields should include 'enableMultiFactor'")
	}
	if _, ok := first["_id"]; !ok {
		t.Error("default fields should include '_id'")
	}
}

func TestAdminsList_Table(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "admin@acme.com") {
		t.Errorf("table output should contain 'admin@acme.com', got:\n%s", out)
	}
	if !strings.Contains(out, "Administrator") {
		t.Errorf("table output should contain 'Administrator', got:\n%s", out)
	}
}

func TestAdminsList_CSV(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list", "--output", "csv"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "admin@acme.com") {
		t.Errorf("CSV output should contain 'admin@acme.com', got:\n%s", out)
	}
}

func TestAdminsList_IDs(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list", "--ids"})

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

func TestAdminsList_Quiet(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list", "--quiet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("quiet output should be empty, got: %q", buf.String())
	}
}

func TestAdminsList_Footer(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"admins", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errBuf.String()
	if !strings.Contains(footer, "3 of 3 items") {
		t.Errorf("footer should contain '3 of 3 items', got: %q", footer)
	}
}

func TestAdminsList_Empty(t *testing.T) {
	setupUsersTest(t)
	ts := startAdminsServer(t, []map[string]any{})
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 0 {
		t.Errorf("got %d admins, want 0", len(result))
	}
}

func TestAdminsList_Filter(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list", "--filter", "roleName=Administrator"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Verify the filter query parameter was sent (the mock returns all items
	// regardless, so we just verify the command didn't error out).
	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
}

func TestAdminsList_Sort(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list", "--sort", "-email"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
}

func TestAdminsList_InvalidFilter(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list", "--filter", "badfilter"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid filter, got nil")
	}
}

func TestAdminsList_Limit(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) > 2 {
		t.Errorf("got %d admins, want at most 2", len(result))
	}
}

func TestAdminsList_RoleInOutput(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	// Verify admin roles are present in the output.
	roles := map[string]bool{}
	for _, a := range result {
		if r, ok := a["roleName"].(string); ok {
			roles[r] = true
		}
	}
	for _, expected := range []string{"Administrator", "Manager", "Read Only"} {
		if !roles[expected] {
			t.Errorf("expected roleName %q in output, got roles: %v", expected, roles)
		}
	}
}

// --- Get Tests ---

func TestAdminsGet_ByID(t *testing.T) {
	setupUsersTest(t)
	ts := startAdminsServer(t, sampleAdmins())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "get", "aabbccddee112233aabb2001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["email"] != "admin@acme.com" {
		t.Errorf("email = %v, want admin@acme.com", result["email"])
	}
}

func TestAdminsGet_ByEmail(t *testing.T) {
	setupUsersTest(t)
	viper.Set("cache.directory", t.TempDir())
	ts := startAdminsServer(t, sampleAdmins())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "get", "admin@acme.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["_id"] != "aabbccddee112233aabb2001" {
		t.Errorf("_id = %v, want aabbccddee112233aabb2001", result["_id"])
	}
}

func TestAdminsGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	viper.Set("cache.directory", t.TempDir())
	ts := startAdminsServer(t, sampleAdmins())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "get", "nobody@acme.com"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not found admin")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestAdminsGet_MissingArg(t *testing.T) {
	setupUsersTest(t)
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "get"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

// --- Create Tests ---

func TestAdminsCreate(t *testing.T) {
	setupUsersTest(t)
	ts := startAdminsServer(t, sampleAdmins())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "create", "--email", "new@acme.com", "--role", "Manager"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["email"] != "new@acme.com" {
		t.Errorf("email = %v, want new@acme.com", result["email"])
	}
	if result["_id"] != "newadminnewadminnewadm01" {
		t.Errorf("_id = %v, want newadminnewadminnewadm01", result["_id"])
	}
}

func TestAdminsCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startAdminsServer(t, sampleAdmins())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	errBuf := &bytes.Buffer{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"admins", "create", "--email", "new@acme.com", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.Code != plan.ExitCodePlan {
		t.Errorf("exit code = %d, want %d", exitErr.Code, plan.ExitCodePlan)
	}
	if !strings.Contains(errBuf.String(), "create") {
		t.Errorf("plan should mention 'create', got: %s", errBuf.String())
	}
}

func TestAdminsCreate_MissingEmail(t *testing.T) {
	setupUsersTest(t)
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "create"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --email")
	}
}

// --- Update Tests ---

func TestAdminsUpdate(t *testing.T) {
	setupUsersTest(t)
	ts := startAdminsServer(t, sampleAdmins())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "update", "aabbccddee112233aabb2001", "--role", "Read Only"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["roleName"] != "Read Only" {
		t.Errorf("roleName = %v, want 'Read Only'", result["roleName"])
	}
}

func TestAdminsUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "update", "aabbccddee112233aabb2001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields to update")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should mention 'no fields to update', got: %v", err)
	}
}

func TestAdminsUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startAdminsServer(t, sampleAdmins())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	errBuf := &bytes.Buffer{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"admins", "update", "aabbccddee112233aabb2001", "--role", "Manager", "--plan"})

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

func TestAdminsDelete_Force(t *testing.T) {
	setupUsersTest(t)
	ts := startAdminsServer(t, sampleAdmins())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "delete", "aabbccddee112233aabb2001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(buf.String(), "admin@acme.com") {
		t.Errorf("output should mention admin email: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "deleted successfully") {
		t.Errorf("output should confirm deletion: %s", buf.String())
	}
}

func TestAdminsDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startAdminsServer(t, sampleAdmins())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	errBuf := &bytes.Buffer{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"admins", "delete", "aabbccddee112233aabb2001", "--plan"})

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

func TestAdminsDelete_NotFound(t *testing.T) {
	setupUsersTest(t)
	viper.Set("cache.directory", t.TempDir())
	ts := startAdminsServer(t, sampleAdmins())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "delete", "nobody@acme.com", "--force"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not found admin")
	}
}

func TestAdminsDelete_MissingArg(t *testing.T) {
	setupUsersTest(t)
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "delete"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

// --- Help Tests ---

func TestAdminsHelp_Subcommands(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "--help"})

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

func TestAdminsHelp_ListFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list", "--help"})

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

func TestAdminsHelp_RootIncludesAdmins(t *testing.T) {
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
	if !strings.Contains(out, "admins") {
		t.Errorf("root help should contain 'admins', got:\n%s", out)
	}
}

func TestAdminsList_Endpoint(t *testing.T) {
	setupUsersTest(t)
	var requestedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results":    []map[string]any{},
			"totalCount": 0,
		})
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if requestedPath != "/users" {
		t.Errorf("expected request to /users, got %q", requestedPath)
	}
}
