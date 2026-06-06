package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// OAuthTokenURL is the JumpCloud OAuth 2.0 token endpoint.
	OAuthTokenURL = "https://admin-oauth.id.jumpcloud.com/oauth2/token"
)

// oauthTokenURL is the token endpoint URL. Overridable in tests.
var oauthTokenURL = OAuthTokenURL

// SetOAuthTokenURL overrides the OAuth token URL and returns the previous value.
// Used by tests in other packages.
func SetOAuthTokenURL(url string) string {
	prev := oauthTokenURL
	oauthTokenURL = url
	return prev
}

// tokenResponse represents the OAuth 2.0 token endpoint response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// TokenCache holds a cached bearer token with its expiry time.
// It is safe for concurrent use.
type TokenCache struct {
	mu           sync.Mutex
	accessToken  string
	expiresAt    time.Time
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

// nowFunc is the function used to get the current time. Overridable in tests.
var nowFunc = time.Now

// NewTokenCache creates a new token cache for the given client credentials.
func NewTokenCache(clientID, clientSecret string) *TokenCache {
	return &TokenCache{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: DefaultTimeout},
	}
}

// Token returns a valid bearer token, refreshing if expired or not yet
// fetched. The context is honored during a fetch — callers with a tight
// deadline (e.g. `jc doctor --probe-timeout 100ms`) get a clean
// context-error return instead of waiting through the http.Client's
// 30s default timeout. KLA-448 closed the context-leak that forced
// jc doctor to wrap its probe in a goroutine.
func (tc *TokenCache) Token(ctx context.Context) (string, error) {
	if ctx == nil {
		// Defensive — http.NewRequestWithContext panics on nil. Cobra
		// invariants give cmd.Context() != nil in production, but tests
		// that construct commands without RunE can leave it nil.
		ctx = context.Background()
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Return cached token if still valid (with 30s buffer).
	if tc.accessToken != "" && nowFunc().Add(30*time.Second).Before(tc.expiresAt) {
		return tc.accessToken, nil
	}

	// Fetch a new token.
	token, expiresIn, err := tc.fetchToken(ctx)
	if err != nil {
		return "", err
	}

	tc.accessToken = token
	tc.expiresAt = nowFunc().Add(time.Duration(expiresIn) * time.Second)
	return tc.accessToken, nil
}

// ExpiresAt returns the token's expiry time. Returns zero time if no token is cached.
func (tc *TokenCache) ExpiresAt() time.Time {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.expiresAt
}

// fetchToken exchanges client credentials for a bearer token. The
// context is propagated to the outbound HTTP request so a caller's
// deadline / cancellation reaches the actual socket — pre-KLA-448
// this used http.NewRequest (no context) and the request would run
// to the http.Client's 30s timeout regardless of what the caller
// asked for.
func (tc *TokenCache) fetchToken(ctx context.Context) (string, int, error) {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("scope", "api")

	req, err := http.NewRequestWithContext(ctx, "POST", oauthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create token request: %w", err)
	}

	// Basic auth with client_id:client_secret.
	creds := base64.StdEncoding.EncodeToString([]byte(tc.clientID + ":" + tc.clientSecret))
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := tc.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("failed to connect to OAuth token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return "", 0, fmt.Errorf("invalid client credentials (HTTP 401). Check your client ID and secret")
		case http.StatusForbidden:
			return "", 0, fmt.Errorf("client credentials lack permission (HTTP 403). Verify the service account scope")
		default:
			return "", 0, fmt.Errorf("OAuth token endpoint returned HTTP %d: %s", resp.StatusCode, truncateBody(body, 200))
		}
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", 0, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", 0, fmt.Errorf("token response missing access_token")
	}

	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600 // Default to 1 hour if not specified.
	}

	return tokenResp.AccessToken, expiresIn, nil
}
