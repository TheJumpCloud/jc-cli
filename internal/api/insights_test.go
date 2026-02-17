package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestInsightsClient creates an InsightsClient pointing at the given test server.
func newTestInsightsClient(serverURL string) *InsightsClient {
	c := NewInsightsClientWithKey("test-key-1234")
	c.BaseURL = serverURL
	return c
}

func TestInsightsClient_QueryEvents_SinglePage(t *testing.T) {
	events := []map[string]any{
		{"timestamp": "2026-02-13T10:00:00Z", "event_type": "sso_auth", "initiated_by": "alice"},
		{"timestamp": "2026-02-13T11:00:00Z", "event_type": "sso_auth", "initiated_by": "bob"},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/events" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	result, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-13T00:00:00Z",
	}, InsightsQueryOptions{})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}
	if len(result.Data) != 2 {
		t.Errorf("got %d results, want 2", len(result.Data))
	}
}

func TestInsightsClient_QueryEvents_MultiplePages(t *testing.T) {
	totalItems := 250
	var requestCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)

		skip := int(body["skip"].(float64))
		limit := int(body["limit"].(float64))

		var items []map[string]any
		for i := skip; i < skip+limit && i < totalItems; i++ {
			items = append(items, map[string]any{
				"timestamp":  fmt.Sprintf("2026-02-13T%02d:00:00Z", i%24),
				"event_type": "sso_auth",
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	result, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, InsightsQueryOptions{})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}
	if len(result.Data) != totalItems {
		t.Errorf("got %d results, want %d", len(result.Data), totalItems)
	}
	// Should make 3 requests: 100 + 100 + 50.
	if got := requestCount.Load(); got != 3 {
		t.Errorf("made %d requests, want 3", got)
	}
}

func TestInsightsClient_QueryEvents_LimitCapsResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		limit := int(body["limit"].(float64))

		var items []map[string]any
		for i := range limit {
			items = append(items, map[string]any{"event_type": fmt.Sprintf("event-%d", i)})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	result, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, InsightsQueryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}
	if len(result.Data) != 10 {
		t.Errorf("got %d results, want 10 (limit)", len(result.Data))
	}
}

func TestInsightsClient_QueryEvents_LimitSmallerThanPageSize(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		limit := int(body["limit"].(float64))

		// The first request should only ask for 5 items (the user limit).
		if limit != 5 {
			t.Errorf("expected limit=5 in request, got limit=%d", limit)
		}

		var items []map[string]any
		for i := range limit {
			items = append(items, map[string]any{"event_type": fmt.Sprintf("event-%d", i)})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	result, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, InsightsQueryOptions{Limit: 5})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}
	if len(result.Data) != 5 {
		t.Errorf("got %d results, want 5", len(result.Data))
	}
}

func TestInsightsClient_QueryEvents_EmptyResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	result, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, InsightsQueryOptions{})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}
	if len(result.Data) != 0 {
		t.Errorf("got %d results, want 0", len(result.Data))
	}
}

func TestInsightsClient_QueryEvents_ContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return full page to trigger pagination.
		var items []map[string]any
		for i := range 100 {
			items = append(items, map[string]any{"event_type": fmt.Sprintf("event-%d", i)})
		}
		json.NewEncoder(w).Encode(items)
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())

	var requestCount atomic.Int32
	origHandler := ts.Config.Handler
	ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if requestCount.Load() >= 1 {
			cancel()
		}
		origHandler.ServeHTTP(w, r)
	})

	c := newTestInsightsClient(ts.URL)
	_, err := c.QueryEvents(ctx, InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, InsightsQueryOptions{})
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestInsightsClient_QueryEvents_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	_, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, InsightsQueryOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusUnauthorized)
	}
	if apiErr.Endpoint != "/events" {
		t.Errorf("Endpoint = %q, want %q", apiErr.Endpoint, "/events")
	}
}

