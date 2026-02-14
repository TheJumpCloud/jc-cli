package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
)

// overrideInsightsClient overrides the newInsightsClient var for testing.
func overrideInsightsClient(t *testing.T, serverURL string) {
	t.Helper()
	old := newInsightsClient
	newInsightsClient = func() (*api.InsightsClient, error) {
		c := api.NewInsightsClientWithKey("test-key-1234")
		c.BaseURL = serverURL
		return c, nil
	}
	t.Cleanup(func() { newInsightsClient = old })
}

// startInsightsServer creates a mock Directory Insights server that handles POST /events.
func startInsightsServer(t *testing.T, events []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"message":"Method Not Allowed"}`))
			return
		}

		if r.URL.Path == "/events" {
			json.NewEncoder(w).Encode(events)
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

// startInsightsServerWithCapture creates a mock server that captures the request body.
func startInsightsServerWithCapture(t *testing.T, events []map[string]any) (*httptest.Server, *map[string]any) {
	t.Helper()
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost && r.URL.Path == "/events" {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			json.NewEncoder(w).Encode(events)
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	return ts, &capturedBody
}

func sampleEvents() []map[string]any {
	return []map[string]any{
		{
			"timestamp":    "2026-02-13T10:00:00Z",
			"event_type":   "sso_auth",
			"initiated_by": map[string]any{"username": "alice"},
			"client_ip":    "10.0.0.1",
			"success":      true,
		},
		{
			"timestamp":    "2026-02-13T11:00:00Z",
			"event_type":   "sso_auth_failed",
			"initiated_by": map[string]any{"username": "bob"},
			"client_ip":    "10.0.0.2",
			"success":      false,
		},
		{
			"timestamp":    "2026-02-13T12:00:00Z",
			"event_type":   "sso_auth",
			"initiated_by": map[string]any{"username": "carol"},
			"client_ip":    "10.0.0.3",
			"success":      true,
		},
	}
}

// --- Query Tests ---

func TestInsightsQuery_JSON(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts := startInsightsServer(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d events, want 3", len(result))
	}
}

func TestInsightsQuery_DefaultFields(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts := startInsightsServer(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	first := result[0]
	for _, field := range []string{"timestamp", "event_type", "initiated_by", "client_ip", "success"} {
		if _, ok := first[field]; !ok {
			t.Errorf("default fields should include %q", field)
		}
	}
}

func TestInsightsQuery_Table(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts := startInsightsServer(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "sso_auth") {
		t.Errorf("table output should contain 'sso_auth', got:\n%s", out)
	}
}

func TestInsightsQuery_Footer(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts := startInsightsServer(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errBuf.String()
	if !strings.Contains(footer, "3 items") {
		t.Errorf("footer should contain '3 items', got: %q", footer)
	}
}

func TestInsightsQuery_EmptyResults(t *testing.T) {
	setupUsersTest(t)
	ts := startInsightsServer(t, []map[string]any{})
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 0 {
		t.Errorf("got %d events, want 0", len(result))
	}
}

func TestInsightsQuery_MultiService(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts, captured := startInsightsServerWithCapture(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso,ldap", "--last", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if (*captured)["service"] != "sso,ldap" {
		t.Errorf("service = %v, want sso,ldap", (*captured)["service"])
	}
}

func TestInsightsQuery_AllService(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts, captured := startInsightsServerWithCapture(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "all", "--last", "7d"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if (*captured)["service"] != "all" {
		t.Errorf("service = %v, want all", (*captured)["service"])
	}
}

func TestInsightsQuery_EventTypeFilter(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts, captured := startInsightsServerWithCapture(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h", "--event-type", "sso_auth_failed"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stf, ok := (*captured)["search_term_filter"].(map[string]any)
	if !ok {
		t.Fatal("search_term_filter missing from request body")
	}
	if stf["event_type"] != "sso_auth_failed" {
		t.Errorf("event_type filter = %v, want sso_auth_failed", stf["event_type"])
	}
}

func TestInsightsQuery_Limit(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts, captured := startInsightsServerWithCapture(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d events, want 2 (limit)", len(result))
	}

	// Verify limit was passed to API.
	if limit, ok := (*captured)["limit"].(float64); !ok || limit != 2 {
		t.Errorf("limit in request = %v, want 2", (*captured)["limit"])
	}
}

func TestInsightsQuery_Sort(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts, captured := startInsightsServerWithCapture(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h", "--sort", "-timestamp"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if (*captured)["sort"] != "-timestamp" {
		t.Errorf("sort = %v, want -timestamp", (*captured)["sort"])
	}
}

func TestInsightsQuery_StartEndTime(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts, captured := startInsightsServerWithCapture(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--start", "2026-02-01", "--end", "2026-02-13"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	startTime, ok := (*captured)["start_time"].(string)
	if !ok {
		t.Fatal("start_time missing from request body")
	}
	if startTime != "2026-02-01T00:00:00Z" {
		t.Errorf("start_time = %v, want 2026-02-01T00:00:00Z", startTime)
	}

	endTime, ok := (*captured)["end_time"].(string)
	if !ok {
		t.Fatal("end_time missing from request body")
	}
	if endTime != "2026-02-13T00:00:00Z" {
		t.Errorf("end_time = %v, want 2026-02-13T00:00:00Z", endTime)
	}
}

func TestInsightsQuery_LastFlag(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts, captured := startInsightsServerWithCapture(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	// Override insights time function for deterministic tests.
	fixed := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	old := api.InsightsNowFunc
	api.InsightsNowFunc = func() time.Time { return fixed }
	defer func() { api.InsightsNowFunc = old }()

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "7d"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	startTime, ok := (*captured)["start_time"].(string)
	if !ok {
		t.Fatal("start_time missing from request body")
	}

	expected := fixed.AddDate(0, 0, -7).UTC().Format(time.RFC3339)
	if startTime != expected {
		t.Errorf("start_time = %v, want %v", startTime, expected)
	}

	// --last should not set end_time (server defaults to now).
	if _, hasEnd := (*captured)["end_time"]; hasEnd {
		t.Error("--last should not set end_time")
	}
}

func TestInsightsQuery_Endpoint(t *testing.T) {
	setupUsersTest(t)
	var requestedPath string
	var requestedMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if requestedPath != "/events" {
		t.Errorf("expected request to /events, got %q", requestedPath)
	}
	if requestedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", requestedMethod)
	}
}

// --- Error Tests ---

func TestInsightsQuery_InvalidService(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "bogus_service", "--last", "24h"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid service, got nil")
	}
	if !strings.Contains(err.Error(), "invalid service") {
		t.Errorf("error should mention 'invalid service', got: %v", err)
	}
}

func TestInsightsQuery_MissingService(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--last", "24h"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --service, got nil")
	}
	if !strings.Contains(err.Error(), "service") {
		t.Errorf("error should mention 'service', got: %v", err)
	}
}

func TestInsightsQuery_NoTimeRange(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing time range, got nil")
	}
	if !strings.Contains(err.Error(), "--last or --start") {
		t.Errorf("error should mention '--last or --start', got: %v", err)
	}
}

func TestInsightsQuery_LastAndStartMutuallyExclusive(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h", "--start", "2026-02-01"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --last + --start, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should mention 'mutually exclusive', got: %v", err)
	}
}

func TestInsightsQuery_InvalidTimeFormat(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "not-a-time"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid time, got nil")
	}
	if !strings.Contains(err.Error(), "invalid time format") {
		t.Errorf("error should mention 'invalid time format', got: %v", err)
	}
}

func TestInsightsQuery_APIError(t *testing.T) {
	setupUsersTest(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for API failure, got nil")
	}
}

func TestInsightsQuery_Quiet(t *testing.T) {
	setupUsersTest(t)
	events := sampleEvents()
	ts := startInsightsServer(t, events)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--service", "sso", "--last", "24h", "--quiet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("quiet output should be empty, got: %q", buf.String())
	}
}

// --- Help Tests ---

func TestInsightsHelp_Subcommands(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "query") {
		t.Errorf("help should contain subcommand 'query', got:\n%s", out)
	}
}

func TestInsightsHelp_QueryFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "query", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, flag := range []string{"--service", "--last", "--start", "--end", "--event-type", "--limit", "--sort"} {
		if !strings.Contains(out, flag) {
			t.Errorf("query help should contain flag %q, got:\n%s", flag, out)
		}
	}
}

func TestInsightsHelp_RootIncludesInsights(t *testing.T) {
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
	if !strings.Contains(out, "insights") {
		t.Errorf("root help should contain 'insights', got:\n%s", out)
	}
}

func TestInsightsQuery_ValidatesServices(t *testing.T) {
	setupUsersTest(t)

	// Test each valid service name.
	for _, svc := range api.ValidInsightsServices {
		ts := startInsightsServer(t, []map[string]any{})
		overrideInsightsClient(t, ts.URL)

		cmd := NewRootCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"insights", "query", "--service", svc, "--last", "24h"})

		if err := cmd.Execute(); err != nil {
			t.Errorf("valid service %q produced error: %v", svc, err)
		}
		ts.Close()
	}
}

// --- Count Server Helpers ---

// startInsightsCountServer creates a mock server for /events/count.
func startInsightsCountServer(t *testing.T, count int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.URL.Path == "/events/count" {
			json.NewEncoder(w).Encode(map[string]int{"count": count})
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

// startInsightsCountServerWithCapture captures the request body.
func startInsightsCountServerWithCapture(t *testing.T, count int) (*httptest.Server, *map[string]any) {
	t.Helper()
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost && r.URL.Path == "/events/count" {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			json.NewEncoder(w).Encode(map[string]int{"count": count})
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	return ts, &capturedBody
}

// --- Count Tests ---

func TestInsightsCount_JSON(t *testing.T) {
	setupUsersTest(t)
	ts := startInsightsCountServer(t, 42)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "count", "--service", "sso", "--last", "7d"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]int
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["count"] != 42 {
		t.Errorf("count = %d, want 42", result["count"])
	}
}

func TestInsightsCount_Table(t *testing.T) {
	setupUsersTest(t)
	ts := startInsightsCountServer(t, 99)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "count", "--service", "sso", "--last", "7d", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "99") {
		t.Errorf("table output should contain '99', got:\n%s", out)
	}
}

func TestInsightsCount_EventTypeFilter(t *testing.T) {
	setupUsersTest(t)
	ts, captured := startInsightsCountServerWithCapture(t, 5)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "count", "--service", "sso", "--event-type", "sso_auth_failed", "--last", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stf, ok := (*captured)["search_term_filter"].(map[string]any)
	if !ok {
		t.Fatal("search_term_filter missing from request body")
	}
	if stf["event_type"] != "sso_auth_failed" {
		t.Errorf("event_type filter = %v, want sso_auth_failed", stf["event_type"])
	}
}

func TestInsightsCount_StartEndTime(t *testing.T) {
	setupUsersTest(t)
	ts, captured := startInsightsCountServerWithCapture(t, 10)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "count", "--service", "sso", "--start", "2026-02-01", "--end", "2026-02-13"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if (*captured)["start_time"] != "2026-02-01T00:00:00Z" {
		t.Errorf("start_time = %v, want 2026-02-01T00:00:00Z", (*captured)["start_time"])
	}
	if (*captured)["end_time"] != "2026-02-13T00:00:00Z" {
		t.Errorf("end_time = %v, want 2026-02-13T00:00:00Z", (*captured)["end_time"])
	}
}

func TestInsightsCount_Endpoint(t *testing.T) {
	setupUsersTest(t)
	var requestedPath string
	var requestedMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"count":0}`))
	}))
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "count", "--service", "sso", "--last", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if requestedPath != "/events/count" {
		t.Errorf("expected request to /events/count, got %q", requestedPath)
	}
	if requestedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", requestedMethod)
	}
}

