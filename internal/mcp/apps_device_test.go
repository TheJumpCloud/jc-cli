package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startDeviceViewServer mounts V1 + V2 + Insights endpoints used by
// fetchDeviceViewData on a single httptest server, with deterministic
// fixtures. The shape mirrors startUserViewServer in apps_user_test.go
// — same control points so changes to one keep the other discoverable.
func startDeviceViewServer(
	t *testing.T,
	device map[string]any,
	groups []map[string]any,
	policyAssocs []map[string]any,
	policyCatalog []map[string]any,
	uptime []map[string]any,
	loggedInUsers []map[string]any,
	disks []map[string]any,
	events []map[string]any,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		// V1: device search by hostname (resolver path).
		case r.URL.Path == "/api/search/systems" && r.Method == "POST":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results":    []map[string]any{device},
				"totalCount": 1,
			})
		case r.URL.Path == "/api/systems" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results":    []map[string]any{device},
				"totalCount": 1,
			})
		case strings.HasPrefix(r.URL.Path, "/api/systems/") && r.Method == "GET":
			// Device detail by ID.
			_ = json.NewEncoder(w).Encode(device)

		// V2: device → group memberships.
		case strings.HasPrefix(r.URL.Path, "/api/v2/systems/") && strings.HasSuffix(r.URL.Path, "/memberof") && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(groups)

		// V2: device → policy associations.
		case strings.HasPrefix(r.URL.Path, "/api/v2/systems/") && strings.HasSuffix(r.URL.Path, "/associations") && r.Method == "GET":
			// Filter by ?targets=policy is assumed; the fixture serves
			// the same associations regardless. (V2 API does the real
			// filtering in production; tests only need the shape.)
			_ = json.NewEncoder(w).Encode(policyAssocs)

		// V2: policy catalog for name lookup.
		case r.URL.Path == "/api/v2/policies" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(policyCatalog)

		// V2: systeminsights tables. Each table responds with its
		// dedicated fixture; the system_id filter passes through
		// query parameters but the test server just returns the rows.
		case r.URL.Path == "/api/v2/systeminsights/uptime" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(uptime)
		case r.URL.Path == "/api/v2/systeminsights/logged_in_users" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(loggedInUsers)
		case r.URL.Path == "/api/v2/systeminsights/disk_info" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(disks)

		// Insights events (POST) — filtered by system.id; the fixture
		// is returned unconditionally so the test doesn't depend on
		// the body shape.
		case r.URL.Path == "/insights/directory/v1/events" && r.Method == "POST":
			_ = json.NewEncoder(w).Encode(events)

		default:
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]string{"path": r.URL.Path, "method": r.Method})
		}
	}))
}

func TestConnectivityBucket(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		lastContact string
		want        string
	}{
		{"online <1h", now.Add(-30 * time.Minute).Format(time.RFC3339), "online"},
		{"recent <24h", now.Add(-6 * time.Hour).Format(time.RFC3339), "recent"},
		{"stale <7d", now.Add(-3 * 24 * time.Hour).Format(time.RFC3339), "stale"},
		{"offline >=7d", now.Add(-30 * 24 * time.Hour).Format(time.RFC3339), "offline"},
		{"empty string", "", "offline"},
		{"bad timestamp", "not a timestamp", "offline"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := connectivityBucket(c.lastContact, now); got != c.want {
				t.Errorf("connectivityBucket(%q) = %q, want %q", c.lastContact, got, c.want)
			}
		})
	}
}

