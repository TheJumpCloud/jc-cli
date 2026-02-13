package api

import (
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/klaassen-consulting/jc/internal/version"
	"github.com/klaassen-consulting/jc/internal/config"
)

const (
	// BaseURL is the JumpCloud API v1 base URL.
	BaseURL = "https://console.jumpcloud.com/api"

	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 30 * time.Second
)

// ErrNoAPIKey is returned when no API key is configured.
var ErrNoAPIKey = fmt.Errorf("no API key configured. Run jc auth login or set JC_API_KEY")

// Client is an authenticated JumpCloud API client.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	apiKey  string
}

// NewClient creates a new API client using the currently configured API key.
// Returns ErrNoAPIKey if no key is available from flags, env, or config.
func NewClient() (*Client, error) {
	key := config.APIKey()
	if key == "" {
		return nil, ErrNoAPIKey
	}
	return NewClientWithKey(key), nil
}

// NewClientWithKey creates a new API client with the given API key.
func NewClientWithKey(apiKey string) *Client {
	return &Client{
		HTTP: &http.Client{
			Timeout:   DefaultTimeout,
			Transport: &authTransport{apiKey: apiKey, base: http.DefaultTransport},
		},
		BaseURL: BaseURL,
		apiKey:  apiKey,
	}
}

// RedactKey returns a redacted version of an API key, showing only the last 4
// characters. Returns "(none)" for empty keys.
func RedactKey(key string) string {
	if key == "" {
		return "(none)"
	}
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

// userAgent returns the User-Agent string for API requests.
func userAgent() string {
	return fmt.Sprintf("jc/%s (Go; %s/%s)", version.Number, runtime.GOOS, runtime.GOARCH)
}

// authTransport is an http.RoundTripper that injects authentication headers
// and standard request headers into every outgoing request.
type authTransport struct {
	apiKey string
	base   http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid mutating the original.
	r := req.Clone(req.Context())

	r.Header.Set("x-api-key", t.apiKey)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", userAgent())

	return t.base.RoundTrip(r)
}
