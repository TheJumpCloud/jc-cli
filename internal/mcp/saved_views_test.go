package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startSavedViewsV2Server serves the /api/v2/saved-views family for MCP tool
// tests, mirroring the live contract (probed 2026-07-31): list wrapped in
// {views}, POST returns bare object, PUT takes {savedView:{…}}, DELETE → {}.
func startSavedViewsV2Server(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	view := map[string]any{
		"id": "aaa111aaa111aaa111aaa111", "adminId": "adm999", "name": "My Devices",
		"source": "devices", "columns": []any{"hostname"},
		"configuration": map[string]any{"filters": []any{}, "sort": "hostname"},
		"shared":        false, "isDefault": false,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
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

func TestMCPSavedViewsList(t *testing.T) {
	srv := startSavedViewsV2Server(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "saved_views_list", map[string]any{}))
	var wrapper struct {
		Data  []json.RawMessage `json:"data"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &wrapper); err != nil {
		t.Fatalf("result not JSON: %v\n%s", err, out)
	}
	if len(wrapper.Data) != 1 {
		t.Errorf("expected 1 view, got %d", len(wrapper.Data))
	}
}

func TestMCPSavedViewsCreate_ExecuteBuildsBody(t *testing.T) {
	var body map[string]any
	srv := startSavedViewsV2Server(t, &body)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	callTool(t, cs, "saved_views_create", map[string]any{
		"name": "My Devices", "source": "devices", "columns": "hostname, os", "execute": true,
	})
	if body["name"] != "My Devices" || body["source"] != "devices" {
		t.Errorf("create body wrong: %v", body)
	}
	cols, _ := body["columns"].([]any)
	if len(cols) != 2 {
		t.Errorf("columns not split: %v", body["columns"])
	}
}

// TestMCPSavedViewsCreate_PlanNoPost: without execute, no POST fires.
func TestMCPSavedViewsCreate_PlanNoPost(t *testing.T) {
	var body map[string]any
	srv := startSavedViewsV2Server(t, &body)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "saved_views_create", map[string]any{
		"name": "X", "source": "users",
	}))
	if body != nil {
		t.Errorf("plan must not POST, captured body: %v", body)
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected a plan result, got: %s", out)
	}
}

func TestMCPSavedViewsCreate_RequiresSource(t *testing.T) {
	srv := startSavedViewsV2Server(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	// source is a required schema property, so present-but-empty reaches the
	// handler's own validation (mirrors the CLI's --source "" path).
	out := getResultText(t, callTool(t, cs, "saved_views_create", map[string]any{
		"name": "X", "source": "", "execute": true,
	}))
	if !strings.Contains(out, "source is required") {
		t.Errorf("expected source-required error, got: %s", out)
	}
}

// TestMCPSavedViewsUpdate_RMWWraps: a partial update wraps in {savedView},
// preserves untouched fields, and strips adminId.
func TestMCPSavedViewsUpdate_RMWWraps(t *testing.T) {
	var put map[string]any
	srv := startSavedViewsV2Server(t, &put)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	callTool(t, cs, "saved_views_update", map[string]any{
		"identifier": "aaa111aaa111aaa111aaa111", "name": "Renamed", "execute": true,
	})
	sv, ok := put["savedView"].(map[string]any)
	if !ok {
		t.Fatalf("PUT body not wrapped in savedView: %v", put)
	}
	if sv["name"] != "Renamed" || sv["source"] != "devices" {
		t.Errorf("RMW wrong: %v", sv)
	}
	if _, ok := sv["adminId"]; ok {
		t.Error("adminId must be stripped")
	}
}

func TestMCPSavedViewsUpdate_PlanNoPut(t *testing.T) {
	var put map[string]any
	srv := startSavedViewsV2Server(t, &put)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "saved_views_update", map[string]any{
		"identifier": "aaa111aaa111aaa111aaa111", "name": "Renamed",
	}))
	if _, ok := put["savedView"]; ok {
		t.Errorf("plan must not PUT, captured: %v", put)
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan result, got: %s", out)
	}
}

func TestMCPSavedViewsDelete_PlanAndExecute(t *testing.T) {
	var cap = map[string]any{}
	srv := startSavedViewsV2Server(t, &cap)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "saved_views_delete", map[string]any{
		"identifier": "aaa111aaa111aaa111aaa111",
	}))
	if _, deleted := cap["deletedPath"]; deleted {
		t.Error("plan must not DELETE")
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan result, got: %s", out)
	}

	callTool(t, cs, "saved_views_delete", map[string]any{
		"identifier": "aaa111aaa111aaa111aaa111", "execute": true,
	})
	if cap["deletedPath"] != "/saved-views/aaa111aaa111aaa111aaa111" {
		t.Errorf("delete hit wrong path: %v", cap["deletedPath"])
	}
}
