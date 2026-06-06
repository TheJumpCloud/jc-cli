package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Pre-KLA-448 these scenarios would all wait through internal timeouts
// (30s OAuth http.Client, uninterruptible backoff sleep). Post-fix the
// context cancels at every layer.

// TestTokenCache_TokenRespectsContextTimeout drives the OAuth token
// endpoint with an httptest server that responds slowly, then calls
// Token with a 100ms context. The call must return within ~2s (the
// server's slow-respond cap) — pre-fix it would wait up to 30s (the
// http.Client's default Timeout).
func TestTokenCache_TokenRespectsContextTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 2s is enough to outlast the 100ms ctx deadline + reasonable
		// slack but short enough that the test process won't hang if
		// cancellation breaks.
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()

	prev := SetOAuthTokenURL(slow.URL)
	defer SetOAuthTokenURL(prev)

	tc := NewTokenCache("client-id", "client-secret")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := tc.Token(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err chain should contain context.DeadlineExceeded so errors.Is callers can handle it; got: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Token returned in %v, want fast cancel; pre-fix this would wait up to 30s (server slow-respond was 2s)", elapsed)
	}
}

// TestTokenCache_TokenRespectsContextCancellation tests the
// Ctrl-C / parent-cancel path. Identical structure to the timeout
// case but the cancellation is explicit.
func TestTokenCache_TokenRespectsContextCancellation(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	hung := slow // keep variable name for body below

	prev := SetOAuthTokenURL(hung.URL)
	defer SetOAuthTokenURL(prev)

	tc := NewTokenCache("client-id", "client-secret")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := tc.Token(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err chain should contain context.Canceled; got: %v", err)
	}
}

// TestRetryTransport_BackoffRespectsContext verifies the retry loop's
// backoff sleep no longer blocks past a context deadline. Without the
// fix, a 1s retry backoff would run to completion even with a 50ms
// context — making `jc doctor --probe-timeout 100ms` regularly take
// seconds.
func TestRetryTransport_BackoffRespectsContext(t *testing.T) {
	var attemptCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable) // 503 → retryable
	}))
	defer ts.Close()

	rt := newRetryTransport(http.DefaultTransport)
	client := &http.Client{Transport: rt}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL, nil)

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if resp != nil {
		resp.Body.Close()
	}

	if err == nil {
		t.Fatal("expected error when context cancels during backoff, got nil response")
	}
	// Should fail before the 1s backoff that follows the first retry.
	if elapsed > 1*time.Second {
		t.Errorf("retry loop ran for %v after context expired; expected ctx-cancel during backoff to short-circuit", elapsed)
	}
}

// TestRetryTransport_PreservesContextErrorInChain pins that the
// retry transport's error return is errors.Is-friendly for
// context.DeadlineExceeded. doctor's classifyProbeError depends on
// this for its "timeout" classification.
func TestRetryTransport_PreservesContextErrorInChain(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	rt := newRetryTransport(http.DefaultTransport)
	client := &http.Client{Transport: rt}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	time.Sleep(20 * time.Millisecond) // ensure ctx fires before the request

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL, nil)
	_, err := client.Do(req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err chain should contain context.DeadlineExceeded; got: %v", err)
	}
}
