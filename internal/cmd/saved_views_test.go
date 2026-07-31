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

// startSavedViewsServer mirrors the live /saved-views shape (probed
// 2026-07-31): list wrapped in {totalCount, views}, id under "id", POST
// returns the bare object, PUT takes a {savedView:{…}} wrapper and returns
// the bare object, DELETE returns {}. There is no GET-by-id endpoint.
func startSavedViewsServer(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	view := map[string]any{
		"id": "aaa111aaa111aaa111aaa111", "adminId": "adm999", "name": "My Devices",
		"source": "devices", "columns": []any{"hostname", "os"},
		"configuration": map[string]any{"filters": []any{}, "sort": "hostname"},
		"shared":        false, "isDefault": false,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/saved-views" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"views": []map[string]any{view}, "totalCount": 1})
		case p == "/saved-views" && r.Method == http.MethodPost:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(view)
		case strings.HasPrefix(p, "/saved-views/") && r.Method == http.MethodPut:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(view)
		case strings.HasPrefix(p, "/saved-views/") && r.Method == http.MethodDelete:
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

func runViews(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	viper.Set("cache.enabled", false)
	root := NewRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"saved-views"}, args...))
	err := root.Execute()
	return out.String(), errBuf.String(), err
}

func TestSavedViewsList(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startSavedViewsServer(t, nil).URL)
	out, errBuf, err := runViews(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []map[string]any
	if e := json.Unmarshal([]byte(out), &rows); e != nil || len(rows) != 1 {
		t.Fatalf("ResponseKey unwrap failed (want 1 row): %v\n%s", e, out)
	}
	if rows[0]["name"] != "My Devices" {
		t.Errorf("row = %v", rows[0])
	}
	if !strings.Contains(errBuf, "1 items") {
		t.Errorf("footer missing: %s", errBuf)
	}
}

// TestSavedViewsList_IDs guards the --ids path emits the id field (used by a
// list|delete pipeline).
func TestSavedViewsList_IDs(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startSavedViewsServer(t, nil).URL)
	out, _, err := runViews(t, "list", "--ids")
	if err != nil {
		t.Fatalf("list --ids: %v", err)
	}
	if strings.TrimSpace(out) != "aaa111aaa111aaa111aaa111" {
		t.Errorf("--ids must emit the id, got %q", out)
	}
}

// TestSavedViewsGet_FetchFromList: there is no GET-by-id, so get reads the
// object back out of the list and outputs the bare object.
func TestSavedViewsGet_FetchFromList(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startSavedViewsServer(t, nil).URL)
	out, _, err := runViews(t, "get", "My Devices")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var obj map[string]any
	if e := json.Unmarshal([]byte(out), &obj); e != nil {
		t.Fatalf("not JSON: %v", e)
	}
	if obj["name"] != "My Devices" || obj["source"] != "devices" {
		t.Errorf("get returned wrong object: %v", obj)
	}
}

func TestSavedViewsCreate_Body(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startSavedViewsServer(t, &body).URL)
	if _, _, err := runViews(t, "create", "--name", "My Devices", "--source", "devices",
		"--columns", "hostname, os ,", "--sort", "hostname", "--shared"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if body["name"] != "My Devices" || body["source"] != "devices" {
		t.Errorf("body name/source wrong: %v", body)
	}
	cols, _ := body["columns"].([]any)
	if len(cols) != 2 || cols[0] != "hostname" || cols[1] != "os" {
		t.Errorf("columns not split/trimmed: %v", body["columns"])
	}
	cfg, _ := body["configuration"].(map[string]any)
	if cfg["sort"] != "hostname" {
		t.Errorf("configuration.sort not set: %v", body["configuration"])
	}
	if body["shared"] != true {
		t.Errorf("shared flag not carried: %v", body["shared"])
	}
}

func TestSavedViewsCreate_RequiresSource(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startSavedViewsServer(t, nil).URL)
	if _, _, err := runViews(t, "create", "--name", "X", "--source", ""); err == nil || !strings.Contains(err.Error(), "source is required") {
		t.Fatalf("expected empty-source error, got %v", err)
	}
}

// TestSavedViewsUpdate_RMW: a partial update (only --name) must preserve
// source/columns/configuration via read-modify-write, wrap the body in
// {savedView}, and strip the server-managed adminId.
func TestSavedViewsUpdate_RMW(t *testing.T) {
	setupUsersTest(t)
	var put map[string]any
	overrideV2Client(t, startSavedViewsServer(t, &put).URL)
	if _, _, err := runViews(t, "update", "aaa111aaa111aaa111aaa111", "--name", "Renamed"); err != nil {
		t.Fatalf("update: %v", err)
	}
	sv, ok := put["savedView"].(map[string]any)
	if !ok {
		t.Fatalf("PUT body not wrapped in savedView: %v", put)
	}
	if sv["name"] != "Renamed" {
		t.Errorf("name not applied: %v", sv["name"])
	}
	if sv["source"] != "devices" {
		t.Errorf("source clobbered by partial update: %v", sv["source"])
	}
	cols, _ := sv["columns"].([]any)
	if len(cols) != 2 {
		t.Errorf("columns clobbered: %v", sv["columns"])
	}
	if _, ok := sv["adminId"]; ok {
		t.Error("server-managed adminId must be stripped from PUT body")
	}
	// id stays (the SavedView schema carries it and live PUT accepts it).
	if sv["id"] != "aaa111aaa111aaa111aaa111" {
		t.Errorf("id should be preserved in body: %v", sv["id"])
	}
}

// TestSavedViews_PlanNoMutation guards --plan previews instead of executing:
// create --plan issues no POST and returns ExitError(10).
func TestSavedViews_PlanNoMutation(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startSavedViewsServer(t, &body).URL)
	_, errBuf, err := runViews(t, "create", "--name", "X", "--source", "users", "--plan")
	var exitErr *ExitError
	if !errorAs(err, &exitErr) || exitErr.Code != 10 {
		t.Fatalf("expected plan ExitError(10), got: %v", err)
	}
	if body != nil {
		t.Errorf("--plan must not POST, but a create body was captured: %v", body)
	}
	if !strings.Contains(errBuf, "Plan:") {
		t.Errorf("expected a plan preview on stderr, got: %s", errBuf)
	}
}

func TestSavedViewsDelete(t *testing.T) {
	setupUsersTest(t)
	var cap = map[string]any{}
	overrideV2Client(t, startSavedViewsServer(t, &cap).URL)
	out, _, err := runViews(t, "delete", "aaa111aaa111aaa111aaa111", "--force")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if cap["deletedPath"] != "/saved-views/aaa111aaa111aaa111aaa111" {
		t.Errorf("delete path: %v", cap["deletedPath"])
	}
	// The success message resolves the real name out of the list.
	if !strings.Contains(out, "My Devices") {
		t.Errorf("delete message should show resolved name, got: %q", out)
	}
}
