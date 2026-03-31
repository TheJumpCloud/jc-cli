package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startDashboardServer creates a combined V1 + V2 + Insights mock server
// with known test data for the dashboard_view tool.
func startDashboardServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		// V1: users
		case r.URL.Path == "/api/systemusers" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{
				"totalCount": 4,
				"results": []map[string]any{
					{"_id": "aabbccddee112233aabbcc01", "username": "alice", "activated": true, "totp_enabled": true},
					{"_id": "aabbccddee112233aabbcc02", "username": "bob", "activated": true, "totp_enabled": false},
					{"_id": "aabbccddee112233aabbcc03", "username": "carol", "suspended": true, "totp_enabled": true},
					{"_id": "aabbccddee112233aabbcc04", "username": "dave", "account_locked": true, "totp_enabled": false},
				},
			})

		// V1: devices
		case r.URL.Path == "/api/systems" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{
				"totalCount": 3,
				"results": []map[string]any{
					{"_id": "aabbccddee112233aabbcc11", "os": "Mac OS X", "lastContact": "2026-03-31T11:30:00Z"},
					{"_id": "aabbccddee112233aabbcc12", "os": "Windows", "lastContact": "2026-03-30T10:00:00Z"},
					{"_id": "aabbccddee112233aabbcc13", "os": "Linux", "lastContact": "2026-03-01T00:00:00Z"},
				},
			})

		// V1: commands
		case r.URL.Path == "/api/commands" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{
				"totalCount": 5,
				"results":    []map[string]any{},
			})

		// V1: applications
		case r.URL.Path == "/api/applications" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{
				"totalCount": 3,
				"results":    []map[string]any{},
			})

		// V2: user groups
		case r.URL.Path == "/api/v2/usergroups" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "aabbccddee112233aabbcc21", "name": "Engineering"},
				{"id": "aabbccddee112233aabbcc22", "name": "Marketing"},
			})

		// V2: system groups
		case r.URL.Path == "/api/v2/systemgroups" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "aabbccddee112233aabbcc31", "name": "Laptops"},
			})

		// V2: policies
		case r.URL.Path == "/api/v2/policies" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "aabbccddee112233aabbcc41", "name": "FileVault"},
				{"id": "aabbccddee112233aabbcc42", "name": "Firewall"},
			})

		// Insights: event count
		case r.URL.Path == "/insights/directory/v1/events/count" && r.Method == "POST":
			json.NewEncoder(w).Encode(map[string]int{"count": 1234})

		default:
			w.WriteHeader(404)
		}
	}))
}

