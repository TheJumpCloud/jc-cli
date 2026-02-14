package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startPoliciesServer creates a mock JumpCloud V2 server that handles /policies endpoints.
// It also handles /policies/{id}/policystatuses for policy results.
func startPoliciesServer(t *testing.T, policies []map[string]any, statuses map[string][]map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /policies — list endpoint.
		if r.URL.Path == "/policies" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(policies)
			return
		}

		// POST /policies — create endpoint.
		if r.URL.Path == "/policies" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "newpolicynewpolicynewpo1"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /policies/{id}.
		if strings.HasPrefix(r.URL.Path, "/policies/") {
			rest := strings.TrimPrefix(r.URL.Path, "/policies/")

			// Check for /policies/{id}/policystatuses
			parts := strings.SplitN(rest, "/", 2)
			id := parts[0]

			if len(parts) == 2 && parts[1] == "policystatuses" {
				// Return policy statuses for this policy.
				if s, ok := statuses[id]; ok {
					json.NewEncoder(w).Encode(s)
				} else {
					json.NewEncoder(w).Encode([]map[string]any{})
				}
				return
			}

			// Find the policy by ID for GET/PUT/DELETE.
			var found map[string]any
			for _, p := range policies {
				if p["id"] == id {
					found = p
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
				// Merge input into a copy of found.
				merged := make(map[string]any)
				for k, v := range found {
					merged[k] = v
				}
				for k, v := range input {
					merged[k] = v
				}
				json.NewEncoder(w).Encode(merged)
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

func samplePolicies() []map[string]any {
	return []map[string]any{
		{
			"id":       "aabbccddee112233aabb1001",
			"name":     "Disk Encryption",
			"template": map[string]any{"id": "tmpl001", "name": "FileVault 2"},
			"os":       "darwin",
		},
		{
			"id":       "aabbccddee112233aabb1002",
			"name":     "Screen Lock",
			"template": map[string]any{"id": "tmpl002", "name": "Screen Lock"},
			"os":       "darwin",
		},
		{
			"id":       "aabbccddee112233aabb1003",
			"name":     "Firewall",
			"template": map[string]any{"id": "tmpl003", "name": "Firewall"},
			"os":       "windows",
		},
	}
}

func samplePolicyStatuses() map[string][]map[string]any {
	return map[string][]map[string]any{
		"aabbccddee112233aabb1001": {
			{
				"id":        "status001status001status01",
				"policyID":  "aabbccddee112233aabb1001",
				"systemID":  "sys001sys001sys001sys001",
				"status":    "applied",
				"startedAt": "2026-02-13T10:00:00Z",
				"endedAt":   "2026-02-13T10:00:05Z",
			},
			{
				"id":        "status002status002status02",
				"policyID":  "aabbccddee112233aabb1001",
				"systemID":  "sys002sys002sys002sys002",
				"status":    "failed",
				"startedAt": "2026-02-13T10:01:00Z",
				"endedAt":   "2026-02-13T10:01:03Z",
			},
		},
	}
}

// --- List Tests ---

func TestPoliciesList_JSON(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d policies, want 3", len(result))
	}
}

func TestPoliciesList_Table(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Disk Encryption") {
		t.Errorf("table output should contain 'Disk Encryption', got:\n%s", out)
	}
}

func TestPoliciesList_CSV(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "list", "--output", "csv"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Disk Encryption") {
		t.Errorf("CSV output should contain 'Disk Encryption', got:\n%s", out)
	}
}

func TestPoliciesList_IDs(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "list", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d ID lines, want 3", len(lines))
	}
	if lines[0] != "aabbccddee112233aabb1001" {
		t.Errorf("first ID = %q, want aabbccddee112233aabb1001", lines[0])
	}
}

func TestPoliciesList_Quiet(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "list", "--quiet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("quiet output should be empty, got: %q", buf.String())
	}
}

func TestPoliciesList_Footer(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"policies", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errBuf.String()
	if !strings.Contains(footer, "3 items") {
		t.Errorf("footer should contain '3 items', got: %q", footer)
	}
}

func TestPoliciesList_Empty(t *testing.T) {
	setupUsersTest(t)
	ts := startPoliciesServer(t, []map[string]any{}, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 0 {
		t.Errorf("got %d policies, want 0", len(result))
	}
}

func TestPoliciesList_Filter(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "list", "--filter", "os=darwin"})

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

func TestPoliciesList_Sort(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "list", "--sort", "-name"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
}

func TestPoliciesList_InvalidFilter(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "list", "--filter", "badfilter"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid filter, got nil")
	}
}

func TestPoliciesList_Limit(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "list", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) > 2 {
		t.Errorf("got %d policies, want at most 2", len(result))
	}
}

// --- Get Tests ---

func TestPoliciesGet_ByID(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "get", "aabbccddee112233aabb1001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "Disk Encryption" {
		t.Errorf("name = %q, want 'Disk Encryption'", result["name"])
	}
}

func TestPoliciesGet_ByName(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "get", "Disk Encryption"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb1001" {
		t.Errorf("id = %q, want 'aabbccddee112233aabb1001'", result["id"])
	}
}

func TestPoliciesGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	ts := startPoliciesServer(t, policies, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "get", "NonExistentPolicy"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found policy, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestPoliciesGet_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "get"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing arg, got nil")
	}
}

