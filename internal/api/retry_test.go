package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryTransport_NoRetryOnSuccess(t *testing.T) {
	var callCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewClientWithKey("test-key")
	c.BaseURL = ts.URL

	resp, err := c.HTTP.Get(ts.URL + "/test")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	resp.Body.Close()

	if got := callCount.Load(); got != 1 {
		t.Errorf("expected 1 call, got %d", got)
	}
}

func TestRetryTransport_RetriesOnServerError(t *testing.T) {
	var callCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	rt := newRetryTransport(http.DefaultTransport)
	rt.sleepFn = func(d time.Duration) {} // no-op sleep for fast tests

	client := &http.Client{Transport: &authTransport{apiKey: "test", base: &loggingTransport{base: rt, apiKey: "test"}}}

	resp, err := client.Get(ts.URL + "/test")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if got := callCount.Load(); got != 3 {
		t.Errorf("expected 3 calls (2 failures + 1 success), got %d", got)
	}
}

func TestRetryTransport_RetriesOn429(t *testing.T) {
	var callCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	rt := newRetryTransport(http.DefaultTransport)
	rt.sleepFn = func(d time.Duration) {}

	client := &http.Client{Transport: rt}
	resp, err := client.Get(ts.URL + "/test")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if got := callCount.Load(); got != 2 {
		t.Errorf("expected 2 calls, got %d", got)
	}
}

func TestRetryTransport_RetriesOnBadGateway(t *testing.T) {
	retryCodes := []int{
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	}

	for _, code := range retryCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var callCount atomic.Int32

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := callCount.Add(1)
				if n == 1 {
					w.WriteHeader(code)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			rt := newRetryTransport(http.DefaultTransport)
			rt.sleepFn = func(d time.Duration) {}

			client := &http.Client{Transport: rt}
			resp, err := client.Get(ts.URL + "/test")
			if err != nil {
				t.Fatalf("GET error: %v", err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200, got %d", resp.StatusCode)
			}
		})
	}
}

func TestRetryTransport_NoRetryOn4xx(t *testing.T) {
	nonRetryCodes := []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
	}

	for _, code := range nonRetryCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var callCount atomic.Int32

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount.Add(1)
				w.WriteHeader(code)
			}))
			defer ts.Close()

			rt := newRetryTransport(http.DefaultTransport)
			rt.sleepFn = func(d time.Duration) {}

			client := &http.Client{Transport: rt}
			resp, err := client.Get(ts.URL + "/test")
			if err != nil {
				t.Fatalf("GET error: %v", err)
			}
			resp.Body.Close()

			if got := callCount.Load(); got != 1 {
				t.Errorf("expected 1 call (no retry), got %d", got)
			}
		})
	}
}

func TestRetryTransport_MaxRetriesExceeded(t *testing.T) {
	var callCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	rt := newRetryTransport(http.DefaultTransport)
	rt.sleepFn = func(d time.Duration) {}

	client := &http.Client{Transport: rt}
	resp, err := client.Get(ts.URL + "/test")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	resp.Body.Close()

	// After max retries, should return the last response.
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
	// Should have made 1 initial + 3 retries = 4 total calls.
	if got := callCount.Load(); got != 4 {
		t.Errorf("expected 4 calls (1 + 3 retries), got %d", got)
	}
}

func TestRetryTransport_BackoffDurations(t *testing.T) {
	var sleepDurations []time.Duration

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	rt := newRetryTransport(http.DefaultTransport)
	rt.sleepFn = func(d time.Duration) {
		sleepDurations = append(sleepDurations, d)
	}

	client := &http.Client{Transport: rt}
	resp, _ := client.Get(ts.URL + "/test")
	if resp != nil {
		resp.Body.Close()
	}

	// Should have 3 sleep calls (one per retry).
	if len(sleepDurations) != 3 {
		t.Fatalf("expected 3 sleeps, got %d", len(sleepDurations))
	}

	// Verify exponential increase: each should be roughly 2^attempt seconds + jitter.
	// Attempt 0: ~1s, Attempt 1: ~2s, Attempt 2: ~4s (plus jitter up to 1s).
	for i, d := range sleepDurations {
		minExpected := time.Duration(1<<i) * time.Second
		maxExpected := minExpected + time.Second // jitter is up to 1s
		if maxExpected > maxBackoff {
			maxExpected = maxBackoff
		}
		if d < minExpected || d > maxExpected {
			t.Errorf("sleep %d: duration %v not in expected range [%v, %v]", i, d, minExpected, maxExpected)
		}
	}
}

func TestRetryTransport_ContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	rt := newRetryTransport(http.DefaultTransport)
	rt.sleepFn = func(d time.Duration) {}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/test", nil)
	_, err := rt.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestIsRetryableStatus(t *testing.T) {
	tests := []struct {
		code     int
		expected bool
	}{
		{200, false},
		{201, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.code), func(t *testing.T) {
			if got := isRetryableStatus(tt.code); got != tt.expected {
				t.Errorf("isRetryableStatus(%d) = %v, want %v", tt.code, got, tt.expected)
			}
		})
	}
}