func TestInsightsClient_QueryEvents_PaginationParams(t *testing.T) {
	var capturedBodies []map[string]any
	totalItems := 5

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		capturedBodies = append(capturedBodies, body)

		skip := int(body["skip"].(float64))
		limit := int(body["limit"].(float64))

		var items []map[string]any
		end := skip + limit
		if end > totalItems {
			end = totalItems
		}
		for i := skip; i < end; i++ {
			items = append(items, map[string]any{"event_type": fmt.Sprintf("event-%d", i)})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	result, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, InsightsQueryOptions{})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}
	if len(result.Data) != 5 {
		t.Errorf("got %d results, want 5", len(result.Data))
	}

	// Verify first request has skip=0.
	if len(capturedBodies) < 1 {
		t.Fatalf("expected at least 1 request, got %d", len(capturedBodies))
	}
	if capturedBodies[0]["skip"] != float64(0) {
		t.Errorf("first request skip = %v, want 0", capturedBodies[0]["skip"])
	}
	svc, ok := capturedBodies[0]["service"].([]any)
	if !ok || len(svc) != 1 || svc[0] != "sso" {
		t.Errorf("service = %v, want [sso]", capturedBodies[0]["service"])
	}
}

func TestInsightsClient_QueryEvents_WithSort(t *testing.T) {
	var capturedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	_, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, InsightsQueryOptions{Sort: "-timestamp"})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}
	if capturedBody["sort"] != "-timestamp" {
		t.Errorf("sort = %v, want -timestamp", capturedBody["sort"])
	}
}

func TestInsightsClient_QueryEvents_WithFilter(t *testing.T) {
	var capturedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	_, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
		SearchTermFilter: map[string]any{
			"event_type": "sso_auth_failed",
		},
	}, InsightsQueryOptions{})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}

	stf, ok := capturedBody["search_term_filter"].(map[string]any)
	if !ok {
		t.Fatal("search_term_filter missing from request body")
	}
	if stf["event_type"] != "sso_auth_failed" {
		t.Errorf("event_type filter = %v, want sso_auth_failed", stf["event_type"])
	}
}

func TestInsightsClient_QueryEvents_WithFields(t *testing.T) {
	var capturedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	_, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
		Fields:    []string{"timestamp", "event_type", "initiated_by"},
	}, InsightsQueryOptions{})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}

	fields, ok := capturedBody["fields"].([]any)
	if !ok {
		t.Fatal("fields missing from request body")
	}
	if len(fields) != 3 {
		t.Errorf("got %d fields, want 3", len(fields))
	}
}

func TestInsightsClient_QueryEvents_WithEndTime(t *testing.T) {
	var capturedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	_, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
		EndTime:   "2026-02-13T00:00:00Z",
	}, InsightsQueryOptions{})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}
	if capturedBody["end_time"] != "2026-02-13T00:00:00Z" {
		t.Errorf("end_time = %v, want 2026-02-13T00:00:00Z", capturedBody["end_time"])
	}
}

// --- CountEvents Tests ---

func TestInsightsClient_CountEvents_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/events/count" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"count":42}`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	count, err := c.CountEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CountEvents error: %v", err)
	}
	if count != 42 {
		t.Errorf("count = %d, want 42", count)
	}
}

func TestInsightsClient_CountEvents_WithFilter(t *testing.T) {
	var capturedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"count":5}`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	count, err := c.CountEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
		SearchTermFilter: map[string]any{
			"event_type": "sso_auth_failed",
		},
	})
	if err != nil {
		t.Fatalf("CountEvents error: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}

	stf, ok := capturedBody["search_term_filter"].(map[string]any)
	if !ok {
		t.Fatal("search_term_filter missing from request body")
	}
	if stf["event_type"] != "sso_auth_failed" {
		t.Errorf("event_type filter = %v, want sso_auth_failed", stf["event_type"])
	}
}

func TestInsightsClient_CountEvents_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	_, err := c.CountEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
}

// --- DistinctEvents Tests ---

func TestInsightsClient_DistinctEvents_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/events/distinct" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["field"] != "initiated_by.username" {
			t.Errorf("field = %v, want initiated_by.username", body["field"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`["alice","bob","carol"]`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	items, err := c.DistinctEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, "initiated_by.username")
	if err != nil {
		t.Fatalf("DistinctEvents error: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("got %d distinct values, want 3", len(items))
	}
}

func TestInsightsClient_DistinctEvents_Empty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	items, err := c.DistinctEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, "event_type")
	if err != nil {
		t.Fatalf("DistinctEvents error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("got %d distinct values, want 0", len(items))
	}
}

func TestInsightsClient_DistinctEvents_ObjectResponse(t *testing.T) {
	// The real API returns an object like {"field_name": [{key, doc_count}, ...], ...}.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"event_type":[{"key":"sso_auth","doc_count":42},{"key":"ldap_bind","doc_count":7}],"doc_count_error_upper_bound":0,"sum_other_doc_count":0}`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	items, err := c.DistinctEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, "event_type")
	if err != nil {
		t.Fatalf("DistinctEvents error: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("got %d distinct values, want 2", len(items))
	}
}

