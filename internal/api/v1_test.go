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
	result, err := c.ListAll(context.Background(), "/systemusers", ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 2 {
		t.Errorf("got %d results, want 2", len(result.Data))
	}
	if result.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2", result.TotalCount)
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
	result, err := c.ListAll(context.Background(), "/systemusers", ListOptions{PageSize: 100})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != totalItems {
		t.Errorf("got %d results, want %d", len(result.Data), totalItems)
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
	result, err := c.ListAll(context.Background(), "/systemusers", ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 10 {
		t.Errorf("got %d results, want 10 (limit)", len(result.Data))
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
	result, err := c.ListAll(context.Background(), "/systemusers", ListOptions{
		Limit:    5,
		PageSize: 100,
	})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 5 {
		t.Errorf("got %d results, want 5", len(result.Data))
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
	result, err := c.ListAll(context.Background(), "/systemusers", ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 0 {
		t.Errorf("got %d results, want 0", len(result.Data))
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

// --- Create Tests ---

func TestV1Client_Create_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/systemusers" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"_id":"new123","username":"jdoe","email":"jdoe@acme.com"}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	body := map[string]string{"username": "jdoe", "email": "jdoe@acme.com"}
	result, err := c.Create(context.Background(), "/systemusers", body)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	var user map[string]any
	if err := json.Unmarshal(result, &user); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if user["_id"] != "new123" {
		t.Errorf("_id = %v, want new123", user["_id"])
	}
}

func TestV1Client_Create_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"message":"User already exists"}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.Create(context.Background(), "/systemusers", map[string]string{"username": "dup"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusConflict {
		t.Errorf("StatusCode = %d, want 409", apiErr.StatusCode)
	}
}

func TestV1Client_Create_201Created(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"_id":"new456","username":"newuser"}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	result, err := c.Create(context.Background(), "/systemusers", map[string]string{"username": "newuser"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	var user map[string]any
	json.Unmarshal(result, &user)
	if user["_id"] != "new456" {
		t.Errorf("_id = %v, want new456", user["_id"])
	}
}

// --- Update Tests ---

func TestV1Client_Update_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/systemusers/abc123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_id":"abc123","username":"alice","department":"Sales"}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	body := map[string]string{"department": "Sales"}
	result, err := c.Update(context.Background(), "/systemusers/abc123", body)
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}

	var user map[string]any
	json.Unmarshal(result, &user)
	if user["department"] != "Sales" {
		t.Errorf("department = %v, want Sales", user["department"])
	}
}

func TestV1Client_Update_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.Update(context.Background(), "/systemusers/nonexistent", map[string]string{"dept": "X"})
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

// --- Delete Tests ---

func TestV1Client_Delete_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/systemusers/abc123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_id":"abc123","username":"alice"}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	result, err := c.Delete(context.Background(), "/systemusers/abc123")
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	var user map[string]any
	json.Unmarshal(result, &user)
	if user["_id"] != "abc123" {
		t.Errorf("_id = %v, want abc123", user["_id"])
	}
}

func TestV1Client_Delete_204NoContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.Delete(context.Background(), "/systemusers/abc123")
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
}

func TestV1Client_Delete_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.Delete(context.Background(), "/systemusers/nonexistent")
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

// --- Search Tests ---

func TestV1Client_Search_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/search/systemusers" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response([]map[string]any{
			{"_id": "1", "username": "alice"},
			{"_id": "2", "username": "alicia"},
		}, 2))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	searchBody := map[string]any{
		"searchFilter": map[string]any{
			"searchTerm": "ali",
			"fields":     []string{"username"},
		},
	}
	result, err := c.Search(context.Background(), "/search/systemusers", searchBody, SearchOptions{})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Data) != 2 {
		t.Errorf("got %d results, want 2", len(result.Data))
	}
	if result.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2", result.TotalCount)
	}
}

func TestV1Client_Search_WithLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response([]map[string]any{
			{"_id": "1", "username": "alice"},
			{"_id": "2", "username": "alicia"},
			{"_id": "3", "username": "alex"},
		}, 3))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	result, err := c.Search(context.Background(), "/search/systemusers", map[string]any{}, SearchOptions{Limit: 2})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Data) != 2 {
		t.Errorf("got %d results, want 2 (limit=2)", len(result.Data))
	}
}

func TestV1Client_Search_EmptyResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response([]map[string]any{}, 0))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	result, err := c.Search(context.Background(), "/search/systemusers", map[string]any{}, SearchOptions{})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Data) != 0 {
		t.Errorf("got %d results, want 0", len(result.Data))
	}
}

func TestV1Client_Search_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.Search(context.Background(), "/search/systemusers", map[string]any{}, SearchOptions{})
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
}

