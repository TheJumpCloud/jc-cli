package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// newTestV2Client creates a V2Client pointing at the given test server.
func newTestV2Client(serverURL string) *V2Client {
	c := NewV2ClientWithKey("test-key-1234")
	c.BaseURL = serverURL
	return c
}

// v2Response builds a V2-style bare JSON array response.
func v2Response(items []map[string]any) []byte {
	b, _ := json.Marshal(items)
	return b
}

// --- ListAll Tests ---

func TestV2Client_ListAll_SinglePage(t *testing.T) {
	items := []map[string]any{
		{"id": "1", "name": "Group A"},
		{"id": "2", "name": "Group B"},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No Link header = single page.
		w.Write(v2Response(items))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	result, err := c.ListAll(context.Background(), "/usergroups", V2ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 2 {
		t.Errorf("got %d results, want 2", len(result.Data))
	}
}

func TestV2Client_ListAll_MultiplePages(t *testing.T) {
	var requestCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := requestCount.Add(1)

		var items []map[string]any
		switch page {
		case 1:
			for i := range 100 {
				items = append(items, map[string]any{"id": fmt.Sprintf("id-%d", i)})
			}
			// Set Link header pointing to next page.
			nextURL := fmt.Sprintf("<%s/usergroups?limit=100&skip=100>; rel=\"next\"", r.Host)
			// Use the full URL for the test server.
			nextURL = fmt.Sprintf("<http://%s/usergroups?limit=100&skip=100>; rel=\"next\"", r.Host)
			w.Header().Set("Link", nextURL)
		case 2:
			for i := 100; i < 150; i++ {
				items = append(items, map[string]any{"id": fmt.Sprintf("id-%d", i)})
			}
			// No Link header = last page.
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(v2Response(items))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	result, err := c.ListAll(context.Background(), "/usergroups", V2ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 150 {
		t.Errorf("got %d results, want 150", len(result.Data))
	}
	if got := requestCount.Load(); got != 2 {
		t.Errorf("made %d requests, want 2", got)
	}
}

func TestV2Client_ListAll_LimitCapsResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var items []map[string]any
		for i := range 100 {
			items = append(items, map[string]any{"id": fmt.Sprintf("id-%d", i)})
		}
		// Pretend there's a next page.
		nextURL := fmt.Sprintf("<http://%s/usergroups?limit=100&skip=100>; rel=\"next\"", r.Host)
		w.Header().Set("Link", nextURL)
		w.Header().Set("Content-Type", "application/json")
		w.Write(v2Response(items))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	result, err := c.ListAll(context.Background(), "/usergroups", V2ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 10 {
		t.Errorf("got %d results, want 10 (limit)", len(result.Data))
	}
}

func TestV2Client_ListAll_LimitSmallerThanPageSize(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := r.URL.Query().Get("limit")
		// When limit < DefaultV2PageSize, the request should use the user's limit.
		if limit != "5" {
			t.Errorf("expected limit=5 in request, got limit=%s", limit)
		}

		var items []map[string]any
		for i := range 5 {
			items = append(items, map[string]any{"id": fmt.Sprintf("id-%d", i)})
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(v2Response(items))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	result, err := c.ListAll(context.Background(), "/usergroups", V2ListOptions{Limit: 5})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 5 {
		t.Errorf("got %d results, want 5", len(result.Data))
	}
}

func TestV2Client_ListAll_EmptyResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	result, err := c.ListAll(context.Background(), "/usergroups", V2ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 0 {
		t.Errorf("got %d results, want 0", len(result.Data))
	}
}

func TestV2Client_ListAll_ContextCancellation(t *testing.T) {
	var requestCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		var items []map[string]any
		for i := range 100 {
			items = append(items, map[string]any{"id": fmt.Sprintf("id-%d", i)})
		}
		nextURL := fmt.Sprintf("<http://%s/usergroups?limit=100&skip=100>; rel=\"next\"", r.Host)
		w.Header().Set("Link", nextURL)
		w.Header().Set("Content-Type", "application/json")
		w.Write(v2Response(items))
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())

	c := newTestV2Client(ts.URL)

	// Cancel after the first page.
	go func() {
		for requestCount.Load() < 1 {
			// spin until first request completes
		}
		cancel()
	}()

	_, err := c.ListAll(ctx, "/usergroups", V2ListOptions{})
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestV2Client_ListAll_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	_, err := c.ListAll(context.Background(), "/usergroups", V2ListOptions{})
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
	if apiErr.Endpoint != "/usergroups" {
		t.Errorf("Endpoint = %q, want %q", apiErr.Endpoint, "/usergroups")
	}
}

func TestV2Client_ListAll_SortParam(t *testing.T) {
	var capturedSort string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSort = r.URL.Query().Get("sort")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	_, err := c.ListAll(context.Background(), "/usergroups", V2ListOptions{Sort: "-name"})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if capturedSort != "-name" {
		t.Errorf("sort = %q, want %q", capturedSort, "-name")
	}
}

func TestV2Client_ListAll_FilterParams(t *testing.T) {
	var capturedFilters []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedFilters = r.URL.Query()["filter"]
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	_, err := c.ListAll(context.Background(), "/usergroups", V2ListOptions{
		Filter: []string{"name:eq:Engineering", "type:eq:user_group"},
	})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(capturedFilters) != 2 {
		t.Fatalf("expected 2 filter params, got %d: %v", len(capturedFilters), capturedFilters)
	}
	if capturedFilters[0] != "name:eq:Engineering" {
		t.Errorf("filter[0] = %q, want %q", capturedFilters[0], "name:eq:Engineering")
	}
	if capturedFilters[1] != "type:eq:user_group" {
		t.Errorf("filter[1] = %q, want %q", capturedFilters[1], "type:eq:user_group")
	}
}

func TestV2Client_ListAll_SearchParam(t *testing.T) {
	var capturedSearch string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSearch = r.URL.Query().Get("search")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	_, err := c.ListAll(context.Background(), "/usergroups", V2ListOptions{Search: "engineering"})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if capturedSearch != "engineering" {
		t.Errorf("search = %q, want %q", capturedSearch, "engineering")
	}
}

func TestV2Client_ListAll_FollowsLinkHeaders(t *testing.T) {
	var capturedURLs []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURLs = append(capturedURLs, r.URL.String())

		var items []map[string]any
		switch {
		case !strings.Contains(r.URL.String(), "skip="):
			// First page.
			items = []map[string]any{{"id": "1"}, {"id": "2"}}
			nextURL := fmt.Sprintf("<http://%s/usergroups?limit=2&skip=2>; rel=\"next\"", r.Host)
			w.Header().Set("Link", nextURL)
		case strings.Contains(r.URL.String(), "skip=2"):
			// Second page.
			items = []map[string]any{{"id": "3"}}
			// No Link header = last page.
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(v2Response(items))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	result, err := c.ListAll(context.Background(), "/usergroups", V2ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 3 {
		t.Errorf("got %d results, want 3", len(result.Data))
	}
	if len(capturedURLs) != 2 {
		t.Errorf("expected 2 requests, got %d: %v", len(capturedURLs), capturedURLs)
	}
}

// --- Get Tests ---

func TestV2Client_Get_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usergroups/grp123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"grp123","name":"Engineering"}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	result, err := c.Get(context.Background(), "/usergroups/grp123")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}

	var group map[string]any
	if err := json.Unmarshal(result, &group); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if group["name"] != "Engineering" {
		t.Errorf("name = %v, want Engineering", group["name"])
	}
}

func TestV2Client_Get_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	_, err := c.Get(context.Background(), "/usergroups/nonexistent")
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

func TestV2Client_Get_ContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	c := newTestV2Client(ts.URL)
	_, err := c.Get(ctx, "/usergroups/abc")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// --- Create Tests ---

func TestV2Client_Create_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/usergroups" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"new123","name":"NewGroup"}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	body := map[string]string{"name": "NewGroup"}
	result, err := c.Create(context.Background(), "/usergroups", body)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	var group map[string]any
	if err := json.Unmarshal(result, &group); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if group["id"] != "new123" {
		t.Errorf("id = %v, want new123", group["id"])
	}
}

func TestV2Client_Create_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"message":"Group already exists"}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	_, err := c.Create(context.Background(), "/usergroups", map[string]string{"name": "dup"})
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

// --- Update Tests ---

func TestV2Client_Update_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/usergroups/grp123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"grp123","name":"Updated Group"}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	body := map[string]string{"name": "Updated Group"}
	result, err := c.Update(context.Background(), "/usergroups/grp123", body)
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}

	var group map[string]any
	json.Unmarshal(result, &group)
	if group["name"] != "Updated Group" {
		t.Errorf("name = %v, want Updated Group", group["name"])
	}
}

