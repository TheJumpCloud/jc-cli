package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

