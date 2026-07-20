package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// directoriesFixture mirrors the live /directories shape (2026-07-16
// recon): mixed types, one healthy OAuth integration, one broken, and
// non-OAuth types with no oAuthStatus at all.
const directoriesFixture = `[
	{"id":"d1","type":"g_suite","name":"Workspace","organizationId":"o1"},
	{"id":"d2","type":"office_365","name":"Broken O365",
	 "oAuthStatus":{"expiry":{"seconds":"1703732260"},"error":"invalid_grant","errorMessage":"AADSTS9002313: Invalid request. Request is malformed or invalid. Trace ID: x Correlation ID: y Timestamp: z"}},
	{"id":"d3","type":"office_365","name":"Healthy O365",
	 "oAuthStatus":{"expiry":{"seconds":"1913732260"}}},
	{"id":"d4","type":"ldap_server","name":"JumpCloud LDAP"},
	{"id":"d5","type":"active_directory","name":"DC=corp"}
]`

func TestDirectoriesList_HealthDerivation(t *testing.T) {
	setupUsersTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/directories" {
			_, _ = w.Write([]byte(directoriesFixture))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	overrideV2Client(t, srv.URL)

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"directories", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}

	var rows []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, stdout.String())
	}
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}
	health := map[string]string{}
	for _, r := range rows {
		health[r["id"].(string)] = r["health"].(string)
	}
	// No oAuthStatus at all → ok (LDAP/AD/g_suite).
	for _, id := range []string{"d1", "d4", "d5"} {
		if health[id] != "ok" {
			t.Errorf("%s health = %q, want ok", id, health[id])
		}
	}
	// oAuthStatus without error → ok.
	if health["d3"] != "ok" {
		t.Errorf("d3 health = %q, want ok", health["d3"])
	}
	// Broken grant: code + truncated message, never the raw nested blob.
	if !strings.HasPrefix(health["d2"], "error: invalid_grant — AADSTS9002313") {
		t.Errorf("d2 health = %q", health["d2"])
	}
	if len(health["d2"]) > 120 {
		t.Errorf("health message not truncated: %d chars", len(health["d2"]))
	}
	if !strings.Contains(stderr.String(), "── 5 items ──") {
		t.Errorf("footer missing: %s", stderr.String())
	}
}

// TestFlattenDirectoryHealth_NullElement guards the nil-map panic: a
// JSON-null array element unmarshals to a nil map, and assigning
// health to it would panic. It must pass through untouched.
func TestFlattenDirectoryHealth_NullElement(t *testing.T) {
	out, err := flattenDirectoryHealth(json.RawMessage(`null`))
	if err != nil {
		t.Fatalf("null element: %v", err)
	}
	if strings.TrimSpace(string(out)) != "null" {
		t.Errorf("null element rewritten to %q, want null", string(out))
	}
}
