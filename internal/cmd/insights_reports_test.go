package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type insReportsServer struct {
	*httptest.Server
	requests []string
	sort     string
}

func newInsReportsServer(t *testing.T, reports func() []map[string]any, content map[string]string) *insReportsServer {
	t.Helper()
	rs := &insReportsServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.requests = append(rs.requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/reports":
			rs.sort = r.URL.Query().Get("sort")
			json.NewEncoder(w).Encode(reports())
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/reports/"):
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]any{"id": "r1", "type": strings.TrimPrefix(r.URL.Path, "/reports/"), "status": "PENDING"})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/content"):
			if body, ok := content[r.URL.Path]; ok {
				w.Write([]byte(body))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"report not found"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(rs.Close)
	return rs
}

func twoInsReports() []map[string]any {
	return []map[string]any{
		{"id": "r1", "type": "users-to-devices", "status": "COMPLETED", "created_at": "2026-11-01T00:00:00Z", "expiration": "2026-12-01T00:00:00Z",
			"artifacts": []map[string]any{{"id": "a-csv", "format": "CSV"}, {"id": "a-json", "format": "JSON"}}},
		{"id": "r2", "type": "os-version", "status": "PENDING", "created_at": "2026-11-02T00:00:00Z", "expiration": "2026-12-02T00:00:00Z", "artifacts": []map[string]any{}},
	}
}

func runInsReports(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(append([]string{"insights", "reports"}, args...))
	err := cmd.Execute()
	return out.String(), errb.String(), err
}

func TestInsightsReportsHelp(t *testing.T) {
	out, _, err := runInsReports(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"list", "request", "download"} {
		if !strings.Contains(out, s) {
			t.Errorf("help missing %s", s)
		}
	}
}

func TestInsightsReportsList(t *testing.T) {
	setupUsersTest(t)
	rs := newInsReportsServer(t, twoInsReports, nil)
	overrideInsightsClient(t, rs.URL)

	out, _, err := runInsReports(t, "list", "--sort", "-CREATED_AT", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.requests) != 1 || rs.requests[0] != "GET /reports" || rs.sort != "-CREATED_AT" {
		t.Errorf("requests=%v sort=%q", rs.requests, rs.sort)
	}
	for _, s := range []string{"r1", "users-to-devices", "COMPLETED", "2026-12-01", "CSV,JSON", "r2"} {
		if !strings.Contains(out, s) {
			t.Errorf("table missing %q:\n%s", s, out)
		}
	}

	out, _, err = runInsReports(t, "list", "--status", "COMPLETED", "--type", "users-to-devices", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "r2") || !strings.Contains(out, "r1") {
		t.Errorf("filter wrong:\n%s", out)
	}
}

func TestInsightsReportsRequest(t *testing.T) {
	setupUsersTest(t)
	rs := newInsReportsServer(t, twoInsReports, nil)
	overrideInsightsClient(t, rs.URL)

	_, errOut, err := runInsReports(t, "request", "users-to-devices", "--plan")
	if reportsExitCode(err) != 10 {
		t.Fatalf("plan err=%v", err)
	}
	if !strings.Contains(errOut, "POST") || !strings.Contains(errOut, "/insights/directory/v1/reports/users-to-devices") {
		t.Errorf("plan output: %s", errOut)
	}
	if len(rs.requests) != 0 {
		t.Fatalf("plan sent %v", rs.requests)
	}

	_, _, err = runInsReports(t, "request", "bogus")
	if reportsExitCode(err) != 2 || len(rs.requests) != 0 {
		t.Errorf("unknown type err=%v requests=%v", err, rs.requests)
	}

	out, _, err := runInsReports(t, "request", "users-to-devices")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.requests) != 1 || rs.requests[0] != "POST /reports/users-to-devices" || !strings.Contains(out, "PENDING") {
		t.Errorf("requests=%v out=%s", rs.requests, out)
	}
}

func TestInsightsReportsRequestWait(t *testing.T) {
	old := reportPollInterval
	reportPollInterval = time.Millisecond
	t.Cleanup(func() { reportPollInterval = old })

	for _, tc := range []struct {
		final   string
		wantErr string
	}{{"COMPLETED", ""}, {"FAILED", "FAILED"}, {"PENDING", "timed out"}} {
		setupUsersTest(t)
		var polls int32
		rs := newInsReportsServer(t, func() []map[string]any {
			status := "PENDING"
			if atomic.AddInt32(&polls, 1) >= 3 {
				status = tc.final
			}
			return []map[string]any{{"id": "r1", "type": "os-version", "status": status}}
		}, nil)
		overrideInsightsClient(t, rs.URL)

		args := []string{"request", "os-version", "--wait"}
		if tc.final == "PENDING" {
			args = append(args, "--timeout", "100ms")
		}
		out, _, err := runInsReports(t, args...)
		if tc.wantErr == "" {
			if err != nil || !strings.Contains(out, "COMPLETED") {
				t.Errorf("completed: err=%v out=%s", err, out)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: err=%v", tc.final, err)
		}
	}
}

func TestInsightsReportsDownload(t *testing.T) {
	setupUsersTest(t)
	content := map[string]string{
		"/reports/r1/artifacts/a-json/content": `{"rows":[1,2]}`,
		"/reports/r1/artifacts/a-csv/content":  "a,b\n1,2\n",
	}
	rs := newInsReportsServer(t, twoInsReports, content)
	overrideInsightsClient(t, rs.URL)

	file := filepath.Join(t.TempDir(), "out.json")
	if _, _, err := runInsReports(t, "download", "r1", "--format", "json", "--output-file", file); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != `{"rows":[1,2]}` {
		t.Errorf("file = %q", b)
	}
	if _, _, err := runInsReports(t, "download", "r1", "--format", "json", "--output-file", file); err == nil {
		t.Error("overwrite without --force succeeded")
	}

	out, errOut, err := runInsReports(t, "download", "r1")
	if err != nil || out != "a,b\n1,2\n" {
		t.Errorf("stdout=%q err=%v (%s)", out, err, errOut)
	}

	if _, _, err := runInsReports(t, "download", "r2"); err == nil || !strings.Contains(err.Error(), "PENDING") {
		t.Errorf("pending err=%v", err)
	}
	if _, _, err := runInsReports(t, "download", "nope", "--artifact", "x"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("404 err=%v", err)
	}
}

func reportsExitCode(err error) int {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	var ce *CLIError
	if errors.As(err, &ce) {
		return ce.ExitCode()
	}
	return 1
}