func TestV2Client_Update_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	_, err := c.Update(context.Background(), "/usergroups/nonexistent", map[string]string{"name": "X"})
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

func TestV2Client_Delete_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/usergroups/grp123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"grp123","name":"Deleted Group"}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	result, err := c.Delete(context.Background(), "/usergroups/grp123")
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	var group map[string]any
	json.Unmarshal(result, &group)
	if group["id"] != "grp123" {
		t.Errorf("id = %v, want grp123", group["id"])
	}
}

func TestV2Client_Delete_204NoContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	_, err := c.Delete(context.Background(), "/usergroups/grp123")
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
}

func TestV2Client_Delete_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	_, err := c.Delete(context.Background(), "/usergroups/nonexistent")
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

// --- Patch Tests ---

func TestV2Client_Patch_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/usergroups/grp123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"grp123","name":"Patched Group"}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	body := map[string]string{"name": "Patched Group"}
	result, err := c.Patch(context.Background(), "/usergroups/grp123", body)
	if err != nil {
		t.Fatalf("Patch error: %v", err)
	}

	var group map[string]any
	json.Unmarshal(result, &group)
	if group["name"] != "Patched Group" {
		t.Errorf("name = %v, want Patched Group", group["name"])
	}
}

func TestV2Client_Patch_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"Invalid field"}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	_, err := c.Patch(context.Background(), "/usergroups/grp123", map[string]string{"invalid": "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
}

// --- Constructor Tests ---

func TestV2Client_BaseURL(t *testing.T) {
	c := NewV2ClientWithKey("test-key")
	if c.BaseURL != V2BaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, V2BaseURL)
	}
	if !strings.HasPrefix(c.BaseURL, "https://console.jumpcloud.com/api/v2") {
		t.Errorf("BaseURL should point to JumpCloud V2 API, got %q", c.BaseURL)
	}
}