func TestInsightsCount_InvalidService(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "count", "--service", "bogus", "--last", "24h"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid service, got nil")
	}
	if !strings.Contains(err.Error(), "invalid service") {
		t.Errorf("error should mention 'invalid service', got: %v", err)
	}
}

func TestInsightsCount_MissingService(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "count", "--last", "24h"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --service, got nil")
	}
}

func TestInsightsCount_NoTimeRange(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "count", "--service", "sso"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing time range, got nil")
	}
	if !strings.Contains(err.Error(), "--last or --start") {
		t.Errorf("error should mention '--last or --start', got: %v", err)
	}
}

func TestInsightsCount_Quiet(t *testing.T) {
	setupUsersTest(t)
	ts := startInsightsCountServer(t, 42)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "count", "--service", "sso", "--last", "24h", "--quiet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("quiet output should be empty, got: %q", buf.String())
	}
}

func TestInsightsCount_APIError(t *testing.T) {
	setupUsersTest(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "count", "--service", "sso", "--last", "24h"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for API failure, got nil")
	}
}

// --- Distinct Server Helpers ---

// startInsightsDistinctServer creates a mock server for /events/distinct.
func startInsightsDistinctServer(t *testing.T, values []any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.URL.Path == "/events/distinct" {
			json.NewEncoder(w).Encode(values)
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

// startInsightsDistinctServerWithCapture captures the request body.
func startInsightsDistinctServerWithCapture(t *testing.T, values []any) (*httptest.Server, *map[string]any) {
	t.Helper()
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost && r.URL.Path == "/events/distinct" {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			json.NewEncoder(w).Encode(values)
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	return ts, &capturedBody
}

// --- Distinct Tests ---

func TestInsightsDistinct_JSON(t *testing.T) {
	setupUsersTest(t)
	values := []any{"alice", "bob", "carol"}
	ts := startInsightsDistinctServer(t, values)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--service", "sso", "--field", "initiated_by.username", "--last", "30d"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []string
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d values, want 3", len(result))
	}
	if result[0] != "alice" {
		t.Errorf("first value = %q, want 'alice'", result[0])
	}
}

func TestInsightsDistinct_Table(t *testing.T) {
	setupUsersTest(t)
	values := []any{"10.0.0.1", "10.0.0.2"}
	ts := startInsightsDistinctServer(t, values)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--service", "sso", "--field", "client_ip", "--last", "7d", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "10.0.0.1") {
		t.Errorf("table output should contain '10.0.0.1', got:\n%s", out)
	}
}

func TestInsightsDistinct_Footer(t *testing.T) {
	setupUsersTest(t)
	values := []any{"alice", "bob", "carol"}
	ts := startInsightsDistinctServer(t, values)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"insights", "distinct", "--service", "sso", "--field", "initiated_by.username", "--last", "30d"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errBuf.String()
	if !strings.Contains(footer, "3 items") {
		t.Errorf("footer should contain '3 items', got: %q", footer)
	}
}

func TestInsightsDistinct_EmptyResults(t *testing.T) {
	setupUsersTest(t)
	ts := startInsightsDistinctServer(t, []any{})
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--service", "sso", "--field", "client_ip", "--last", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 0 {
		t.Errorf("got %d values, want 0", len(result))
	}
}

