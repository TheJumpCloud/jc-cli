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

func TestGraphTraverse_UserToApplication(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"users:aa11bb22cc33dd44ee550001": {
			"application": {
				{"to": map[string]any{"type": "application", "id": "aabbccddee112233aabb2001"}},
				{"to": map[string]any{"type": "application", "id": "aabbccddee112233aabb2002"}},
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
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "application"})

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

	// Verify flattened structure: top-level "type" and "id", no nested "to".
	for i, item := range result {
		if _, ok := item["to"]; ok {
			t.Errorf("result[%d] should not have nested 'to' key after flattening", i)
		}
		if _, ok := item["type"]; !ok {
			t.Errorf("result[%d] missing flattened 'type' key", i)
		}
		if _, ok := item["id"]; !ok {
			t.Errorf("result[%d] missing flattened 'id' key", i)
		}
	}
}

func TestGraphTraverse_UserByName(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"users:aa11bb22cc33dd44ee550001": {
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
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:jdoe", "--to", "application"})

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

func TestGraphTraverse_DeviceToUserGroup(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"systems:bb11cc22dd33ee44ff550001": {
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
	cmd.SetArgs([]string{"graph", "traverse", "--from", "device:bb11cc22dd33ee44ff550001", "--to", "user_group"})

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
	cmd.SetArgs([]string{"graph", "traverse", "--from", "device:JDOE-MBP", "--to", "user_group"})

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
			"user": {
				{"to": map[string]any{"type": "user", "id": "aa11bb22cc33dd44ee550001"}},
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
	cmd.SetArgs([]string{"graph", "traverse", "--from", "device_group:macOS Fleet", "--to", "user"})

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
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "application"})

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
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "application", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Fatal("expected non-empty table output")
	}
	// Flattened associations should produce TYPE and ID column headers.
	if !strings.Contains(out, "TYPE") {
		t.Errorf("table output missing 'TYPE' column header:\n%s", out)
	}
	if !strings.Contains(out, "ID") {
		t.Errorf("table output missing 'ID' column header:\n%s", out)
	}
	// Should contain the actual data values.
	if !strings.Contains(out, "application") {
		t.Errorf("table output missing 'application' value:\n%s", out)
	}
	if !strings.Contains(out, "aabbccddee112233aabb2001") {
		t.Errorf("table output missing association ID:\n%s", out)
	}
}

func TestGraphTraverse_Footer(t *testing.T) {
	setupGraphTest(t)

	associations := map[string]map[string][]map[string]any{
		"users:aa11bb22cc33dd44ee550001": {
			"application": {
				{"to": map[string]any{"type": "application", "id": "aabbccddee112233aabb2001"}},
				{"to": map[string]any{"type": "application", "id": "aabbccddee112233aabb2002"}},
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
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "application"})

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
			"application": {
				{"id": "aabbccddee112233aabb2001", "to": map[string]any{"type": "application", "id": "aabbccddee112233aabb2001"}},
				{"id": "aabbccddee112233aabb2002", "to": map[string]any{"type": "application", "id": "aabbccddee112233aabb2002"}},
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
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "application", "--ids"})

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
	// Error should mention the source type and its valid targets.
	if !strings.Contains(err.Error(), "for source \"user\"") {
		t.Errorf("error should mention source type: %v", err)
	}
	if !strings.Contains(err.Error(), "application") {
		t.Errorf("error should list valid targets for user: %v", err)
	}
}

func TestGraphTraverse_InvalidTargetForSource(t *testing.T) {
	setupGraphTest(t)

	// "command" is a valid target for device, but NOT for user.
	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:jdoe", "--to", "command"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error: command is not a valid target for user source")
	}
	if !strings.Contains(err.Error(), "invalid target type") {
		t.Errorf("error should mention invalid target type: %v", err)
	}
	if !strings.Contains(err.Error(), "for source \"user\"") {
		t.Errorf("error should mention source type: %v", err)
	}
}

