package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
)

// overrideV2Client redirects V2 API calls to the given test server.
func overrideV2Client(t *testing.T, serverURL string) {
	t.Helper()
	orig := newV2Client
	newV2Client = func() (*api.V2Client, error) {
		c := api.NewV2ClientWithKey("test-key-1234")
		c.BaseURL = serverURL
		return c, nil
	}
	t.Cleanup(func() { newV2Client = orig })
}

// startUserGroupsServer creates a mock JumpCloud V2 server that handles /usergroups endpoints.
// V2 responses are bare JSON arrays (not wrapped like V1).
func startUserGroupsServer(t *testing.T, groups []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /usergroups — list endpoint.
		if r.URL.Path == "/usergroups" && r.Method == http.MethodGet {
			// V2 returns bare JSON array.
			json.NewEncoder(w).Encode(groups)
			return
		}

		// POST /usergroups — create endpoint.
		if r.URL.Path == "/usergroups" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)

			// Simulate API response — echo back fields with a generated id.
			input["id"] = "new123new123new123new123"
			input["type"] = "custom"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /usergroups/{id}.
		if strings.HasPrefix(r.URL.Path, "/usergroups/") {
			id := strings.TrimPrefix(r.URL.Path, "/usergroups/")

			// Find the group.
			var found map[string]any
			for _, g := range groups {
				if g["id"] == id {
					found = g
					break
				}
			}

			if found == nil {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}

			switch r.Method {
			case http.MethodGet:
				json.NewEncoder(w).Encode(found)
				return
			case http.MethodPut:
				var input map[string]any
				json.NewDecoder(r.Body).Decode(&input)
				// Merge updated fields into found.
				for k, v := range input {
					found[k] = v
				}
				json.NewEncoder(w).Encode(found)
				return
			case http.MethodDelete:
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(found)
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleGroups() []map[string]any {
	return []map[string]any{
		{
			"id":          "aabbccddee112233aabb0001",
			"name":        "Engineering",
			"description": "Engineering team",
			"type":        "custom",
		},
		{
			"id":          "aabbccddee112233aabb0002",
			"name":        "Marketing",
			"description": "Marketing department",
			"type":        "custom",
		},
		{
			"id":          "aabbccddee112233aabb0003",
			"name":        "Finance",
			"description": "Finance team",
			"type":        "custom",
		},
	}
}

// --- List Tests ---

func TestGroupsUserList_JSON(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d groups, want 3", len(result))
	}
}

func TestGroupsUserList_Table(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Engineering") {
		t.Errorf("output missing 'Engineering': %s", out)
	}
	if !strings.Contains(out, "Marketing") {
		t.Errorf("output missing 'Marketing': %s", out)
	}
}

func TestGroupsUserList_IDs(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "list", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d lines, want 3: %v", len(lines), lines)
	}
	if lines[0] != "aabbccddee112233aabb0001" {
		t.Errorf("first ID = %q, want %q", lines[0], "aabbccddee112233aabb0001")
	}
}

func TestGroupsUserList_Quiet(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "list", "--quiet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected empty output, got %q", buf.String())
	}
}

func TestGroupsUserList_Footer(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"groups", "user", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(errBuf.String(), "── 3 items ──") {
		t.Errorf("footer missing '── 3 items ──': %s", errBuf.String())
	}
}

func TestGroupsUserList_Empty(t *testing.T) {
	setupUsersTest(t)
	ts := startUserGroupsServer(t, []map[string]any{})
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("got %d items, want 0", len(result))
	}
}

func TestGroupsUserList_Filter(t *testing.T) {
	setupUsersTest(t)

	var capturedURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "list", "--filter", "name=Engineering"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(capturedURL, "filter=name%3Aeq%3AEngineering") {
		t.Errorf("URL missing V2 filter param: %s", capturedURL)
	}
}

func TestGroupsUserList_Sort(t *testing.T) {
	setupUsersTest(t)

	var capturedURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "list", "--sort", "-name"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(capturedURL, "sort=-name") {
		t.Errorf("URL missing sort param: %s", capturedURL)
	}
}

func TestGroupsUserList_InvalidFilter(t *testing.T) {
	setupUsersTest(t)
	ts := startUserGroupsServer(t, []map[string]any{})
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "list", "--filter", "badfilter"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid filter")
	}
	if !strings.Contains(err.Error(), "invalid filter") {
		t.Errorf("error = %q, want to contain 'invalid filter'", err.Error())
	}
}

// --- Get Tests ---

func TestGroupsUserGet_ByID(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "get", "aabbccddee112233aabb0001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "Engineering" {
		t.Errorf("name = %q, want %q", result["name"], "Engineering")
	}
}

