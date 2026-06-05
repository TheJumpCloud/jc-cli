package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTokenCache_FetchToken_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format.
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify Basic auth header.
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			t.Errorf("expected Basic auth header, got %q", auth)
		}

		// Verify Content-Type.
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("expected application/x-www-form-urlencoded, got %q", ct)
		}

		// Verify form body.
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if gt := r.FormValue("grant_type"); gt != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %q", gt)
		}
		if scope := r.FormValue("scope"); scope != "api" {
			t.Errorf("expected scope=api, got %q", scope)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-bearer-token-123",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer ts.Close()

	orig := oauthTokenURL
	oauthTokenURL = ts.URL
	defer func() { oauthTokenURL = orig }()

	tc := NewTokenCache("test-client-id", "test-client-secret")
	token, err := tc.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}
	if token != "test-bearer-token-123" {
		t.Errorf("token = %q, want %q", token, "test-bearer-token-123")
	}
}

func TestTokenCache_CachesToken(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "cached-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer ts.Close()

	orig := oauthTokenURL
	oauthTokenURL = ts.URL
	defer func() { oauthTokenURL = orig }()

	tc := NewTokenCache("client-id", "client-secret")

	// First call should fetch.
	_, err := tc.Token(context.Background())
	if err != nil {
		t.Fatalf("first Token() error: %v", err)
	}

	// Second call should use cache.
	token, err := tc.Token(context.Background())
	if err != nil {
		t.Fatalf("second Token() error: %v", err)
	}
	if token != "cached-token" {
		t.Errorf("token = %q, want %q", token, "cached-token")
	}
	if callCount != 1 {
		t.Errorf("expected 1 server call (cached), got %d", callCount)
	}
}

func TestTokenCache_RefreshesExpiredToken(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "refreshed-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer ts.Close()

	origURL := oauthTokenURL
	oauthTokenURL = ts.URL
	defer func() { oauthTokenURL = origURL }()

	// Override nowFunc to simulate time passing.
	origNow := nowFunc
	fakeNow := time.Now()
	nowFunc = func() time.Time { return fakeNow }
	defer func() { nowFunc = origNow }()

	tc := NewTokenCache("client-id", "client-secret")

	// First call.
	_, _ = tc.Token(context.Background())
	if callCount != 1 {
		t.Fatalf("expected 1 call after first Token(), got %d", callCount)
	}

	// Advance time past expiry (3600s + 30s buffer).
	fakeNow = fakeNow.Add(3631 * time.Second)

	// Should refresh.
	_, err := tc.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error after expiry: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (refresh after expiry), got %d", callCount)
	}
}

func TestTokenCache_InvalidCredentials(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer ts.Close()

	orig := oauthTokenURL
	oauthTokenURL = ts.URL
	defer func() { oauthTokenURL = orig }()

	tc := NewTokenCache("bad-id", "bad-secret")
	_, err := tc.Token(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid credentials")
	}
	if !strings.Contains(err.Error(), "invalid client credentials") {
		t.Errorf("error should mention 'invalid client credentials', got: %v", err)
	}
}

func TestTokenCache_ForbiddenCredentials(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"insufficient_scope"}`))
	}))
	defer ts.Close()

	orig := oauthTokenURL
	oauthTokenURL = ts.URL
	defer func() { oauthTokenURL = orig }()

	tc := NewTokenCache("client-id", "client-secret")
	_, err := tc.Token(context.Background())
	if err == nil {
		t.Fatal("expected error for forbidden credentials")
	}
	if !strings.Contains(err.Error(), "lack permission") {
		t.Errorf("error should mention 'lack permission', got: %v", err)
	}
}

func TestTokenCache_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server_error"}`))
	}))
	defer ts.Close()

	orig := oauthTokenURL
	oauthTokenURL = ts.URL
	defer func() { oauthTokenURL = orig }()

	tc := NewTokenCache("client-id", "client-secret")
	_, err := tc.Token(context.Background())
	if err == nil {
		t.Fatal("expected error for server error")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error should mention 'HTTP 500', got: %v", err)
	}
}

func TestTokenCache_EmptyAccessToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer ts.Close()

	orig := oauthTokenURL
	oauthTokenURL = ts.URL
	defer func() { oauthTokenURL = orig }()

	tc := NewTokenCache("client-id", "client-secret")
	_, err := tc.Token(context.Background())
	if err == nil {
		t.Fatal("expected error for empty access token")
	}
	if !strings.Contains(err.Error(), "missing access_token") {
		t.Errorf("error should mention 'missing access_token', got: %v", err)
	}
}

func TestTokenCache_DefaultExpiresIn(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// expires_in omitted (defaults to 0 in JSON).
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "token-no-expiry",
			"token_type":   "Bearer",
		})
	}))
	defer ts.Close()

	orig := oauthTokenURL
	oauthTokenURL = ts.URL
	defer func() { oauthTokenURL = orig }()

	origNow := nowFunc
	fakeNow := time.Now()
	nowFunc = func() time.Time { return fakeNow }
	defer func() { nowFunc = origNow }()

	tc := NewTokenCache("client-id", "client-secret")
	_, err := tc.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}

	// Should have defaulted to 1 hour.
	expiresAt := tc.ExpiresAt()
	expected := fakeNow.Add(3600 * time.Second)
	if !expiresAt.Equal(expected) {
		t.Errorf("expiresAt = %v, want %v", expiresAt, expected)
	}
}

func TestTokenCache_ExpiresAt_ZeroBeforeFetch(t *testing.T) {
	tc := NewTokenCache("client-id", "client-secret")
	if expiresAt := tc.ExpiresAt(); !expiresAt.IsZero() {
		t.Errorf("ExpiresAt() should be zero before first fetch, got %v", expiresAt)
	}
}

func TestTokenCache_ConnectionError(t *testing.T) {
	orig := oauthTokenURL
	oauthTokenURL = "http://127.0.0.1:1" // No server listening.
	defer func() { oauthTokenURL = orig }()

	tc := NewTokenCache("client-id", "client-secret")
	_, err := tc.Token(context.Background())
	if err == nil {
		t.Fatal("expected error for connection failure")
	}
	if !strings.Contains(err.Error(), "connect") && !strings.Contains(err.Error(), "connection") {
		t.Errorf("error should mention connection failure, got: %v", err)
	}
}