func TestGraphTraverse_EmptyFromIdentifier(t *testing.T) {
	setupGraphTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:", "--to", "application"})

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
	cmd.SetArgs([]string{"graph", "traverse", "--from", "user:aa11bb22cc33dd44ee550001", "--to", "application"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if requestedPath != "/users/aa11bb22cc33dd44ee550001/associations" {
		t.Errorf("unexpected API path: %s", requestedPath)
	}
	if requestedTargets != "application" {
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
	cmd.SetArgs([]string{"graph", "traverse", "--from", "device_group:dd11ee22ff33dd11ee220001", "--to", "user"})

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
	// Each source type should accept all its own valid targets.
	for src, targets := range validTargetsBySource {
		for _, tgt := range targets {
			if !isValidTargetType(src, tgt) {
				t.Errorf("isValidTargetType(%q, %q) = false, want true", src, tgt)
			}
		}
	}
	// User-friendly aliases should also be accepted.
	if !isValidTargetType("user", "device") {
		t.Error("isValidTargetType(\"user\", \"device\") = false, want true (alias for system)")
	}
	if !isValidTargetType("user", "device_group") {
		t.Error("isValidTargetType(\"user\", \"device_group\") = false, want true (alias for system_group)")
	}
	// Invalid target for any source.
	if isValidTargetType("user", "invalid") {
		t.Error("isValidTargetType(\"user\", \"invalid\") = true, want false")
	}
	// Valid target but wrong source.
	if isValidTargetType("application", "command") {
		t.Error("isValidTargetType(\"application\", \"command\") = true, want false")
	}
}

func TestGraphTraverse_TargetAliasMapping(t *testing.T) {
	setupGraphTest(t)

	tests := []struct {
		name       string
		fromSource string // source type for --from (must support the target)
		fromID     string
		toFlag     string
		wantParam  string
	}{
		{"device maps to system", "user", "aa11bb22cc33dd44ee550001", "device", "system"},
		{"device_group maps to system_group", "user", "aa11bb22cc33dd44ee550001", "device_group", "system_group"},
		{"system passes through", "user", "aa11bb22cc33dd44ee550001", "system", "system"},
		{"user_group passes through", "device", "bb11cc22dd33ee44ff550001", "user_group", "user_group"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupGraphTest(t)

			var requestedTargets string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "/associations") {
					requestedTargets = r.URL.Query().Get("targets")
					json.NewEncoder(w).Encode([]map[string]any{})
					return
				}
				if r.URL.Path == "/systemusers" {
					json.NewEncoder(w).Encode(map[string]any{"results": []any{}, "totalCount": 0})
					return
				}
				if r.URL.Path == "/systems" {
					json.NewEncoder(w).Encode(map[string]any{"results": []any{}, "totalCount": 0})
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
			cmd.SetArgs([]string{"graph", "traverse", "--from", tt.fromSource + ":" + tt.fromID, "--to", tt.toFlag})

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute error: %v", err)
			}

			if requestedTargets != tt.wantParam {
				t.Errorf("targets param = %q, want %q", requestedTargets, tt.wantParam)
			}
		})
	}
}

func TestFlattenAssociations(t *testing.T) {
	input := []json.RawMessage{
		json.RawMessage(`{"to":{"type":"user_group","id":"abc123"}}`),
		json.RawMessage(`{"to":{"type":"system","id":"def456"},"attributes":{"extra":true}}`),
		json.RawMessage(`{"no_to_key":"value"}`),
	}

	result := flattenAssociations(input)

	if len(result) != 3 {
		t.Fatalf("got %d results, want 3", len(result))
	}

	// First: simple flatten.
	var m0 map[string]any
	json.Unmarshal(result[0], &m0)
	if m0["type"] != "user_group" {
		t.Errorf("result[0] type = %v, want user_group", m0["type"])
	}
	if m0["id"] != "abc123" {
		t.Errorf("result[0] id = %v, want abc123", m0["id"])
	}
	if _, ok := m0["to"]; ok {
		t.Error("result[0] should not have 'to' key")
	}

	// Second: flatten with extra top-level key preserved.
	var m1 map[string]any
	json.Unmarshal(result[1], &m1)
	if m1["type"] != "system" {
		t.Errorf("result[1] type = %v, want system", m1["type"])
	}
	if m1["attributes"] == nil {
		t.Error("result[1] should preserve 'attributes' key")
	}

	// Third: no "to" key — passed through unchanged.
	var m2 map[string]any
	json.Unmarshal(result[2], &m2)
	if m2["no_to_key"] != "value" {
		t.Errorf("result[2] should pass through unchanged: %v", m2)
	}
}