func TestGroupsUserGet_ByName(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "get", "Engineering"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "Engineering" {
		t.Errorf("name = %q, want %q", result["name"], "Engineering")
	}
}

func TestGroupsUserGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "get", "aabbccddee112233aabb9999"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found group")
	}
}

func TestGroupsUserGet_NameNotFound(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "get", "NonExistentGroup"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found group name")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestGroupsUserGet_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "get"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

// --- Create Tests ---

func TestGroupsUserCreate(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "create", "--name", "QA Team", "--description", "Quality assurance"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "QA Team" {
		t.Errorf("name = %q, want %q", result["name"], "QA Team")
	}
	if result["id"] != "new123new123new123new123" {
		t.Errorf("id = %q, want %q", result["id"], "new123new123new123new123")
	}
}

func TestGroupsUserCreate_NameOnly(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "create", "--name", "Minimal Group"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "Minimal Group" {
		t.Errorf("name = %q, want %q", result["name"], "Minimal Group")
	}
}

func TestGroupsUserCreate_MissingName(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "create"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --name")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want to contain 'required'", err.Error())
	}
}

func TestGroupsUserCreate_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedMethod, capturedPath string
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": "new123new123new123new123", "name": "Test"})
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "create", "--name", "Test", "--description", "Desc"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/usergroups" {
		t.Errorf("path = %q, want /usergroups", capturedPath)
	}
	if capturedBody["name"] != "Test" {
		t.Errorf("body name = %v, want Test", capturedBody["name"])
	}
	if capturedBody["description"] != "Desc" {
		t.Errorf("body description = %v, want Desc", capturedBody["description"])
	}
}

// --- Update Tests ---

func TestGroupsUserUpdate_ByID(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "update", "aabbccddee112233aabb0001", "--description", "Updated description"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["description"] != "Updated description" {
		t.Errorf("description = %q, want %q", result["description"], "Updated description")
	}
}

func TestGroupsUserUpdate_ByName(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "update", "Engineering", "--name", "Eng"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "Eng" {
		t.Errorf("name = %q, want %q", result["name"], "Eng")
	}
}

func TestGroupsUserUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "update", "aabbccddee112233aabb0001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error = %q, want to contain 'no fields to update'", err.Error())
	}
}

func TestGroupsUserUpdate_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedMethod, capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/usergroups" && r.Method == http.MethodGet {
			// ListAll for resolver
			json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"id": "aabbccddee112233aabb0001", "name": "Updated"})
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "update", "aabbccddee112233aabb0001", "--name", "Updated"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedMethod != "PUT" {
		t.Errorf("method = %q, want PUT", capturedMethod)
	}
	if capturedPath != "/usergroups/aabbccddee112233aabb0001" {
		t.Errorf("path = %q, want /usergroups/aabbccddee112233aabb0001", capturedPath)
	}
}

// --- Delete Tests ---

func TestGroupsUserDelete_Force(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "delete", "aabbccddee112233aabb0001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "deleted successfully") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "Engineering") {
		t.Errorf("output missing group name: %s", buf.String())
	}
}

func TestGroupsUserDelete_ForceByName(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "delete", "Engineering", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "deleted successfully") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}
}

func TestGroupsUserDelete_ConfirmYes(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	// Override viper force to false and inject confirmation.
	viper.Set("force", false)
	overrideConfirmReader(t, "y\n")

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "delete", "aabbccddee112233aabb0001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "deleted successfully") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}
}

func TestGroupsUserDelete_ConfirmNo(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	viper.Set("force", false)
	overrideConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"groups", "user", "delete", "aabbccddee112233aabb0001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected no stdout output, got %q", buf.String())
	}
	if !strings.Contains(errBuf.String(), "Cancelled") {
		t.Errorf("stderr missing 'Cancelled': %s", errBuf.String())
	}
}

func TestGroupsUserDelete_EmptyInput(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	viper.Set("force", false)
	overrideConfirmReader(t, "\n")

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"groups", "user", "delete", "aabbccddee112233aabb0001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected no stdout output, got %q", buf.String())
	}
}

func TestGroupsUserDelete_NotFound(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "delete", "aabbccddee112233aabb9999", "--force"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found group")
	}
}

func TestGroupsUserDelete_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "delete"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestGroupsUserDelete_ConfirmPromptShowsName(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	viper.Set("force", false)
	overrideConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	var errBuf bytes.Buffer
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"groups", "user", "delete", "aabbccddee112233aabb0001"})

	_ = cmd.Execute()

	if !strings.Contains(errBuf.String(), "Engineering") {
		t.Errorf("prompt missing group name: %s", errBuf.String())
	}
}