// --- Results Tests ---

func TestPoliciesResults_JSON(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	statuses := samplePolicyStatuses()
	ts := startPoliciesServer(t, policies, statuses)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "results", "aabbccddee112233aabb1001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d results, want 2", len(result))
	}
}

func TestPoliciesResults_Table(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	statuses := samplePolicyStatuses()
	ts := startPoliciesServer(t, policies, statuses)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "results", "aabbccddee112233aabb1001", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "applied") {
		t.Errorf("table output should contain 'applied', got:\n%s", out)
	}
	if !strings.Contains(out, "failed") {
		t.Errorf("table output should contain 'failed', got:\n%s", out)
	}
}

func TestPoliciesResults_Footer(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	statuses := samplePolicyStatuses()
	ts := startPoliciesServer(t, policies, statuses)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"policies", "results", "aabbccddee112233aabb1001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errBuf.String()
	if !strings.Contains(footer, "2 items") {
		t.Errorf("footer should contain '2 items', got: %q", footer)
	}
}

func TestPoliciesResults_Limit(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	statuses := samplePolicyStatuses()
	ts := startPoliciesServer(t, policies, statuses)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "results", "aabbccddee112233aabb1001", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) > 1 {
		t.Errorf("got %d results, want at most 1", len(result))
	}
}

func TestPoliciesResults_ByName(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	statuses := samplePolicyStatuses()
	ts := startPoliciesServer(t, policies, statuses)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "results", "Disk Encryption"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d results, want 2", len(result))
	}
}

func TestPoliciesResults_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "results"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing arg, got nil")
	}
}

func TestPoliciesResults_Empty(t *testing.T) {
	setupUsersTest(t)
	policies := samplePolicies()
	// No statuses for this policy.
	ts := startPoliciesServer(t, policies, map[string][]map[string]any{})
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "results", "aabbccddee112233aabb1001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 0 {
		t.Errorf("got %d results, want 0", len(result))
	}
}

// --- Create Tests ---

func TestPoliciesCreate(t *testing.T) {
	setupUsersTest(t)
	ts := startPoliciesServer(t, samplePolicies(), nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "create", "--name", "Test Policy", "--template-id", "tmpl001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "Test Policy" {
		t.Errorf("name = %q, want 'Test Policy'", result["name"])
	}
	if result["id"] != "newpolicynewpolicynewpo1" {
		t.Errorf("id = %q, want 'newpolicynewpolicynewpo1'", result["id"])
	}
}

func TestPoliciesCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startPoliciesServer(t, samplePolicies(), nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "create", "--name", "Test", "--template-id", "tmpl001", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
	if exitErr.Code != 10 {
		t.Errorf("exit code = %d, want 10", exitErr.Code)
	}
}

func TestPoliciesCreate_MissingName(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "create", "--template-id", "tmpl001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --name, got nil")
	}
}

// --- Update Tests ---

func TestPoliciesUpdate(t *testing.T) {
	setupUsersTest(t)
	ts := startPoliciesServer(t, samplePolicies(), nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "update", "aabbccddee112233aabb1001", "--name", "New Name"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "New Name" {
		t.Errorf("name = %q, want 'New Name'", result["name"])
	}
}

func TestPoliciesUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "update", "aabbccddee112233aabb1001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should mention 'no fields to update', got: %v", err)
	}
}

func TestPoliciesUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startPoliciesServer(t, samplePolicies(), nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "update", "aabbccddee112233aabb1001", "--name", "New Name", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
	if exitErr.Code != 10 {
		t.Errorf("exit code = %d, want 10", exitErr.Code)
	}
}

// --- Delete Tests ---

func TestPoliciesDelete_Force(t *testing.T) {
	setupUsersTest(t)
	ts := startPoliciesServer(t, samplePolicies(), nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "delete", "aabbccddee112233aabb1001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestPoliciesDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startPoliciesServer(t, samplePolicies(), nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "delete", "aabbccddee112233aabb1001", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
	if exitErr.Code != 10 {
		t.Errorf("exit code = %d, want 10", exitErr.Code)
	}
}

func TestPoliciesDelete_NotFound(t *testing.T) {
	setupUsersTest(t)
	ts := startPoliciesServer(t, samplePolicies(), nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "delete", "aabbccddee112233aabb9999", "--force"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found policy, got nil")
	}
}

func TestPoliciesDelete_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "delete"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing arg, got nil")
	}
}

// --- Help Tests ---

func TestPoliciesHelp_Subcommands(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, sub := range []string{"list", "get", "create", "update", "delete", "results"} {
		if !strings.Contains(out, sub) {
			t.Errorf("help should contain subcommand %q, got:\n%s", sub, out)
		}
	}
}

func TestPoliciesHelp_ListFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "list", "--help"})

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

func TestPoliciesHelp_ResultsFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"policies", "results", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, flag := range []string{"--limit", "--sort"} {
		if !strings.Contains(out, flag) {
			t.Errorf("results help should contain flag %q, got:\n%s", flag, out)
		}
	}
}

func TestPoliciesHelp_RootIncludesPolicies(t *testing.T) {
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
	if !strings.Contains(out, "policies") {
		t.Errorf("root help should contain 'policies', got:\n%s", out)
	}
}
