package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
			rest := strings.TrimPrefix(r.URL.Path, "/softwareapps/")
			parts := strings.SplitN(rest, "/", 2)
			id := parts[0]

			// Sub-resource: GET /softwareapps/{id}/statuses
			if len(parts) == 2 && parts[1] == "statuses" && r.Method == http.MethodGet {
				// Check that the app exists.
				var found bool
				for _, app := range apps {
					if app["id"] == id {
						found = true
						break
					}
				}
				if !found {
					w.WriteHeader(http.StatusNotFound)
					w.Write([]byte(`{"message":"Not Found"}`))
					return
				}
				statuses := []map[string]any{
					{"systemId": "aabbccddee112233aabb0001", "status": "installed", "lastUpdate": "2024-06-01T12:00:00Z"},
					{"systemId": "aabbccddee112233aabb0002", "status": "pending", "lastUpdate": "2024-06-02T08:00:00Z"},
				}
				json.NewEncoder(w).Encode(statuses)
				return
			}

			// Sub-resource: GET /softwareapps/{id}/associations?targets=system
			if len(parts) == 2 && parts[1] == "associations" && r.Method == http.MethodGet {
				targets := r.URL.Query().Get("targets")
				if targets != "system" {
					json.NewEncoder(w).Encode([]map[string]any{})
					return
				}
				var found bool
				for _, app := range apps {
					if app["id"] == id {
						found = true
						break
					}
				}
				if !found {
					w.WriteHeader(http.StatusNotFound)
					w.Write([]byte(`{"message":"Not Found"}`))
					return
				}
				associations := []map[string]any{
					{"to": map[string]any{"id": "aabbccddee112233aabb0001", "type": "system"}, "attributes": nil},
					{"to": map[string]any{"id": "aabbccddee112233aabb0002", "type": "system"}, "attributes": nil},
				}
				json.NewEncoder(w).Encode(associations)
				return
			}

			// Sub-resource: POST /softwareapps/{id}/reclaim-licenses
			if len(parts) == 2 && parts[1] == "reclaim-licenses" && r.Method == http.MethodPost {
				var found bool
				for _, app := range apps {
					if app["id"] == id {
						found = true
						break
					}
				}
				if !found {
					w.WriteHeader(http.StatusNotFound)
					w.Write([]byte(`{"message":"Not Found"}`))
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Find the app by ID for GET/PUT/DELETE.
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

// --- Statuses Tests ---

func TestSoftwareStatuses(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "statuses", "aabbccddee112233aabb5001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d statuses, want 2", len(result))
	}
	if result[0]["status"] != "installed" {
		t.Errorf("first status = %q, want 'installed'", result[0]["status"])
	}
}

func TestSoftwareStatuses_NotFound(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "statuses", "NonExistentApp"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found software app, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

// --- Associations Tests ---

func TestSoftwareAssociations(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "associations", "aabbccddee112233aabb5001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d associations, want 2", len(result))
	}
	// Verify nested "to" field is present.
	to, ok := result[0]["to"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'to' field to be a map, got %T", result[0]["to"])
	}
	if to["type"] != "system" {
		t.Errorf("first association to.type = %q, want 'system'", to["type"])
	}
}

// --- Reclaim License Tests ---

func TestSoftwareReclaimLicense(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)
	overrideV1Client(t, ts.URL)

	// Use a 24-char hex device ID so resolver short-circuits without V1 API call.
	deviceID := "aabbccddee112233aabb0001"

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "reclaim-license", "aabbccddee112233aabb5001", "--device", deviceID, "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "License reclaimed successfully") {
		t.Errorf("output should confirm license reclaim, got: %s", out)
	}
}

func TestSoftwareReclaimLicense_Plan(t *testing.T) {
	setupUsersTest(t)
	apps := sampleSoftwareApps()
	ts := startSoftwareServer(t, apps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)
	overrideV1Client(t, ts.URL)

	deviceID := "aabbccddee112233aabb0001"

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "reclaim-license", "aabbccddee112233aabb5001", "--device", deviceID, "--plan"})

	err := cmd.Execute()
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

// --- --file upload Tests ---

func TestValidatePackageExtension(t *testing.T) {
	cases := []struct {
		path    string
		wantErr bool
	}{
		{"MyApp.pkg", false},
		{"installer.MSI", false},
		{"MyApp.ipa", false},
		{"relative/path/MyApp.pkg", false},
		{"MyApp.dmg", true},
		{"MyApp.exe", true},
		{"MyApp.deb", true},
		{"no-extension", true},
	}
	for _, c := range cases {
		err := validatePackageExtension(c.path)
		if (err != nil) != c.wantErr {
			t.Errorf("validatePackageExtension(%q) err=%v, wantErr=%v", c.path, err, c.wantErr)
		}
	}
}

func TestHashFileSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.bin")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	// Known sha256 of "hello".
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	got, err := hashFileSHA256(path)
	if err != nil {
		t.Fatalf("hashFileSHA256 err: %v", err)
	}
	if got != want {
		t.Errorf("hashFileSHA256 = %q, want %q", got, want)
	}
}

