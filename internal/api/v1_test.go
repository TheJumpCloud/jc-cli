package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// newTestV1Client creates a V1Client pointing at the given test server.
func newTestV1Client(serverURL string) *V1Client {
	c := NewV1ClientWithKey("test-key-1234")
	c.BaseURL = serverURL
	return c
}

// v1Response builds a V1-style JSON response with results and totalCount.
func v1Response(items []map[string]any, total int) []byte {
	b, _ := json.Marshal(map[string]any{
		"results":    items,
		"totalCount": total,
	})
	return b
}

func TestV1Client_ListAll_SinglePage(t *testing.T) {
	items := []map[string]any{
		{"_id": "1", "username": "alice"},
		{"_id": "2", "username": "bob"},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items, 2))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	results, err := c.ListAll(context.Background(), "/systemusers", ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2", len(results))
	}
}

func TestV1Client_ListAll_MultiplePages(t *testing.T) {
	totalItems := 250
	var requestCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

		// Generate items for this page.
		var items []map[string]any
		for i := skip; i < skip+limit && i < totalItems; i++ {
			items = append(items, map[string]any{
				"_id":      fmt.Sprintf("id-%d", i),
				"username": fmt.Sprintf("user-%d", i),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items, totalItems))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	results, err := c.ListAll(context.Background(), "/systemusers", ListOptions{PageSize: 100})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(results) != totalItems {
		t.Errorf("got %d results, want %d", len(results), totalItems)
	}
	// Should make 3 requests: 100 + 100 + 50.
	if got := requestCount.Load(); got != 3 {
		t.Errorf("made %d requests, want 3", got)
	}
}

func TestV1Client_ListAll_LimitCapsResults(t *testing.T) {
	totalItems := 500

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

		var items []map[string]any
		for i := skip; i < skip+limit && i < totalItems; i++ {
			items = append(items, map[string]any{"_id": fmt.Sprintf("id-%d", i)})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items, totalItems))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	results, err := c.ListAll(context.Background(), "/systemusers", ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(results) != 10 {
		t.Errorf("got %d results, want 10 (limit)", len(results))
	}
}

func TestV1Client_ListAll_LimitSmallerThanPageSize(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		// The first request should only ask for 5 items (the user limit).
		if limit != 5 {
			t.Errorf("expected limit=5 in request, got limit=%d", limit)
		}

		var items []map[string]any
		for i := range limit {
			items = append(items, map[string]any{"_id": fmt.Sprintf("id-%d", i)})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items, 100))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	results, err := c.ListAll(context.Background(), "/systemusers", ListOptions{
		Limit:    5,
		PageSize: 100,
	})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("got %d results, want 5", len(results))
	}
}

func TestV1Client_ListAll_CustomPageSize(t *testing.T) {
	var capturedLimit int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLimit, _ = strconv.Atoi(r.URL.Query().Get("limit"))

		items := []map[string]any{{"_id": "1"}, {"_id": "2"}}
		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items, 2))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.ListAll(context.Background(), "/systemusers", ListOptions{PageSize: 50})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if capturedLimit != 50 {
		t.Errorf("captured limit = %d, want 50", capturedLimit)
	}
}

func TestV1Client_ListAll_DefaultPageSize(t *testing.T) {
	var capturedLimit int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLimit, _ = strconv.Atoi(r.URL.Query().Get("limit"))

		items := []map[string]any{{"_id": "1"}}
		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items, 1))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.ListAll(context.Background(), "/systemusers", ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if capturedLimit != DefaultPageSize {
		t.Errorf("captured limit = %d, want %d (DefaultPageSize)", capturedLimit, DefaultPageSize)
	}
}

func TestV1Client_ListAll_EmptyResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response([]map[string]any{}, 0))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	results, err := c.ListAll(context.Background(), "/systemusers", ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestV1Client_ListAll_ContextCancellation(t *testing.T) {
	var requestCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))

		var items []map[string]any
		for i := range 100 {
			items = append(items, map[string]any{"_id": fmt.Sprintf("id-%d", skip+i)})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items, 1000))
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())

	c := newTestV1Client(ts.URL)

	// Cancel after the first page.
	go func() {
		for requestCount.Load() < 1 {
			// spin until first request completes
		}
		cancel()
	}()

	_, err := c.ListAll(ctx, "/systemusers", ListOptions{PageSize: 100})
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestV1Client_ListAll_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.ListAll(context.Background(), "/systemusers", ListOptions{})
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
	if apiErr.Endpoint != "/systemusers" {
		t.Errorf("Endpoint = %q, want %q", apiErr.Endpoint, "/systemusers")
	}
}

func TestV1Client_ListAll_PaginationUsesSkipLimit(t *testing.T) {
	var capturedParams []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		skip := r.URL.Query().Get("skip")
		limit := r.URL.Query().Get("limit")
		capturedParams = append(capturedParams, fmt.Sprintf("skip=%s&limit=%s", skip, limit))

		skipN, _ := strconv.Atoi(skip)
		var items []map[string]any
		count := 50
		if skipN >= 100 {
			count = 0
		}
		for i := range count {
			items = append(items, map[string]any{"_id": fmt.Sprintf("id-%d", skipN+i)})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items, 150))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.ListAll(context.Background(), "/systemusers", ListOptions{PageSize: 50})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}

	expected := []string{
		"skip=0&limit=50",
		"skip=50&limit=50",
		"skip=100&limit=50",
	}
	if len(capturedParams) != len(expected) {
		t.Fatalf("expected %d requests, got %d: %v", len(expected), len(capturedParams), capturedParams)
	}
	for i, want := range expected {
		if capturedParams[i] != want {
			t.Errorf("request %d: got %q, want %q", i, capturedParams[i], want)
		}
	}
}

func TestV1Client_Get_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/systemusers/abc123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_id":"abc123","username":"alice"}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	result, err := c.Get(context.Background(), "/systemusers/abc123")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}

	var user map[string]any
	if err := json.Unmarshal(result, &user); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if user["username"] != "alice" {
		t.Errorf("username = %v, want alice", user["username"])
	}
}

func TestV1Client_Get_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.Get(context.Background(), "/systemusers/nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
}

func TestV1Client_Get_ContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	c := newTestV1Client(ts.URL)
	_, err := c.Get(ctx, "/systemusers/abc")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestV1Client_BaseURL(t *testing.T) {
	c := NewV1ClientWithKey("test-key")
	if c.BaseURL != BaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, BaseURL)
	}
	if !strings.HasPrefix(c.BaseURL, "https://console.jumpcloud.com/api") {
		t.Errorf("BaseURL should point to JumpCloud V1 API, got %q", c.BaseURL)
	}
}

func TestNewV1Client_NoAPIKey(t *testing.T) {
	resetViper()
	defer resetViper()

	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_CONFIG", "/nonexistent/config.yaml")

	_, err := NewV1Client()
	if err == nil {
		t.Fatal("expected error when no API key, got nil")
	}
}
