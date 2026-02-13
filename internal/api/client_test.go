package api

import (
	"bytes"
	"crypto/tls"
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

// --- Transport layer tests (US-007) ---

func TestBaseTransport_TLS12Minimum(t *testing.T) {
	bt := baseTransport()
	if bt.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig is nil")
	}
	if bt.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d (TLS 1.2)", bt.TLSClientConfig.MinVersion, tls.VersionTLS12)
	}
}

func TestBaseTransport_ConnectionPooling(t *testing.T) {
	bt := baseTransport()
	if bt.MaxIdleConns != 100 {
		t.Errorf("MaxIdleConns = %d, want 100", bt.MaxIdleConns)
	}
	if bt.MaxIdleConnsPerHost != 10 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 10", bt.MaxIdleConnsPerHost)
	}
	if bt.IdleConnTimeout != 90*1e9 {
		t.Errorf("IdleConnTimeout = %v, want 90s", bt.IdleConnTimeout)
	}
}

func TestLoggingTransport_NoLogWhenSilent(t *testing.T) {
	resetViper()
	defer resetViper()

	var buf bytes.Buffer
	origWriter := logWriter
	logWriter = &buf
	defer func() { logWriter = origWriter }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewClientWithKey("test-key-1234")
	c.BaseURL = ts.URL

	resp, err := c.HTTP.Get(ts.URL + "/test")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	resp.Body.Close()

	if buf.Len() != 0 {
		t.Errorf("expected no log output when verbose/debug disabled, got: %q", buf.String())
	}
}

func TestLoggingTransport_VerboseOutput(t *testing.T) {
	resetViper()
	defer resetViper()

	viper.Set("verbose", true)

	var buf bytes.Buffer
	origWriter := logWriter
	logWriter = &buf
	defer func() { logWriter = origWriter }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewClientWithKey("test-key-1234")
	c.BaseURL = ts.URL

	resp, err := c.HTTP.Get(ts.URL + "/test")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	resp.Body.Close()

	output := buf.String()

	// Verbose should log: method, URL, status code, duration.
	if !strings.Contains(output, "GET") {
		t.Errorf("verbose output should contain method, got: %q", output)
	}
	if !strings.Contains(output, "/test") {
		t.Errorf("verbose output should contain URL path, got: %q", output)
	}
	if !strings.Contains(output, "200") {
		t.Errorf("verbose output should contain status code 200, got: %q", output)
	}
	// Should contain a duration like "(Xms)".
	if !strings.Contains(output, "ms)") && !strings.Contains(output, "µs)") && !strings.Contains(output, "s)") {
		t.Errorf("verbose output should contain duration, got: %q", output)
	}
	// Should NOT contain request/response headers (that's debug-only).
	if strings.Contains(output, "Request Headers:") {
		t.Errorf("verbose output should not contain request headers, got: %q", output)
	}
}

func TestLoggingTransport_DebugOutput(t *testing.T) {
	resetViper()
	defer resetViper()

	viper.Set("debug", true)

	var buf bytes.Buffer
	origWriter := logWriter
	logWriter = &buf
	defer func() { logWriter = origWriter }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Response", "test-value")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewClientWithKey("secret-api-key-9999")
	c.BaseURL = ts.URL

	resp, err := c.HTTP.Get(ts.URL + "/debug-test")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	resp.Body.Close()

	output := buf.String()

	// Debug should contain request headers.
	if !strings.Contains(output, "Request Headers:") {
		t.Errorf("debug output should contain 'Request Headers:', got: %q", output)
	}
	// Debug should contain response headers.
	if !strings.Contains(output, "Response Headers:") {
		t.Errorf("debug output should contain 'Response Headers:', got: %q", output)
	}
	// Debug should also log the verbose line (method, URL, status).
	if !strings.Contains(output, "GET") {
		t.Errorf("debug output should contain method, got: %q", output)
	}
	if !strings.Contains(output, "200") {
		t.Errorf("debug output should contain status code 200, got: %q", output)
	}
}

func TestLoggingTransport_DebugRedactsAPIKey(t *testing.T) {
	resetViper()
	defer resetViper()

	viper.Set("debug", true)

	var buf bytes.Buffer
	origWriter := logWriter
	logWriter = &buf
	defer func() { logWriter = origWriter }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	apiKey := "super-secret-api-key-ABCD"
	c := NewClientWithKey(apiKey)
	c.BaseURL = ts.URL

	resp, err := c.HTTP.Get(ts.URL + "/test")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	resp.Body.Close()

	output := buf.String()

	// The full API key should NEVER appear in debug output.
	if strings.Contains(output, apiKey) {
		t.Errorf("debug output should NOT contain the full API key %q, got: %q", apiKey, output)
	}
	// The redacted version should appear.
	if !strings.Contains(output, "****ABCD") {
		t.Errorf("debug output should contain redacted key '****ABCD', got: %q", output)
	}
}

func TestLoggingTransport_VerboseLogsError(t *testing.T) {
	resetViper()
	defer resetViper()

	viper.Set("verbose", true)

	var buf bytes.Buffer
	origWriter := logWriter
	logWriter = &buf
	defer func() { logWriter = origWriter }()

	c := NewClientWithKey("test-key")
	c.BaseURL = "http://127.0.0.1:1" // Port 1 should be unreachable.

	_, err := c.HTTP.Get(c.BaseURL + "/fail")
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}

	output := buf.String()
	if !strings.Contains(output, "ERROR") {
		t.Errorf("verbose output should contain 'ERROR' on connection failure, got: %q", output)
	}
	if !strings.Contains(output, "GET") {
		t.Errorf("verbose output should contain method on error, got: %q", output)
	}
}

func TestNewClientWithKey_UsesHTTPS(t *testing.T) {
	c := NewClientWithKey("test-key")
	if !strings.HasPrefix(c.BaseURL, "https://") {
		t.Errorf("BaseURL should use HTTPS, got: %q", c.BaseURL)
	}
}

func TestNewClientWithKey_DefaultTimeout(t *testing.T) {
	c := NewClientWithKey("test-key")
	if c.HTTP.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", c.HTTP.Timeout, DefaultTimeout)
	}
}

func TestNewClientWithKey_TransportChain(t *testing.T) {
	c := NewClientWithKey("test-key")

	// The outer transport should be authTransport.
	at, ok := c.HTTP.Transport.(*authTransport)
	if !ok {
		t.Fatalf("expected outer transport to be *authTransport, got %T", c.HTTP.Transport)
	}

	// The next transport should be loggingTransport.
	lt, ok := at.base.(*loggingTransport)
	if !ok {
		t.Fatalf("expected inner transport to be *loggingTransport, got %T", at.base)
	}

	// The innermost transport should be *http.Transport (the base).
	_, ok = lt.base.(*http.Transport)
	if !ok {
		t.Fatalf("expected base transport to be *http.Transport, got %T", lt.base)
	}
}