// --- Help/Structure Tests ---

func TestGroupsCmd_Help(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "user") {
		t.Errorf("help missing 'user' subcommand: %s", out)
	}
}

func TestGroupsUserCmd_Help(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, sub := range []string{"list", "get", "create", "update", "delete"} {
		if !strings.Contains(out, sub) {
			t.Errorf("help missing %q subcommand: %s", sub, out)
		}
	}
}

func TestGroupsUserList_HelpIncludesFilterSort(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "list", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "--filter") {
		t.Errorf("help missing --filter: %s", out)
	}
	if !strings.Contains(out, "--sort") {
		t.Errorf("help missing --sort: %s", out)
	}
	if !strings.Contains(out, "--limit") {
		t.Errorf("help missing --limit: %s", out)
	}
}

// --- CSV Output Test ---

func TestGroupsUserList_CSV(t *testing.T) {
	setupUsersTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "list", "--output", "csv"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Header + 3 data rows.
	if len(lines) < 4 {
		t.Errorf("expected at least 4 lines (header + 3 data), got %d: %s", len(lines), out)
	}
}

// --- Limit Test ---

func TestGroupsUserList_Limit(t *testing.T) {
	setupUsersTest(t)

	var capturedURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "aabbccddee112233aabb0001", "name": "G1"},
		})
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "user", "list", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(capturedURL, "limit=1") {
		t.Errorf("URL missing limit param: %s", capturedURL)
	}
}

// ========================================================================
// Device Groups Tests
// ========================================================================

// startDeviceGroupsServer creates a mock JumpCloud V2 server that handles /systemgroups endpoints.
func startDeviceGroupsServer(t *testing.T, groups []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /systemgroups — list endpoint.
		if r.URL.Path == "/systemgroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(groups)
			return
		}

		// POST /systemgroups — create endpoint.
		if r.URL.Path == "/systemgroups" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "dg0123dg0123dg0123dg0123"
			input["type"] = "custom"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /systemgroups/{id}.
		if strings.HasPrefix(r.URL.Path, "/systemgroups/") {
			id := strings.TrimPrefix(r.URL.Path, "/systemgroups/")

			var found map[string]any
			for _, g := range groups {
				if g["id"] == id {
					found = g
					break
				}
			}

			if found == nil {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}

			switch r.Method {
			case http.MethodGet:
				json.NewEncoder(w).Encode(found)
				return
			case http.MethodPut:
				var input map[string]any
				json.NewDecoder(r.Body).Decode(&input)
				for k, v := range input {
					found[k] = v
				}
				json.NewEncoder(w).Encode(found)
				return
			case http.MethodDelete:
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(found)
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleDeviceGroups() []map[string]any {
	return []map[string]any{
		{
			"id":          "dd11ee22ff33dd11ee220001",
			"name":        "macOS Fleet",
			"description": "All macOS devices",
			"type":        "custom",
		},
		{
			"id":          "dd11ee22ff33dd11ee220002",
			"name":        "Windows Servers",
			"description": "Windows server fleet",
			"type":        "custom",
		},
		{
			"id":          "dd11ee22ff33dd11ee220003",
			"name":        "Linux Workers",
			"description": "Linux worker nodes",
			"type":        "custom",
		},
	}
}

// --- Device Groups List Tests ---

func TestGroupsDeviceList_JSON(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d groups, want 3", len(result))
	}
}

func TestGroupsDeviceList_Table(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "macOS Fleet") {
		t.Errorf("output missing 'macOS Fleet': %s", out)
	}
	if !strings.Contains(out, "Windows Servers") {
		t.Errorf("output missing 'Windows Servers': %s", out)
	}
}

func TestGroupsDeviceList_CSV(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "list", "--output", "csv"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 4 {
		t.Errorf("expected at least 4 lines (header + 3 data), got %d: %s", len(lines), out)
	}
}

func TestGroupsDeviceList_IDs(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "list", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d lines, want 3: %v", len(lines), lines)
	}
	if lines[0] != "dd11ee22ff33dd11ee220001" {
		t.Errorf("first ID = %q, want %q", lines[0], "dd11ee22ff33dd11ee220001")
	}
}

func TestGroupsDeviceList_Quiet(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "list", "--quiet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected empty output, got %q", buf.String())
	}
}

func TestGroupsDeviceList_Footer(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"groups", "device", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(errBuf.String(), "── 3 items ──") {
		t.Errorf("footer missing '── 3 items ──': %s", errBuf.String())
	}
}

