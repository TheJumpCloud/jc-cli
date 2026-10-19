package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// headerRecorder answers every request and records what arrived.
func headerRecorder(t *testing.T) (*httptest.Server, func() []http.Header) {
	t.Helper()
	var mu sync.Mutex
	var seen []http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	return srv, func() []http.Header {
		mu.Lock()
		defer mu.Unlock()
		return append([]http.Header(nil), seen...)
	}
}

func v2Against(url string) *V2Client {
	c := NewV2ClientWithKey("test-key")
	c.BaseURL = url
	return c
}

func TestSetExtraHeaders_SendsTheHeader(t *testing.T) {
	srv, seen := headerRecorder(t)
	c := v2Against(srv.URL)
	c.SetExtraHeaders(http.Header{"x-org-id": []string{"org-1"}})

	if _, err := c.Get(context.Background(), "/agents"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got := seen()
	if len(got) != 1 {
		t.Fatalf("got %d requests, want 1", len(got))
	}
	if v := got[0].Get("x-org-id"); v != "org-1" {
		t.Errorf("x-org-id = %q, want org-1", v)
	}
}

// The defect that made this per-client. A header set on one client must not
// appear on another's requests: in `jc mcp serve`, which is long-lived and
// serves tool calls concurrently, a process-wide header rode along on
// whatever else was in flight — and for a multi-tenant key x-org-id SELECTS
// the organization, so an unrelated call could be answered against an
// organization its caller never named.
func TestSetExtraHeaders_DoesNotLeakToAnotherClient(t *testing.T) {
	srv, seen := headerRecorder(t)

	configured := v2Against(srv.URL)
	configured.SetExtraHeaders(http.Header{"x-org-id": []string{"org-1"}})
	plain := v2Against(srv.URL)

	if _, err := configured.Get(context.Background(), "/agents"); err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Get(context.Background(), "/users"); err != nil {
		t.Fatal(err)
	}

	got := seen()
	if len(got) != 2 {
		t.Fatalf("got %d requests, want 2", len(got))
	}
	if got[0].Get("x-org-id") != "org-1" {
		t.Errorf("the configured client did not send the header: %q", got[0].Get("x-org-id"))
	}
	if v := got[1].Get("x-org-id"); v != "" {
		t.Errorf("an unconfigured client sent x-org-id %q — a header set for one call "+
			"reached a request that never asked for it", v)
	}
}

// Two clients carrying DIFFERENT org ids, used concurrently. Under the
// process-wide version this is the case that could answer a request against
// the wrong organization, and the case where interleaved restores left one
// caller's header installed permanently.
func TestSetExtraHeaders_ConcurrentClientsDoNotCross(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Each request asserts on ITSELF: the org id must match the path it
		// was sent to. A crossed header fails here rather than in a count.
		want := "org-" + r.URL.Path[len("/t"):]
		if got := r.Header.Get("x-org-id"); got != want {
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte(`{"message":"wrong org: got ` + got + ` want ` + want + `"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 200)

	for i := 0; i < 20; i++ {
		for _, n := range []string{"1", "2"} {
			wg.Add(1)
			go func(n string) {
				defer wg.Done()
				c := v2Against(srv.URL)
				c.SetExtraHeaders(http.Header{"x-org-id": []string{"org-" + n}})
				if _, err := c.Get(context.Background(), "/t"+n); err != nil {
					errs <- err
				}
			}(n)
		}
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("a concurrent request carried the wrong organization id: %v", err)
	}
}

// The safety property is structural: the auth transports are outermost and
// Set (not Add) these headers, so they run after the extra-header transport
// and overwrite anything that collides. An extra header therefore cannot
// substitute a credential even if one reached this far.
func TestSetExtraHeaders_CannotOverrideAuthentication(t *testing.T) {
	srv, seen := headerRecorder(t)
	c := v2Against(srv.URL)
	c.SetExtraHeaders(http.Header{
		"x-api-key":     []string{"attacker-key"},
		"Authorization": []string{"Bearer attacker-token"},
		"User-Agent":    []string{"not-jc"},
	})

	if _, err := c.Get(context.Background(), "/agents"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got := seen()[0]
	if v := got.Get("x-api-key"); v != "test-key" {
		t.Errorf("x-api-key = %q, want the client's own key — an extra header overrode "+
			"authentication", v)
	}
	if got.Get("User-Agent") == "not-jc" {
		t.Error("an extra header overrode the User-Agent the client sets")
	}
}

func TestSetExtraHeaders_EmptyIsANoOp(t *testing.T) {
	srv, seen := headerRecorder(t)
	c := v2Against(srv.URL)
	before := c.HTTP.Transport

	c.SetExtraHeaders(nil)
	c.SetExtraHeaders(http.Header{})

	if c.HTTP.Transport != before {
		t.Error("an empty header set still wrapped the transport; a caller that may or may " +
			"not have a header to add should not need to branch")
	}
	if _, err := c.Get(context.Background(), "/agents"); err != nil {
		t.Fatal(err)
	}
	if n := len(seen()); n != 1 {
		t.Errorf("got %d requests, want 1", n)
	}
}

// Mutating the caller's map afterwards must not change what the client sends.
func TestSetExtraHeaders_ClonesTheHeaders(t *testing.T) {
	srv, seen := headerRecorder(t)
	c := v2Against(srv.URL)

	h := http.Header{"x-org-id": []string{"org-1"}}
	c.SetExtraHeaders(h)
	h.Set("x-org-id", "org-2")

	if _, err := c.Get(context.Background(), "/agents"); err != nil {
		t.Fatal(err)
	}
	if v := seen()[0].Get("x-org-id"); v != "org-1" {
		t.Errorf("x-org-id = %q, want org-1 — the headers were not copied", v)
	}
}

// A v1 client gets the same treatment through the embedded *Client.
func TestSetExtraHeaders_V1Client(t *testing.T) {
	srv, seen := headerRecorder(t)
	c := NewV1ClientWithKey("test-key")
	c.BaseURL = srv.URL
	c.SetExtraHeaders(http.Header{"x-org-id": []string{"org-1"}})

	// The v1 list path tolerates the bare array this fake returns badly, so
	// use the raw getter the api command uses.
	if _, err := c.Get(context.Background(), "/systemusers"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v := seen()[0].Get("x-org-id"); v != "org-1" {
		t.Errorf("x-org-id = %q on a v1 client, want org-1", v)
	}
}
