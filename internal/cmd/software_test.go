package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startSoftwareServer creates a mock JumpCloud V2 server that handles /softwareapps endpoints.
func startSoftwareServer(t *testing.T, apps []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /softwareapps — list endpoint.
		if r.URL.Path == "/softwareapps" && r.Method == http.MethodGet {
			// Check for filter query param for simple displayName filtering.
			filters := r.URL.Query()["filter"]
			if len(filters) > 0 {
				var filtered []map[string]any
				for _, app := range apps {
					for _, f := range filters {
						// V2 filter format: displayName:eq:Value
						parts := strings.SplitN(f, ":", 3)
						if len(parts) == 3 && parts[0] == "displayName" && parts[1] == "eq" {
							if app["displayName"] == parts[2] {
								filtered = append(filtered, app)
							}
						}
					}
				}
				json.NewEncoder(w).Encode(filtered)
				return
			}
			json.NewEncoder(w).Encode(apps)
			return
		}

		// POST /softwareapps — create endpoint.
		if r.URL.Path == "/softwareapps" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "new123new123new123new123"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /softwareapps/{id}.
		if strings.HasPrefix(r.URL.Path, "/softwareapps/") {
			id := strings.TrimPrefix(r.URL.Path, "/softwareapps/")

			var found map[string]any
			for _, app := range apps {
				if app["id"] == id {
					found = app
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

func sampleSoftwareApps() []map[string]any {
	return []map[string]any{
		{
			"id":          "aabbccddee112233aabb5001",
			"displayName": "Firefox",
			"settings":    []any{map[string]any{"packageId": "firefox-pkg", "packageManager": "CHOCOLATEY", "desiredState": "INSTALL"}},
			"createdAt":   "2024-01-15T10:00:00Z",
			"updatedAt":   "2024-06-01T12:00:00Z",
		},
		{
			"id":          "aabbccddee112233aabb5002",
			"displayName": "Slack",
			"settings":    []any{map[string]any{"packageId": "slack-pkg", "packageManager": "APPLE_CUSTOM", "desiredState": "INSTALL"}},
			"createdAt":   "2024-02-10T08:00:00Z",
			"updatedAt":   "2024-05-20T15:00:00Z",
		},
	}
}

// --- List Tests ---

func TestSoftwareList_JSON(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d software apps, want 2", len(result))
	}
}

func TestSoftwareList_Limit(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "list", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d software apps, want 1", len(result))
	}
}

func TestSoftwareList_Filter(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "list", "--filter", "displayName=Firefox"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d software apps, want 1", len(result))
	}
	if result[0]["displayName"] != "Firefox" {
		t.Errorf("displayName = %q, want 'Firefox'", result[0]["displayName"])
	}
}

// --- Get Tests ---

func TestSoftwareGet(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "get", "aabbccddee112233aabb5001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["displayName"] != "Firefox" {
		t.Errorf("displayName = %q, want 'Firefox'", result["displayName"])
	}
}

func TestSoftwareGet_ByName(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "get", "Firefox"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb5001" {
		t.Errorf("id = %q, want 'aabbccddee112233aabb5001'", result["id"])
	}
}

func TestSoftwareGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "get", "NonExistentApp"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found software app, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

// --- Create Tests ---

func TestSoftwareCreate(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "create", "--name", "Chrome", "--settings", `{"test":true}`})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["displayName"] != "Chrome" {
		t.Errorf("displayName = %q, want 'Chrome'", result["displayName"])
	}
	if result["id"] != "new123new123new123new123" {
		t.Errorf("id = %q, want 'new123new123new123new123'", result["id"])
	}
}

func TestSoftwareCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "create", "--name", "Chrome", "--plan"})

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

func TestSoftwareCreate_MissingName(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "create"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --name, got nil")
	}
}

// --- Update Tests ---

func TestSoftwareUpdate(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "update", "aabbccddee112233aabb5001", "--name", "New Name"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["displayName"] != "New Name" {
		t.Errorf("displayName = %q, want 'New Name'", result["displayName"])
	}
}

func TestSoftwareUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "update", "aabbccddee112233aabb5001", "--name", "New Name", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestSoftwareUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "update", "aabbccddee112233aabb5001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should mention 'no fields to update', got: %v", err)
	}
}

// --- Delete Tests ---

func TestSoftwareDelete(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "delete", "aabbccddee112233aabb5001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestSoftwareDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "delete", "aabbccddee112233aabb5001", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestSoftwareDelete_Cancel(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	// Override confirmReader to simulate "n" answer.
	orig := confirmReader
	confirmReader = bufio.NewReader(strings.NewReader("n\n"))
	t.Cleanup(func() { confirmReader = orig })

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"software", "delete", "aabbccddee112233aabb5001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "Cancelled.") {
		t.Errorf("stderr should contain 'Cancelled.', got: %q", stderr)
	}
}
