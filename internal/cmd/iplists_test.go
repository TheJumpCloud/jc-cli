package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startIPListsServer creates a mock JumpCloud V2 server that handles /iplists endpoints.
func startIPListsServer(t *testing.T, ipLists []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /iplists — list endpoint.
		if r.URL.Path == "/iplists" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(ipLists)
			return
		}

		// POST /iplists — create endpoint.
		if r.URL.Path == "/iplists" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "new123new123new123new123"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /iplists/{id}.
		if strings.HasPrefix(r.URL.Path, "/iplists/") {
			id := strings.TrimPrefix(r.URL.Path, "/iplists/")

			var found map[string]any
			for _, ipl := range ipLists {
				if ipl["id"] == id {
					found = ipl
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

func sampleIPLists() []map[string]any {
	return []map[string]any{
		{
			"id":          "aabbccddee112233aabb2001",
			"name":        "Office IPs",
			"description": "Corporate office IP ranges",
			"ips":         []any{"10.0.0.0/24", "192.168.1.0/24"},
		},
		{
			"id":          "aabbccddee112233aabb2002",
			"name":        "VPN IPs",
			"description": "VPN egress addresses",
			"ips":         []any{"203.0.113.0/24"},
		},
		{
			"id":          "aabbccddee112233aabb2003",
			"name":        "Blocked IPs",
			"description": "Known bad actors",
			"ips":         []any{"198.51.100.1", "198.51.100.2"},
		},
	}
}

// --- List Tests ---

func TestIPListsList_JSON(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d IP lists, want 3", len(result))
	}
}

func TestIPListsList_Table(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Office IPs") {
		t.Errorf("table output should contain 'Office IPs', got:\n%s", out)
	}
}

func TestIPListsList_Footer(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"iplists", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errBuf.String()
	if !strings.Contains(footer, "3 items") {
		t.Errorf("footer should contain '3 items', got: %q", footer)
	}
}

func TestIPListsList_Empty(t *testing.T) {
	setupUsersTest(t)
	ts := startIPListsServer(t, []map[string]any{})
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 0 {
		t.Errorf("got %d IP lists, want 0", len(result))
	}
}

func TestIPListsList_IDs(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "list", "--ids"})

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

// --- Get Tests ---

func TestIPListsGet_ByID(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "get", "aabbccddee112233aabb2001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "Office IPs" {
		t.Errorf("name = %q, want 'Office IPs'", result["name"])
	}
}

func TestIPListsGet_ByName(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "get", "Office IPs"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb2001" {
		t.Errorf("id = %q, want 'aabbccddee112233aabb2001'", result["id"])
	}
}

func TestIPListsGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "get", "NonExistentList"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found IP list, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestIPListsGet_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "get"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing arg, got nil")
	}
}

// --- Create Tests ---

func TestIPListsCreate(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "create", "--name", "New List", "--ips", "10.0.0.1,10.0.0.2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "New List" {
		t.Errorf("name = %q, want 'New List'", result["name"])
	}
	if result["id"] != "new123new123new123new123" {
		t.Errorf("id = %q, want 'new123new123new123new123'", result["id"])
	}
}

func TestIPListsCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "create", "--name", "Test List", "--ips", "10.0.0.1", "--plan"})

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

func TestIPListsCreate_MissingRequired(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "create", "--name", "Test"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --ips, got nil")
	}
}

// --- Update Tests ---

func TestIPListsUpdate(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "update", "aabbccddee112233aabb2001", "--name", "Updated IPs"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "Updated IPs" {
		t.Errorf("name = %q, want 'Updated IPs'", result["name"])
	}
}

func TestIPListsUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "update", "aabbccddee112233aabb2001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should mention 'no fields to update', got: %v", err)
	}
}

func TestIPListsUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "update", "aabbccddee112233aabb2001", "--name", "New Name", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

// --- Delete Tests ---

func TestIPListsDelete_Force(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "delete", "aabbccddee112233aabb2001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestIPListsDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "delete", "aabbccddee112233aabb2001", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestIPListsDelete_NotFound(t *testing.T) {
	setupUsersTest(t)
	ipLists := sampleIPLists()
	ts := startIPListsServer(t, ipLists)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "delete", "NonExistentList", "--force"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found IP list, got nil")
	}
}

func TestIPListsDelete_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "delete"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing arg, got nil")
	}
}

// --- Help Tests ---

func TestIPListsHelp_Subcommands(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"iplists", "--help"})

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

// --- parseIPFlag Tests ---

func TestParseIPFlag(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"10.0.0.1", 1},
		{"10.0.0.1,10.0.0.2", 2},
		{"10.0.0.1, 10.0.0.2, 10.0.0.3", 3},
		{"10.0.0.0/24", 1},
		{"", 0},
	}

	for _, tt := range tests {
		result := parseIPFlag(tt.input)
		if len(result) != tt.want {
			t.Errorf("parseIPFlag(%q) = %d entries, want %d", tt.input, len(result), tt.want)
		}
	}
}
