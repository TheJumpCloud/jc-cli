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

// startInsightsServer serves deterministic fixtures for /events and
// /events/count endpoints consumed by fetchInsightsViewData.
func startInsightsServer(t *testing.T, events []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/insights/directory/v1/events":
			// Basic body parse to honor the search_term_filter for tests that
			// exercise event_type/user filtering.
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			filter, _ := body["search_term_filter"].(map[string]any)

			filtered := events
			if len(filter) > 0 {
				filtered = nil
				for _, e := range events {
					match := true
					for k, v := range filter {
						switch k {
						case "event_type":
							if e["event_type"] != v {
								match = false
							}
						case "initiated_by.username":
							ib, _ := e["initiated_by"].(map[string]any)
							if ib == nil || ib["username"] != v {
								match = false
							}
						}
					}
					if match {
						filtered = append(filtered, e)
					}
				}
			}
			_ = json.NewEncoder(w).Encode(filtered)
		case "/insights/directory/v1/events/count":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			filter, _ := body["search_term_filter"].(map[string]any)
			filtered := events
			if len(filter) > 0 {
				filtered = nil
				for _, e := range events {
					match := true
					for k, v := range filter {
						switch k {
						case "event_type":
							if e["event_type"] != v {
								match = false
							}
						case "initiated_by.username":
							ib, _ := e["initiated_by"].(map[string]any)
							if ib == nil || ib["username"] != v {
								match = false
							}
						}
					}
					if match {
						filtered = append(filtered, e)
					}
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]int{"count": len(filtered)})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestBucketSizeFor(t *testing.T) {
	cases := []struct {
		window time.Duration
		want   time.Duration
	}{
		{1 * time.Hour, 5 * time.Minute},
		{6 * time.Hour, 5 * time.Minute},
		{12 * time.Hour, time.Hour},
		{48 * time.Hour, time.Hour},
		{7 * 24 * time.Hour, 6 * time.Hour},
		{8 * 24 * time.Hour, 24 * time.Hour},
		{30 * 24 * time.Hour, 24 * time.Hour},
	}
	for _, c := range cases {
		if got := bucketSizeFor(c.window); got != c.want {
			t.Errorf("bucketSizeFor(%v) = %v, want %v", c.window, got, c.want)
		}
	}
}

func TestResolveInsightsWindow_DefaultsTo24h(t *testing.T) {
	now := time.Date(2026, 4, 24, 18, 0, 0, 0, time.UTC)
	start, end, err := resolveInsightsWindow(insightsViewArgs{}, now)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !end.Equal(now) {
		t.Errorf("end = %v, want %v", end, now)
	}
	if end.Sub(start) != 24*time.Hour {
		t.Errorf("window = %v, want 24h", end.Sub(start))
	}
}

func TestResolveInsightsWindow_LastOverridesDefault(t *testing.T) {
	now := time.Date(2026, 4, 24, 18, 0, 0, 0, time.UTC)
	start, end, err := resolveInsightsWindow(insightsViewArgs{Last: "7d"}, now)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if end.Sub(start) < 6*24*time.Hour || end.Sub(start) > 8*24*time.Hour {
		t.Errorf("window = %v, want ~7d", end.Sub(start))
	}
}

func TestResolveInsightsWindow_StartBeforeEndRequired(t *testing.T) {
	now := time.Date(2026, 4, 24, 18, 0, 0, 0, time.UTC)
	_, _, err := resolveInsightsWindow(insightsViewArgs{
		Start: "2026-04-25T00:00:00Z",
		End:   "2026-04-24T00:00:00Z",
	}, now)
	if err == nil {
		t.Error("expected error when start >= end")
	}
}

func TestFetchInsightsViewData_Aggregates(t *testing.T) {
	setupToolTest(t)

	// Fixed "now" so bucket boundaries are deterministic.
	origNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = origNow })

	// Five events across a 24h window: three sso_auth_failed, two password_change.
	// Two users: alice (3 events), bob (2 events).
	events := []map[string]any{
		{"timestamp": "2026-04-24T11:30:00Z", "event_type": "sso_auth_failed", "initiated_by": map[string]any{"username": "alice"}},
		{"timestamp": "2026-04-24T11:00:00Z", "event_type": "sso_auth_failed", "initiated_by": map[string]any{"username": "alice"}},
		{"timestamp": "2026-04-24T06:15:00Z", "event_type": "sso_auth_failed", "initiated_by": map[string]any{"username": "bob"}},
		{"timestamp": "2026-04-24T03:00:00Z", "event_type": "password_change", "initiated_by": map[string]any{"username": "alice"}},
		{"timestamp": "2026-04-23T20:30:00Z", "event_type": "password_change", "initiated_by": map[string]any{"username": "bob"}},
	}
	ts := startInsightsServer(t, events)
	t.Cleanup(ts.Close)
	overrideInsightsClientForTest(t, ts.URL)

	data, err := fetchInsightsViewData(context.Background(), insightsViewArgs{Last: "24h"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if data.Total != 5 {
		t.Errorf("Total = %d, want 5", data.Total)
	}
	if data.Sampled != 5 {
		t.Errorf("Sampled = %d, want 5", data.Sampled)
	}
	if len(data.EventTypes) != 2 {
		t.Errorf("EventTypes len = %d, want 2 (%v)", len(data.EventTypes), data.EventTypes)
	}
	if data.TopUsers[0].Username != "alice" || data.TopUsers[0].Count != 3 {
		t.Errorf("top user = %+v, want alice/3", data.TopUsers[0])
	}
	if data.TopUsers[1].Username != "bob" || data.TopUsers[1].Count != 2 {
		t.Errorf("2nd user = %+v, want bob/2", data.TopUsers[1])
	}

	// Bucket size for 24h window is 1h, so we expect 25 bins (inclusive).
	if len(data.Bins) < 24 || len(data.Bins) > 26 {
		t.Errorf("bins len = %d, want ~25 for 24h window", len(data.Bins))
	}

	// Sum the per-bucket counts and confirm they equal total events that
	// fell inside the window. (Events outside the window would not bin; all
	// 5 are within 24h of noon on the 24th so all should bin.)
	sum := 0
	for _, b := range data.Bins {
		for _, c := range b.Counts {
			sum += c
		}
	}
	if sum != 5 {
		t.Errorf("sum of bin counts = %d, want 5", sum)
	}
}

func TestFetchInsightsViewData_EventTypeFilter(t *testing.T) {
	setupToolTest(t)
	origNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = origNow })

	events := []map[string]any{
		{"timestamp": "2026-04-24T11:30:00Z", "event_type": "sso_auth_failed", "initiated_by": map[string]any{"username": "alice"}},
		{"timestamp": "2026-04-24T10:00:00Z", "event_type": "password_change", "initiated_by": map[string]any{"username": "bob"}},
	}
	ts := startInsightsServer(t, events)
	t.Cleanup(ts.Close)
	overrideInsightsClientForTest(t, ts.URL)

	data, err := fetchInsightsViewData(context.Background(), insightsViewArgs{
		Last:      "24h",
		EventType: "sso_auth_failed",
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if data.Total != 1 {
		t.Errorf("filtered total = %d, want 1", data.Total)
	}
	if len(data.EventTypes) != 1 || data.EventTypes[0] != "sso_auth_failed" {
		t.Errorf("event types = %v, want only sso_auth_failed", data.EventTypes)
	}
}

func TestInsightsView_HasUIMetadata(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var found *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == "insights_view" {
			found = tool
			break
		}
	}
	if found == nil {
		t.Fatal("insights_view tool not found in ListTools result")
	}

	meta := map[string]any(found.Meta)
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("expected _meta.ui to be a map, got %T", meta["ui"])
	}
	if uri, _ := ui["resourceUri"].(string); uri != insightsResourceURI {
		t.Errorf("resourceUri = %q, want %q", uri, insightsResourceURI)
	}
}

func TestInsightsResource_ServesHTMLWithInjection(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{
		URI: insightsResourceURI,
	})
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
		t.Error("served HTML missing common.js injection (window.jcApp)")
	}
	if strings.Contains(c.Text, appCommonMarker) {
		t.Error("served HTML still contains the injection marker")
	}
	if !strings.Contains(c.Text, "Directory Insights") {
		t.Error("served HTML missing page title")
	}
}