func TestInsightsClient_DistinctEvents_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Forbidden"}`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	_, err := c.DistinctEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, "event_type")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want 403", apiErr.StatusCode)
	}
}

// --- Constructor Tests ---

func TestNewInsightsClient_BaseURL(t *testing.T) {
	c := NewInsightsClientWithKey("test-key")
	if c.BaseURL != InsightsBaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, InsightsBaseURL)
	}
}

func TestNewInsightsClient_NoAPIKey(t *testing.T) {
	resetViper()
	defer resetViper()

	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_CONFIG", "/nonexistent/config.yaml")

	_, err := NewInsightsClient()
	if err == nil {
		t.Fatal("expected error when no API key, got nil")
	}
}

func TestNewInsightsClient_SharesTransport(t *testing.T) {
	c := NewInsightsClientWithKey("test-key")
	if c.HTTP == nil {
		t.Fatal("HTTP client is nil")
	}
	if c.HTTP.Transport == nil {
		t.Fatal("Transport is nil")
	}
}

func TestNewInsightsClient_AuthHeader(t *testing.T) {
	var capturedHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	_, _ = c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso",
		StartTime: "2026-02-01T00:00:00Z",
	}, InsightsQueryOptions{})

	if capturedHeader != "test-key-1234" {
		t.Errorf("x-api-key = %q, want test-key-1234", capturedHeader)
	}
}

// --- ValidateService Tests ---

func TestValidateService_Valid(t *testing.T) {
	tests := []string{"sso", "ldap", "radius", "all", "mdm", "directory", "software", "systems", "password_manager", "alert", "notifications", "asset_management", "access_management", "reports", "object_storage", "saas_app_management", "workflows"}
	for _, s := range tests {
		if err := ValidateService(s); err != nil {
			t.Errorf("ValidateService(%q) returned error: %v", s, err)
		}
	}
}

func TestValidateService_MultipleValid(t *testing.T) {
	if err := ValidateService("sso,ldap,directory"); err != nil {
		t.Errorf("ValidateService(\"sso,ldap,directory\") returned error: %v", err)
	}
}

func TestValidateService_Invalid(t *testing.T) {
	err := ValidateService("invalid_service")
	if err == nil {
		t.Fatal("expected error for invalid service, got nil")
	}
	if !contains(err.Error(), "invalid service") {
		t.Errorf("error message %q should contain 'invalid service'", err.Error())
	}
}

func TestValidateService_PartiallyInvalid(t *testing.T) {
	err := ValidateService("sso,bogus")
	if err == nil {
		t.Fatal("expected error for partially invalid services, got nil")
	}
}

func TestValidateService_EmptyIgnored(t *testing.T) {
	if err := ValidateService("sso,,ldap"); err != nil {
		t.Errorf("ValidateService with empty segment returned error: %v", err)
	}
}

// --- ParseTimeRange Tests ---