func TestDashboardTool_Response(t *testing.T) {
	setupToolTest(t)

	origNow := nowFunc
	nowFunc = func() time.Time {
		return time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { nowFunc = origNow })

	ts := startDashboardServer(t)
	t.Cleanup(ts.Close)

	overrideV1ClientForTest(t, ts.URL)
	overrideV2ClientForTest(t, ts.URL)
	overrideInsightsClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "dashboard_view", nil)

	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}

	text := getResultText(t, result)
	var data dashboardData
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse dashboard JSON: %v\n%s", err, text)
	}

	// Users
	if data.Users.Total != 4 {
		t.Errorf("users total: got %d, want 4", data.Users.Total)
	}
	if data.Users.Active != 2 {
		t.Errorf("users active: got %d, want 2", data.Users.Active)
	}
	if data.Users.Suspended != 1 {
		t.Errorf("users suspended: got %d, want 1", data.Users.Suspended)
	}
	if data.Users.Locked != 1 {
		t.Errorf("users locked: got %d, want 1", data.Users.Locked)
	}
	if data.Users.MFAEnabled != 2 {
		t.Errorf("users mfa_enabled: got %d, want 2", data.Users.MFAEnabled)
	}
	if data.Users.MFAPercentage != 50.0 {
		t.Errorf("users mfa_percentage: got %f, want 50.0", data.Users.MFAPercentage)
	}

	// Devices
	if data.Devices.Total != 3 {
		t.Errorf("devices total: got %d, want 3", data.Devices.Total)
	}
	if data.Devices.OSBreakdown["Mac OS X"] != 1 {
		t.Errorf("devices Mac OS X: got %d, want 1", data.Devices.OSBreakdown["Mac OS X"])
	}
	if data.Devices.OSBreakdown["Windows"] != 1 {
		t.Errorf("devices Windows: got %d, want 1", data.Devices.OSBreakdown["Windows"])
	}
	// Connectivity: with now=2026-03-31T12:00:00Z:
	//   Mac: lastContact 30min ago → Online
	//   Windows: lastContact ~26h ago → Stale (>24h, <7d)
	//   Linux: lastContact 30d ago → Offline
	if data.Devices.Connectivity.Online != 1 {
		t.Errorf("devices online: got %d, want 1", data.Devices.Connectivity.Online)
	}
	if data.Devices.Connectivity.Stale != 1 {
		t.Errorf("devices stale: got %d, want 1", data.Devices.Connectivity.Stale)
	}
	if data.Devices.Connectivity.Offline != 1 {
		t.Errorf("devices offline: got %d, want 1", data.Devices.Connectivity.Offline)
	}

	// Resources
	if data.Resources.UserGroups != 2 {
		t.Errorf("user groups: got %d, want 2", data.Resources.UserGroups)
	}
	if data.Resources.DeviceGroups != 1 {
		t.Errorf("device groups: got %d, want 1", data.Resources.DeviceGroups)
	}
	if data.Resources.Commands != 5 {
		t.Errorf("commands: got %d, want 5", data.Resources.Commands)
	}
	if data.Resources.Policies != 2 {
		t.Errorf("policies: got %d, want 2", data.Resources.Policies)
	}
	if data.Resources.Applications != 3 {
		t.Errorf("applications: got %d, want 3", data.Resources.Applications)
	}

	// Events
	if data.Events.Last24h != 1234 {
		t.Errorf("events last_24h: got %d, want 1234", data.Events.Last24h)
	}

	// Timestamp
	if data.Timestamp != "2026-03-31T12:00:00Z" {
		t.Errorf("timestamp: got %s, want 2026-03-31T12:00:00Z", data.Timestamp)
	}
}

func TestDashboardTool_HasUIMetadata(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var found *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == "dashboard_view" {
			found = tool
			break
		}
	}
	if found == nil {
		t.Fatal("dashboard_view tool not found in ListTools result")
	}

	// Verify _meta.ui.resourceUri is set.
	meta := map[string]any(found.Meta)
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("expected _meta.ui to be a map, got %T: %v", meta["ui"], meta)
	}
	uri, ok := ui["resourceUri"].(string)
	if !ok || uri != "ui://jc/dashboard" {
		t.Errorf("expected _meta.ui.resourceUri = %q, got %q", "ui://jc/dashboard", uri)
	}
}

func TestDashboardResource_ServesHTML(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "ui://jc/dashboard",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	if len(result.Contents) == 0 {
		t.Fatal("expected non-empty resource contents")
	}
	content := result.Contents[0]
	if content.MIMEType != "text/html" {
		t.Errorf("expected MIME type text/html, got %q", content.MIMEType)
	}
	if content.Text == "" {
		t.Fatal("expected non-empty HTML content")
	}

	// Verify HTML structural markers.
	for _, marker := range []string{"<html", "</html>", "postMessage", "dashboard_view", "JumpCloud Dashboard"} {
		if !strings.Contains(content.Text, marker) {
			t.Errorf("expected HTML to contain %q", marker)
		}
	}
}

func TestDashboardResource_InResourceList(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}

	found := false
	for _, r := range result.Resources {
		if r.URI == "ui://jc/dashboard" {
			found = true
			if r.MIMEType != "text/html" {
				t.Errorf("expected MIME type text/html, got %q", r.MIMEType)
			}
			break
		}
	}
	if !found {
		t.Error("ui://jc/dashboard not found in ListResources")
	}
}

func TestDashboardTool_Filtering(t *testing.T) {
	setupToolTest(t)

	// Block dashboard tools via glob pattern.
	cs := connectToolTestServer(t, Options{BlockedTools: []string{"dashboard_*"}})

	ctx := context.Background()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range result.Tools {
		if tool.Name == "dashboard_view" {
			t.Error("dashboard_view should be filtered out by BlockedTools pattern")
		}
	}
}

