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

func startRolesServer(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	role := map[string]any{
		"id": "aaa111aaa111aaa111aaa111", "name": "CI Bot", "description": "runs CI",
		"scopes": []any{"commands", "systems"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
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

func runRoles(t *testing.T, args ...string) (string, error) {
	t.Helper()
	viper.Set("cache.enabled", false)
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(new(bytes.Buffer))
	root.SetArgs(append([]string{"roles"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestRolesList(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startRolesServer(t, nil).URL)
	out, err := runRoles(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []map[string]any
	if e := json.Unmarshal([]byte(out), &rows); e != nil || len(rows) != 1 {
		t.Fatalf("ResponseKey unwrap failed: %v\n%s", e, out)
	}
}

func TestRolesCreate_Body(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startRolesServer(t, &body).URL)
	if _, err := runRoles(t, "create", "--name", "CI Bot", "--scopes", "commands, systems ,", "--description", "runs CI"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if body["name"] != "CI Bot" || body["description"] != "runs CI" {
		t.Errorf("body wrong: %v", body)
	}
	sc, _ := body["scopes"].([]any)
	if len(sc) != 2 || sc[0] != "commands" || sc[1] != "systems" {
		t.Errorf("scopes not split/trimmed: %v", body["scopes"])
	}
}

func TestRolesCreate_RequiresScopes(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startRolesServer(t, nil).URL)
	if _, err := runRoles(t, "create", "--name", "X", "--scopes", " , ,"); err == nil || !strings.Contains(err.Error(), "at least one scope") {
		t.Fatalf("expected empty-scopes error, got %v", err)
	}
}

// TestRolesUpdate_RMW: a partial update (only --name) must preserve scopes
// and description via read-modify-write, and strip the server-managed id.
func TestRolesUpdate_RMW(t *testing.T) {
	setupUsersTest(t)
	var put map[string]any
	overrideV2Client(t, startRolesServer(t, &put).URL)
	if _, err := runRoles(t, "update", "aaa111aaa111aaa111aaa111", "--name", "Renamed"); err != nil {
		t.Fatalf("update: %v", err)
	}
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

// TestRoles_PlanNoMutation guards the Bugbot finding: --plan must preview,
// not execute. create --plan issues no POST and returns ExitError(10).
func TestRoles_PlanNoMutation(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startRolesServer(t, &body).URL)
	root := NewRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	viper.Set("cache.enabled", false)
	root.SetArgs([]string{"roles", "create", "--name", "X", "--scopes", "commands", "--plan"})
	err := root.Execute()
	var exitErr *ExitError
	if !errorAs(err, &exitErr) || exitErr.Code != 10 {
		t.Fatalf("expected plan ExitError(10), got: %v", err)
	}
	if body != nil {
		t.Errorf("--plan must not POST, but a create body was captured: %v", body)
	}
	if !strings.Contains(errBuf.String(), "Plan:") {
		t.Errorf("expected a plan preview on stderr, got: %s", errBuf.String())
	}
}

func TestRolesDelete(t *testing.T) {
	setupUsersTest(t)
	var cap = map[string]any{}
	overrideV2Client(t, startRolesServer(t, &cap).URL)
	if _, err := runRoles(t, "delete", "aaa111aaa111aaa111aaa111", "--force"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if cap["deletedPath"] != "/roles/aaa111aaa111aaa111aaa111" {
		t.Errorf("delete path: %v", cap["deletedPath"])
	}
}
