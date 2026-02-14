package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sampleADs() []map[string]any {
	return []map[string]any{
		{
			"id":              "aabbccddee112233aabb7001",
			"domain":          "corp.example.com",
			"useCase":         "ADASAUTHORITY",
			"groupsEnabled":   true,
			"delegationState": "ENABLED",
		},
		{
			"id":              "aabbccddee112233aabb7002",
			"domain":          "dev.example.com",
			"useCase":         "ADASAUTHORITY",
			"groupsEnabled":   false,
			"delegationState": "DISABLED",
		},
	}
}

// startADServer creates a mock JumpCloud V2 server that handles /activedirectories endpoints.
func startADServer(t *testing.T, ads []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /activedirectories — list endpoint.
		if r.URL.Path == "/activedirectories" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(ads)
			return
		}

		// POST /activedirectories — create endpoint.
		if r.URL.Path == "/activedirectories" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "new123new123new123new123"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /activedirectories/{id}.
		if strings.HasPrefix(r.URL.Path, "/activedirectories/") {
			id := strings.TrimPrefix(r.URL.Path, "/activedirectories/")

			var found map[string]any
			for _, ad := range ads {
				if ad["id"] == id {
					found = ad
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

// --- List Tests ---

func TestADList_JSON(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d ADs, want 2", len(result))
	}
}

func TestADList_Limit(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "list", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) > 1 {
		t.Errorf("got %d ADs with --limit 1, want at most 1", len(result))
	}
}

// --- Get Tests ---

func TestADGet(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "get", "aabbccddee112233aabb7001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["domain"] != "corp.example.com" {
		t.Errorf("domain = %q, want 'corp.example.com'", result["domain"])
	}
}

func TestADGet_ByDomain(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "get", "corp.example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb7001" {
		t.Errorf("id = %q, want 'aabbccddee112233aabb7001'", result["id"])
	}
}

func TestADGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "get", "aabbccddee112233aabb0099"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found AD, got nil")
	}
}

// --- Create Tests ---

func TestADCreate(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "create", "--domain", "new.example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["domain"] != "new.example.com" {
		t.Errorf("domain = %q, want 'new.example.com'", result["domain"])
	}
	if result["id"] != "new123new123new123new123" {
		t.Errorf("id = %q, want 'new123new123new123new123'", result["id"])
	}
}

func TestADCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "create", "--domain", "new.example.com", "--plan"})

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

func TestADCreate_MissingDomain(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "create"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --domain, got nil")
	}
}

// --- Update Tests ---

func TestADUpdate(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "update", "aabbccddee112233aabb7001", "--use-case", "ADASAUTHORITY"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["useCase"] != "ADASAUTHORITY" {
		t.Errorf("useCase = %q, want 'ADASAUTHORITY'", result["useCase"])
	}
}

func TestADUpdate_GroupsEnabled(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "update", "aabbccddee112233aabb7002", "--groups-enabled"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["groupsEnabled"] != true {
		t.Errorf("groupsEnabled = %v, want true", result["groupsEnabled"])
	}
}

func TestADUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "update", "aabbccddee112233aabb7001", "--use-case", "ADASAUTHORITY", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestADUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "update", "aabbccddee112233aabb7001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should mention 'no fields to update', got: %v", err)
	}
}

// --- Delete Tests ---

func TestADDelete(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "delete", "aabbccddee112233aabb7001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `AD "corp.example.com" deleted successfully.`) {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestADDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ad", "delete", "aabbccddee112233aabb7001", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestADDelete_Cancel(t *testing.T) {
	setupUsersTest(t)
	ads := sampleADs()
	ts := startADServer(t, ads)
	defer ts.Close()
	overrideV2Client(t, ts.URL)
	overrideConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"ad", "delete", "aabbccddee112233aabb7001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "Cancelled.") {
		t.Errorf("stderr should contain 'Cancelled.', got: %q", stderr)
	}
}
