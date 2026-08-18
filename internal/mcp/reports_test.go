package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// reportsCap, when non-nil, receives the last write body/marker from the mock.
var reportsCap *map[string]any

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
			json.NewEncoder(w).Encode(map[string]any{"reportTemplate": map[string]any{"id": "685150083284a86b5f131f7e", "displayName": "Android Applications", "searchRequest": map[string]any{"fields": map[string]any{}}}})
		case p == "/reports/scheduled/runs/run1" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"run": map[string]any{"id": "run1", "status": "COMPLETE"}})
		case (p == "/reports/custom" || p == "/reports/custom-reports" || p == "/reports/scheduled") && r.Method == http.MethodPost:
			if reportsCap != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, reportsCap)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "6a84b421d97b965cd89c01b4", "displayName": "created"})
		case p == "/reports/export" && r.Method == http.MethodPost:
			if reportsCap != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, reportsCap)
			}
			json.NewEncoder(w).Encode(map[string]any{"downloadUrl": "https://s3.example/r.csv"})
		case strings.HasSuffix(p, "/trigger") && r.Method == http.MethodPost:
			if reportsCap != nil {
				(*reportsCap)["triggered"] = p
			}
			w.Write([]byte(`{}`))
		case r.Method == http.MethodDelete:
			if reportsCap != nil {
				(*reportsCap)["deleted"] = p
			}
			w.Write([]byte(`{}`))
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

func TestMCPReports_CreatePlanAndExecute(t *testing.T) {
	cap := map[string]any{}
	reportsCap = &cap
	t.Cleanup(func() { reportsCap = nil })
	overrideV2ClientForTest(t, startReportsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	// Plan: no POST.
	out := getResultText(t, callTool(t, cs, "reports_custom_create", map[string]any{"report_json": `{"displayName":"X"}`}))
	if len(cap) != 0 {
		t.Errorf("plan must not POST: %v", cap)
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan: %s", out)
	}

	// Execute: body wrapped in {reportView}.
	callTool(t, cs, "reports_builder_create", map[string]any{"report_json": `{"displayName":"B"}`, "execute": true})
	if inner, ok := cap["customReport"].(map[string]any); !ok || inner["displayName"] != "B" {
		t.Errorf("builder create not wrapped in {customReport}: %v", cap)
	}
}

func TestMCPReports_Delete(t *testing.T) {
	cap := map[string]any{}
	reportsCap = &cap
	t.Cleanup(func() { reportsCap = nil })
	overrideV2ClientForTest(t, startReportsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})
	callTool(t, cs, "reports_custom_delete", map[string]any{"identifier": "6a84b421d97b965cd89c01b4", "execute": true})
	if cap["deleted"] != "/reports/custom/6a84b421d97b965cd89c01b4" {
		t.Errorf("delete path: %v", cap["deleted"])
	}
}

func TestMCPReports_ExportFromTemplate(t *testing.T) {
	cap := map[string]any{}
	reportsCap = &cap
	t.Cleanup(func() { reportsCap = nil })
	overrideV2ClientForTest(t, startReportsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "reports_export", map[string]any{
		"report_name": "E", "from_template": "685150083284a86b5f131f7e", "execute": true,
	}))
	if cap["reportName"] != "E" {
		t.Errorf("export body: %v", cap)
	}
	if _, ok := cap["searchRequest"]; !ok {
		t.Error("export must include searchRequest from template")
	}
	if !strings.Contains(out, "downloadUrl") {
		t.Errorf("export downloadUrl: %s", out)
	}
}