func TestParseTimeRange_Hours(t *testing.T) {
	fixed := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	old := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return fixed }
	defer func() { InsightsNowFunc = old }()

	result, err := ParseTimeRange("24h")
	if err != nil {
		t.Fatalf("ParseTimeRange(\"24h\") error: %v", err)
	}
	expected := fixed.Add(-24 * time.Hour)
	if !result.Equal(expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestParseTimeRange_Days(t *testing.T) {
	fixed := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	old := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return fixed }
	defer func() { InsightsNowFunc = old }()

	result, err := ParseTimeRange("7d")
	if err != nil {
		t.Fatalf("ParseTimeRange(\"7d\") error: %v", err)
	}
	expected := fixed.AddDate(0, 0, -7)
	if !result.Equal(expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestParseTimeRange_Months(t *testing.T) {
	fixed := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	old := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return fixed }
	defer func() { InsightsNowFunc = old }()

	result, err := ParseTimeRange("1m")
	if err != nil {
		t.Fatalf("ParseTimeRange(\"1m\") error: %v", err)
	}
	expected := fixed.AddDate(0, -1, 0)
	if !result.Equal(expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestParseTimeRange_LastPrefix(t *testing.T) {
	fixed := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	old := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return fixed }
	defer func() { InsightsNowFunc = old }()

	result, err := ParseTimeRange("last 30d")
	if err != nil {
		t.Fatalf("ParseTimeRange(\"last 30d\") error: %v", err)
	}
	expected := fixed.AddDate(0, 0, -30)
	if !result.Equal(expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestParseTimeRange_RFC3339(t *testing.T) {
	result, err := ParseTimeRange("2026-02-01T10:30:00Z")
	if err != nil {
		t.Fatalf("ParseTimeRange error: %v", err)
	}
	expected := time.Date(2026, 2, 1, 10, 30, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestParseTimeRange_DateOnly(t *testing.T) {
	result, err := ParseTimeRange("2026-02-01")
	if err != nil {
		t.Fatalf("ParseTimeRange error: %v", err)
	}
	expected := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestParseTimeRange_Invalid(t *testing.T) {
	_, err := ParseTimeRange("not-a-time")
	if err == nil {
		t.Fatal("expected error for invalid time format, got nil")
	}
}

func TestParseTimeRange_LastHours(t *testing.T) {
	fixed := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)
	old := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return fixed }
	defer func() { InsightsNowFunc = old }()

	result, err := ParseTimeRange("last 1h")
	if err != nil {
		t.Fatalf("ParseTimeRange(\"last 1h\") error: %v", err)
	}
	expected := fixed.Add(-1 * time.Hour)
	if !result.Equal(expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

// --- MultiService Tests ---

func TestInsightsClient_QueryEvents_MultiService(t *testing.T) {
	var capturedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	_, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "sso,ldap",
		StartTime: "2026-02-01T00:00:00Z",
	}, InsightsQueryOptions{})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}
	// Service should be sent as an array.
	svc, ok := capturedBody["service"].([]any)
	if !ok {
		t.Fatalf("service is not an array: %T = %v", capturedBody["service"], capturedBody["service"])
	}
	if len(svc) != 2 || svc[0] != "sso" || svc[1] != "ldap" {
		t.Errorf("service = %v, want [sso, ldap]", svc)
	}
}

// --- Pagination Correctness ---

func TestInsightsClient_QueryEvents_PaginationSkipValues(t *testing.T) {
	var capturedSkips []int
	totalItems := 250

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		skip := int(body["skip"].(float64))
		capturedSkips = append(capturedSkips, skip)

		var items []map[string]any
		limit := int(body["limit"].(float64))
		end := skip + limit
		if end > totalItems {
			end = totalItems
		}
		for i := skip; i < end; i++ {
			items = append(items, map[string]any{"n": strconv.Itoa(i)})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}))
	defer ts.Close()

	c := newTestInsightsClient(ts.URL)
	result, err := c.QueryEvents(context.Background(), InsightsQuery{
		Service:   "all",
		StartTime: "2026-02-01T00:00:00Z",
	}, InsightsQueryOptions{})
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}
	if len(result.Data) != totalItems {
		t.Errorf("got %d results, want %d", len(result.Data), totalItems)
	}

	// With DefaultPageSize=100 and 250 items: skip=0, skip=100, skip=200.
	expectedSkips := []int{0, 100, 200}
	if len(capturedSkips) != len(expectedSkips) {
		t.Fatalf("expected %d requests, got %d: %v", len(expectedSkips), len(capturedSkips), capturedSkips)
	}
	for i, want := range expectedSkips {
		if capturedSkips[i] != want {
			t.Errorf("request %d: skip = %d, want %d", i, capturedSkips[i], want)
		}
	}
}

// helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// === Battle Tests: Fuzz ===

func FuzzParseTimeRange(f *testing.F) {
	f.Add("24h")
	f.Add("7d")
	f.Add("1m")
	f.Add("last 30d")
	f.Add("2026-01-01")
	f.Add("")
	f.Add("-24h")
	f.Add("999999h")
	f.Add("2026-01-01T00:00:00Z")
	f.Add("  24h  ")

	// Fix the clock for reproducible results.
	origNow := InsightsNowFunc
	InsightsNowFunc = func() time.Time {
		return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	}
	f.Cleanup(func() { InsightsNowFunc = origNow })

	f.Fuzz(func(t *testing.T, input string) {
		result, err := ParseTimeRange(input)
		if err != nil {
			return // errors are fine, just no panics
		}
		if result.IsZero() {
			t.Error("successful parse returned zero time")
		}
	})
}

// === Battle Tests: Edge Cases ===

func TestParseTimeRange_NegativeDuration(t *testing.T) {
	origNow := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) }
	defer func() { InsightsNowFunc = origNow }()

	_, err := ParseTimeRange("-24h")
	if err == nil {
		t.Fatal("expected error for negative duration, got nil")
	}
}

