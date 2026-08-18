package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func startReportsV2Server(t *testing.T) *httptest.Server {
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
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
		if key, ok := lists[p]; ok && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{key: []map[string]any{{"id": key + "-1", "displayName": key}}, "totalCount": 1})
			return
		}
		switch {
		case p == "/reports/jumpcloud/685150083284a86b5f131f7e" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"reportTemplate": map[string]any{"id": "685150083284a86b5f131f7e", "displayName": "Android Applications"}})
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

func TestMCPReports_FamilyLists(t *testing.T) {
	overrideV2ClientForTest(t, startReportsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})
	for _, tool := range []string{
		"reports_templates_list", "reports_custom_list", "reports_builder_list",
		"reports_saved_list", "reports_scheduled_list", "reports_scheduled_runs",
	} {
		out := getResultText(t, callTool(t, cs, tool, map[string]any{}))
		var wrapper struct {
			Data []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &wrapper); err != nil || len(wrapper.Data) != 1 {
			t.Fatalf("%s: envelope not unwrapped: %v\n%s", tool, err, out)
		}
	}
}

func TestMCPReports_TemplateGetUnwraps(t *testing.T) {
	overrideV2ClientForTest(t, startReportsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "reports_templates_get", map[string]any{"identifier": "685150083284a86b5f131f7e"}))
	var obj map[string]any
	if json.Unmarshal([]byte(out), &obj); obj["displayName"] != "Android Applications" {
		t.Errorf("get did not unwrap {reportTemplate}: %v", obj)
	}
}

func TestMCPReports_ScheduledRunGet(t *testing.T) {
	overrideV2ClientForTest(t, startReportsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "reports_scheduled_run_get", map[string]any{"identifier": "run1"}))
	if !strings.Contains(out, "COMPLETE") {
		t.Errorf("run get: %s", out)
	}
}