func TestGroupsDeviceList_Empty(t *testing.T) {
	setupUsersTest(t)
	ts := startDeviceGroupsServer(t, []map[string]any{})
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("got %d items, want 0", len(result))
	}
}

func TestGroupsDeviceList_Filter(t *testing.T) {
	setupUsersTest(t)

	var capturedURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "list", "--filter", "name=macOS Fleet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(capturedURL, "filter=name%3Aeq%3AmacOS+Fleet") && !strings.Contains(capturedURL, "filter=name%3Aeq%3AmacOS%20Fleet") {
		t.Errorf("URL missing V2 filter param: %s", capturedURL)
	}
}

func TestGroupsDeviceList_Sort(t *testing.T) {
	setupUsersTest(t)

	var capturedURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "list", "--sort", "-name"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(capturedURL, "sort=-name") {
		t.Errorf("URL missing sort param: %s", capturedURL)
	}
}

func TestGroupsDeviceList_InvalidFilter(t *testing.T) {
	setupUsersTest(t)
	ts := startDeviceGroupsServer(t, []map[string]any{})
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "list", "--filter", "badfilter"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid filter")
	}
	if !strings.Contains(err.Error(), "invalid filter") {
		t.Errorf("error = %q, want to contain 'invalid filter'", err.Error())
	}
}

func TestGroupsDeviceList_Limit(t *testing.T) {
	setupUsersTest(t)

	var capturedURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "dd11ee22ff33dd11ee220001", "name": "G1"},
		})
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "list", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(capturedURL, "limit=1") {
		t.Errorf("URL missing limit param: %s", capturedURL)
	}
}

// --- Device Groups Get Tests ---

func TestGroupsDeviceGet_ByID(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "get", "dd11ee22ff33dd11ee220001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "macOS Fleet" {
		t.Errorf("name = %q, want %q", result["name"], "macOS Fleet")
	}
}

func TestGroupsDeviceGet_ByName(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "get", "macOS Fleet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "macOS Fleet" {
		t.Errorf("name = %q, want %q", result["name"], "macOS Fleet")
	}
}

func TestGroupsDeviceGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "get", "dd11ee22ff33dd11ee229999"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found group")
	}
}

func TestGroupsDeviceGet_NameNotFound(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "get", "NonExistentGroup"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found group name")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestGroupsDeviceGet_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "get"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

// --- Device Groups Create Tests ---

func TestGroupsDeviceCreate(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "create", "--name", "Ubuntu Servers", "--description", "Ubuntu server fleet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "Ubuntu Servers" {
		t.Errorf("name = %q, want %q", result["name"], "Ubuntu Servers")
	}
	if result["id"] != "dg0123dg0123dg0123dg0123" {
		t.Errorf("id = %q, want %q", result["id"], "dg0123dg0123dg0123dg0123")
	}
}

func TestGroupsDeviceCreate_NameOnly(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "create", "--name", "Minimal Group"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "Minimal Group" {
		t.Errorf("name = %q, want %q", result["name"], "Minimal Group")
	}
}

func TestGroupsDeviceCreate_MissingName(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "create"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --name")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want to contain 'required'", err.Error())
	}
}

func TestGroupsDeviceCreate_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedMethod, capturedPath string
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": "dg0123dg0123dg0123dg0123", "name": "Test"})
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "create", "--name", "Test", "--description", "Desc"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/systemgroups" {
		t.Errorf("path = %q, want /systemgroups", capturedPath)
	}
	if capturedBody["name"] != "Test" {
		t.Errorf("body name = %v, want Test", capturedBody["name"])
	}
	if capturedBody["description"] != "Desc" {
		t.Errorf("body description = %v, want Desc", capturedBody["description"])
	}
}

// --- Device Groups Update Tests ---

func TestGroupsDeviceUpdate_ByID(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "update", "dd11ee22ff33dd11ee220001", "--description", "Updated description"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["description"] != "Updated description" {
		t.Errorf("description = %q, want %q", result["description"], "Updated description")
	}
}

func TestGroupsDeviceUpdate_ByName(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "update", "macOS Fleet", "--name", "macOS Devices"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "macOS Devices" {
		t.Errorf("name = %q, want %q", result["name"], "macOS Devices")
	}
}

func TestGroupsDeviceUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "update", "dd11ee22ff33dd11ee220001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error = %q, want to contain 'no fields to update'", err.Error())
	}
}

func TestGroupsDeviceUpdate_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedMethod, capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/systemgroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"id": "dd11ee22ff33dd11ee220001", "name": "Updated"})
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "update", "dd11ee22ff33dd11ee220001", "--name", "Updated"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedMethod != "PUT" {
		t.Errorf("method = %q, want PUT", capturedMethod)
	}
	if capturedPath != "/systemgroups/dd11ee22ff33dd11ee220001" {
		t.Errorf("path = %q, want /systemgroups/dd11ee22ff33dd11ee220001", capturedPath)
	}
}

