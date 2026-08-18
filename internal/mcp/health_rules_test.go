package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startHealthRulesV2Server serves the /api/v2/healthmonitoring family for MCP
// tool tests, mirroring the live contract (probed 2026-08-18).
func startHealthRulesV2Server(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	rule := map[string]any{
		"objectId": "bbb222bbb222bbb222bbb222", "name": "Command Execution Failure",
		"category": "RULE_CATEGORY_SYSTEM", "status": "RULE_STATUS_ENABLED",
	}
	tmpl := map[string]any{
		"objectId": "664cbeded18e0095a0010000", "name": "New Users in JumpCloud Directory",
		"category": "RULE_CATEGORY_DIRECTORY", "type": "User addition monitoring",
	}
	capBody := func(r *http.Request) {
		if capture != nil {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, capture)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
		switch {
		case p == "/healthmonitoring/rules" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"rules": []map[string]any{rule}, "count": 1})
		case p == "/healthmonitoring/rules" && r.Method == http.MethodPost:
			capBody(r)
			json.NewEncoder(w).Encode(map[string]any{"rule": rule})
		case p == "/healthmonitoring/rules-stats" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"systemInsightsRequiredCount": 4})
		case p == "/healthmonitoring/ruletemplates" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"templates": []map[string]any{tmpl}, "count": 1})
		case strings.HasPrefix(p, "/healthmonitoring/ruletemplates/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"template": tmpl})
		case strings.HasSuffix(p, "/status") && r.Method == http.MethodPatch:
			capBody(r)
			json.NewEncoder(w).Encode(map[string]any{"rule": rule})
		case strings.HasPrefix(p, "/healthmonitoring/rules/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"rule": rule})
		case strings.HasPrefix(p, "/healthmonitoring/rules/") && r.Method == http.MethodPatch:
			capBody(r)
			json.NewEncoder(w).Encode(map[string]any{"rule": rule})
		case strings.HasPrefix(p, "/healthmonitoring/rules/") && r.Method == http.MethodDelete:
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

func TestMCPHealthRulesList_Unwrap(t *testing.T) {
	overrideV2ClientForTest(t, startHealthRulesV2Server(t, nil).URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "health_rules_list", map[string]any{}))
	var wrapper struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &wrapper); err != nil || len(wrapper.Data) != 1 {
		t.Fatalf("ResponseKey unwrap failed: %v\n%s", err, out)
	}
}

func TestMCPHealthRulesGet_Unwraps(t *testing.T) {
	overrideV2ClientForTest(t, startHealthRulesV2Server(t, nil).URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "health_rules_get", map[string]any{"identifier": "bbb222bbb222bbb222bbb222"}))
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if obj["name"] != "Command Execution Failure" {
		t.Errorf("get did not unwrap {rule}: %v", obj)
	}
}

func TestMCPHealthRulesStatsAndTemplates(t *testing.T) {
	overrideV2ClientForTest(t, startHealthRulesV2Server(t, nil).URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "health_rules_stats", map[string]any{}))
	if !strings.Contains(out, "systemInsightsRequiredCount") {
		t.Errorf("stats missing field: %s", out)
	}
	out = getResultText(t, callTool(t, cs, "health_rule_templates_list", map[string]any{}))
	var wrapper struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &wrapper); err != nil || len(wrapper.Data) != 1 {
		t.Fatalf("templates not unwrapped: %v\n%s", err, out)
	}
	out = getResultText(t, callTool(t, cs, "health_rule_templates_get", map[string]any{"identifier": "664cbeded18e0095a0010000"}))
	var obj map[string]any
	if json.Unmarshal([]byte(out), &obj); obj["name"] != "New Users in JumpCloud Directory" {
		t.Errorf("template get did not unwrap: %v", obj)
	}
}

func TestMCPHealthRulesStatus_ExecuteAndValidation(t *testing.T) {
	var body map[string]any
	overrideV2ClientForTest(t, startHealthRulesV2Server(t, &body).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "health_rules_status", map[string]any{
		"identifier": "bbb222bbb222bbb222bbb222", "status": "paused", "execute": true,
	}))
	if !strings.Contains(out, "invalid status") {
		t.Errorf("expected invalid-status error, got: %s", out)
	}
	if body != nil {
		t.Errorf("invalid status must not PATCH, captured: %v", body)
	}

	callTool(t, cs, "health_rules_status", map[string]any{
		"identifier": "bbb222bbb222bbb222bbb222", "status": "disabled", "execute": true,
	})
	if body["status"] != "RULE_STATUS_DISABLED" {
		t.Errorf("status not normalized: %v", body["status"])
	}
}

func TestMCPHealthRulesCreate_ExecuteAndPlan(t *testing.T) {
	var body map[string]any
	overrideV2ClientForTest(t, startHealthRulesV2Server(t, &body).URL)
	cs := connectToolTestServer(t, Options{})

	// Plan: no POST.
	out := getResultText(t, callTool(t, cs, "health_rules_create", map[string]any{
		"rule_json": `{"name":"My Rule"}`,
	}))
	if body != nil {
		t.Errorf("plan must not POST, captured: %v", body)
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}

	// Execute: body wrapped in {rule}.
	callTool(t, cs, "health_rules_create", map[string]any{
		"rule_json": `{"name":"My Rule"}`, "execute": true,
	})
	inner, ok := body["rule"].(map[string]any)
	if !ok || inner["name"] != "My Rule" {
		t.Errorf("create body not wrapped in {rule}: %v", body)
	}
}

func TestMCPHealthRulesUpdate_Body(t *testing.T) {
	var body map[string]any
	overrideV2ClientForTest(t, startHealthRulesV2Server(t, &body).URL)
	cs := connectToolTestServer(t, Options{})
	// A body from health_rules_get is already {rule:…}; it must round-trip.
	callTool(t, cs, "health_rules_update", map[string]any{
		"identifier": "bbb222bbb222bbb222bbb222", "rule_json": `{"rule":{"name":"Updated"}}`, "execute": true,
	})
	inner, _ := body["rule"].(map[string]any)
	if inner["name"] != "Updated" {
		t.Errorf("update body = %v", body)
	}
}

func TestMCPHealthRulesDelete_PathAndPlan(t *testing.T) {
	var cap = map[string]any{}
	overrideV2ClientForTest(t, startHealthRulesV2Server(t, &cap).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "health_rules_delete", map[string]any{"identifier": "bbb222bbb222bbb222bbb222"}))
	if _, deleted := cap["deletedPath"]; deleted {
		t.Error("plan must not DELETE")
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}

	callTool(t, cs, "health_rules_delete", map[string]any{"identifier": "bbb222bbb222bbb222bbb222", "execute": true})
	if cap["deletedPath"] != "/healthmonitoring/rules/bbb222bbb222bbb222bbb222" {
		t.Errorf("delete hit wrong path: %v", cap["deletedPath"])
	}
}