// startSoftwareUploadServer mocks the two-step upload flow: POST /softwareapps
// returns an uploadUrl pointing back to /upload/{id} on the same server, the
// PUT to that URL is accepted, and the subsequent GET /softwareapps/{id}
// returns the version status configured by the test.
func startSoftwareUploadServer(t *testing.T, finalStatus, rejectedReason string) (*httptest.Server, *int32) {
	t.Helper()
	var uploads int32
	mux := http.NewServeMux()

	created := map[string]map[string]any{}

	mux.HandleFunc("/softwareapps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id := "upload123upload123upload"
		input["id"] = id
		created[id] = input

		resp := map[string]any{
			"id":          id,
			"displayName": input["displayName"],
			"settings":    input["settings"],
			"uploadUrl":   "http://" + r.Host + "/upload/" + id,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/upload/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		atomic.AddInt32(&uploads, 1)
		// Drain body to exercise streaming code path.
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/softwareapps/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/softwareapps/")
		app, ok := created[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Project finalStatus into settings[0].storedPackage.versions[0] for GET responses.
		out := map[string]any{
			"id":          app["id"],
			"displayName": app["displayName"],
			"settings": []any{map[string]any{
				"storedPackage": map[string]any{
					"versions": []any{map[string]any{
						"status":         finalStatus,
						"rejectedReason": rejectedReason,
					}},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	return httptest.NewServer(mux), &uploads
}

func writePkg(t *testing.T, name, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("writing temp pkg: %v", err)
	}
	return path
}

func TestSoftwareCreate_File_Available(t *testing.T) {
	setupUsersTest(t)
	ts, uploads := startSoftwareUploadServer(t, "AVAILABLE", "")
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	pkg := writePkg(t, "MyApp.pkg", "fake package bytes")

	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"software", "create", "--name", "MyApp", "--file", pkg, "--package-manager", "APPLE_CUSTOM"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v\nstderr: %s", err, errBuf.String())
	}

	if atomic.LoadInt32(uploads) != 1 {
		t.Errorf("expected 1 upload PUT, got %d", atomic.LoadInt32(uploads))
	}
	if !strings.Contains(errBuf.String(), "AVAILABLE") {
		t.Errorf("stderr should mention AVAILABLE, got: %s", errBuf.String())
	}
}

func TestSoftwareCreate_File_Rejected(t *testing.T) {
	setupUsersTest(t)
	ts, _ := startSoftwareUploadServer(t, "REJECTED", "package is not signed")
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	pkg := writePkg(t, "Bad.pkg", "unsigned junk")

	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"software", "create", "--name", "Bad", "--file", pkg, "--package-manager", "APPLE_CUSTOM"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for rejected package, got nil")
	}
	if !strings.Contains(err.Error(), "package is not signed") {
		t.Errorf("error should surface rejectedReason, got: %v", err)
	}
}

func TestSoftwareCreate_File_NoWait(t *testing.T) {
	setupUsersTest(t)
	// finalStatus unused when --no-wait; set to PENDING to prove we don't poll.
	ts, uploads := startSoftwareUploadServer(t, "PENDING", "")
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	pkg := writePkg(t, "MyApp.pkg", "bytes")

	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"software", "create", "--name", "MyApp", "--file", pkg, "--package-manager", "APPLE_CUSTOM", "--no-wait"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v\nstderr: %s", err, errBuf.String())
	}
	if atomic.LoadInt32(uploads) != 1 {
		t.Errorf("expected 1 upload PUT, got %d", atomic.LoadInt32(uploads))
	}
	if strings.Contains(errBuf.String(), "Waiting for server-side validation") {
		t.Errorf("--no-wait should skip polling; stderr contained waiting message: %s", errBuf.String())
	}
}

func TestSoftwareCreate_File_ConflictFlags(t *testing.T) {
	setupUsersTest(t)
	pkg := writePkg(t, "MyApp.pkg", "bytes")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "with --settings",
			args: []string{"software", "create", "--name", "X", "--file", pkg, "--package-manager", "APPLE_CUSTOM", "--settings", "[]"},
			want: "cannot be combined with --settings",
		},
		{
			name: "with --package-id",
			args: []string{"software", "create", "--name", "X", "--file", pkg, "--package-manager", "APPLE_CUSTOM", "--package-id", "foo"},
			want: "cannot be combined with --package-id",
		},
		{
			name: "missing --package-manager",
			args: []string{"software", "create", "--name", "X", "--file", pkg},
			want: "--package-manager is required with --file",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := NewRootCmd()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(c.args)

			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want substring %q", err, c.want)
			}
		})
	}
}

func TestSoftwareCreate_File_BadExtension(t *testing.T) {
	setupUsersTest(t)
	pkg := writePkg(t, "MyApp.dmg", "bytes")

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "create", "--name", "X", "--file", pkg, "--package-manager", "APPLE_CUSTOM"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for .dmg, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported package format") {
		t.Errorf("error = %v, want 'unsupported package format'", err)
	}
}

func TestSoftwareCreate_File_EmptyFile(t *testing.T) {
	setupUsersTest(t)
	pkg := writePkg(t, "MyApp.pkg", "")

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"software", "create", "--name", "X", "--file", pkg, "--package-manager", "APPLE_CUSTOM"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for empty file, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %v, want 'empty'", err)
	}
}
