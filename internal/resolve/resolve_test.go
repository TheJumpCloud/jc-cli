package resolve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/spf13/viper"
)

// testV1Client creates a V1 client pointing at the given test server URL.
func testV1Client(serverURL string) *api.V1Client {
	c := api.NewV1ClientWithKey("test-key")
	c.BaseURL = serverURL
	return c
}

// startTestServer creates a mock server that returns canned users/systems data.
func startTestServer(t *testing.T, endpoint string, items []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == endpoint && r.Method == http.MethodGet {
			skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit == 0 {
				limit = 100
			}

			end := skip + limit
			if end > len(items) {
				end = len(items)
			}
			var page []map[string]any
			if skip < len(items) {
				page = items[skip:end]
			}
			if page == nil {
				page = []map[string]any{}
			}

			resp := map[string]any{
				"results":    page,
				"totalCount": len(items),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	viper.SetDefault("cache.enabled", true)
	viper.SetDefault("cache.ttl", 300)
	viper.SetDefault("cache.directory", "")
}

// --- IsID Tests ---

func TestIsID_Valid(t *testing.T) {
	tests := []string{
		"507f1f77bcf86cd799439011",
		"aaaaaaaaaaaaaaaaaaaaaaaa",
		"AAAAAAAAAAAAAAAAAAAAAAAA",
		"123456789012345678901234",
	}
	for _, id := range tests {
		if !IsID(id) {
			t.Errorf("IsID(%q) = false, want true", id)
		}
	}
}

func TestIsID_Invalid(t *testing.T) {
	tests := []string{
		"alice",
		"jdoe-mbp",
		"short123",
		"507f1f77bcf86cd79943901",  // 23 chars
		"507f1f77bcf86cd7994390111", // 25 chars
		"507f1f77bcf86cd79943901g",  // non-hex 'g'
		"",
	}
	for _, id := range tests {
		if IsID(id) {
			t.Errorf("IsID(%q) = true, want false", id)
		}
	}
}

// --- Resolve Tests ---

func TestResolve_IDPassthrough(t *testing.T) {
	resetViper(t)

	// No server needed — ID should be returned directly.
	resolver := NewResolver(nil)
	id, err := resolver.Resolve(context.Background(), "507f1f77bcf86cd799439011", UserConfig)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if id != "507f1f77bcf86cd799439011" {
		t.Errorf("id = %q, want %q", id, "507f1f77bcf86cd799439011")
	}
}

func TestResolve_UserByUsername(t *testing.T) {
	resetViper(t)
	// Disable cache for this test.
	viper.Set("cache.enabled", false)

	users := []map[string]any{
		{"_id": "aaa111aaa111aaa111aaa111", "username": "alice", "email": "alice@example.com"},
		{"_id": "bbb222bbb222bbb222bbb222", "username": "bob", "email": "bob@example.com"},
	}
	ts := startTestServer(t, "/systemusers", users)
	defer ts.Close()

	resolver := NewResolver(testV1Client(ts.URL))
	id, err := resolver.Resolve(context.Background(), "alice", UserConfig)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if id != "aaa111aaa111aaa111aaa111" {
		t.Errorf("id = %q, want %q", id, "aaa111aaa111aaa111aaa111")
	}
}

func TestResolve_DeviceByHostname(t *testing.T) {
	resetViper(t)
	viper.Set("cache.enabled", false)

	devices := []map[string]any{
		{"_id": "dev111dev111dev111dev111", "hostname": "JDOE-MBP", "os": "Mac OS X"},
		{"_id": "dev222dev222dev222dev222", "hostname": "SERVER-01", "os": "Ubuntu"},
	}
	ts := startTestServer(t, "/systems", devices)
	defer ts.Close()

	resolver := NewResolver(testV1Client(ts.URL))
	id, err := resolver.Resolve(context.Background(), "JDOE-MBP", DeviceConfig)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if id != "dev111dev111dev111dev111" {
		t.Errorf("id = %q, want %q", id, "dev111dev111dev111dev111")
	}
}

func TestResolve_CaseInsensitive(t *testing.T) {
	resetViper(t)
	viper.Set("cache.enabled", false)

	users := []map[string]any{
		{"_id": "aaa111aaa111aaa111aaa111", "username": "Alice", "email": "alice@example.com"},
	}
	ts := startTestServer(t, "/systemusers", users)
	defer ts.Close()

	resolver := NewResolver(testV1Client(ts.URL))
	id, err := resolver.Resolve(context.Background(), "alice", UserConfig)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if id != "aaa111aaa111aaa111aaa111" {
		t.Errorf("id = %q, want %q", id, "aaa111aaa111aaa111aaa111")
	}
}

func TestResolve_NotFound(t *testing.T) {
	resetViper(t)
	viper.Set("cache.enabled", false)

	users := []map[string]any{
		{"_id": "aaa111aaa111aaa111aaa111", "username": "alice", "email": "alice@example.com"},
	}
	ts := startTestServer(t, "/systemusers", users)
	defer ts.Close()

	resolver := NewResolver(testV1Client(ts.URL))
	_, err := resolver.Resolve(context.Background(), "nonexistent", UserConfig)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !containsStr(err.Error(), "not found") {
		t.Errorf("error should say 'not found': %v", err)
	}
}

func TestResolve_Ambiguous(t *testing.T) {
	resetViper(t)
	viper.Set("cache.enabled", false)

	// Two devices with the same hostname (edge case).
	devices := []map[string]any{
		{"_id": "dev111dev111dev111dev111", "hostname": "duplicate", "os": "Mac OS X"},
		{"_id": "dev222dev222dev222dev222", "hostname": "duplicate", "os": "Ubuntu"},
	}
	ts := startTestServer(t, "/systems", devices)
	defer ts.Close()

	resolver := NewResolver(testV1Client(ts.URL))
	_, err := resolver.Resolve(context.Background(), "duplicate", DeviceConfig)
	if err == nil {
		t.Fatal("expected ambiguous error, got nil")
	}
	if !containsStr(err.Error(), "ambiguous") {
		t.Errorf("error should say 'ambiguous': %v", err)
	}
	if !containsStr(err.Error(), "dev111dev111dev111dev111") {
		t.Errorf("error should list matching IDs: %v", err)
	}
}

// --- Cache Tests ---

func TestResolve_CacheHit(t *testing.T) {
	resetViper(t)

	cacheDir := t.TempDir()
	viper.Set("cache.directory", cacheDir)

	// Pre-populate the cache.
	cf := cacheFile{
		"alice": {
			ID:        "cached-id-123456789012",
			Timestamp: time.Now(),
		},
	}
	data, _ := json.Marshal(cf)
	os.WriteFile(filepath.Join(cacheDir, "users.json"), data, 0600)

	// Server should NOT be called because cache hit should resolve it.
	var apiCalled bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	resolver := NewResolver(testV1Client(ts.URL))
	id, err := resolver.Resolve(context.Background(), "alice", UserConfig)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if id != "cached-id-123456789012" {
		t.Errorf("id = %q, want cached value", id)
	}
	if apiCalled {
		t.Error("API should not be called on cache hit")
	}
}

func TestResolve_CacheExpired(t *testing.T) {
	resetViper(t)
	viper.Set("cache.ttl", 300)

	cacheDir := t.TempDir()
	viper.Set("cache.directory", cacheDir)

	// Cache entry from 10 minutes ago (expired with 300s TTL).
	cf := cacheFile{
		"alice": {
			ID:        "old-cached-id-0000000000",
			Timestamp: time.Now().Add(-10 * time.Minute),
		},
	}
	data, _ := json.Marshal(cf)
	os.WriteFile(filepath.Join(cacheDir, "users.json"), data, 0600)

	users := []map[string]any{
		{"_id": "fresh-id-1234567890123", "username": "alice", "email": "alice@example.com"},
	}
	ts := startTestServer(t, "/systemusers", users)
	defer ts.Close()

	resolver := NewResolver(testV1Client(ts.URL))
	id, err := resolver.Resolve(context.Background(), "alice", UserConfig)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if id != "fresh-id-1234567890123" {
		t.Errorf("id = %q, want fresh API value", id)
	}
}

func TestResolve_CacheStored(t *testing.T) {
	resetViper(t)

	cacheDir := t.TempDir()
	viper.Set("cache.directory", cacheDir)

	users := []map[string]any{
		{"_id": "aaa111aaa111aaa111aaa111", "username": "alice", "email": "alice@example.com"},
	}
	ts := startTestServer(t, "/systemusers", users)
	defer ts.Close()

	resolver := NewResolver(testV1Client(ts.URL))
	_, err := resolver.Resolve(context.Background(), "alice", UserConfig)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	// Verify cache file was written.
	cacheData, err := os.ReadFile(filepath.Join(cacheDir, "users.json"))
	if err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	var cf cacheFile
	if err := json.Unmarshal(cacheData, &cf); err != nil {
		t.Fatalf("cache file not valid JSON: %v", err)
	}

	entry, ok := cf["alice"]
	if !ok {
		t.Fatal("cache missing 'alice' entry")
	}
	if entry.ID != "aaa111aaa111aaa111aaa111" {
		t.Errorf("cached id = %q, want %q", entry.ID, "aaa111aaa111aaa111aaa111")
	}
}

func TestResolve_NoCacheFlag(t *testing.T) {
	resetViper(t)
	viper.Set("no-cache", true)

	cacheDir := t.TempDir()
	viper.Set("cache.directory", cacheDir)

	// Pre-populate cache with stale data.
	cf := cacheFile{
		"alice": {
			ID:        "stale-cached-id-000000",
			Timestamp: time.Now(),
		},
	}
	data, _ := json.Marshal(cf)
	os.WriteFile(filepath.Join(cacheDir, "users.json"), data, 0600)

	// API should be called despite cache hit because --no-cache is set.
	users := []map[string]any{
		{"_id": "fresh-api-id-1234567890", "username": "alice", "email": "alice@example.com"},
	}
	ts := startTestServer(t, "/systemusers", users)
	defer ts.Close()

	resolver := NewResolver(testV1Client(ts.URL))
	id, err := resolver.Resolve(context.Background(), "alice", UserConfig)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if id != "fresh-api-id-1234567890" {
		t.Errorf("id = %q, want fresh API value (--no-cache should bypass cache)", id)
	}
}

func TestResolve_CacheDisabled(t *testing.T) {
	resetViper(t)
	viper.Set("cache.enabled", false)

	cacheDir := t.TempDir()
	viper.Set("cache.directory", cacheDir)

	users := []map[string]any{
		{"_id": "aaa111aaa111aaa111aaa111", "username": "alice", "email": "alice@example.com"},
	}
	ts := startTestServer(t, "/systemusers", users)
	defer ts.Close()

	resolver := NewResolver(testV1Client(ts.URL))
	_, err := resolver.Resolve(context.Background(), "alice", UserConfig)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	// Cache file should NOT be written when cache is disabled.
	_, err = os.ReadFile(filepath.Join(cacheDir, "users.json"))
	if err == nil {
		t.Error("cache file should not exist when cache is disabled")
	}
}

func TestResolve_CacheDirCreated(t *testing.T) {
	resetViper(t)

	cacheDir := filepath.Join(t.TempDir(), "nested", "cache", "jc")
	viper.Set("cache.directory", cacheDir)

	users := []map[string]any{
		{"_id": "aaa111aaa111aaa111aaa111", "username": "alice", "email": "alice@example.com"},
	}
	ts := startTestServer(t, "/systemusers", users)
	defer ts.Close()

	resolver := NewResolver(testV1Client(ts.URL))
	_, err := resolver.Resolve(context.Background(), "alice", UserConfig)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	// Verify directory was created.
	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatalf("cache directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("cache path should be a directory")
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("cache dir permissions = %v, want 0700", info.Mode().Perm())
	}
}

// helper
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