// --- Device Groups Delete Tests ---

func TestGroupsDeviceDelete_Force(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "delete", "dd11ee22ff33dd11ee220001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "deleted successfully") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "macOS Fleet") {
		t.Errorf("output missing group name: %s", buf.String())
	}
}

func TestGroupsDeviceDelete_ForceByName(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "delete", "macOS Fleet", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "deleted successfully") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}
}

func TestGroupsDeviceDelete_ConfirmYes(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	viper.Set("force", false)
	overrideConfirmReader(t, "y\n")

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "delete", "dd11ee22ff33dd11ee220001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "deleted successfully") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}
}

func TestGroupsDeviceDelete_ConfirmNo(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	viper.Set("force", false)
	overrideConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"groups", "device", "delete", "dd11ee22ff33dd11ee220001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected no stdout output, got %q", buf.String())
	}
	if !strings.Contains(errBuf.String(), "Cancelled") {
		t.Errorf("stderr missing 'Cancelled': %s", errBuf.String())
	}
}

func TestGroupsDeviceDelete_EmptyInput(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	viper.Set("force", false)
	overrideConfirmReader(t, "\n")

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"groups", "device", "delete", "dd11ee22ff33dd11ee220001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected no stdout output, got %q", buf.String())
	}
}

func TestGroupsDeviceDelete_NotFound(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "delete", "dd11ee22ff33dd11ee229999", "--force"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found group")
	}
}

func TestGroupsDeviceDelete_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "delete"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestGroupsDeviceDelete_ConfirmPromptShowsName(t *testing.T) {
	setupUsersTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	viper.Set("force", false)
	overrideConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	var errBuf bytes.Buffer
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"groups", "device", "delete", "dd11ee22ff33dd11ee220001"})

	_ = cmd.Execute()

	if !strings.Contains(errBuf.String(), "macOS Fleet") {
		t.Errorf("prompt missing group name: %s", errBuf.String())
	}
}

// --- Device Groups Help/Structure Tests ---

func TestGroupsCmd_HelpIncludesDevice(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "device") {
		t.Errorf("help missing 'device' subcommand: %s", out)
	}
}

func TestGroupsDeviceCmd_Help(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, sub := range []string{"list", "get", "create", "update", "delete"} {
		if !strings.Contains(out, sub) {
			t.Errorf("help missing %q subcommand: %s", sub, out)
		}
	}
}

func TestGroupsDeviceList_HelpIncludesFilterSort(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "device", "list", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "--filter") {
		t.Errorf("help missing --filter: %s", out)
	}
	if !strings.Contains(out, "--sort") {
		t.Errorf("help missing --sort: %s", out)
	}
	if !strings.Contains(out, "--limit") {
		t.Errorf("help missing --limit: %s", out)
	}
}

// ========================================================================
// Group Membership Tests
// ========================================================================

// membershipRecord tracks membership operations for test assertions.
type membershipRecord struct {
	GroupID string
	Op      string
	Type    string
	ID      string
}