func TestFetchDeviceViewData_Aggregates(t *testing.T) {
	setupToolTest(t)

	// Anchor "now" so the recent-events window and the connectivity
	// bucket calculation are both deterministic.
	origNow := nowFunc
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = origNow })

	device := map[string]any{
		"_id":           "bb11cc22dd33ee44ff550001",
		"displayName":   "Alice's MBP",
		"hostname":      "alice-mbp",
		"os":            "Mac OS X",
		"version":       "14.4.1",
		"serialNumber":  "C02ABCDEFG12",
		"lastContact":   now.Add(-30 * time.Minute).Format(time.RFC3339), // online
		"agentVersion":  "1.110.0",
		"created":       "2025-01-15T10:00:00Z",
		"active":        true,
		"fde":           map[string]any{"active": true},
		"mdmEnrollment": map[string]any{"enrolled": true},
	}
	groups := []map[string]any{
		{"id": "sg-eng", "attributes": map[string]any{"name": "Engineering Macs"}},
		{"id": "sg-fde", "attributes": map[string]any{"name": "FDE Required"}},
	}
	policyAssocs := []map[string]any{
		{"to": map[string]any{"type": "policy", "id": "pol-fde"}},
		{"to": map[string]any{"type": "policy", "id": "pol-fw"}},
		{"to": map[string]any{"type": "policy", "id": "pol-unknown"}}, // not in catalog
	}
	policyCatalog := []map[string]any{
		{"id": "pol-fde", "name": "FDE Required"},
		{"id": "pol-fw", "name": "Firewall On"},
		// pol-unknown deliberately omitted to exercise the missing-name fallback.
	}
	uptime := []map[string]any{
		{"total_seconds": 95400}, // 1d 2h
	}
	loggedInUsers := []map[string]any{
		{"user": "alice", "type": "user", "time": "2026-05-20T08:00:00Z", "host": "console"},
	}
	disks := []map[string]any{
		{"name": "/dev/disk1s1", "mountpoint": "/", "size": int64(500_000_000_000), "free": int64(120_000_000_000)},
	}
	events := []map[string]any{
		{"timestamp": "2026-05-19T11:00:00Z", "service": "directory", "event_type": "system_update", "success": true},
	}

	ts := startDeviceViewServer(t, device, groups, policyAssocs, policyCatalog, uptime, loggedInUsers, disks, events)
	t.Cleanup(ts.Close)
	overrideV1ClientForTest(t, ts.URL)
	overrideV2ClientForTest(t, ts.URL)
	overrideInsightsClientForTest(t, ts.URL)

	data, err := fetchDeviceViewData(context.Background(), deviceViewArgs{Device: "alice-mbp"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	// Header
	if data.Device.Hostname != "alice-mbp" {
		t.Errorf("hostname = %q, want alice-mbp", data.Device.Hostname)
	}
	if data.Device.OS != "Mac OS X" || data.Device.OSVersion != "14.4.1" {
		t.Errorf("OS = %q/%q", data.Device.OS, data.Device.OSVersion)
	}
	if data.Device.SerialNumber != "C02ABCDEFG12" {
		t.Errorf("serial = %q", data.Device.SerialNumber)
	}
	if data.Device.AgentVersion != "1.110.0" {
		t.Errorf("agent = %q", data.Device.AgentVersion)
	}

	// Status badges
	if !data.Status.Active {
		t.Errorf("status.active = false, want true")
	}
	if data.Status.Connectivity != "online" {
		t.Errorf("connectivity = %q, want online", data.Status.Connectivity)
	}
	if !data.Status.FDEEnabled || !data.Status.MDMEnrolled {
		t.Errorf("status flags wrong: %+v", data.Status)
	}

	// Groups: sorted alphabetically.
	if len(data.Groups) != 2 {
		t.Fatalf("groups len = %d (%+v)", len(data.Groups), data.Groups)
	}
	if data.Groups[0].Name != "Engineering Macs" || data.Groups[1].Name != "FDE Required" {
		t.Errorf("groups not sorted: %+v", data.Groups)
	}

	// Policies: 3 associations, 2 with catalog names, 1 with empty name.
	// Named policies should sort before unnamed.
	if len(data.Policies) != 3 {
		t.Fatalf("policies len = %d (%+v)", len(data.Policies), data.Policies)
	}
	if data.Policies[0].Name == "" || data.Policies[1].Name == "" {
		t.Errorf("named policies should sort first, got: %+v", data.Policies)
	}
	if data.Policies[2].Name != "" || data.Policies[2].ID != "pol-unknown" {
		t.Errorf("unnamed policy should sort last and keep its ID, got: %+v", data.Policies[2])
	}

	// System insights snapshot
	if data.SystemInsights == nil {
		t.Fatal("system_insights nil; expected populated snapshot")
	}
	if data.SystemInsights.UptimeSeconds != 95400 {
		t.Errorf("uptime = %d, want 95400", data.SystemInsights.UptimeSeconds)
	}
	if len(data.SystemInsights.LoggedInUsers) != 1 || data.SystemInsights.LoggedInUsers[0].User != "alice" {
		t.Errorf("logged_in_users = %+v", data.SystemInsights.LoggedInUsers)
	}
	if len(data.SystemInsights.Disks) != 1 || data.SystemInsights.Disks[0].Mountpoint != "/" {
		t.Errorf("disks = %+v", data.SystemInsights.Disks)
	}

	// Insights events
	if len(data.RecentEvents) != 1 {
		t.Errorf("recent_events len = %d, want 1", len(data.RecentEvents))
	}
}

// A disk that is completely full reports FreeBytes == 0. Marshal must
// emit `"free_bytes":0` (no `omitempty`) so the iframe's number-check
// passes and the usage bar fills to 100 % — Bugbot caught this on
// PR #29. Pin the contract here so a future omitempty regression
// fails loudly.
func TestDeviceInsightsDisk_FullDiskKeepsFreeBytesField(t *testing.T) {
	full := deviceInsightsDisk{
		Name: "/dev/sda1", Mountpoint: "/",
		SizeBytes: 500_000_000_000,
		FreeBytes: 0,
	}
	b, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"free_bytes":0`) {
		t.Errorf("full-disk row should serialize free_bytes:0; got: %s", b)
	}

	// And confirm a disk with no size metadata still drops size_bytes
	// (that's the case where omitempty is correct).
	missing := deviceInsightsDisk{Name: "/dev/sda1"}
	b, err = json.Marshal(missing)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"size_bytes"`) {
		t.Errorf("missing-size disk should omit size_bytes; got: %s", b)
	}
}

