package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// startReportsServer mirrors the v2 /reports/* list+get envelopes (probed
// 2026-08-18): each list returns {<key>: [...], totalCount}; jumpcloud get
// returns {reportTemplate}.
func startReportsServer(t *testing.T) *httptest.Server {
	t.Helper()
	lists := map[string]string{
		"/reports/jumpcloud":      "reportTemplates",
		"/reports/custom":         "reportViews",
		"/reports/custom-reports": "customReports",
		"/reports/saved-reports":  "savedReports",
		"/reports/scheduled":      "scheduledReports",
		"/reports/scheduled/runs": "runs",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		if key, ok := lists[p]; ok && r.Method == http.MethodGet {
			item := map[string]any{"id": key + "-1", "displayName": key + " one"}
			json.NewEncoder(w).Encode(map[string]any{key: []map[string]any{item}, "totalCount": 1})
			return
		}
		switch {
		case p == "/reports/jumpcloud/685150083284a86b5f131f7e" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"reportTemplate": map[string]any{"id": "685150083284a86b5f131f7e", "displayName": "Android Applications"}})
		case strings.HasPrefix(p, "/reports/scheduled/") && strings.HasSuffix(p, "/runs") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"runs": []map[string]any{{"id": "run9", "status": "COMPLETE"}}, "totalCount": 1})
		case p == "/reports/scheduled/runs/run1" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"run": map[string]any{"id": "run1", "status": "COMPLETE"}})
		default:
			t.Errorf("unexpected: %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runReports(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	viper.Set("cache.enabled", false)
	root := NewRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"reports"}, args...))
	err := root.Execute()
	return out.String(), errBuf.String(), err
}

func TestReports_AllFamilyLists(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startReportsServer(t).URL)
	for _, fam := range []string{"templates", "custom", "builder", "saved", "scheduled"} {
		out, errBuf, err := runReports(t, fam, "list")
		if err != nil {
			t.Fatalf("%s list: %v", fam, err)
		}
		var rows []map[string]any
		if e := json.Unmarshal([]byte(out), &rows); e != nil || len(rows) != 1 {
			t.Fatalf("%s: envelope not unwrapped: %v\n%s", fam, e, out)
		}
		if !strings.Contains(errBuf, "1 items") {
			t.Errorf("%s: footer missing: %s", fam, errBuf)
		}
	}
}

func TestReports_TemplateGetUnwraps(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startReportsServer(t).URL)
	out, _, err := runReports(t, "templates", "get", "685150083284a86b5f131f7e")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var obj map[string]any
	if json.Unmarshal([]byte(out), &obj); obj["displayName"] != "Android Applications" {
		t.Errorf("get did not unwrap {reportTemplate}: %v", obj)
	}
}

func TestReports_ScheduledRuns(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startReportsServer(t).URL)
	// All runs.
	out, _, err := runReports(t, "scheduled", "runs")
	if err != nil {
		t.Fatalf("runs: %v", err)
	}
	var rows []map[string]any
	if e := json.Unmarshal([]byte(out), &rows); e != nil || len(rows) != 1 {
		t.Fatalf("runs envelope not unwrapped: %v\n%s", e, out)
	}
	// Runs for a specific schedule (id passed directly).
	if _, _, err := runReports(t, "scheduled", "runs", "5ec71e8e96bfda0611fc6c5b"); err != nil {
		t.Fatalf("runs <id>: %v", err)
	}
	// A single run.
	out, _, err = runReports(t, "scheduled", "run", "run1")
	if err != nil {
		t.Fatalf("run get: %v", err)
	}
	if !strings.Contains(out, "run1") {
		t.Errorf("run get did not unwrap: %s", out)
	}
}

func TestReports_IDsOnly(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startReportsServer(t).URL)
	out, _, err := runReports(t, "templates", "list", "--ids")
	if err != nil {
		t.Fatalf("--ids: %v", err)
	}
	if strings.TrimSpace(out) != "reportTemplates-1" {
		t.Errorf("--ids should emit id, got %q", out)
	}
}