func TestV1Client_Search_PaginationParams(t *testing.T) {
	var capturedBodies []map[string]any
	totalItems := 5

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		capturedBodies = append(capturedBodies, body)

		skip := 0
		limit := 100
		if v, ok := body["skip"].(float64); ok {
			skip = int(v)
		}
		if v, ok := body["limit"].(float64); ok {
			limit = int(v)
		}

		var items []map[string]any
		end := skip + limit
		if end > totalItems {
			end = totalItems
		}
		for i := skip; i < end; i++ {
			items = append(items, map[string]any{"_id": fmt.Sprintf("id-%d", i)})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items, totalItems))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	result, err := c.Search(context.Background(), "/search/systemusers", map[string]any{
		"searchFilter": map[string]any{"searchTerm": "test"},
	}, SearchOptions{})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if len(result.Data) != 5 {
		t.Errorf("got %d results, want 5", len(result.Data))
	}

	// Verify pagination params were injected into request bodies.
	if len(capturedBodies) < 1 {
		t.Fatalf("expected at least 1 request, got %d", len(capturedBodies))
	}
	if capturedBodies[0]["skip"] != float64(0) {
		t.Errorf("first request skip = %v, want 0", capturedBodies[0]["skip"])
	}
	// Verify that original search filter is preserved.
	if sf, ok := capturedBodies[0]["searchFilter"].(map[string]any); ok {
		if sf["searchTerm"] != "test" {
			t.Errorf("searchTerm = %v, want test", sf["searchTerm"])
		}
	} else {
		t.Error("searchFilter missing from paginated request")
	}
}

func TestV1Client_Search_WithSort(t *testing.T) {
	var capturedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response([]map[string]any{}, 0))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.Search(context.Background(), "/search/systemusers", map[string]any{}, SearchOptions{Sort: "-username"})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if capturedBody["sort"] != "-username" {
		t.Errorf("sort = %v, want -username", capturedBody["sort"])
	}
}

// --- Pagination Edge Cases ---

func TestV1Client_ListAll_NullResultsArray(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Server returns null results (not an empty array).
		w.Write([]byte(`{"results": null, "totalCount": 0}`))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	_, err := c.ListAll(context.Background(), "/systemusers", ListOptions{})
	// json.Unmarshal(null, &[]json.RawMessage{}) returns nil error with nil slice,
	// but our code does json.Unmarshal(listResp.Results, &pageItems) where
	// listResp.Results is json.RawMessage("null"). This actually succeeds and
	// sets pageItems to nil. len(nil) == 0 so pagination stops.
	// However, the Results field is json.RawMessage which may be "null" bytes.
	// Let's verify the behavior:
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
}

func TestV1Client_ListAll_ExactPageBoundary(t *testing.T) {
	totalItems := 200
	var requestCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
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
	result, err := c.ListAll(context.Background(), "/systemusers", ListOptions{PageSize: 100})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 200 {
		t.Errorf("got %d results, want 200", len(result.Data))
	}
	// With totalCount=200 and pageSize=100: page1 gets 100 (skip=0), page2 gets 100 (skip=100).
	// After page2, skip=200 >= totalCount=200, so it stops. Exactly 2 requests.
	if got := requestCount.Load(); got != 2 {
		t.Errorf("made %d requests, want 2", got)
	}
}

func TestV1Client_ListAll_TotalCountMismatch(t *testing.T) {
	// Server claims totalCount=50 but only returns 10 items total.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))

		var items []map[string]any
		if skip == 0 {
			for i := range 10 {
				items = append(items, map[string]any{"_id": fmt.Sprintf("id-%d", i)})
			}
		}
		// Second page returns empty (server lied about totalCount).

		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items, 50))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	result, err := c.ListAll(context.Background(), "/systemusers", ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	// Should stop when len(pageItems)==0, returning what we got.
	if len(result.Data) != 10 {
		t.Errorf("got %d results, want 10", len(result.Data))
	}
}

func TestV1Client_ListAll_LimitOne(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit != 1 {
			t.Errorf("expected limit=1 in request, got limit=%d", limit)
		}

		var items []map[string]any
		for i := range 50 {
			items = append(items, map[string]any{"_id": fmt.Sprintf("id-%d", i)})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items[:limit], 50))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	result, err := c.ListAll(context.Background(), "/systemusers", ListOptions{Limit: 1})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 1 {
		t.Errorf("got %d results, want 1", len(result.Data))
	}
}

func TestV1Client_ListAll_NegativeLimit(t *testing.T) {
	totalItems := 5
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var items []map[string]any
		for i := range totalItems {
			items = append(items, map[string]any{"_id": fmt.Sprintf("id-%d", i)})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(v1Response(items, totalItems))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	// Negative limit should behave like no limit (opts.Limit > 0 check fails).
	result, err := c.ListAll(context.Background(), "/systemusers", ListOptions{Limit: -1})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != totalItems {
		t.Errorf("got %d results, want %d (all items)", len(result.Data), totalItems)
	}
}

func TestV1Client_ListAll_EmptyIntermediatePage(t *testing.T) {
	var requestCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := requestCount.Add(1)

		var items []map[string]any
		switch page {
		case 1:
			for i := range 100 {
				items = append(items, map[string]any{"_id": fmt.Sprintf("id-%d", i)})
			}
		case 2:
			// Empty second page before totalCount reached.
			items = []map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		// Claim 500 total, but second page is empty.
		w.Write(v1Response(items, 500))
	}))
	defer ts.Close()

	c := newTestV1Client(ts.URL)
	result, err := c.ListAll(context.Background(), "/systemusers", ListOptions{PageSize: 100})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	// Should stop on empty page (len(pageItems) == 0), not loop forever.
	if len(result.Data) != 100 {
		t.Errorf("got %d results, want 100", len(result.Data))
	}
	if got := requestCount.Load(); got != 2 {
		t.Errorf("made %d requests, want 2", got)
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
