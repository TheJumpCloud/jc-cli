package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startAlertsV2Server serves the /api/v2/alerts family for MCP tool tests,
// mirroring the live contract (probed 2026-08-07).
func startAlertsV2Server(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	al := map[string]any{
		"objectId": "aaa111aaa111aaa111aaa111", "title": "Device Uptime Monitoring",
		"severity": "ALERT_SEVERITY_MEDIUM", "status": "ALERT_STATUS_OPEN",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
		switch {
		case p == "/alerts" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"alerts": []map[string]any{al}, "count": 1})
		case p == "/alerts-stats" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"context": map[string]any{"count": 21}, "totalCount": 21})
		case strings.HasSuffix(p, "/occurrences") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"alertOccurrences": []map[string]any{{"occurredAt": "2026-08-05T10:00:00Z"}}, "count": 1})
		case strings.HasSuffix(p, "/notes") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"notes": []map[string]any{{"alertNote": "n"}}})
		case strings.HasSuffix(p, "/notes") && r.Method == http.MethodPost:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"note": map[string]any{"alertNoteObjectId": "n1", "alertNote": "n"}})
		case strings.HasSuffix(p, "/status") && r.Method == http.MethodPost:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"alert": al})
		case (p == "/alerts/bulk-delete" || p == "/alerts/bulk-update") && r.Method == http.MethodPost:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"affectedCount": 3})
		case strings.HasPrefix(p, "/alerts/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"alert": al})
		case strings.HasPrefix(p, "/alerts/") && r.Method == http.MethodDelete:
			if capture != nil {
				(*capture)["deletedPath"] = p
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

func TestMCPAlertsList(t *testing.T) {
	overrideV2ClientForTest(t, startAlertsV2Server(t, nil).URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "alerts_list", map[string]any{}))
	var wrapper struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &wrapper); err != nil || len(wrapper.Data) != 1 {
		t.Fatalf("ResponseKey unwrap failed: %v\n%s", err, out)
	}
}

func TestMCPAlertsGet_Unwraps(t *testing.T) {
	overrideV2ClientForTest(t, startAlertsV2Server(t, nil).URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "alerts_get", map[string]any{"identifier": "aaa111aaa111aaa111aaa111"}))
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if obj["title"] != "Device Uptime Monitoring" {
		t.Errorf("get did not unwrap alert: %v", obj)
	}
	if _, wrapped := obj["alert"]; wrapped {
		t.Error("response still wrapped")
	}
}

func TestMCPAlertsStats(t *testing.T) {
	overrideV2ClientForTest(t, startAlertsV2Server(t, nil).URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "alerts_stats", map[string]any{}))
	if !strings.Contains(out, "totalCount") {
		t.Errorf("stats missing totalCount: %s", out)
	}
}

func TestMCPAlertsOccurrencesAndNotes(t *testing.T) {
	overrideV2ClientForTest(t, startAlertsV2Server(t, nil).URL)
	cs := connectToolTestServer(t, Options{})
	for _, tool := range []string{"alerts_occurrences", "alerts_notes"} {
		out := getResultText(t, callTool(t, cs, tool, map[string]any{"identifier": "aaa111aaa111aaa111aaa111"}))
		var wrapper struct {
			Data []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &wrapper); err != nil || len(wrapper.Data) != 1 {
			t.Fatalf("%s not unwrapped to array: %v\n%s", tool, err, out)
		}
	}
}

func TestMCPAlertsAddNote_ExecuteAndPlan(t *testing.T) {
	var body map[string]any
	overrideV2ClientForTest(t, startAlertsV2Server(t, &body).URL)
	cs := connectToolTestServer(t, Options{})

	// Plan: no POST.
	out := getResultText(t, callTool(t, cs, "alerts_add_note", map[string]any{
		"identifier": "aaa111aaa111aaa111aaa111", "note": "n",
	}))
	if body != nil {
		t.Errorf("plan must not POST, captured: %v", body)
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}

	// Execute: body carries alertNote.
	callTool(t, cs, "alerts_add_note", map[string]any{
		"identifier": "aaa111aaa111aaa111aaa111", "note": "looking", "execute": true,
	})
	if body["alertNote"] != "looking" {
		t.Errorf("note body wrong: %v", body)
	}
}

