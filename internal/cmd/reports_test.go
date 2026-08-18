package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// reportsCapture, when non-nil, receives the last write request body / marker
// from startReportsServer. Set and reset by the write tests.
var reportsCapture *map[string]any

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
			json.NewEncoder(w).Encode(map[string]any{"reportTemplate": map[string]any{"id": "685150083284a86b5f131f7e", "displayName": "Android Applications", "searchRequest": map[string]any{"fields": map[string]any{}}}})
		case strings.HasPrefix(p, "/reports/scheduled/") && strings.HasSuffix(p, "/runs") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"runs": []map[string]any{{"id": "run9", "status": "COMPLETE"}}, "totalCount": 1})
		case p == "/reports/scheduled/runs/run1" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"run": map[string]any{"id": "run1", "status": "COMPLETE"}})
		// Writes.
		case (p == "/reports/custom" || p == "/reports/custom-reports" || p == "/reports/scheduled") && r.Method == http.MethodPost:
			if reportsCapture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, reportsCapture)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "6a84b421d97b965cd89c01b4", "displayName": "created"})
		case p == "/reports/export" && r.Method == http.MethodPost:
			if reportsCapture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, reportsCapture)
			}
			json.NewEncoder(w).Encode(map[string]any{"downloadUrl": "https://s3.example/report.csv"})
		case strings.HasSuffix(p, "/trigger") && r.Method == http.MethodPost:
			if reportsCapture != nil {
				(*reportsCapture)["triggered"] = p
			}
			w.Write([]byte(`{}`))
		case r.Method == http.MethodPut:
			if reportsCapture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, reportsCapture)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "6a84b421d97b965cd89c01b4", "displayName": "updated"})
		case r.Method == http.MethodDelete:
			if reportsCapture != nil {
				(*reportsCapture)["deleted"] = p
			}
			w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/reports/"):
			// Single-get for a writable family (e.g. the delete precursor read).
			json.NewEncoder(w).Encode(map[string]any{"id": "6a84b421d97b965cd89c01b4", "displayName": "existing report"})
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

func TestReports_CreateWrapsBody(t *testing.T) {
	setupUsersTest(t)
	cap := map[string]any{}
	reportsCapture = &cap
	t.Cleanup(func() { reportsCapture = nil })
	overrideV2Client(t, startReportsServer(t).URL)
	for _, tc := range []struct{ fam, key string }{
		{"custom", "reportView"}, {"builder", "customReport"}, {"scheduled", "scheduledReport"},
	} {
		cap = map[string]any{}
		viper.Set("cache.enabled", false)
		root := NewRootCmd()
		var o, e bytes.Buffer
		root.SetOut(&o)
		root.SetErr(&e)
		root.SetIn(strings.NewReader(`{"displayName":"New"}`))
		root.SetArgs([]string{"reports", tc.fam, "create", "--report-file", "-"})
		if err := root.Execute(); err != nil {
			t.Fatalf("%s create: %v", tc.fam, err)
		}
		inner, ok := cap[tc.key].(map[string]any)
		if !ok || inner["displayName"] != "New" {
			t.Errorf("%s create body not wrapped in {%s}: %v", tc.fam, tc.key, cap)
		}
	}
}

func TestReports_TemplatesNotWritable(t *testing.T) {
	// Read-only families must expose no write subcommands.
	reportsCmd := newReportsCmd()
	hasChild := func(parent, child string) bool {
		for _, c := range reportsCmd.Commands() {
			if c.Name() != parent {
				continue
			}
			for _, sub := range c.Commands() {
				if sub.Name() == child {
					return true
				}
			}
		}
		return false
	}
	for _, fam := range []string{"templates", "saved"} {
		for _, w := range []string{"create", "update", "delete"} {
			if hasChild(fam, w) {
				t.Errorf("%s must not have %s", fam, w)
			}
		}
	}
	// Writable families must expose them.
	for _, fam := range []string{"custom", "builder", "scheduled"} {
		if !hasChild(fam, "create") {
			t.Errorf("%s should have create", fam)
		}
	}
}

func TestReports_Delete(t *testing.T) {
	setupUsersTest(t)
	cap := map[string]any{}
	reportsCapture = &cap
	t.Cleanup(func() { reportsCapture = nil })
	overrideV2Client(t, startReportsServer(t).URL)
	out, _, err := runReports(t, "custom", "delete", "6a84b421d97b965cd89c01b4", "--force")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if cap["deleted"] != "/reports/custom/6a84b421d97b965cd89c01b4" {
		t.Errorf("delete path: %v", cap["deleted"])
	}
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("delete msg: %q", out)
	}
}

func TestReports_ExportFromTemplate(t *testing.T) {
	setupUsersTest(t)
	cap := map[string]any{}
	reportsCapture = &cap
	t.Cleanup(func() { reportsCapture = nil })
	overrideV2Client(t, startReportsServer(t).URL)
	out, _, err := runReports(t, "export", "--name", "My Export", "--from-template", "685150083284a86b5f131f7e")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if cap["reportName"] != "My Export" || cap["notifyByEmail"] != false {
		t.Errorf("export body: %v", cap)
	}
	if _, ok := cap["searchRequest"]; !ok {
		t.Error("export must include searchRequest pulled from template")
	}
	if !strings.Contains(out, "downloadUrl") {
		t.Errorf("export should return downloadUrl: %s", out)
	}
}

func TestReports_ExportRequiresOneSource(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startReportsServer(t).URL)
	if _, _, err := runReports(t, "export", "--name", "X"); err == nil {
		t.Error("export with no source should error")
	}
}

func TestReports_ScheduledTrigger(t *testing.T) {
	setupUsersTest(t)
	cap := map[string]any{}
	reportsCapture = &cap
	t.Cleanup(func() { reportsCapture = nil })
	overrideV2Client(t, startReportsServer(t).URL)
	if _, _, err := runReports(t, "scheduled", "trigger", "7761ca72-a7b5-40a1-b242-5d7d01ca6821"); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if cap["triggered"] != "/reports/scheduled/7761ca72-a7b5-40a1-b242-5d7d01ca6821/trigger" {
		t.Errorf("trigger path: %v", cap["triggered"])
	}
}

func TestReports_PlanNoMutation(t *testing.T) {
	setupUsersTest(t)
	cap := map[string]any{}
	reportsCapture = &cap
	t.Cleanup(func() { reportsCapture = nil })
	overrideV2Client(t, startReportsServer(t).URL)
	root := NewRootCmd()
	var o, e bytes.Buffer
	root.SetOut(&o)
	root.SetErr(&e)
	root.SetIn(strings.NewReader(`{"displayName":"X"}`))
	root.SetArgs([]string{"reports", "custom", "create", "--report-file", "-", "--plan"})
	err := root.Execute()
	var exitErr *ExitError
	if !errorAs(err, &exitErr) || exitErr.Code != 10 {
		t.Fatalf("expected plan ExitError(10), got %v", err)
	}
	if len(cap) != 0 {
		t.Errorf("--plan must not POST: %v", cap)
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
