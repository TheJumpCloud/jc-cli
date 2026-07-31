package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startRolesV2Server serves the /api/v2/roles family for MCP tool tests,
// mirroring the live contract (probed 2026-07-24): list wrapped in {results},
// single GET + POST bare, PUT full-object replace bare.
func startRolesV2Server(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	role := map[string]any{
		"id": "aaa111aaa111aaa111aaa111", "name": "CI Bot", "description": "runs CI",
		"scopes": []any{"commands", "systems"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
		switch {
		case p == "/roles" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{role}, "totalCount": 1})
		case p == "/roles" && r.Method == http.MethodPost:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(role)
		case strings.HasPrefix(p, "/roles/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(role)
		case strings.HasPrefix(p, "/roles/") && r.Method == http.MethodPut:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(role)
		case strings.HasPrefix(p, "/roles/") && r.Method == http.MethodDelete:
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

func TestMCPRolesList(t *testing.T) {
	srv := startRolesV2Server(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "roles_list", map[string]any{}))
	var wrapper struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &wrapper); err != nil || len(wrapper.Data) != 1 {
		t.Fatalf("ResponseKey unwrap failed: %v\n%s", err, out)
	}
}

func TestMCPRolesCreate_ExecuteBody(t *testing.T) {
	var body map[string]any
	srv := startRolesV2Server(t, &body)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	callTool(t, cs, "roles_create", map[string]any{
		"name": "CI Bot", "scopes": "commands, systems ,", "description": "runs CI", "execute": true,
	})
	if body["name"] != "CI Bot" || body["description"] != "runs CI" {
		t.Errorf("body wrong: %v", body)
	}
	sc, _ := body["scopes"].([]any)
	if len(sc) != 2 || sc[0] != "commands" || sc[1] != "systems" {
		t.Errorf("scopes not split/trimmed: %v", body["scopes"])
	}
}

func TestMCPRolesCreate_RequiresScopes(t *testing.T) {
	srv := startRolesV2Server(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "roles_create", map[string]any{
		"name": "X", "scopes": " , ,", "execute": true,
	}))
	if !strings.Contains(out, "at least one scope") {
		t.Errorf("expected empty-scopes error, got: %s", out)
	}
}

func TestMCPRolesCreate_PlanNoPost(t *testing.T) {
	var body map[string]any
	srv := startRolesV2Server(t, &body)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "roles_create", map[string]any{"name": "X", "scopes": "commands"}))
	if body != nil {
		t.Errorf("plan must not POST, captured: %v", body)
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}
}

// TestMCPRolesUpdate_RMW: partial update (only name) preserves scopes and
// description via read-modify-write and strips server-managed id.
func TestMCPRolesUpdate_RMW(t *testing.T) {
	var put map[string]any
	srv := startRolesV2Server(t, &put)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	callTool(t, cs, "roles_update", map[string]any{
		"identifier": "aaa111aaa111aaa111aaa111", "name": "Renamed", "execute": true,
	})
	if put["name"] != "Renamed" {
		t.Errorf("name not applied: %v", put["name"])
	}
	sc, _ := put["scopes"].([]any)
	if len(sc) != 2 {
		t.Errorf("scopes clobbered by partial update: %v", put["scopes"])
	}
	if put["description"] != "runs CI" {
		t.Errorf("description clobbered: %v", put["description"])
	}
	if _, ok := put["id"]; ok {
		t.Error("server-managed id must be stripped from PUT body")
	}
}

func TestMCPRolesUpdate_PlanNoPut(t *testing.T) {
	var put map[string]any
	srv := startRolesV2Server(t, &put)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "roles_update", map[string]any{
		"identifier": "aaa111aaa111aaa111aaa111", "name": "Renamed",
	}))
	if put["name"] != nil {
		t.Errorf("plan must not PUT, captured: %v", put)
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}
}

func TestMCPRolesDelete_PathAndPlan(t *testing.T) {
	var cap = map[string]any{}
	srv := startRolesV2Server(t, &cap)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "roles_delete", map[string]any{"identifier": "aaa111aaa111aaa111aaa111"}))
	if _, deleted := cap["deletedPath"]; deleted {
		t.Error("plan must not DELETE")
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}

	callTool(t, cs, "roles_delete", map[string]any{"identifier": "aaa111aaa111aaa111aaa111", "execute": true})
	if cap["deletedPath"] != "/roles/aaa111aaa111aaa111aaa111" {
		t.Errorf("delete hit wrong path: %v", cap["deletedPath"])
	}
}
