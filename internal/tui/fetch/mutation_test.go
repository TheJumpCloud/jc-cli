package fetch

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
)

func TestDeleteV1(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"_id":"aabbccddee112233aabb0011"}`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache:       NewCache(),
		NewV1Client: func() (*api.V1Client, error) { return newTestV1Client(srv.URL) },
	}

	// Pre-populate cache to verify invalidation.
	f.Cache.Set("v1:users:/systemusers:{}", nil, ListTTL)

	gen := NextGeneration()
	cmd := f.DeleteV1("users", "/systemusers", "aabbccddee112233aabb0011", gen)
	msg := cmd()

	result, ok := msg.(MutationResultMsg)
	if !ok {
		t.Fatalf("expected MutationResultMsg, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Generation != gen {
		t.Errorf("generation = %d, want %d", result.Generation, gen)
	}
	if method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", method)
	}
	if path != "/api/systemusers/aabbccddee112233aabb0011" {
		t.Errorf("path = %s, want /api/systemusers/aabbccddee112233aabb0011", path)
	}

	// Cache should be invalidated.
	if _, ok := f.Cache.Get("v1:users:/systemusers:{}"); ok {
		t.Error("cache should be invalidated after delete")
	}
}

func TestDeleteV2(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache:       NewCache(),
		NewV2Client: func() (*api.V2Client, error) { return newTestV2Client(srv.URL) },
	}

	gen := NextGeneration()
	cmd := f.DeleteV2("iplists", "/iplists", "aabbccddee112233aabb0022", gen)
	msg := cmd()

	result := msg.(MutationResultMsg)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", method)
	}
	if path != "/api/v2/iplists/aabbccddee112233aabb0022" {
		t.Errorf("path = %s, want /api/v2/iplists/aabbccddee112233aabb0022", path)
	}
}

func TestCreateV1(t *testing.T) {
	var method, path string
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"_id":"new001","username":"alice"}`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache:       NewCache(),
		NewV1Client: func() (*api.V1Client, error) { return newTestV1Client(srv.URL) },
	}

	f.Cache.Set("v1:users:/systemusers:{}", nil, ListTTL)

	gen := NextGeneration()
	cmd := f.CreateV1("users", "/systemusers", map[string]any{
		"username": "alice",
		"email":    "alice@example.com",
	}, gen)
	msg := cmd()

	result := msg.(MutationResultMsg)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Data == nil {
		t.Error("data should not be nil for create")
	}
	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if path != "/api/systemusers" {
		t.Errorf("path = %s, want /api/systemusers", path)
	}
	if receivedBody["username"] != "alice" {
		t.Errorf("body username = %v, want 'alice'", receivedBody["username"])
	}
	if receivedBody["email"] != "alice@example.com" {
		t.Errorf("body email = %v, want 'alice@example.com'", receivedBody["email"])
	}

	if _, ok := f.Cache.Get("v1:users:/systemusers:{}"); ok {
		t.Error("cache should be invalidated after create")
	}
}

func TestCreateV2(t *testing.T) {
	var method string
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"new002","name":"Test List"}`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache:       NewCache(),
		NewV2Client: func() (*api.V2Client, error) { return newTestV2Client(srv.URL) },
	}

	gen := NextGeneration()
	cmd := f.CreateV2("iplists", "/iplists", map[string]any{
		"name": "Test List",
	}, gen)
	msg := cmd()

	result := msg.(MutationResultMsg)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if receivedBody["name"] != "Test List" {
		t.Errorf("body name = %v, want 'Test List'", receivedBody["name"])
	}
}

func TestUpdateV1(t *testing.T) {
	var method, path string
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		fmt.Fprint(w, `{"_id":"aabbccddee112233aabb0033","email":"updated@example.com"}`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache:       NewCache(),
		NewV1Client: func() (*api.V1Client, error) { return newTestV1Client(srv.URL) },
	}

	gen := NextGeneration()
	cmd := f.UpdateV1("users", "/systemusers", "aabbccddee112233aabb0033", map[string]any{
		"email": "updated@example.com",
	}, gen)
	msg := cmd()

	result := msg.(MutationResultMsg)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if method != http.MethodPut {
		t.Errorf("method = %s, want PUT", method)
	}
	if path != "/api/systemusers/aabbccddee112233aabb0033" {
		t.Errorf("path = %s, want /api/systemusers/aabbccddee112233aabb0033", path)
	}
	if receivedBody["email"] != "updated@example.com" {
		t.Errorf("body email = %v, want 'updated@example.com'", receivedBody["email"])
	}
}

func TestUpdateV2(t *testing.T) {
	var method, path string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		fmt.Fprint(w, `{"id":"aabbccddee112233aabb0044","name":"Updated"}`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache:       NewCache(),
		NewV2Client: func() (*api.V2Client, error) { return newTestV2Client(srv.URL) },
	}

	gen := NextGeneration()
	cmd := f.UpdateV2("iplists", "/iplists", "aabbccddee112233aabb0044", map[string]any{
		"name": "Updated",
	}, gen)
	msg := cmd()

	result := msg.(MutationResultMsg)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if method != http.MethodPut {
		t.Errorf("method = %s, want PUT", method)
	}
	if path != "/api/v2/iplists/aabbccddee112233aabb0044" {
		t.Errorf("path = %s, want /api/v2/iplists/aabbccddee112233aabb0044", path)
	}
}

func TestDeleteV1_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache:       NewCache(),
		NewV1Client: func() (*api.V1Client, error) { return newTestV1Client(srv.URL) },
	}

	gen := NextGeneration()
	cmd := f.DeleteV1("users", "/systemusers", "aabbccddee112233aabb0055", gen)
	msg := cmd()

	result := msg.(MutationResultMsg)
	if result.Err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestCreateV2_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"message":"name is required"}`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache:       NewCache(),
		NewV2Client: func() (*api.V2Client, error) { return newTestV2Client(srv.URL) },
	}

	gen := NextGeneration()
	cmd := f.CreateV2("iplists", "/iplists", map[string]any{}, gen)
	msg := cmd()

	result := msg.(MutationResultMsg)
	if result.Err == nil {
		t.Error("expected error for 400 response")
	}
}