func TestMCPAlertsStatus_ExecuteAndValidation(t *testing.T) {
	var body map[string]any
	overrideV2ClientForTest(t, startAlertsV2Server(t, &body).URL)
	cs := connectToolTestServer(t, Options{})

	// Invalid status is rejected before any POST.
	out := getResultText(t, callTool(t, cs, "alerts_status", map[string]any{
		"identifier": "aaa111aaa111aaa111aaa111", "status": "snoozed", "execute": true,
	}))
	if !strings.Contains(out, "invalid status") {
		t.Errorf("expected invalid-status error, got: %s", out)
	}
	if body != nil {
		t.Errorf("invalid status must not POST, captured: %v", body)
	}

	// Valid status normalizes to the enum.
	callTool(t, cs, "alerts_status", map[string]any{
		"identifier": "aaa111aaa111aaa111aaa111", "status": "resolved", "execute": true,
	})
	if body["status"] != "ALERT_STATUS_RESOLVED" {
		t.Errorf("status not normalized: %v", body["status"])
	}
}

func TestMCPAlertsBulkDelete_GuardPlanExecute(t *testing.T) {
	var body map[string]any
	overrideV2ClientForTest(t, startAlertsV2Server(t, &body).URL)
	cs := connectToolTestServer(t, Options{})

	// Empty filter is refused before any POST.
	out := getResultText(t, callTool(t, cs, "alerts_bulk_delete", map[string]any{"execute": true}))
	if !strings.Contains(out, "no filter") {
		t.Errorf("expected empty-filter refusal, got: %s", out)
	}
	if body != nil {
		t.Errorf("empty filter must not POST, captured: %v", body)
	}

	// Plan: no POST.
	out = getResultText(t, callTool(t, cs, "alerts_bulk_delete", map[string]any{"severity": []any{"high"}}))
	if body != nil {
		t.Errorf("plan must not POST, captured: %v", body)
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}

	// Execute: normalized filter is sent, affectedCount reported.
	out = getResultText(t, callTool(t, cs, "alerts_bulk_delete", map[string]any{"severity": []any{"high"}, "execute": true}))
	if body["filter"].(map[string]any)["severity"].([]any)[0] != "ALERT_SEVERITY_HIGH" {
		t.Errorf("severity not normalized: %v", body["filter"])
	}
	if !strings.Contains(out, "3 alert(s) deleted") {
		t.Errorf("affectedCount not reported: %s", out)
	}
}

func TestMCPAlertsBulkUpdate_Body(t *testing.T) {
	var body map[string]any
	overrideV2ClientForTest(t, startAlertsV2Server(t, &body).URL)
	cs := connectToolTestServer(t, Options{})

	// Invalid target status rejected, no POST.
	out := getResultText(t, callTool(t, cs, "alerts_bulk_update", map[string]any{
		"title": "x", "set_status": "snoozed", "execute": true,
	}))
	if !strings.Contains(out, "invalid status") {
		t.Errorf("expected invalid-status error, got: %s", out)
	}
	if body != nil {
		t.Errorf("invalid status must not POST, captured: %v", body)
	}

	callTool(t, cs, "alerts_bulk_update", map[string]any{
		"status": []any{"open"}, "set_status": "resolved", "remark": "sweep", "execute": true,
	})
	uf := body["updateField"].(map[string]any)
	if uf["status"] != "ALERT_STATUS_RESOLVED" {
		t.Errorf("updateField.status = %v", uf["status"])
	}
	if body["remark"] != "sweep" {
		t.Errorf("remark = %v", body["remark"])
	}
}

func TestMCPAlertsDelete_PathAndPlan(t *testing.T) {
	var cap = map[string]any{}
	overrideV2ClientForTest(t, startAlertsV2Server(t, &cap).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "alerts_delete", map[string]any{"identifier": "aaa111aaa111aaa111aaa111"}))
	if _, deleted := cap["deletedPath"]; deleted {
		t.Error("plan must not DELETE")
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}

	callTool(t, cs, "alerts_delete", map[string]any{"identifier": "aaa111aaa111aaa111aaa111", "execute": true})
	if cap["deletedPath"] != "/alerts/aaa111aaa111aaa111aaa111" {
		t.Errorf("delete hit wrong path: %v", cap["deletedPath"])
	}
}
