package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/config"
)

func resetViper() {
	viper.Reset()
}

func TestNewClient_WithAPIKey(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "test-key-1234")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("active_profile: default\n"), 0600)

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if c == nil {
		t.Fatal("NewClient() returned nil client")
	}
	if c.BaseURL != BaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, BaseURL)
	}
}

func TestNewClient_NoAPIKey(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	_, err := NewClient()
	if err == nil {
		t.Fatal("expected error when no API key, got nil")
	}
	if err != ErrNoAPIKey {
		t.Errorf("expected ErrNoAPIKey, got: %v", err)
	}
}

func TestNewClient_MissingAPIKeyErrorMessage(t *testing.T) {
	msg := ErrNoAPIKey.Error()
	if !strings.Contains(msg, "jc auth login") {
		t.Errorf("error message should suggest 'jc auth login', got: %q", msg)
	}
	if !strings.Contains(msg, "JC_API_KEY") {
		t.Errorf("error message should mention JC_API_KEY, got: %q", msg)
	}
}

func TestNewClientWithKey(t *testing.T) {
	c := NewClientWithKey("my-test-key")
	if c == nil {
		t.Fatal("NewClientWithKey() returned nil")
	}
	if c.BaseURL != BaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, BaseURL)
	}
	if c.HTTP == nil {
		t.Fatal("HTTP client is nil")
	}
}

func TestAuthTransport_InjectsHeaders(t *testing.T) {
	var capturedHeaders http.Header

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewClientWithKey("test-api-key-abcd")
	c.BaseURL = ts.URL

	resp, err := c.HTTP.Get(ts.URL + "/test")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	resp.Body.Close()

	// Verify x-api-key header injected.
	if got := capturedHeaders.Get("x-api-key"); got != "test-api-key-abcd" {
		t.Errorf("x-api-key = %q, want %q", got, "test-api-key-abcd")
	}

	// Verify Content-Type header.
	if got := capturedHeaders.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	// Verify Accept header.
	if got := capturedHeaders.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want %q", got, "application/json")
	}

	// Verify User-Agent header.
	if got := capturedHeaders.Get("User-Agent"); !strings.HasPrefix(got, "jc/") {
		t.Errorf("User-Agent should start with 'jc/', got %q", got)
	}
}

func TestRedactKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"empty key", "", "(none)"},
		{"short key (1 char)", "a", "****"},
		{"short key (4 chars)", "abcd", "****"},
		{"normal key", "my-api-key-1234", "****1234"},
		{"long key", "abcdefghijklmnopqrstuvwxyz", "****wxyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactKey(tt.key)
			if got != tt.want {
				t.Errorf("RedactKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestRedactKey_NeverExposesFullKey(t *testing.T) {
	keys := []string{
		"secret-api-key-12345678",
		"abc",
		"short",
		"a-very-long-api-key-that-should-be-hidden",
	}
	for _, key := range keys {
		redacted := RedactKey(key)
		// The full key should never appear in the redacted output.
		if redacted == key {
			t.Errorf("RedactKey(%q) returned the original key unredacted", key)
		}
		// The redacted key should contain **** for non-empty keys.
		if key != "" && !strings.Contains(redacted, "****") {
			t.Errorf("RedactKey(%q) = %q, missing redaction asterisks", key, redacted)
		}
	}
}

func TestNewClient_FromConfigProfile(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_PROFILE", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: myorg
profiles:
  myorg:
    api_key: "profile-key-5678"
    org_id: "org-123"
`), 0600)

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if c == nil {
		t.Fatal("NewClient() returned nil")
	}
}

func TestNewClient_EnvKeyOverridesProfileKey(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "env-override-key")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: default
profiles:
  default:
    api_key: "config-key"
`), 0600)

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	// Should succeed because JC_API_KEY provides the key.
	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if c == nil {
		t.Fatal("NewClient() returned nil")
	}
}

func TestUserAgent_Format(t *testing.T) {
	ua := userAgent()
	if !strings.HasPrefix(ua, "jc/") {
		t.Errorf("userAgent() should start with 'jc/', got %q", ua)
	}
	if !strings.Contains(ua, "Go;") {
		t.Errorf("userAgent() should contain 'Go;', got %q", ua)
	}
}
