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

// startAlertsServer mirrors the live /alerts shape (probed 2026-08-07): list
// wrapped in {alerts, count}, id under objectId, single GET + status POST
// wrapped in {alert}, occurrences {alertOccurrences}, notes {notes}, add-note
// {note}, stats bare {context, totalCount}, DELETE → {}.
func startAlertsServer(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	al := map[string]any{
		"objectId": "aaa111aaa111aaa111aaa111", "title": "Device Uptime Monitoring",
		"severity": "ALERT_SEVERITY_MEDIUM", "status": "ALERT_STATUS_OPEN",
		"sourceName": "host-1", "lastOccurredAt": "2026-08-05T10:00:00Z",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/alerts" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"alerts": []map[string]any{al}, "count": 1})
		case p == "/alerts-stats" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"context": map[string]any{"count": 21}, "totalCount": 21})
		case strings.HasSuffix(p, "/occurrences") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"alertOccurrences": []map[string]any{{"occurredAt": "2026-08-05T10:00:00Z"}}, "count": 1})
		case strings.HasSuffix(p, "/notes") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"notes": []map[string]any{{"alertNote": "looking into it"}}})
		case strings.HasSuffix(p, "/notes") && r.Method == http.MethodPost:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"note": map[string]any{"alertNote": "looking into it", "alertNoteObjectId": "n1"}})
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

func runAlerts(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	viper.Set("cache.enabled", false)
	root := NewRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"alerts"}, args...))
	err := root.Execute()
	return out.String(), errBuf.String(), err
}

func TestAlertsList(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startAlertsServer(t, nil).URL)
	out, errBuf, err := runAlerts(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []map[string]any
	if e := json.Unmarshal([]byte(out), &rows); e != nil || len(rows) != 1 {
		t.Fatalf("ResponseKey unwrap failed: %v\n%s", e, out)
	}
	if rows[0]["title"] != "Device Uptime Monitoring" {
		t.Errorf("row = %v", rows[0])
	}
	if !strings.Contains(errBuf, "1 items") {
		t.Errorf("footer missing: %s", errBuf)
	}
}

func TestAlertsList_IDs(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startAlertsServer(t, nil).URL)
	out, _, err := runAlerts(t, "list", "--ids")
	if err != nil {
		t.Fatalf("list --ids: %v", err)
	}
	if strings.TrimSpace(out) != "aaa111aaa111aaa111aaa111" {
		t.Errorf("--ids must emit objectId, got %q", out)
	}
}

func TestAlertsGet_Unwraps(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startAlertsServer(t, nil).URL)
	out, _, err := runAlerts(t, "get", "aaa111aaa111aaa111aaa111")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var obj map[string]any
	if e := json.Unmarshal([]byte(out), &obj); e != nil {
		t.Fatalf("not JSON: %v", e)
	}
	if obj["title"] != "Device Uptime Monitoring" {
		t.Errorf("get did not unwrap alert: %v", obj)
	}
	if _, wrapped := obj["alert"]; wrapped {
		t.Error("response still wrapped in alert envelope")
	}
}

func TestAlertsStats(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startAlertsServer(t, nil).URL)
	out, _, err := runAlerts(t, "stats")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(out, "totalCount") {
		t.Errorf("stats output missing totalCount: %s", out)
	}
}

func TestAlertsOccurrences(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startAlertsServer(t, nil).URL)
	out, _, err := runAlerts(t, "occurrences", "aaa111aaa111aaa111aaa111")
	if err != nil {
		t.Fatalf("occurrences: %v", err)
	}
	var rows []map[string]any
	if e := json.Unmarshal([]byte(out), &rows); e != nil || len(rows) != 1 {
		t.Fatalf("occurrences not unwrapped to array: %v\n%s", e, out)
	}
}

func TestAlertsNotes(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startAlertsServer(t, nil).URL)
	out, _, err := runAlerts(t, "notes", "aaa111aaa111aaa111aaa111")
	if err != nil {
		t.Fatalf("notes: %v", err)
	}
	var rows []map[string]any
	if e := json.Unmarshal([]byte(out), &rows); e != nil || len(rows) != 1 {
		t.Fatalf("notes not unwrapped to array: %v\n%s", e, out)
	}
}

func TestAlertsAddNote_Body(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startAlertsServer(t, &body).URL)
	out, _, err := runAlerts(t, "add-note", "aaa111aaa111aaa111aaa111", "looking into it")
	if err != nil {
		t.Fatalf("add-note: %v", err)
	}
	if body["alertNote"] != "looking into it" {
		t.Errorf("note body wrong: %v", body)
	}
	// Output is the unwrapped note.
	if !strings.Contains(out, "alertNoteObjectId") {
		t.Errorf("add-note did not surface unwrapped note: %s", out)
	}
}

