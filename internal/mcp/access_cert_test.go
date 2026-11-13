package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	acTestRunID = "6ad0e800617e39fb0663b365"
	acTestURL   = "https://bucket.example.test/report.pdf?X-Amz-Signature=fixture"
)

func startAccessCertMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("non-GET request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch p := r.URL.Path; {
		case strings.HasSuffix(p, "/audit-report") && strings.Contains(p, acTestRunID):
			w.Write([]byte(`{"status":"exports_ready","documentHash":"abc123","artifacts":{"csv":{"url":"` + acTestURL + `","expiresAt":"2026-11-13T10:00:00Z"},"pdf":{"url":"` + acTestURL + `","expiresAt":"2026-11-13T10:05:00Z"}}}`))
		case strings.HasSuffix(p, "/audit-report"):
			w.WriteHeader(409)
			w.Write([]byte(`{"message":"audit report is only available for closed campaign runs"}`))
		case strings.HasSuffix(p, "/settings"):
			w.Write([]byte(`{"featureEnabled":true}`))
		case strings.Contains(p, "/campaign-runs/") && !strings.HasSuffix(p, "/review-items"):
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"campaign run not found"}`))
		case strings.Contains(p, "/campaign/"):
			w.WriteHeader(404)
		default:
			w.Write([]byte(`{"results":[{"id":"1"}],"totalCount":1,"metadata":{"applicationIds":[],"userGroupIds":[]}}`))
		}
	}))
	t.Cleanup(ts.Close)
	overrideV2ClientForTest(t, ts.URL)
	return ts
}

func TestAccessCertAuditReport_NoURLs(t *testing.T) {
	startAccessCertMCPServer(t)
	cs := connectToolTestServer(t, Options{})
	res := callTool(t, cs, "access_cert_audit_report_get", map[string]any{"runId": acTestRunID})
	if res.IsError {
		t.Fatalf("error: %s", getResultText(t, res))
	}
	text := getResultText(t, res)
	for _, bad := range []string{acTestURL, "https://", "X-Amz", `"url"`} {
		if strings.Contains(text, bad) {
			t.Fatalf("result leaks %q: %s", bad, text)
		}
	}
	for _, want := range []string{"exports_ready", "abc123", "csv", "pdf", "2026-11-13T10:05:00Z", "CLI"} {
		if !strings.Contains(text, want) {
			t.Errorf("result missing %q: %s", want, text)
		}
	}
}

func TestAccessCertAuditReport_409(t *testing.T) {
	startAccessCertMCPServer(t)
	cs := connectToolTestServer(t, Options{})
	res := callTool(t, cs, "access_cert_audit_report_get", map[string]any{"runId": "000000000000000000000001"})
	if !res.IsError || !strings.Contains(getResultText(t, res), "audit report is only available for closed campaign runs") {
		t.Fatalf("got %v %s", res.IsError, getResultText(t, res))
	}
}

func TestAccessCertRunGet_NotFound(t *testing.T) {
	startAccessCertMCPServer(t)
	cs := connectToolTestServer(t, Options{})
	res := callTool(t, cs, "access_cert_run_get", map[string]any{"runId": "000000000000000000000000"})
	if !res.IsError || !strings.Contains(getResultText(t, res), "campaign run not found") {
		t.Fatalf("got %s", getResultText(t, res))
	}
}

func TestAccessCert_InvalidIDRejectedLocally(t *testing.T) {
	startAccessCertMCPServer(t)
	cs := connectToolTestServer(t, Options{})
	for tool, arg := range map[string]string{
		"access_cert_run_get": "runId", "access_cert_campaign_get": "campaignId",
		"access_cert_review_items_list": "runId", "access_cert_audit_report_get": "runId",
	} {
		res := callTool(t, cs, tool, map[string]any{arg: "bad-id"})
		if !res.IsError || !strings.Contains(getResultText(t, res), "24-character hex") {
			t.Errorf("%s: got %s", tool, getResultText(t, res))
		}
	}
}

func TestAccessCert_ReadOnlyGETOnly(t *testing.T) {
	startAccessCertMCPServer(t)
	cs := connectToolTestServer(t, Options{})
	calls := map[string]map[string]any{
		"access_cert_settings_get":      {},
		"access_cert_campaigns_list":    {"metadata": true},
		"access_cert_campaign_get":      {"campaignId": acTestRunID},
		"access_cert_runs_list":         {"status": "all"},
		"access_cert_run_get":           {"runId": acTestRunID},
		"access_cert_review_items_list": {"runId": acTestRunID},
		"access_cert_audit_report_get":  {"runId": acTestRunID},
		"access_cert_admin_queue_list":  {},
	}
	for name, args := range calls {
		callTool(t, cs, name, args)
	}
}

func TestAccessCertRunsList_DescriptionStatesActiveDefault(t *testing.T) {
	startAccessCertMCPServer(t)
	cs := connectToolTestServer(t, Options{})
	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "access_cert_runs_list" {
			if !strings.Contains(tool.Description, "ACTIVE runs only") {
				t.Fatal(tool.Description)
			}
			return
		}
	}
	t.Fatal("tool not registered")
}
