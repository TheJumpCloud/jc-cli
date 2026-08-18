package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func startSearchV2ServerV1(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	rows := map[string]map[string]any{
		"/search/systems":        {"_id": "sys1", "displayName": "web-01"},
		"/search/systemusers":    {"_id": "usr1", "username": "jdoe"},
		"/search/commands":       {"_id": "cmd1", "name": "restart"},
		"/search/commandresults": {"_id": "cr1", "name": "restart"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		row, ok := rows[strings.TrimPrefix(r.URL.Path, "/api")]
		if !ok || r.Method != http.MethodPost {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if capture != nil {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, capture)
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{row}, "totalCount": 1})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMCPSearch_AllResources(t *testing.T) {
	overrideV1ClientForTest(t, startSearchV2ServerV1(t, nil).URL)
	cs := connectToolTestServer(t, Options{})
	for _, tool := range []string{"search_systems", "search_users", "search_commands", "search_command_results"} {
		out := getResultText(t, callTool(t, cs, tool, map[string]any{"term": "x"}))
		var wrapper struct {
			Data []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &wrapper); err != nil || len(wrapper.Data) != 1 {
			t.Fatalf("%s: envelope not unwrapped: %v\n%s", tool, err, out)
		}
	}
}

func TestMCPSearch_BodyAndFilter(t *testing.T) {
	var body map[string]any
	overrideV1ClientForTest(t, startSearchV2ServerV1(t, &body).URL)
	cs := connectToolTestServer(t, Options{})

	// term builds searchFilter with default fields.
	callTool(t, cs, "search_users", map[string]any{"term": "john"})
	sf, ok := body["searchFilter"].(map[string]any)
	if !ok || sf["searchTerm"] != "john" {
		t.Fatalf("searchFilter wrong: %v", body["searchFilter"])
	}

	// filter-only, no term → no searchFilter.
	body = nil
	callTool(t, cs, "search_commands", map[string]any{"filter": []any{"commandType=linux"}})
	if _, ok := body["searchFilter"]; ok {
		t.Error("filter-only search must omit searchFilter")
	}
	if _, ok := body["filter"]; !ok {
		t.Errorf("filter missing: %v", body)
	}
}
