package api

import (
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"time"
)

const (
	// DefaultMaxRetries is the default number of retry attempts for transient errors.
	DefaultMaxRetries = 3

	// maxBackoff is the maximum backoff duration.
	maxBackoff = 30 * time.Second
)

// retrySleepFn is the default sleep function for retry backoff.
// Tests can override this to avoid real delays.
var retrySleepFn = time.Sleep

// retryTransport is an http.RoundTripper that retries transient errors
// with exponential backoff and jitter.
//
// Retryable status codes: 429, 500, 502, 503, 504.
// Backoff formula: min(2^attempt * 1s + jitter, 30s).
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	sleepFn    func(time.Duration) // overridable for testing
}

func newRetryTransport(base http.RoundTripper) *retryTransport {
	return &retryTransport{
		base:       base,
		maxRetries: DefaultMaxRetries,
		sleepFn:    retrySleepFn,
	}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := range t.maxRetries + 1 {
		// Check for context cancellation before each attempt.
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}

		resp, err = t.base.RoundTrip(req)

		// Connection-level error — retry.
		if err != nil {
			if attempt < t.maxRetries {
				t.backoff(attempt)
				continue
			}
			return nil, err
		}

		// Success or non-retryable status — return immediately.
		if !isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}

		// Retryable status code — drain body and retry.
		if attempt < t.maxRetries {
			drainAndClose(resp)
			t.backoff(attempt)
			continue
		}

		// Final attempt — return the response as-is.
		return resp, nil
	}

	return resp, err
}

// backoff sleeps for an exponentially increasing duration with jitter.
// Formula: min(2^attempt * 1s + jitter, 30s)
func (t *retryTransport) backoff(attempt int) {
	base := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	jitter := time.Duration(rand.Int64N(int64(time.Second)))
	wait := base + jitter
	if wait > maxBackoff {
		wait = maxBackoff
	}
	t.sleepFn(wait)
}

// isRetryableStatus returns true for HTTP status codes that should trigger a retry.
func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// drainAndClose reads and discards the response body then closes it.
// This enables HTTP connection reuse.
func drainAndClose(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}
