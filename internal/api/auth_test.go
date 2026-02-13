package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateAPIKey_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request targets /organizations.
		if r.URL.Path != "/organizations" {
			t.Errorf("expected path /organizations, got %s", r.URL.Path)
		}

		// Verify API key is present.
		if got := r.Header.Get("x-api-key"); got != "valid-key" {
			t.Errorf("x-api-key = %q, want %q", got, "valid-key")
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"_id":         "org-abc123",
					"displayName": "Acme Corp",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := NewClientWithKey("valid-key")
	c.BaseURL = ts.URL

	org, err := c.ValidateAPIKey()
	if err != nil {
		t.Fatalf("ValidateAPIKey() error: %v", err)
	}
	if org.ID != "org-abc123" {
		t.Errorf("org.ID = %q, want %q", org.ID, "org-abc123")
	}
	if org.DisplayName != "Acme Corp" {
		t.Errorf("org.DisplayName = %q, want %q", org.DisplayName, "Acme Corp")
	}
}

func TestValidateAPIKey_SingleOrgResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Some API versions may return a direct object.
		resp := map[string]interface{}{
			"_id":         "org-direct",
			"displayName": "Direct Org",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := NewClientWithKey("valid-key")
	c.BaseURL = ts.URL

	org, err := c.ValidateAPIKey()
	if err != nil {
		t.Fatalf("ValidateAPIKey() error: %v", err)
	}
	if org.ID != "org-direct" {
		t.Errorf("org.ID = %q, want %q", org.ID, "org-direct")
	}
}

func TestValidateAPIKey_Unauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()

	c := NewClientWithKey("bad-key")
	c.BaseURL = ts.URL

	_, err := c.ValidateAPIKey()
	if err == nil {
		t.Fatal("expected error for unauthorized, got nil")
	}
	if !strings.Contains(err.Error(), "invalid API key") {
		t.Errorf("error should mention 'invalid API key', got: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention HTTP 401, got: %v", err)
	}
	if !strings.Contains(err.Error(), "jc auth login") {
		t.Errorf("error should suggest 'jc auth login', got: %v", err)
	}
}

func TestValidateAPIKey_Forbidden(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Forbidden"}`))
	}))
	defer ts.Close()

	c := NewClientWithKey("limited-key")
	c.BaseURL = ts.URL

	_, err := c.ValidateAPIKey()
	if err == nil {
		t.Fatal("expected error for forbidden, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention HTTP 403, got: %v", err)
	}
}

func TestValidateAPIKey_ServerError(t *testing.T) {
	origSleep := retrySleepFn
	retrySleepFn = func(d time.Duration) {} // no-op for fast tests
	defer func() { retrySleepFn = origSleep }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"Internal Server Error"}`))
	}))
	defer ts.Close()

	c := NewClientWithKey("some-key")
	c.BaseURL = ts.URL

	_, err := c.ValidateAPIKey()
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention HTTP 500, got: %v", err)
	}
}

func TestValidateAPIKey_EmptyResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"results": []interface{}{},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := NewClientWithKey("valid-key")
	c.BaseURL = ts.URL

	_, err := c.ValidateAPIKey()
	if err == nil {
		t.Fatal("expected error for empty results, got nil")
	}
	if !strings.Contains(err.Error(), "no organizations") {
		t.Errorf("error should mention 'no organizations', got: %v", err)
	}
}

func TestValidateAPIKey_ConnectionError(t *testing.T) {
	origSleep := retrySleepFn
	retrySleepFn = func(d time.Duration) {} // no-op for fast tests
	defer func() { retrySleepFn = origSleep }()

	// Use a non-existent URL to simulate connection failure.
	c := NewClientWithKey("some-key")
	c.BaseURL = "http://127.0.0.1:1" // Port 1 should be closed

	_, err := c.ValidateAPIKey()
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to connect") {
		t.Errorf("error should mention connection failure, got: %v", err)
	}
}

func TestValidateAPIKey_HeadersInjected(t *testing.T) {
	var gotKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"_id": "org-1", "displayName": "Test"},
			},
		})
	}))
	defer ts.Close()

	c := NewClientWithKey("my-secret-key")
	c.BaseURL = ts.URL

	_, err := c.ValidateAPIKey()
	if err != nil {
		t.Fatalf("ValidateAPIKey() error: %v", err)
	}

	if gotKey != "my-secret-key" {
		t.Errorf("x-api-key header = %q, want %q", gotKey, "my-secret-key")
	}
}

func TestTruncateBody(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		maxLen int
		want   string
	}{
		{"short body", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated", "hello world this is long", 10, "hello worl..."},
		{"empty", "", 10, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateBody([]byte(tt.body), tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateBody(%q, %d) = %q, want %q", tt.body, tt.maxLen, got, tt.want)
			}
		})
	}
}
