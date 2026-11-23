package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
)

const siemWindow = "--since=2026-01-01T00:00:00Z --until=2026-01-01T00:30:00Z"

func runSiem(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"insights", "siem"}, args...))
	err := cmd.Execute()
	return buf.String(), err
}

func exitOf(err error) int {
	return ToCLIError(err).ExitCode()
}

func TestSiemHelpListsExactlyTwo(t *testing.T) {
	cmd := newInsightsSiemCmd()
	var names []string
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}
	if len(names) != 2 || !(names[0] == "events" && names[1] == "test" || names[0] == "test" && names[1] == "events") {
		t.Fatalf("subcommands = %v", names)
	}
}

func TestSiemTest(t *testing.T) {
	setupUsersTest(t)
	for _, ok := range []bool{true, false} {
		var n int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&n, 1)
			if r.Method != http.MethodPost || r.URL.Path != "/data-export/v2/siem/connection-test" {
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			}
			if r.ContentLength > 0 {
				t.Errorf("body sent")
			}
			fmt.Fprintf(w, `{"ok":%v,"org_id":"org-1"}`, ok)
		}))
		overrideInsightsClient(t, ts.URL)
		out, err := runSiem(t, "test")
		ts.Close()
		if n != 1 {
			t.Fatalf("requests = %d", n)
		}
		if !strings.Contains(out, "org-1") || !strings.Contains(out, "ok") {
			t.Errorf("out = %q", out)
		}
		if ok && err != nil {
			t.Errorf("ok true: err %v", err)
		}
		if !ok && err == nil {
			t.Errorf("ok false: expected error")
		}
	}
}

func TestSiemHosts(t *testing.T) {
	t.Setenv("JC_INSIGHTS_BASE_URL", "")
	t.Setenv("JC_REGION", "eu")
	c := api.NewInsightsClientWithKey("test-key-1234")
	if got := c.DataExportBaseURL(); got != "https://api.eu.jumpcloud.com/data-export/v2" {
		t.Errorf("eu = %s", got)
	}
	t.Setenv("JC_INSIGHTS_BASE_URL", "http://localhost:9/")
	c = api.NewInsightsClientWithKey("test-key-1234")
	if got := c.DataExportBaseURL(); got != "http://localhost:9/data-export/v2" {
		t.Errorf("override = %s", got)
	}
}

func TestSiemEventsLocalValidation(t *testing.T) {
	setupUsersTest(t)
	var n int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&n, 1) }))
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	many := make([]string, 26)
	for i := range many {
		many[i] = fmt.Sprintf("t%02d", i)
	}
	cases := map[string]struct {
		args []string
		want string
	}{
		"long window": {[]string{"events", "--since=2026-01-01T00:00:00Z", "--until=2026-01-01T01:30:00Z", "--event-type=all"}, "one hour"},
		"last 2h":     {[]string{"events", "--last=2h", "--event-type=all"}, "one hour"},
		"until<since": {[]string{"events", "--since=2026-01-01T01:00:00Z", "--until=2026-01-01T00:30:00Z", "--event-type=all"}, "--until"},
		"until=since": {[]string{"events", "--since=2026-01-01T01:00:00Z", "--until=2026-01-01T01:00:00Z", "--event-type=all"}, "--until"},
		"no type":     {[]string{"events", "--last=30m"}, "event-type"},
		"26 types":    {[]string{"events", "--last=30m", "--event-type=" + strings.Join(many, ",")}, "25"},
	}
	for name, c := range cases {
		_, err := runSiem(t, c.args...)
		if err == nil || exitOf(err) != ExitUsage || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if n != 0 {
		t.Errorf("%d requests sent", n)
	}
}

func TestSiemEventsPaging(t *testing.T) {
	setupUsersTest(t)
	var pages int32
	var accept atomic.Value
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&pages, 1)
		accept.Store(r.Header.Get("Accept"))
		if r.URL.Path != "/data-export/v2/events" || r.URL.Query().Get("event_types") != "all" {
			t.Errorf("bad request %s", r.URL.String())
		}
		switch r.URL.Query().Get("next_cursor") {
		case "":
			fmt.Fprint(w, `{"events":[{"id":1},{"id":2}],"next_cursor":"a"}`)
		case "a":
			fmt.Fprint(w, `{"events":[{"id":3},{"id":4}],"next_cursor":"b"}`)
		default:
			fmt.Fprint(w, `{"events":[{"id":5}]}`)
		}
	}))
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)

	out, err := runSiem(t, "events", "--since=2026-01-01T00:00:00Z", "--until=2026-01-01T00:30:00Z", "--event-type=all", "-o", "ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 5 || !strings.Contains(lines[4], "5") {
		t.Errorf("out = %q", out)
	}
	if pages != 3 {
		t.Errorf("pages = %d", pages)
	}
	if accept.Load() != "application/json" {
		t.Errorf("accept = %v", accept.Load())
	}

	pages = 0
	out, err = runSiem(t, "events", "--since=2026-01-01T00:00:00Z", "--until=2026-01-01T00:30:00Z", "--event-type=all", "--limit=3", "--ocsf", "-o", "ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 3 {
		t.Errorf("limit out = %q", out)
	}
	if pages != 2 {
		t.Errorf("limit pages = %d", pages)
	}
	if accept.Load() != "application/ocsf+json" {
		t.Errorf("accept = %v", accept.Load())
	}
}

func TestSiemEventsRepeatedCursorStops(t *testing.T) {
	setupUsersTest(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"events":[{"id":1}],"next_cursor":"same"}`)
	}))
	defer ts.Close()
	overrideInsightsClient(t, ts.URL)
	if _, err := runSiem(t, "events", "--last=30m", "--event-type=all"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSiemEventsErrors(t *testing.T) {
	setupUsersTest(t)
	viper.Set("plan", false)
	cases := []struct {
		status int
		body   string
		want   string
		exit   int
	}{
		{422, `{"error":"too_recent","available_until":"2026-01-01T00:00:00Z","message":"x"}`, "2026-01-01T00:00:00Z", ExitGeneral},
		{402, `{"message":"Organization lacks Directory Insights Premium entitlement"}`, "Premium entitlement", ExitGeneral},
		{429, `{"message":"slow down"}`, "", ExitRateLimit},
	}
	for _, c := range cases {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			fmt.Fprint(w, c.body)
		}))
		overrideInsightsClient(t, ts.URL)
		_, err := runSiem(t, "events", "--last=30m", "--event-type=all")
		ts.Close()
		if err == nil || !strings.Contains(err.Error(), c.want) || exitOf(err) != c.exit {
			t.Errorf("%d: err=%v exit=%d", c.status, err, exitOf(err))
		}
		var un *api.SIEMUnavailableError
		if c.status == 422 && !errors.As(ToCLIError(err), &un) && !strings.Contains(ToCLIError(err).Suggestion, "--until") {
			t.Errorf("422 suggestion missing")
		}
	}
}
