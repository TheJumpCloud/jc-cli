package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startAdminsServer creates a mock JumpCloud V2 server that handles /administrators endpoints.
// V2 responses are bare JSON arrays (not wrapped like V1).
func startAdminsServer(t *testing.T, admins []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /administrators — list endpoint.
		if r.URL.Path == "/administrators" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(admins)
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleAdmins() []map[string]any {
	return []map[string]any{
		{
			"id":                 "aabbccddee112233aabb2001",
			"email":             "admin@acme.com",
			"role":              "Administrator",
			"enableMultiFactor": true,
			"firstname":         "Alice",
			"lastname":          "Admin",
		},
		{
			"id":                 "aabbccddee112233aabb2002",
			"email":             "manager@acme.com",
			"role":              "Manager",
			"enableMultiFactor": false,
			"firstname":         "Bob",
			"lastname":          "Manager",
		},
		{
			"id":                 "aabbccddee112233aabb2003",
			"email":             "readonly@acme.com",
			"role":              "Read Only",
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
	overrideV2Client(t, ts.URL)

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
	overrideV2Client(t, ts.URL)

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

	// Default fields should include email, role, enableMultiFactor.
	first := result[0]
	if _, ok := first["email"]; !ok {
		t.Error("default fields should include 'email'")
	}
	if _, ok := first["role"]; !ok {
		t.Error("default fields should include 'role'")
	}
	if _, ok := first["enableMultiFactor"]; !ok {
		t.Error("default fields should include 'enableMultiFactor'")
	}
}

func TestAdminsList_Table(t *testing.T) {
	setupUsersTest(t)
	admins := sampleAdmins()
	ts := startAdminsServer(t, admins)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

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
	overrideV2Client(t, ts.URL)

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
	overrideV2Client(t, ts.URL)

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
	overrideV2Client(t, ts.URL)

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
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"admins", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errBuf.String()
	if !strings.Contains(footer, "3 items") {
		t.Errorf("footer should contain '3 items', got: %q", footer)
	}
}

func TestAdminsList_Empty(t *testing.T) {
	setupUsersTest(t)
	ts := startAdminsServer(t, []map[string]any{})
	defer ts.Close()
	overrideV2Client(t, ts.URL)

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
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list", "--filter", "role=Administrator"})

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
	overrideV2Client(t, ts.URL)

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
	overrideV2Client(t, ts.URL)

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
	overrideV2Client(t, ts.URL)

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
	overrideV2Client(t, ts.URL)

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
		if r, ok := a["role"].(string); ok {
			roles[r] = true
		}
	}
	for _, expected := range []string{"Administrator", "Manager", "Read Only"} {
		if !roles[expected] {
			t.Errorf("expected role %q in output, got roles: %v", expected, roles)
		}
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
	if !strings.Contains(out, "list") {
		t.Errorf("help should contain subcommand 'list', got:\n%s", out)
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
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admins", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if requestedPath != "/administrators" {
		t.Errorf("expected request to /administrators, got %q", requestedPath)
	}
}