func TestDashboardTool_PartialFailure(t *testing.T) {
	setupToolTest(t)

	origSleep := api.SetRetrySleepFn(func(time.Duration) {})
	t.Cleanup(func() { api.SetRetrySleepFn(origSleep) })

	origNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = origNow })

	// Server where Insights and V2 policies fail, but users/devices/other V2 succeed.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/systemusers":
			json.NewEncoder(w).Encode(map[string]any{
				"totalCount": 2,
				"results": []map[string]any{
					{"_id": "aabbccddee112233aabbcc01", "username": "alice", "activated": true},
					{"_id": "aabbccddee112233aabbcc02", "username": "bob", "activated": true},
				},
			})
		case r.URL.Path == "/api/systems":
			json.NewEncoder(w).Encode(map[string]any{"totalCount": 0, "results": []map[string]any{}})
		case r.URL.Path == "/api/commands":
			json.NewEncoder(w).Encode(map[string]any{"totalCount": 0, "results": []map[string]any{}})
		case r.URL.Path == "/api/applications":
			json.NewEncoder(w).Encode(map[string]any{"totalCount": 0, "results": []map[string]any{}})
		case r.URL.Path == "/api/v2/usergroups":
			json.NewEncoder(w).Encode([]map[string]any{{"id": "aabbccddee112233aabbcc21", "name": "Eng"}})
		case r.URL.Path == "/api/v2/systemgroups":
			json.NewEncoder(w).Encode([]map[string]any{})
		case r.URL.Path == "/api/v2/policies":
			w.WriteHeader(500) // simulate failure
		case r.URL.Path == "/insights/directory/v1/events/count":
			w.WriteHeader(500) // simulate failure
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(ts.Close)

	overrideV1ClientForTest(t, ts.URL)
	overrideV2ClientForTest(t, ts.URL)
	overrideInsightsClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "dashboard_view", nil)

	// Should succeed (partial data available).
	if result.IsError {
		t.Fatalf("expected success with partial data, got error: %s", getResultText(t, result))
	}

	text := getResultText(t, result)
	var data dashboardData
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Sections that succeeded should have real data.
	if data.Users.Total != 2 {
		t.Errorf("users total: got %d, want 2", data.Users.Total)
	}
	if data.Resources.UserGroups != 1 {
		t.Errorf("user groups: got %d, want 1", data.Resources.UserGroups)
	}

	// Failed sections produce zeros but warnings are surfaced.
	if data.Events.Last24h != 0 {
		t.Errorf("events should be 0 on failure, got %d", data.Events.Last24h)
	}
	if len(data.Warnings) == 0 {
		t.Error("expected warnings for partial failures")
	}
}

func TestDashboardTool_TotalFailure(t *testing.T) {
	setupToolTest(t)

	origSleep := api.SetRetrySleepFn(func(time.Duration) {})
	t.Cleanup(func() { api.SetRetrySleepFn(origSleep) })

	origNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = origNow })

	// Server where ALL endpoints fail.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(ts.Close)

	overrideV1ClientForTest(t, ts.URL)
	overrideV2ClientForTest(t, ts.URL)
	overrideInsightsClientForTest(t, ts.URL)

	cs := connectToolTestServer(t, Options{})
	result := callTool(t, cs, "dashboard_view", nil)

	if !result.IsError {
		t.Fatal("expected error when all API calls fail")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "all API calls failed") {
		t.Errorf("expected 'all API calls failed' in error, got: %s", text)
	}
}

func TestDashboardHTML_Integrity(t *testing.T) {
	if dashboardHTML == "" {
		t.Fatal("dashboardHTML embed is empty")
	}

	required := []string{
		"<!DOCTYPE html>",
		"<html",
		"</html>",
		"postMessage",
		"dashboard_view",
		"tools/call",
		"ui/ready",
		"JumpCloud Dashboard",
	}
	for _, s := range required {
		if !strings.Contains(dashboardHTML, s) {
			t.Errorf("embedded HTML missing required content: %q", s)
		}
	}
}