// startMembershipServer creates a combined V1+V2 mock server that handles:
//   - V1: /systemusers (for user name resolution)
//   - V1: /systems (for device name resolution)
//   - V2: /usergroups, /usergroups/{id}/members
//   - V2: /systemgroups, /systemgroups/{id}/membership
func startMembershipServer(t *testing.T, users []map[string]any, devices []map[string]any, userGroups []map[string]any, deviceGroups []map[string]any, records *[]membershipRecord) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// V1: /systemusers — list for user name resolution.
		if r.URL.Path == "/systemusers" && r.Method == http.MethodGet {
			resp := map[string]any{
				"results":    users,
				"totalCount": len(users),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// V1: /systems — list for device name resolution.
		if r.URL.Path == "/systems" && r.Method == http.MethodGet {
			resp := map[string]any{
				"results":    devices,
				"totalCount": len(devices),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// V2: /usergroups — list for group name resolution.
		if r.URL.Path == "/usergroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(userGroups)
			return
		}

		// V2: /systemgroups — list for group name resolution.
		if r.URL.Path == "/systemgroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(deviceGroups)
			return
		}

		// V2: /usergroups/{id}/members — membership operations.
		if strings.HasSuffix(r.URL.Path, "/members") && r.Method == http.MethodPost {
			parts := strings.Split(r.URL.Path, "/")
			// /usergroups/{id}/members → parts[1]=usergroups, parts[2]=id, parts[3]=members
			if len(parts) >= 4 && parts[1] == "usergroups" {
				var body map[string]string
				json.NewDecoder(r.Body).Decode(&body)

				if records != nil {
					*records = append(*records, membershipRecord{
						GroupID: parts[2],
						Op:      body["op"],
						Type:    body["type"],
						ID:      body["id"],
					})
				}

				// Simulate 204 No Content success.
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		// V2: /systemgroups/{id}/membership — membership operations.
		if strings.HasSuffix(r.URL.Path, "/membership") && r.Method == http.MethodPost {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 4 && parts[1] == "systemgroups" {
				var body map[string]string
				json.NewDecoder(r.Body).Decode(&body)

				if records != nil {
					*records = append(*records, membershipRecord{
						GroupID: parts[2],
						Op:      body["op"],
						Type:    body["type"],
						ID:      body["id"],
					})
				}

				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func membershipSampleUsers() []map[string]any {
	return []map[string]any{
		{"_id": "aa11bb22cc33dd44ee550001", "username": "jdoe", "email": "jdoe@acme.com"},
		{"_id": "aa11bb22cc33dd44ee550002", "username": "jsmith", "email": "jsmith@acme.com"},
	}
}

func membershipSampleDevices() []map[string]any {
	return []map[string]any{
		{"_id": "bb11cc22dd33ee44ff550001", "hostname": "JDOE-MBP", "os": "Mac OS X"},
		{"_id": "bb11cc22dd33ee44ff550002", "hostname": "JSMITH-WIN", "os": "Windows"},
	}
}

func setupMembershipTest(t *testing.T, records *[]membershipRecord) *httptest.Server {
	t.Helper()
	setupUsersTest(t)
	// Point cache to a temp dir to avoid stale entries from real usage.
	viper.Set("cache.directory", filepath.Join(t.TempDir(), "cache"))
	ts := startMembershipServer(t, membershipSampleUsers(), membershipSampleDevices(), sampleGroups(), sampleDeviceGroups(), records)
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)
	return ts
}

// --- Add Member Tests ---

func TestGroupsAddMember_UserToGroup(t *testing.T) {
	var records []membershipRecord
	ts := setupMembershipTest(t, &records)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "add-member", "aabbccddee112233aabb0001", "--user", "aa11bb22cc33dd44ee550001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "Added user") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 membership record, got %d", len(records))
	}
	if records[0].Op != "add" {
		t.Errorf("op = %q, want %q", records[0].Op, "add")
	}
	if records[0].Type != "user" {
		t.Errorf("type = %q, want %q", records[0].Type, "user")
	}
	if records[0].ID != "aa11bb22cc33dd44ee550001" {
		t.Errorf("id = %q, want %q", records[0].ID, "aa11bb22cc33dd44ee550001")
	}
	if records[0].GroupID != "aabbccddee112233aabb0001" {
		t.Errorf("groupID = %q, want %q", records[0].GroupID, "aabbccddee112233aabb0001")
	}
}

func TestGroupsAddMember_UserByName(t *testing.T) {
	var records []membershipRecord
	ts := setupMembershipTest(t, &records)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "add-member", "Engineering", "--user", "jdoe"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "Added user") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	// Group name "Engineering" should resolve to group ID aabbccddee112233aabb0001.
	if records[0].GroupID != "aabbccddee112233aabb0001" {
		t.Errorf("groupID = %q, want %q", records[0].GroupID, "aabbccddee112233aabb0001")
	}
	// User "jdoe" should resolve to user ID u001...
	if records[0].ID != "aa11bb22cc33dd44ee550001" {
		t.Errorf("userID = %q, want %q", records[0].ID, "aa11bb22cc33dd44ee550001")
	}
}

func TestGroupsAddMember_DeviceToGroup(t *testing.T) {
	var records []membershipRecord
	ts := setupMembershipTest(t, &records)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "add-member", "dd11ee22ff33dd11ee220001", "--device", "bb11cc22dd33ee44ff550001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "Added device") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Op != "add" {
		t.Errorf("op = %q, want %q", records[0].Op, "add")
	}
	if records[0].Type != "system" {
		t.Errorf("type = %q, want %q", records[0].Type, "system")
	}
}

func TestGroupsAddMember_DeviceByName(t *testing.T) {
	var records []membershipRecord
	ts := setupMembershipTest(t, &records)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "add-member", "macOS Fleet", "--device", "JDOE-MBP"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "Added device") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].GroupID != "dd11ee22ff33dd11ee220001" {
		t.Errorf("groupID = %q, want %q", records[0].GroupID, "dd11ee22ff33dd11ee220001")
	}
	if records[0].ID != "bb11cc22dd33ee44ff550001" {
		t.Errorf("deviceID = %q, want %q", records[0].ID, "bb11cc22dd33ee44ff550001")
	}
}

