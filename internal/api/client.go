package api

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/version"
)

const (
	// BaseURL is the JumpCloud API v1 base URL.
	BaseURL = "https://console.jumpcloud.com/api"

	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 30 * time.Second

	// maxDebugBodySize is the maximum body size logged in --debug mode.
	maxDebugBodySize = 4096
)

// ErrNoAPIKey is returned when no API key is configured.
var ErrNoAPIKey = fmt.Errorf("no API key configured. Run jc auth login or set JC_API_KEY")

// logWriter is the writer used for verbose/debug output. Defaults to os.Stderr.
// Tests can override this.
var logWriter io.Writer = os.Stderr

// Client is an authenticated JumpCloud API client.
type Client struct {
	HTTP       *http.Client
	BaseURL    string
	apiKey     string
	AuthMethod string // "api_key" or "service_account"
	TokenCache *TokenCache
}

// NewClient creates a new API client using the currently configured credentials.
// For service accounts, it creates an OAuth bearer-token client.
// For API key auth, it creates an x-api-key client.
// Returns ErrNoAPIKey if no credentials are available.
func NewClient() (*Client, error) {
	if config.AuthMethod() == "service_account" {
		clientID := config.ClientID()
		clientSecret := config.ClientSecret()
		if clientID != "" && clientSecret != "" {
			tc := NewTokenCache(clientID, clientSecret)
			return NewClientWithToken(tc), nil
		}
	}

	key := config.APIKey()
	if key == "" {
		return nil, ErrNoAPIKey
	}
	return NewClientWithKey(key), nil
}

// NewClientWithKey creates a new API client with the given API key.
func NewClientWithKey(apiKey string) *Client {
	base := baseTransport()

	// Build the transport chain: auth → logging → retry → base (TLS-enforced HTTP transport).
	// Auth wraps logging so that logging sees the injected headers (x-api-key, etc.)
	// and can redact the API key in debug output.
	// Retry wraps base so transient errors (429, 5xx) are retried with backoff.
	var transport http.RoundTripper = newRetryTransport(base)
	transport = &loggingTransport{base: transport, apiKey: apiKey}
	transport = &authTransport{apiKey: apiKey, base: transport}

	return &Client{
		HTTP: &http.Client{
			Timeout:   DefaultTimeout,
			Transport: transport,
		},
		BaseURL:    BaseURL,
		apiKey:     apiKey,
		AuthMethod: "api_key",
	}
}

// NewClientWithToken creates a new API client that authenticates using OAuth 2.0
// bearer tokens obtained from a service account's client credentials.
// Tokens are cached and auto-refreshed on expiry.
func NewClientWithToken(tc *TokenCache) *Client {
	base := baseTransport()

	var transport http.RoundTripper = newRetryTransport(base)
	transport = &loggingTransport{base: transport}
	transport = &bearerAuthTransport{tokenCache: tc, base: transport}

	return &Client{
		HTTP: &http.Client{
			Timeout:   DefaultTimeout,
			Transport: transport,
		},
		BaseURL:    BaseURL,
		AuthMethod: "service_account",
		TokenCache: tc,
	}
}

// bearerAuthTransport is an http.RoundTripper that injects a Bearer token
// from the TokenCache into every outgoing request.
type bearerAuthTransport struct {
	tokenCache *TokenCache
	base       http.RoundTripper
}

func (t *bearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.tokenCache.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to obtain bearer token: %w", err)
	}

	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", userAgent())

	return t.base.RoundTrip(r)
}

// baseTransport returns an http.Transport with TLS 1.2+ enforcement and
// connection pooling enabled.
func baseTransport() *http.Transport {
	return &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
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

// loggingTransport is an http.RoundTripper that logs request/response details
// based on --verbose and --debug flags.
//
// --verbose logs: method, URL, status code, and duration.
// --debug  logs: full request/response headers and body (with API key redacted).
type loggingTransport struct {
	base   http.RoundTripper
	apiKey string
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	verbose := viper.GetBool("verbose")
	debug := viper.GetBool("debug")
	if !verbose && !debug {
		return t.base.RoundTrip(req)
	}

	start := time.Now()

	// Debug: log request headers before sending.
	if debug {
		t.logRequestDebug(req)
	}

	resp, err := t.base.RoundTrip(req)
	duration := time.Since(start)

	if err != nil {
		fmt.Fprintf(logWriter, "→ %s %s ERROR (%s): %v\n", req.Method, req.URL, duration.Round(time.Millisecond), err)
		return nil, err
	}

	// Verbose: method, URL, status, duration.
	fmt.Fprintf(logWriter, "→ %s %s %d (%s)\n", req.Method, req.URL, resp.StatusCode, duration.Round(time.Millisecond))

	// Debug: log response headers.
	if debug {
		t.logResponseDebug(resp)
	}

	return resp, nil
}

func (t *loggingTransport) logRequestDebug(req *http.Request) {
	fmt.Fprintf(logWriter, "  Request Headers:\n")
	for name, values := range req.Header {
		for _, v := range values {
			if strings.EqualFold(name, "x-api-key") {
				v = RedactKey(t.apiKey)
			}
			if strings.EqualFold(name, "authorization") {
				v = redactAuthHeader(v)
			}
			fmt.Fprintf(logWriter, "    %s: %s\n", name, v)
		}
	}
}

// redactAuthHeader redacts the token in an Authorization header value.
// "Bearer abc...xyz" becomes "Bearer ****xyz".
func redactAuthHeader(value string) string {
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 {
		return "****"
	}
	return parts[0] + " " + RedactKey(parts[1])
}

func (t *loggingTransport) logResponseDebug(resp *http.Response) {
	fmt.Fprintf(logWriter, "  Response Headers:\n")
	for name, values := range resp.Header {
		for _, v := range values {
			fmt.Fprintf(logWriter, "    %s: %s\n", name, v)
		}
	}
}