func TestInsightsDistinct_FieldParam(t *testing.T) {
	setupUsersTest(t)
	values := []any{"alice"}
	ts, captured := startInsightsDistinctServerWithCapture(t, values)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--service", "sso", "--field", "initiated_by.username", "--last", "30d"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if (*captured)["field"] != "initiated_by.username" {
		t.Errorf("field = %v, want initiated_by.username", (*captured)["field"])
	}
}

func TestInsightsDistinct_EventTypeFilter(t *testing.T) {
	setupUsersTest(t)
	values := []any{"alice"}
	ts, captured := startInsightsDistinctServerWithCapture(t, values)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--service", "sso", "--field", "initiated_by.username", "--last", "24h", "--event-type", "sso_auth_failed"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stf, ok := (*captured)["search_term_filter"].(map[string]any)
	if !ok {
		t.Fatal("search_term_filter missing from request body")
	}
	if stf["event_type"] != "sso_auth_failed" {
		t.Errorf("event_type filter = %v, want sso_auth_failed", stf["event_type"])
	}
}

func TestInsightsDistinct_Endpoint(t *testing.T) {
	setupUsersTest(t)
	var requestedPath string
	var requestedMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--service", "sso", "--field", "client_ip", "--last", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if requestedPath != "/events/distinct" {
		t.Errorf("expected request to /events/distinct, got %q", requestedPath)
	}
	if requestedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", requestedMethod)
	}
}