func TestParseTimeRange_ZeroDuration(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	origNow := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return now }
	defer func() { InsightsNowFunc = origNow }()

	result, err := ParseTimeRange("0h")
	if err != nil {
		t.Fatalf("ParseTimeRange error: %v", err)
	}
	// 0h means now - 0 = now, so result == now.
	if !result.Equal(now) {
		t.Errorf("result %v != now %v for 0h duration", result, now)
	}
}

func TestParseTimeRange_LargeOverflow(t *testing.T) {
	origNow := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) }
	defer func() { InsightsNowFunc = origNow }()

	// Should not panic even with huge numbers.
	_, err := ParseTimeRange("999999999h")
	// May succeed or error, but must not panic.
	_ = err
}

func TestParseTimeRange_CaseSensitivity(t *testing.T) {
	origNow := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) }
	defer func() { InsightsNowFunc = origNow }()

	_, err := ParseTimeRange("24H")
	if err == nil {
		t.Fatal("expected error for uppercase unit, got nil")
	}
}

func TestParseTimeRange_FloatingPoint(t *testing.T) {
	origNow := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) }
	defer func() { InsightsNowFunc = origNow }()

	_, err := ParseTimeRange("3.5h")
	if err == nil {
		t.Fatal("expected error for floating point duration, got nil")
	}
}

func TestParseTimeRange_LastPrefixCase(t *testing.T) {
	origNow := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) }
	defer func() { InsightsNowFunc = origNow }()

	// "Last" with capital L should not match "last " prefix.
	_, err := ParseTimeRange("Last 24h")
	if err == nil {
		t.Fatal("expected error for capitalized 'Last' prefix, got nil")
	}
}

func TestParseTimeRange_LeadingWhitespace(t *testing.T) {
	origNow := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) }
	defer func() { InsightsNowFunc = origNow }()

	result, err := ParseTimeRange("  24h  ")
	if err != nil {
		t.Fatalf("ParseTimeRange error: %v", err)
	}
	if result.IsZero() {
		t.Error("expected non-zero time for trimmed input")
	}
}

func TestParseTimeRange_EmptyInput(t *testing.T) {
	_, err := ParseTimeRange("")
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
}

func TestParseTimeRange_OnlyWhitespace(t *testing.T) {
	_, err := ParseTimeRange("   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only input, got nil")
	}
}

func TestParseTimeRange_InvalidUnit(t *testing.T) {
	origNow := InsightsNowFunc
	InsightsNowFunc = func() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) }
	defer func() { InsightsNowFunc = origNow }()

	_, err := ParseTimeRange("24x")
	if err == nil {
		t.Fatal("expected error for invalid unit 'x', got nil")
	}
}

// === Battle Tests: Concurrency ===

func TestInsightsClient_ConcurrentQuery(t *testing.T) {
	// Set up a test server that returns a valid insights response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"event_type":"login","timestamp":"2026-01-01T00:00:00Z"}]`))
	}))
	defer server.Close()

	client := newTestInsightsClient(server.URL)

	var wg sync.WaitGroup
	errs := make(chan error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(service string) {
			defer wg.Done()
			query := InsightsQuery{
				Service:   service,
				StartTime: "2026-01-01T00:00:00Z",
				EndTime:   "2026-01-02T00:00:00Z",
			}
			_, err := client.QueryEvents(context.Background(), query, InsightsQueryOptions{})
			if err != nil {
				errs <- err
			}
		}(fmt.Sprintf("svc%d", i))
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent query error: %v", err)
	}
}