func TestGroupsAddMember_MissingUserOrDevice(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "add-member", "Engineering"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --user/--device")
	}
	if !strings.Contains(err.Error(), "specify --user or --device") {
		t.Errorf("error = %q, want to contain 'specify --user or --device'", err.Error())
	}
}

func TestGroupsAddMember_BothUserAndDevice(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "add-member", "Engineering", "--user", "jdoe", "--device", "JDOE-MBP"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for both --user and --device")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Errorf("error = %q, want to contain 'not both'", err.Error())
	}
}

func TestGroupsAddMember_MissingGroupArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "add-member"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing group argument")
	}
}

func TestGroupsAddMember_AlreadyMember(t *testing.T) {
	setupUsersTest(t)

	// Create server that returns 409 Conflict.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/usergroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(sampleGroups())
			return
		}
		if r.URL.Path == "/systemusers" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"results": membershipSampleUsers(), "totalCount": 2})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/members") && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"message":"already a member"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "add-member", "Engineering", "--user", "jdoe"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "already a member") {
		t.Errorf("output missing 'already a member': %s", buf.String())
	}
}

func TestGroupsAddMember_APIEndpoint(t *testing.T) {
	var capturedMethod, capturedPath string
	var capturedBody map[string]string
	setupUsersTest(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/usergroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(sampleGroups())
			return
		}
		if r.URL.Path == "/systemusers" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"results": membershipSampleUsers(), "totalCount": 2})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/members") {
			capturedMethod = r.Method
			capturedPath = r.URL.Path
			json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "add-member", "aabbccddee112233aabb0001", "--user", "aa11bb22cc33dd44ee550001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/usergroups/aabbccddee112233aabb0001/members" {
		t.Errorf("path = %q, want /usergroups/aabbccddee112233aabb0001/members", capturedPath)
	}
	if capturedBody["op"] != "add" {
		t.Errorf("body op = %q, want add", capturedBody["op"])
	}
	if capturedBody["type"] != "user" {
		t.Errorf("body type = %q, want user", capturedBody["type"])
	}
	if capturedBody["id"] != "aa11bb22cc33dd44ee550001" {
		t.Errorf("body id = %q, want aa11bb22cc33dd44ee550001", capturedBody["id"])
	}
}

func TestGroupsAddMember_DeviceAPIEndpoint(t *testing.T) {
	var capturedPath string
	var capturedBody map[string]string
	setupUsersTest(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/systemgroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(sampleDeviceGroups())
			return
		}
		if r.URL.Path == "/systems" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"results": membershipSampleDevices(), "totalCount": 2})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/membership") {
			capturedPath = r.URL.Path
			json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "add-member", "dd11ee22ff33dd11ee220001", "--device", "bb11cc22dd33ee44ff550001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systemgroups/dd11ee22ff33dd11ee220001/membership" {
		t.Errorf("path = %q, want /systemgroups/dd11ee22ff33dd11ee220001/membership", capturedPath)
	}
	if capturedBody["op"] != "add" {
		t.Errorf("body op = %q, want add", capturedBody["op"])
	}
	if capturedBody["type"] != "system" {
		t.Errorf("body type = %q, want system", capturedBody["type"])
	}
}

// --- Remove Member Tests ---

func TestGroupsRemoveMember_UserFromGroup(t *testing.T) {
	var records []membershipRecord
	ts := setupMembershipTest(t, &records)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "remove-member", "aabbccddee112233aabb0001", "--user", "aa11bb22cc33dd44ee550001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "Removed user") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Op != "remove" {
		t.Errorf("op = %q, want %q", records[0].Op, "remove")
	}
	if records[0].Type != "user" {
		t.Errorf("type = %q, want %q", records[0].Type, "user")
	}
}

func TestGroupsRemoveMember_DeviceFromGroup(t *testing.T) {
	var records []membershipRecord
	ts := setupMembershipTest(t, &records)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "remove-member", "dd11ee22ff33dd11ee220001", "--device", "bb11cc22dd33ee44ff550001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "Removed device") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Op != "remove" {
		t.Errorf("op = %q, want %q", records[0].Op, "remove")
	}
	if records[0].Type != "system" {
		t.Errorf("type = %q, want %q", records[0].Type, "system")
	}
}

