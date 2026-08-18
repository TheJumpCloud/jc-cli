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

// startSearchServer mirrors the v1 POST /search/<resource> contract (probed
// 2026-08-18): body {searchFilter, filter, skip, limit, sort} → {results,
// totalCount}. Captures the last request body when capture is non-nil.
func startSearchServer(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	rows := map[string]map[string]any{
		"/search/systems":        {"_id": "sys1", "displayName": "web-01", "hostname": "web-01", "os": "Ubuntu", "active": true},
		"/search/systemusers":    {"_id": "usr1", "username": "jdoe", "email": "j@x.io", "activated": true},
		"/search/commands":       {"_id": "cmd1", "name": "restart", "commandType": "linux"},
		"/search/commandresults": {"_id": "cr1", "name": "restart", "system": "sys1", "user": "usr1"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		row, ok := rows[r.URL.Path]
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

func runSearchCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	viper.Set("cache.enabled", false)
	root := NewRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"search"}, args...))
	err := root.Execute()
	return out.String(), errBuf.String(), err
}

func TestSearch_AllResources(t *testing.T) {
	setupUsersTest(t)
	overrideV1Client(t, startSearchServer(t, nil).URL)
	cases := map[string]string{
		"systems":         "web-01",
		"users":           "jdoe",
		"commands":        "restart",
		"command-results": "restart",
	}
	for sub, want := range cases {
		out, errBuf, err := runSearchCmd(t, sub, "term")
		if err != nil {
			t.Fatalf("%s: %v", sub, err)
		}
		var rows []map[string]any
		if e := json.Unmarshal([]byte(out), &rows); e != nil || len(rows) != 1 {
			t.Fatalf("%s: envelope not unwrapped: %v\n%s", sub, e, out)
		}
		if !strings.Contains(out, want) {
			t.Errorf("%s: missing %q in %s", sub, want, out)
		}
		if !strings.Contains(errBuf, "1 items") {
			t.Errorf("%s: footer missing: %s", sub, errBuf)
		}
	}
}

func TestSearch_Alias(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV1Client(t, startSearchServer(t, &body).URL)
	// "systemusers" alias hits the users resource → /search/systemusers.
	out, _, err := runSearchCmd(t, "systemusers", "jdoe")
	if err != nil {
		t.Fatalf("alias: %v", err)
	}
	if !strings.Contains(out, "jdoe") {
		t.Errorf("alias did not search users: %s", out)
	}
}

func TestSearch_TermBuildsSearchFilter(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV1Client(t, startSearchServer(t, &body).URL)
	if _, _, err := runSearchCmd(t, "users", "john"); err != nil {
		t.Fatalf("search: %v", err)
	}
	sf, ok := body["searchFilter"].(map[string]any)
	if !ok || sf["searchTerm"] != "john" {
		t.Fatalf("searchFilter.searchTerm wrong: %v", body["searchFilter"])
	}
	// Default user search fields applied.
	fields := sf["fields"].([]any)
	if len(fields) == 0 || fields[0] != "username" {
		t.Errorf("default search fields not applied: %v", fields)
	}
}

func TestSearch_MatchAllOmitsSearchFilter(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV1Client(t, startSearchServer(t, &body).URL)
	// No term, just a filter → no searchFilter, filter present.
	if _, _, err := runSearchCmd(t, "commands", "--filter", "commandType=linux"); err != nil {
		t.Fatalf("search: %v", err)
	}
	if _, ok := body["searchFilter"]; ok {
		t.Error("match-all (no term) must omit searchFilter")
	}
	if _, ok := body["filter"]; !ok {
		t.Errorf("filter should be present: %v", body)
	}
}

func TestSearch_SearchFieldOverride(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV1Client(t, startSearchServer(t, &body).URL)
	if _, _, err := runSearchCmd(t, "systems", "web", "--search-field", "hostname"); err != nil {
		t.Fatalf("search: %v", err)
	}
	sf := body["searchFilter"].(map[string]any)
	fields := sf["fields"].([]any)
	if len(fields) != 1 || fields[0] != "hostname" {
		t.Errorf("--search-field override not applied: %v", fields)
	}
}

func TestSearch_IDsOnly(t *testing.T) {
	setupUsersTest(t)
	overrideV1Client(t, startSearchServer(t, nil).URL)
	out, _, err := runSearchCmd(t, "systems", "web", "--ids")
	if err != nil {
		t.Fatalf("--ids: %v", err)
	}
	if strings.TrimSpace(out) != "sys1" {
		t.Errorf("--ids should emit _id, got %q", out)
	}
}