func TestAlertsStatus_Body(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startAlertsServer(t, &body).URL)
	if _, _, err := runAlerts(t, "status", "aaa111aaa111aaa111aaa111", "acknowledged", "--remark", "triaged"); err != nil {
		t.Fatalf("status: %v", err)
	}
	if body["status"] != "ALERT_STATUS_ACKNOWLEDGED" {
		t.Errorf("status not normalized to enum: %v", body["status"])
	}
	if body["remark"] != "triaged" {
		t.Errorf("remark not sent: %v", body["remark"])
	}
}

func TestAlertsStatus_Invalid(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startAlertsServer(t, nil).URL)
	if _, _, err := runAlerts(t, "status", "aaa111aaa111aaa111aaa111", "snoozed"); err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("expected invalid-status error, got %v", err)
	}
}

func TestAlerts_PlanNoMutation(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startAlertsServer(t, &body).URL)
	_, errBuf, err := runAlerts(t, "status", "aaa111aaa111aaa111aaa111", "resolved", "--plan")
	var exitErr *ExitError
	if !errorAs(err, &exitErr) || exitErr.Code != 10 {
		t.Fatalf("expected plan ExitError(10), got: %v", err)
	}
	if body != nil {
		t.Errorf("--plan must not POST, captured: %v", body)
	}
	if !strings.Contains(errBuf, "Plan:") {
		t.Errorf("expected plan preview on stderr, got: %s", errBuf)
	}
}

func TestAlertsBulkDelete_EmptyFilterRefused(t *testing.T) {
	setupUsersTest(t)
	var cap = map[string]any{}
	overrideV2Client(t, startAlertsServer(t, &cap).URL)
	_, _, err := runAlerts(t, "bulk-delete", "--force")
	if err == nil || !strings.Contains(err.Error(), "no filter") {
		t.Fatalf("expected empty-filter refusal, got %v", err)
	}
	if len(cap) != 0 {
		t.Errorf("must not POST on empty filter, captured: %v", cap)
	}
}

func TestAlertsBulkDelete_Body(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startAlertsServer(t, &body).URL)
	out, _, err := runAlerts(t, "bulk-delete", "--severity", "high", "--status", "resolved", "--exclude-id", "keep1", "--force")
	if err != nil {
		t.Fatalf("bulk-delete: %v", err)
	}
	filter := body["filter"].(map[string]any)
	if sev := filter["severity"].([]any); sev[0] != "ALERT_SEVERITY_HIGH" {
		t.Errorf("severity not normalized: %v", filter["severity"])
	}
	if filter["status"].([]any)[0] != "ALERT_STATUS_RESOLVED" {
		t.Errorf("status not normalized: %v", filter["status"])
	}
	if body["excludeIds"].([]any)[0] != "keep1" {
		t.Errorf("excludeIds = %v", body["excludeIds"])
	}
	if !strings.Contains(out, "3 alert(s) deleted") {
		t.Errorf("affectedCount not reported: %q", out)
	}
}

func TestAlertsBulkUpdate_Body(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startAlertsServer(t, &body).URL)
	out, _, err := runAlerts(t, "bulk-update", "--status", "open", "--set-status", "acknowledged", "--remark", "sweep", "--force")
	if err != nil {
		t.Fatalf("bulk-update: %v", err)
	}
	uf := body["updateField"].(map[string]any)
	if uf["status"] != "ALERT_STATUS_ACKNOWLEDGED" {
		t.Errorf("updateField.status = %v", uf["status"])
	}
	if body["remark"] != "sweep" {
		t.Errorf("remark = %v", body["remark"])
	}
	if !strings.Contains(out, "3 alert(s) updated") {
		t.Errorf("affectedCount not reported: %q", out)
	}
}

func TestAlertsBulkUpdate_InvalidStatus(t *testing.T) {
	setupUsersTest(t)
	var cap = map[string]any{}
	overrideV2Client(t, startAlertsServer(t, &cap).URL)
	_, _, err := runAlerts(t, "bulk-update", "--title", "x", "--set-status", "snoozed", "--force")
	if err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("expected invalid-status error, got %v", err)
	}
	if len(cap) != 0 {
		t.Errorf("invalid status must not POST, captured: %v", cap)
	}
}

func TestAlertsBulk_PlanNoMutation(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startAlertsServer(t, &body).URL)
	_, _, err := runAlerts(t, "bulk-delete", "--severity", "low", "--plan")
	var exitErr *ExitError
	if !errorAs(err, &exitErr) || exitErr.Code != 10 {
		t.Fatalf("expected plan ExitError(10), got: %v", err)
	}
	if body != nil {
		t.Errorf("--plan must not POST, captured: %v", body)
	}
}

func TestAlertsDelete(t *testing.T) {
	setupUsersTest(t)
	var cap = map[string]any{}
	overrideV2Client(t, startAlertsServer(t, &cap).URL)
	out, _, err := runAlerts(t, "delete", "aaa111aaa111aaa111aaa111", "--force")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if cap["deletedPath"] != "/alerts/aaa111aaa111aaa111aaa111" {
		t.Errorf("delete path: %v", cap["deletedPath"])
	}
	if !strings.Contains(out, "Device Uptime Monitoring") {
		t.Errorf("delete message should show resolved title, got: %q", out)
	}
}