func TestFetchDeviceViewData_RequiresDevice(t *testing.T) {
	_, err := fetchDeviceViewData(context.Background(), deviceViewArgs{})
	if err == nil || !strings.Contains(err.Error(), "device is required") {
		t.Errorf("expected device-required error, got: %v", err)
	}
}

func TestFetchDeviceViewData_StaleAndOfflineBuckets(t *testing.T) {
	// Smoke test: the connectivity bucket plumbing wires through to
	// the Status field, not just unit-tested in isolation.
	setupToolTest(t)
	origNow := nowFunc
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = origNow })

	device := map[string]any{
		"_id":         "bb11cc22dd33ee44ff550002",
		"hostname":    "stale-host",
		"os":          "Linux",
		"lastContact": now.Add(-3 * 24 * time.Hour).Format(time.RFC3339), // stale bucket
		"active":      true,
	}
	ts := startDeviceViewServer(t, device, nil, nil, nil, nil, nil, nil, nil)
	t.Cleanup(ts.Close)
	overrideV1ClientForTest(t, ts.URL)
	overrideV2ClientForTest(t, ts.URL)
	overrideInsightsClientForTest(t, ts.URL)

	data, err := fetchDeviceViewData(context.Background(), deviceViewArgs{Device: "stale-host"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if data.Status.Connectivity != "stale" {
		t.Errorf("connectivity = %q, want stale", data.Status.Connectivity)
	}
}

func TestDeviceView_HasUIMetadata(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var found *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == "device_view" {
			found = tool
			break
		}
	}
	if found == nil {
		t.Fatal("device_view tool missing")
	}
	meta := map[string]any(found.Meta)
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("expected _meta.ui to be a map, got %T", meta["ui"])
	}
	if uri, _ := ui["resourceUri"].(string); uri != deviceViewResourceURI {
		t.Errorf("resourceUri = %q, want %q", uri, deviceViewResourceURI)
	}
}

func TestDeviceViewResource_ServesHTMLWithInjection(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: deviceViewResourceURI})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(result.Contents) == 0 {
		t.Fatal("empty resource contents")
	}
	c := result.Contents[0]
	if c.MIMEType != mcpAppMIMEType {
		t.Errorf("MIME = %q, want %q", c.MIMEType, mcpAppMIMEType)
	}
	if !strings.Contains(c.Text, "window.jcApp") {
		t.Error("served HTML missing common.js injection")
	}
	if strings.Contains(c.Text, appCommonMarker) {
		t.Error("served HTML still contains injection marker")
	}
	if !strings.Contains(c.Text, "JumpCloud Device Inventory") {
		t.Error("served HTML missing page title")
	}
}