func TestInsightsDistinct_InvalidService(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--service", "bogus", "--field", "client_ip", "--last", "24h"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid service, got nil")
	}
	if !strings.Contains(err.Error(), "invalid service") {
		t.Errorf("error should mention 'invalid service', got: %v", err)
	}
}

func TestInsightsDistinct_MissingService(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--field", "client_ip", "--last", "24h"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --service, got nil")
	}
}

func TestInsightsDistinct_MissingField(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--service", "sso", "--last", "24h"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --field, got nil")
	}
}

func TestInsightsDistinct_NoTimeRange(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--service", "sso", "--field", "client_ip"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing time range, got nil")
	}
	if !strings.Contains(err.Error(), "--last or --start") {
		t.Errorf("error should mention '--last or --start', got: %v", err)
	}
}

func TestInsightsDistinct_Quiet(t *testing.T) {
	setupUsersTest(t)
	values := []any{"alice", "bob"}
	ts := startInsightsDistinctServer(t, values)
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--service", "sso", "--field", "client_ip", "--last", "24h", "--quiet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("quiet output should be empty, got: %q", buf.String())
	}
}

func TestInsightsDistinct_APIError(t *testing.T) {
	setupUsersTest(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--service", "sso", "--field", "client_ip", "--last", "24h"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for API failure, got nil")
	}
}

// --- Help Tests for Count and Distinct ---

func TestInsightsHelp_IncludesCountAndDistinct(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, sub := range []string{"query", "count", "distinct"} {
		if !strings.Contains(out, sub) {
			t.Errorf("insights help should contain subcommand %q, got:\n%s", sub, out)
		}
	}
}

func TestInsightsHelp_CountFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "count", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, flag := range []string{"--service", "--last", "--start", "--end", "--event-type"} {
		if !strings.Contains(out, flag) {
			t.Errorf("count help should contain flag %q, got:\n%s", flag, out)
		}
	}
}

func TestInsightsHelp_DistinctFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"insights", "distinct", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, flag := range []string{"--service", "--last", "--start", "--end", "--event-type", "--field"} {
		if !strings.Contains(out, flag) {
			t.Errorf("distinct help should contain flag %q, got:\n%s", flag, out)
		}
	}
}
