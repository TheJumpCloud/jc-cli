package cmd

import (
	"bytes"
	"encoding/json"
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

			// GET /policies/{id} — get endpoint.
			if r.Method == http.MethodGet {
				for _, p := range policies {
					if p["id"] == id {
						json.NewEncoder(w).Encode(p)
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
	for _, sub := range []string{"list", "get", "results"} {
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
