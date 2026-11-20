package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDataExportBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.jumpcloud.com/insights/directory/v1":     "https://api.jumpcloud.com/data-export/v2",
		"https://api.eu.jumpcloud.com/insights/directory/v1/": "https://api.eu.jumpcloud.com/data-export/v2",
	} {
		if got, err := DataExportBaseURL(in); err != nil || got != want {
			t.Errorf("%s: got %q %v, want %q", in, got, err, want)
		}
	}
	if _, err := DataExportBaseURL("https://proxy.example.com/di"); err == nil {
		t.Error("a base without the insights suffix must be refused, not guessed at")
	}
}

func TestRaw_Statuses(t *testing.T) {
	for _, tc := range []struct {
		status int
		ok     bool
	}{{200, true}, {201, true}, {202, true}, {204, true}, {302, false}, {400, false}, {404, false}, {422, false}} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			if tc.status != 204 {
				w.Write([]byte(`{"x":1}`))
			}
		}))
		c := NewInsightsClientWithKey("k")
		_, err := c.Raw(context.Background(), http.MethodGet, ts.URL, "/p", nil)
		ts.Close()
		if (err == nil) != tc.ok {
			t.Errorf("status %d: err %v", tc.status, err)
		}
		var apiErr *APIError
		if !tc.ok && (!errors.As(err, &apiErr) || apiErr.StatusCode != tc.status) {
			t.Errorf("status %d: want an *APIError with that status, got %v", tc.status, err)
		}
	}
}

// A redirect to another host must not be followed: the transport would put
// the org's API key on the request to that host.
func TestRaw_DoesNotFollowCrossHostRedirect(t *testing.T) {
	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("x-api-key") + r.Header.Get("Authorization")
		w.Write([]byte("report"))
	}))
	defer other.Close()
	// 127.0.0.1 and localhost are different hosts to the client.
	otherURL := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/same" {
			w.Write([]byte("same-host"))
			return
		}
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, "/same", http.StatusFound)
			return
		}
		http.Redirect(w, r, otherURL+"/content", http.StatusFound)
	}))
	defer ts.Close()

	c := NewInsightsClientWithKey("secret-key")
	_, err := c.Raw(context.Background(), http.MethodGet, ts.URL, "/content", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusFound {
		t.Errorf("a cross-host redirect must surface as the 302, got %v", err)
	}
	if leaked != "" {
		t.Errorf("the credential reached the other host: %q", leaked)
	}
	out, err := c.Raw(context.Background(), http.MethodGet, ts.URL, "/hop", nil)
	if err != nil || string(out) != "same-host" {
		t.Errorf("a same-host redirect is followed: %q %v", out, err)
	}
}

func TestIntervalBody(t *testing.T) {
	ok := func(service, start, end, interval string) map[string]any {
		t.Helper()
		b, err := IntervalBody(service, start, end, interval, "")
		if err != nil {
			t.Fatalf("%s %s: %v", service, interval, err)
		}
		return b
	}
	b := ok("sso, ldap", "2026-11-01T00:00:00Z", "", "12h")
	if s := b["service"].([]string); len(s) != 2 || s[1] != "ldap" {
		t.Errorf("services: %v", b["service"])
	}
	if b["interval_unit"] != "h" || b["interval_value"] != "12" || b["end_time"] == "" {
		t.Errorf("body: %v", b)
	}
	for _, bad := range []struct{ start, interval string }{
		{"2026-11-01T00:00:00Z", "0d"}, {"2026-11-01T00:00:00Z", "d"}, {"2026-11-01T00:00:00Z", "1.5h"},
		{"2026-11-01T00:00:00Z", "10x"}, {"", "1d"},
	} {
		if _, err := IntervalBody("all", bad.start, "", bad.interval, ""); err == nil {
			t.Errorf("start %q interval %q must be refused", bad.start, bad.interval)
		}
	}
	if b, _ := IntervalBody("all", "2026-11-01T00:00:00Z", "", "1d", "-0500"); b["timezone"] != "-0500" {
		t.Errorf("timezone: %v", b)
	}
}

func TestParseIntervalBuckets(t *testing.T) {
	if rows, err := ParseIntervalBuckets([]byte(`{"buckets":[{"doc_count":3}]}`)); err != nil || len(rows) != 1 {
		t.Errorf("got %v %v", rows, err)
	}
	for _, bad := range []string{`{}`, `[]`, `x`} {
		if _, err := ParseIntervalBuckets([]byte(bad)); err == nil {
			t.Errorf("%s must be an error", bad)
		}
	}
}
