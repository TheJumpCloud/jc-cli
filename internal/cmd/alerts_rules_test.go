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

// startHealthRulesServer mirrors the live /healthmonitoring shape (probed
// 2026-08-18): rules list wrapped in {rules, count}, id under objectId, single
// GET + create + update + status wrapped in {rule}, templates list {templates},
// stats bare {systemInsightsRequiredCount}, DELETE → {}.
func startHealthRulesServer(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	rule := map[string]any{
		"objectId": "bbb222bbb222bbb222bbb222", "name": "Command Execution Failure",
		"category": "RULE_CATEGORY_SYSTEM", "severity": "RULE_SEVERITY_MEDIUM",
		"status": "RULE_STATUS_ENABLED", "ruleType": "RULE_TYPE_TEMPLATE_BASED",
	}
	tmpl := map[string]any{
		"objectId": "664cbeded18e0095a0010000", "name": "New Users in JumpCloud Directory",
		"category": "RULE_CATEGORY_DIRECTORY", "type": "User addition monitoring",
		"description": "Get alerted when a user is added.",
	}
	capBody := func(r *http.Request) {
		if capture != nil {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, capture)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
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

func runRules(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	viper.Set("cache.enabled", false)
	root := NewRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	if stdin != "" {
		root.SetIn(strings.NewReader(stdin))
	}
	root.SetArgs(append([]string{"alerts", "rules"}, args...))
	err := root.Execute()
	return out.String(), errBuf.String(), err
}

func TestHealthRulesList_Unwrap(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startHealthRulesServer(t, nil).URL)
	out, errBuf, err := runRules(t, "", "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []map[string]any
	if e := json.Unmarshal([]byte(out), &rows); e != nil || len(rows) != 1 {
		t.Fatalf("ResponseKey unwrap failed: %v\n%s", e, out)
	}
	if rows[0]["name"] != "Command Execution Failure" {
		t.Errorf("row = %v", rows[0])
	}
	if !strings.Contains(errBuf, "1 items") {
		t.Errorf("footer missing: %s", errBuf)
	}
}

func TestHealthRulesList_IDs(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startHealthRulesServer(t, nil).URL)
	out, _, err := runRules(t, "", "list", "--ids")
	if err != nil {
		t.Fatalf("list --ids: %v", err)
	}
	if strings.TrimSpace(out) != "bbb222bbb222bbb222bbb222" {
		t.Errorf("--ids must emit objectId, got %q", out)
	}
}

func TestHealthRulesGet_Unwraps(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startHealthRulesServer(t, nil).URL)
	out, _, err := runRules(t, "", "get", "bbb222bbb222bbb222bbb222")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var obj map[string]any
	if e := json.Unmarshal([]byte(out), &obj); e != nil {
		t.Fatalf("not JSON: %v", e)
	}
	if obj["name"] != "Command Execution Failure" {
		t.Errorf("get did not unwrap {rule}: %v", obj)
	}
}

func TestHealthRulesStats_Bare(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startHealthRulesServer(t, nil).URL)
	out, _, err := runRules(t, "", "stats")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	var obj map[string]any
	if e := json.Unmarshal([]byte(out), &obj); e != nil {
		t.Fatalf("not JSON: %v", e)
	}
	if obj["systemInsightsRequiredCount"] != float64(4) {
		t.Errorf("stats body = %v", obj)
	}
}

func TestHealthRulesTemplates(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startHealthRulesServer(t, nil).URL)
	out, _, err := runRules(t, "", "templates", "list")
	if err != nil {
		t.Fatalf("templates list: %v", err)
	}
	var rows []map[string]any
	if e := json.Unmarshal([]byte(out), &rows); e != nil || len(rows) != 1 {
		t.Fatalf("templates ResponseKey unwrap failed: %v\n%s", e, out)
	}
	out, _, err = runRules(t, "", "templates", "get", "664cbeded18e0095a0010000")
	if err != nil {
		t.Fatalf("templates get: %v", err)
	}
	var obj map[string]any
	if json.Unmarshal([]byte(out), &obj); obj["name"] != "New Users in JumpCloud Directory" {
		t.Errorf("templates get did not unwrap: %v", obj)
	}
}

func TestHealthRulesStatus_Body(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startHealthRulesServer(t, &body).URL)
	if _, _, err := runRules(t, "", "status", "bbb222bbb222bbb222bbb222", "disabled"); err != nil {
		t.Fatalf("status: %v", err)
	}
	if body["status"] != "RULE_STATUS_DISABLED" {
		t.Errorf("status not normalized to enum: %v", body["status"])
	}
}

func TestHealthRulesStatus_Invalid(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startHealthRulesServer(t, nil).URL)
	if _, _, err := runRules(t, "", "status", "bbb222bbb222bbb222bbb222", "paused"); err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("expected invalid-status error, got %v", err)
	}
}

func TestHealthRulesCreate_WrapsRule(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startHealthRulesServer(t, &body).URL)
	if _, _, err := runRules(t, `{"name":"My Rule","severity":"RULE_SEVERITY_HIGH"}`, "create", "--rule-file", "-"); err != nil {
		t.Fatalf("create: %v", err)
	}
	inner, ok := body["rule"].(map[string]any)
	if !ok {
		t.Fatalf("create body not wrapped in {rule}: %v", body)
	}
	if inner["name"] != "My Rule" {
		t.Errorf("create inner rule = %v", inner)
	}
}

func TestHealthRulesCreate_AcceptsWrappedFile(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startHealthRulesServer(t, &body).URL)
	// A body captured from `get` is already {"rule":…}; it must round-trip.
	if _, _, err := runRules(t, `{"rule":{"name":"Round Trip"}}`, "create", "--rule-file", "-"); err != nil {
		t.Fatalf("create wrapped: %v", err)
	}
	inner, _ := body["rule"].(map[string]any)
	if inner["name"] != "Round Trip" {
		t.Errorf("wrapped file not unwrapped before re-wrap: %v", body)
	}
}

func TestHealthRulesUpdate_Body(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startHealthRulesServer(t, &body).URL)
	if _, _, err := runRules(t, `{"name":"Updated"}`, "update", "bbb222bbb222bbb222bbb222", "--rule-file", "-"); err != nil {
		t.Fatalf("update: %v", err)
	}
	inner, _ := body["rule"].(map[string]any)
	if inner["name"] != "Updated" {
		t.Errorf("update body = %v", body)
	}
}

func TestHealthRules_PlanNoMutation(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startHealthRulesServer(t, &body).URL)
	_, _, err := runRules(t, "", "status", "bbb222bbb222bbb222bbb222", "disabled", "--plan")
	var exitErr *ExitError
	if !errorAs(err, &exitErr) || exitErr.Code != 10 {
		t.Fatalf("expected plan ExitError(10), got: %v", err)
	}
	if body != nil {
		t.Errorf("--plan must not mutate, captured: %v", body)
	}
}

func TestHealthRulesDelete(t *testing.T) {
	setupUsersTest(t)
	var cap = map[string]any{}
	overrideV2Client(t, startHealthRulesServer(t, &cap).URL)
	out, _, err := runRules(t, "", "delete", "bbb222bbb222bbb222bbb222", "--force")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if cap["deletedPath"] != "/healthmonitoring/rules/bbb222bbb222bbb222bbb222" {
		t.Errorf("delete path: %v", cap["deletedPath"])
	}
	if !strings.Contains(out, "Command Execution Failure") {
		t.Errorf("delete message should show resolved name, got: %q", out)
	}
}