func TestGroupsRemoveMember_ByName(t *testing.T) {
	var records []membershipRecord
	ts := setupMembershipTest(t, &records)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "remove-member", "Engineering", "--user", "jdoe"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "Removed user") {
		t.Errorf("output missing confirmation: %s", buf.String())
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].GroupID != "aabbccddee112233aabb0001" {
		t.Errorf("groupID = %q, want %q", records[0].GroupID, "aabbccddee112233aabb0001")
	}
	if records[0].ID != "aa11bb22cc33dd44ee550001" {
		t.Errorf("userID = %q, want %q", records[0].ID, "aa11bb22cc33dd44ee550001")
	}
}

func TestGroupsRemoveMember_NotAMember(t *testing.T) {
	setupUsersTest(t)

	// Server returns 404 for membership removal (user is not a member).
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/usergroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(sampleGroups())
			return
		}
		if r.URL.Path == "/systemusers" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"results": membershipSampleUsers(), "totalCount": 2})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/members") && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "remove-member", "Engineering", "--user", "jdoe"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "not a member") {
		t.Errorf("output missing 'not a member': %s", buf.String())
	}
}

func TestGroupsRemoveMember_MissingUserOrDevice(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "remove-member", "Engineering"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --user/--device")
	}
	if !strings.Contains(err.Error(), "specify --user or --device") {
		t.Errorf("error = %q, want to contain 'specify --user or --device'", err.Error())
	}
}

func TestGroupsRemoveMember_MissingGroupArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "remove-member", "--user", "jdoe"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing group argument without --all")
	}
	if !strings.Contains(err.Error(), "requires a group") {
		t.Errorf("error = %q, want to contain 'requires a group'", err.Error())
	}
}

// --- Remove All Tests ---

func TestGroupsRemoveMember_AllGroups(t *testing.T) {
	var records []membershipRecord
	ts := setupMembershipTest(t, &records)
	defer ts.Close()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "remove-member", "--all", "--user", "aa11bb22cc33dd44ee550001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should attempt removal from all 3 sample user groups.
	if len(records) != 3 {
		t.Fatalf("expected 3 records (one per group), got %d", len(records))
	}
	for _, rec := range records {
		if rec.Op != "remove" {
			t.Errorf("op = %q, want remove", rec.Op)
		}
		if rec.Type != "user" {
			t.Errorf("type = %q, want user", rec.Type)
		}
		if rec.ID != "aa11bb22cc33dd44ee550001" {
			t.Errorf("userID = %q, want aa11bb22cc33dd44ee550001", rec.ID)
		}
	}

	out := buf.String()
	if !strings.Contains(out, "Removed user") {
		t.Errorf("output missing removal confirmations: %s", out)
	}
}

func TestGroupsRemoveMember_AllGroupsNotMember(t *testing.T) {
	setupUsersTest(t)

	// All membership removals return 404 — user is not in any group.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/usergroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(sampleGroups())
			return
		}
		if r.URL.Path == "/systemusers" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"results": membershipSampleUsers(), "totalCount": 2})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/members") && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "remove-member", "--all", "--user", "jdoe"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "not a member of any groups") {
		t.Errorf("output missing 'not a member of any groups': %s", buf.String())
	}
}

func TestGroupsRemoveMember_AllRequiresUser(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "remove-member", "--all"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --all without --user")
	}
	if !strings.Contains(err.Error(), "--all requires --user") {
		t.Errorf("error = %q, want to contain '--all requires --user'", err.Error())
	}
}

func TestGroupsRemoveMember_AllWithDevice(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "remove-member", "--all", "--device", "JDOE-MBP"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --all with --device")
	}
	if !strings.Contains(err.Error(), "only supported with --user") {
		t.Errorf("error = %q, want to contain 'only supported with --user'", err.Error())
	}
}

// --- Help/Structure Tests ---

func TestGroupsCmd_HelpIncludesAddRemoveMember(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "add-member") {
		t.Errorf("help missing 'add-member': %s", out)
	}
	if !strings.Contains(out, "remove-member") {
		t.Errorf("help missing 'remove-member': %s", out)
	}
}

func TestGroupsAddMember_HelpShowsFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "add-member", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "--user") {
		t.Errorf("help missing --user: %s", out)
	}
	if !strings.Contains(out, "--device") {
		t.Errorf("help missing --device: %s", out)
	}
}

func TestGroupsRemoveMember_HelpShowsFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"groups", "remove-member", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "--user") {
		t.Errorf("help missing --user: %s", out)
	}
	if !strings.Contains(out, "--device") {
		t.Errorf("help missing --device: %s", out)
	}
	if !strings.Contains(out, "--all") {
		t.Errorf("help missing --all: %s", out)
	}
}

