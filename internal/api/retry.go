package api

import (
	"context"
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

// retrySleepFn waits for d, or returns ctx.Err() if the context fires
// first. Pre-KLA-448 this was a plain func(time.Duration) and a
// caller's deadline couldn't cancel the backoff sleep — so a hung
// server + retry loop kept running long past --probe-timeout.
// Overridable for tests that want instant retries.
var retrySleepFn = func(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetRetrySleepFn overrides the retry backoff function.
// Used by cross-package tests to disable real delays. Returns the
// previous function so the caller can restore it on cleanup.
func SetRetrySleepFn(fn func(context.Context, time.Duration) error) func(context.Context, time.Duration) error {
	prev := retrySleepFn
	retrySleepFn = fn
	return prev
}

// retryTransport is an http.RoundTripper that retries transient errors
// with exponential backoff and jitter.
//
// Retryable status codes: 429, 500, 502, 503, 504.
// Backoff formula: min(2^attempt * 1s + jitter, 30s).
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	sleepFn    func(context.Context, time.Duration) error // overridable for testing
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
				if berr := t.backoff(req.Context(), attempt); berr != nil {
					return nil, berr
				}
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
			if berr := t.backoff(req.Context(), attempt); berr != nil {
				return nil, berr
			}
			continue
		}

		// Final attempt — return the response as-is.
		return resp, nil
	}

	return resp, err
}

// backoff waits for an exponentially increasing duration with jitter,
// or returns ctx.Err() if the context fires first. KLA-448 — pre-fix
// the wait was uninterruptible, so a caller's --probe-timeout could
// not cancel a retry loop in progress.
//
// Formula: min(2^attempt * 1s + jitter, 30s)
func (t *retryTransport) backoff(ctx context.Context, attempt int) error {
	base := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	jitter := time.Duration(rand.Int64N(int64(time.Second)))
	wait := base + jitter
	if wait > maxBackoff {
		wait = maxBackoff
	}
	return t.sleepFn(ctx, wait)
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
