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
)

// startGraphServer creates a mock server that handles both V1 endpoints (for name
// resolution) and V2 graph association endpoints.
func startGraphServer(t *testing.T, associations map[string]map[string][]map[string]any) *httptest.Server {
	t.Helper()

	users := []map[string]any{
		{"_id": "aa11bb22cc33dd44ee550001", "username": "jdoe", "email": "jdoe@acme.com"},
		{"_id": "aa11bb22cc33dd44ee550002", "username": "jsmith", "email": "jsmith@acme.com"},
	}
	devices := []map[string]any{
		{"_id": "bb11cc22dd33ee44ff550001", "hostname": "JDOE-MBP", "os": "Mac OS X"},
	}
	userGroups := []map[string]any{
		{"id": "aabbccddee112233aabb0001", "name": "Engineering", "type": "custom"},
		{"id": "aabbccddee112233aabb0002", "name": "Marketing", "type": "custom"},
	}
	deviceGroups := []map[string]any{
		{"id": "dd11ee22ff33dd11ee220001", "name": "macOS Fleet", "type": "custom"},
	}
	apps := []map[string]any{
		{"_id": "aabbccddee112233aabb2001", "name": "Slack", "ssoType": "oidc"},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// V1: /systemusers — for user name resolution.
		if r.URL.Path == "/systemusers" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"results": users, "totalCount": len(users)})
			return
		}

		// V1: /systems — for device name resolution.
		if r.URL.Path == "/systems" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"results": devices, "totalCount": len(devices)})
			return
		}

		// V2: /usergroups — for group name resolution.
		if r.URL.Path == "/usergroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(userGroups)
			return
		}

		// V2: /systemgroups — for group name resolution.
		if r.URL.Path == "/systemgroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(deviceGroups)
			return
		}

		// V1: /applications — for app name resolution.
		if r.URL.Path == "/applications" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"results": apps, "totalCount": len(apps)})
			return
		}

		// V2 graph: /{resource}/{id}/associations?targets={type}
		if strings.Contains(r.URL.Path, "/associations") && r.Method == http.MethodGet {
			targets := r.URL.Query().Get("targets")

			// Extract the resource path key: e.g., "/users/aa11.../associations" → "users:aa11..."
			path := r.URL.Path
			// Remove "/associations" suffix.
			path = strings.TrimSuffix(path, "/associations")
			// path is now like "/users/{id}"
			parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
			if len(parts) == 2 {
				key := parts[0] + ":" + parts[1]
				if assoc, ok := associations[key]; ok {
					if items, ok := assoc[targets]; ok {
						json.NewEncoder(w).Encode(items)
						return
					}
				}
			}

			// Return empty array for unknown associations.
			json.NewEncoder(w).Encode([]map[string]any{})
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func setupGraphTest(t *testing.T) {
	t.Helper()
	setupUsersTest(t)
	viper.Set("cache.directory", filepath.Join(t.TempDir(), "cache"))
}

// --- Traverse Tests ---

func TestGraphTraverse_UserToUserGroup(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"users:aa11bb22cc33dd44ee550001": {
			"user_group": {
				{"to": map[string]any{"type": "user_group", "id": "aabbccddee112233aabb0001"}},
				{"to": map[string]any{"type": "user_group", "id": "aabbccddee112233aabb0002"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "user_group"})

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
}

func TestGraphTraverse_UserByName(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"users:aa11bb22cc33dd44ee550001": {
			"user_group": {
				{"to": map[string]any{"type": "user_group", "id": "aabbccddee112233aabb0001"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:jdoe", "--to", "user_group"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d associations, want 1", len(result))
	}
}

func TestGraphTraverse_DeviceToSystemGroup(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"systems:bb11cc22dd33ee44ff550001": {
			"system_group": {
				{"to": map[string]any{"type": "system_group", "id": "dd11ee22ff33dd11ee220001"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "device:bb11cc22dd33ee44ff550001", "--to", "system_group"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d associations, want 1", len(result))
	}
}

func TestGraphTraverse_DeviceByName(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"systems:bb11cc22dd33ee44ff550001": {
			"system_group": {
				{"to": map[string]any{"type": "system_group", "id": "dd11ee22ff33dd11ee220001"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "device:JDOE-MBP", "--to", "system_group"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d associations, want 1", len(result))
	}
}

func TestGraphTraverse_UserGroupToApplication(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"usergroups:aabbccddee112233aabb0001": {
			"application": {
				{"to": map[string]any{"type": "application", "id": "aabbccddee112233aabb2001"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user_group:aabbccddee112233aabb0001", "--to", "application"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d associations, want 1", len(result))
	}
}

func TestGraphTraverse_UserGroupByName(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"usergroups:aabbccddee112233aabb0001": {
			"application": {
				{"to": map[string]any{"type": "application", "id": "aabbccddee112233aabb2001"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user_group:Engineering", "--to", "application"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d associations, want 1", len(result))
	}
}

func TestGraphTraverse_DeviceGroupByName(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"systemgroups:dd11ee22ff33dd11ee220001": {
			"system": {
				{"to": map[string]any{"type": "system", "id": "bb11cc22dd33ee44ff550001"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "device_group:macOS Fleet", "--to", "system"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d associations, want 1", len(result))
	}
}

func TestGraphTraverse_ApplicationToUserGroup(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"applications:aabbccddee112233aabb2001": {
			"user_group": {
				{"to": map[string]any{"type": "user_group", "id": "aabbccddee112233aabb0001"}},
				{"to": map[string]any{"type": "user_group", "id": "aabbccddee112233aabb0002"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "application:aabbccddee112233aabb2001", "--to", "user_group"})

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
}

func TestGraphTraverse_ApplicationByName(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"applications:aabbccddee112233aabb2001": {
			"user_group": {
				{"to": map[string]any{"type": "user_group", "id": "aabbccddee112233aabb0001"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "application:Slack", "--to", "user_group"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d associations, want 1", len(result))
	}
}

func TestGraphTraverse_EmptyResult(t *testing.T) {
	setupGraphTest(t)

	ts := startGraphServer(t, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "user_group"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should return empty JSON array.
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("expected empty array, got: %s", buf.String())
	}
}

func TestGraphTraverse_TableOutput(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"users:aa11bb22cc33dd44ee550001": {
			"user_group": {
				{"to": map[string]any{"type": "user_group", "id": "aabbccddee112233aabb0001"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "user_group", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	// Table output should contain the "TO" header and the nested to object data.
	if !strings.Contains(strings.ToUpper(out), "TO") {
		t.Errorf("table output missing 'TO' column header:\n%s", out)
	}
	if out == "" {
		t.Error("expected non-empty table output")
	}
}

func TestGraphTraverse_Footer(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"users:aa11bb22cc33dd44ee550001": {
			"user_group": {
				{"to": map[string]any{"type": "user_group", "id": "aabbccddee112233aabb0001"}},
				{"to": map[string]any{"type": "user_group", "id": "aabbccddee112233aabb0002"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "user_group"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(errBuf.String(), "2 items") {
		t.Errorf("footer missing '2 items': %s", errBuf.String())
	}
}

func TestGraphTraverse_IDsOutput(t *testing.T) {
	setupGraphTest(t)

	// Use associations with top-level "id" field so --ids can extract them.
	associations := map[string]map[string][]map[string]any{
		"users:aa11bb22cc33dd44ee550001": {
			"user_group": {
				{"id": "aabbccddee112233aabb0001", "to": map[string]any{"type": "user_group", "id": "aabbccddee112233aabb0001"}},
				{"id": "aabbccddee112233aabb0002", "to": map[string]any{"type": "user_group", "id": "aabbccddee112233aabb0002"}},
			},
		},
	}

	ts := startGraphServer(t, associations)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "user_group", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 ID lines, got %d: %s", len(lines), out)
	}
}

// --- Error Cases ---

func TestGraphTraverse_MissingFromFlag(t *testing.T) {
	setupGraphTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--to", "user_group"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --from flag")
	}
	if !strings.Contains(err.Error(), "from") {
		t.Errorf("error should mention --from: %v", err)
	}
}

func TestGraphTraverse_MissingToFlag(t *testing.T) {
	setupGraphTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:jdoe"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --to flag")
	}
	if !strings.Contains(err.Error(), "to") {
		t.Errorf("error should mention --to: %v", err)
	}
}

func TestGraphTraverse_InvalidFromFormat(t *testing.T) {
	setupGraphTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "invalid", "--to", "user_group"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --from format")
	}
	if !strings.Contains(err.Error(), "type:name-or-id") {
		t.Errorf("error should suggest correct format: %v", err)
	}
}

func TestGraphTraverse_InvalidFromType(t *testing.T) {
	setupGraphTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "invalid_type:foo", "--to", "user_group"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid source type")
	}
	if !strings.Contains(err.Error(), "invalid source type") {
		t.Errorf("error should mention invalid source type: %v", err)
	}
	// Should list valid types in error message.
	if !strings.Contains(err.Error(), "user") {
		t.Errorf("error should list valid types: %v", err)
	}
}

func TestGraphTraverse_InvalidToType(t *testing.T) {
	setupGraphTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:jdoe", "--to", "invalid_target"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid target type")
	}
	if !strings.Contains(err.Error(), "invalid target type") {
		t.Errorf("error should mention invalid target type: %v", err)
	}
}

func TestGraphTraverse_EmptyFromIdentifier(t *testing.T) {
	setupGraphTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:", "--to", "user_group"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for empty identifier in --from")
	}
	if !strings.Contains(err.Error(), "type:name-or-id") {
		t.Errorf("error should suggest correct format: %v", err)
	}
}

func TestGraphTraverse_APIEndpoint(t *testing.T) {
	setupGraphTest(t)

	var requestedPath, requestedTargets string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "/associations") {
			requestedPath = r.URL.Path
			requestedTargets = r.URL.Query().Get("targets")
			json.NewEncoder(w).Encode([]map[string]any{})
			return
		}

		// Fallback: return empty results for name resolution.
		if r.URL.Path == "/systemusers" {
			json.NewEncoder(w).Encode(map[string]any{"results": []any{}, "totalCount": 0})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "user_group"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if requestedPath != "/users/aa11bb22cc33dd44ee550001/associations" {
		t.Errorf("unexpected API path: %s", requestedPath)
	}
	if requestedTargets != "user_group" {
		t.Errorf("unexpected targets param: %s", requestedTargets)
	}
}

func TestGraphTraverse_DeviceGroupAPIEndpoint(t *testing.T) {
	setupGraphTest(t)

	var requestedPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "/associations") {
			requestedPath = r.URL.Path
			json.NewEncoder(w).Encode([]map[string]any{})
			return
		}

		if r.URL.Path == "/systemgroups" {
			json.NewEncoder(w).Encode([]any{})
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "device_group:dd11ee22ff33dd11ee220001", "--to", "system"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if requestedPath != "/systemgroups/dd11ee22ff33dd11ee220001/associations" {
		t.Errorf("unexpected API path: %s", requestedPath)
	}
}

func TestGraphTraverse_HelpShowsFlags(t *testing.T) {
	setupGraphTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"graph", "traverse", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, expected := range []string{"--from", "--to", "user_group", "application"} {
		if !strings.Contains(out, expected) {
			t.Errorf("help should mention %q: %s", expected, out)
		}
	}
}

func TestGraphCmd_HelpShowsTraverse(t *testing.T) {
	setupGraphTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"graph", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(buf.String(), "traverse") {
		t.Errorf("graph help should show traverse subcommand: %s", buf.String())
	}
}

// --- parseFromFlag unit tests ---

func TestParseFromFlag_Valid(t *testing.T) {
	tests := []struct {
		input      string
		wantType   string
		wantIdent  string
	}{
		{"user:jdoe", "user", "jdoe"},
		{"device:JDOE-MBP", "device", "JDOE-MBP"},
		{"user_group:Engineering", "user_group", "Engineering"},
		{"device_group:macOS Fleet", "device_group", "macOS Fleet"},
		{"application:Slack", "application", "Slack"},
		{"user:aa11bb22cc33dd44ee550001", "user", "aa11bb22cc33dd44ee550001"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			typ, ident, err := parseFromFlag(tt.input)
			if err != nil {
				t.Fatalf("parseFromFlag(%q) error: %v", tt.input, err)
			}
			if typ != tt.wantType {
				t.Errorf("type = %q, want %q", typ, tt.wantType)
			}
			if ident != tt.wantIdent {
				t.Errorf("identifier = %q, want %q", ident, tt.wantIdent)
			}
		})
	}
}

func TestParseFromFlag_Invalid(t *testing.T) {
	tests := []struct {
		input string
	}{
		{""},
		{"nocolon"},
		{":noprefix"},
		{"user:"},
		{"badtype:foo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, _, err := parseFromFlag(tt.input)
			if err == nil {
				t.Errorf("parseFromFlag(%q) should return error", tt.input)
			}
		})
	}
}

func TestIsValidSourceType(t *testing.T) {
	for _, typ := range validSourceTypes {
		if !isValidSourceType(typ) {
			t.Errorf("isValidSourceType(%q) = false, want true", typ)
		}
	}
	if isValidSourceType("invalid") {
		t.Error("isValidSourceType(\"invalid\") = true, want false")
	}
}

func TestIsValidTargetType(t *testing.T) {
	for _, typ := range validTargetTypes {
		if !isValidTargetType(typ) {
			t.Errorf("isValidTargetType(%q) = false, want true", typ)
		}
	}
	if isValidTargetType("invalid") {
		t.Error("isValidTargetType(\"invalid\") = true, want false")
	}
}