func TestNewV2Client_NoAPIKey(t *testing.T) {
	resetViper()
	defer resetViper()

	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_CONFIG", "/nonexistent/config.yaml")

	_, err := NewV2Client()
	if err == nil {
		t.Fatal("expected error when no API key, got nil")
	}
}

func TestV2Client_SharesTransportWithV1(t *testing.T) {
	v1 := NewV1ClientWithKey("test-key")
	v2 := NewV2ClientWithKey("test-key")

	// Both should have the same transport chain structure.
	_, v1OK := v1.HTTP.Transport.(*authTransport)
	_, v2OK := v2.HTTP.Transport.(*authTransport)
	if !v1OK || !v2OK {
		t.Error("V1 and V2 clients should both use authTransport as outer transport")
	}
}

func TestV2Client_UsesXAPIKeyAuth(t *testing.T) {
	var capturedKey string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedKey = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	c.apiKey = "my-v2-api-key"
	// Re-create the transport chain with the correct key to capture it.
	c2 := NewV2ClientWithKey("my-v2-api-key")
	c2.BaseURL = ts.URL

	_, err := c2.ListAll(context.Background(), "/usergroups", V2ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if capturedKey != "my-v2-api-key" {
		t.Errorf("x-api-key = %q, want %q", capturedKey, "my-v2-api-key")
	}
}

// --- parseLinkNext Tests ---

func TestParseLinkNext_Valid(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			"simple next",
			`<https://console.jumpcloud.com/api/v2/usergroups?limit=100&skip=100>; rel="next"`,
			"https://console.jumpcloud.com/api/v2/usergroups?limit=100&skip=100",
		},
		{
			"multiple links with next",
			`<https://example.com/prev>; rel="prev", <https://example.com/next>; rel="next"`,
			"https://example.com/next",
		},
		{
			"next with extra whitespace",
			`  <https://example.com/page2>  ;  rel="next"  `,
			"https://example.com/page2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLinkNext(tt.header)
			if got != tt.want {
				t.Errorf("parseLinkNext(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestParseLinkNext_NoNext(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"empty header", ""},
		{"only prev", `<https://example.com/prev>; rel="prev"`},
		{"no angle brackets", `https://example.com/next; rel="next"`},
		{"malformed", `just some text`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLinkNext(tt.header)
			if got != "" {
				t.Errorf("parseLinkNext(%q) = %q, want empty", tt.header, got)
			}
		})
	}
}

// --- V2 Pagination with Multiple Link Relations ---

func TestV2Client_ListAll_LinkHeaderWithPrevAndNext(t *testing.T) {
	var requestCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := requestCount.Add(1)

		items := []map[string]any{{"id": fmt.Sprintf("page%d", page)}}

		switch page {
		case 1:
			nextURL := fmt.Sprintf("<http://%s/groups?limit=1&skip=1>; rel=\"next\"", r.Host)
			w.Header().Set("Link", nextURL)
		case 2:
			// Has both prev and next.
			links := fmt.Sprintf(
				`<http://%s/groups?limit=1&skip=0>; rel="prev", <http://%s/groups?limit=1&skip=2>; rel="next"`,
				r.Host, r.Host,
			)
			w.Header().Set("Link", links)
		case 3:
			// Only prev, no next = last page.
			prevURL := fmt.Sprintf(`<http://%s/groups?limit=1&skip=1>; rel="prev"`, r.Host)
			w.Header().Set("Link", prevURL)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(v2Response(items))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	result, err := c.ListAll(context.Background(), "/groups", V2ListOptions{})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 3 {
		t.Errorf("got %d results, want 3", len(result.Data))
	}
	if got := requestCount.Load(); got != 3 {
		t.Errorf("made %d requests, want 3", got)
	}
}
