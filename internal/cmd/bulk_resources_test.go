package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bulkSpecByUse pulls one spec from the registry.
func bulkSpecByUse(t *testing.T, use string) bulkResourceSpec {
	t.Helper()
	for _, s := range bulkResourceSpecs() {
		if s.Use == use {
			return s
		}
	}
	t.Fatalf("no bulk spec %q", use)
	return bulkResourceSpec{}
}

// TestBulkSpecs_Coverage pins each resource's CSV surface — the
// hand-curated maps are the correctness core (the KLA-466 recon
// showed schema-derived maps emit wrong keys), so drift must be
// deliberate.
func TestBulkSpecs_Coverage(t *testing.T) {
	want := map[string]map[string]string{ // use → header → APIKey
		"user-groups":   {"name": "name", "description": "description"},
		"device-groups": {"name": "name", "description": "description"},
		"devices": {
			"displayname":                    "displayName",
			"allowsshpasswordauthentication": "allowSshPasswordAuthentication",
			"allowmultifactorauthentication": "allowMultiFactorAuthentication",
			"allowpublickeyauthentication":   "allowPublicKeyAuthentication",
		},
		"admins": {
			"email":      "email",
			"role":       "roleName", // THE alias — schema says role, API wants roleName
			"enable-mfa": "enableMultiFactor",
			"firstname":  "firstname",
			"lastname":   "lastname",
		},
	}
	specs := bulkResourceSpecs()
	if len(specs) != len(want) {
		t.Fatalf("registry has %d specs, want %d", len(specs), len(want))
	}
	for _, spec := range specs {
		expected, ok := want[spec.Use]
		if !ok {
			t.Errorf("unexpected spec %q", spec.Use)
			continue
		}
		if len(spec.Fields) != len(expected) {
			t.Errorf("%s: %d fields, want %d", spec.Use, len(spec.Fields), len(expected))
		}
		for header, apiKey := range expected {
			fs, ok := spec.Fields[header]
			if !ok {
				t.Errorf("%s: missing column %q", spec.Use, header)
				continue
			}
			if fs.APIKey != apiKey {
				t.Errorf("%s: column %q maps to %q, want %q", spec.Use, header, fs.APIKey, apiKey)
			}
		}
	}
	if bulkSpecByUse(t, "devices").AllowCreate {
		t.Error("devices must not allow create (enrolled, not created)")
	}
}

func writeBulkCSV(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "batch.csv")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runBulk(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"bulk"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

// startBulkV2Server stubs the V2 group endpoints, capturing bodies.
func startBulkV2Server(t *testing.T, captured *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/usergroups":
			var body bytes.Buffer
			_, _ = body.ReadFrom(r.Body)
			*captured = append(*captured, "POST "+body.String())
			_, _ = w.Write([]byte(`{"id":"g1","name":"created"}`))
		case r.Method == "GET" && r.URL.Path == "/usergroups":
			// Resolver list call: one existing group.
			_, _ = w.Write([]byte(`[{"id":"g0","name":"Existing Group"}]`))
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/usergroups/"):
			*captured = append(*captured, "DELETE "+strings.TrimPrefix(r.URL.Path, "/usergroups/"))
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBulkResource_ValidationErrors(t *testing.T) {
	setupUsersTest(t)

	// Unknown column → refused with the valid list.
	file := writeBulkCSV(t, "name,descripton\nA,typo\n")
	_, _, err := runBulk(t, "user-groups", "--file", file)
	if err == nil || !strings.Contains(err.Error(), `unknown CSV column "descripton"`) {
		t.Errorf("typo column should be refused: %v", err)
	}

	// Create on a no-create resource.
	file = writeBulkCSV(t, "hostname,displayname\nweb-01,Web 01\n")
	_, _, err = runBulk(t, "devices", "--file", file)
	if err == nil || !strings.Contains(err.Error(), "cannot be created") {
		t.Errorf("devices create should be refused: %v", err)
	}

	// Aggregated row problems: bad op, bad bool, missing required —
	// all reported in one pass with line numbers.
	file = writeBulkCSV(t, "name,operation,description\nA,explode,x\n,create,y\n")
	_, _, err = runBulk(t, "user-groups", "--file", file)
	if err == nil {
		t.Fatal("expected aggregated validation error")
	}
	for _, want := range []string{"line 2", `unknown operation "explode"`, "line 3", `requires the "name" column`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("aggregated error missing %q:\n%v", want, err)
		}
	}
}

func TestBulkResource_PlanForceAndExecution(t *testing.T) {
	setupUsersTest(t)
	var captured []string
	srv := startBulkV2Server(t, &captured)
	overrideV2Client(t, srv.URL)

	file := writeBulkCSV(t, "name,description,operation\nNew Group,made by test,create\nExisting Group,,delete\n")

	// --plan previews per row with line numbers, no calls.
	out, _, err := runBulk(t, "user-groups", "--file", file, "--plan")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !strings.Contains(out, "line 2: create New Group") || !strings.Contains(out, "line 3: delete Existing Group") {
		t.Errorf("plan preview wrong:\n%s", out)
	}
	if len(captured) != 0 {
		t.Fatalf("plan must not call the API: %v", captured)
	}

	// Without --force → refused before any call.
	_, _, err = runBulk(t, "user-groups", "--file", file)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected force gate: %v", err)
	}
	if len(captured) != 0 {
		t.Fatalf("force refusal must precede API calls: %v", captured)
	}

	// With --force: create POSTs the mapped body; delete resolves by
	// name then DELETEs the resolved ID.
	stdout, _, err := runBulk(t, "user-groups", "--file", file, "--force")
	if err != nil {
		t.Fatalf("execution failed: %v\n%s", err, stdout)
	}
	if len(captured) != 2 ||
		!strings.Contains(captured[0], `"name":"New Group"`) ||
		!strings.Contains(captured[0], `"description":"made by test"`) ||
		captured[1] != "DELETE g0" {
		t.Errorf("API calls wrong: %v", captured)
	}
	var results []bulkRowResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatalf("results not JSON: %v\n%s", err, stdout)
	}
	if len(results) != 2 || results[0].Status != "ok" || results[1].Status != "ok" {
		t.Errorf("results wrong: %+v", results)
	}
}

func TestBulkResource_AdminsAliasAndBoolCoercion(t *testing.T) {
	setupUsersTest(t)
	var captured []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/users" {
			var body bytes.Buffer
			_, _ = body.ReadFrom(r.Body)
			captured = append(captured, body.String())
			_, _ = w.Write([]byte(`{"_id":"a1"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	overrideV1Client(t, srv.URL)

	file := writeBulkCSV(t, "email,role,enable-mfa\nnew@acme.com,Administrator,true\n")
	_, _, err := runBulk(t, "admins", "--file", file, "--force")
	if err != nil {
		t.Fatalf("admins create failed: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("expected 1 POST, got %d", len(captured))
	}
	// The alias and coercion on the wire: roleName (NOT role), real bool.
	if !strings.Contains(captured[0], `"roleName":"Administrator"`) {
		t.Errorf("role column must map to roleName: %s", captured[0])
	}
	if strings.Contains(captured[0], `"role":`) {
		t.Errorf("raw 'role' key must not reach the API: %s", captured[0])
	}
	if !strings.Contains(captured[0], `"enableMultiFactor":true`) {
		t.Errorf("bool coercion wrong: %s", captured[0])
	}

	// Bad bool aggregates with a line number.
	file = writeBulkCSV(t, "email,enable-mfa\nx@acme.com,maybe\n")
	_, _, err = runBulk(t, "admins", "--file", file, "--force")
	if err == nil || !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "not a boolean") {
		t.Errorf("bool validation wrong: %v", err)
	}
}

func TestBulkResource_PartialFailureIsolation(t *testing.T) {
	setupUsersTest(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/usergroups" {
			calls++
			if calls == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"already exists"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"g2"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	overrideV2Client(t, srv.URL)

	file := writeBulkCSV(t, "name\nDupe Group\nFresh Group\n")
	stdout, _, err := runBulk(t, "user-groups", "--file", file, "--force")
	if err == nil || !strings.Contains(err.Error(), "1 of 2 user-groups operations failed") {
		t.Fatalf("expected partial failure, got %v", err)
	}
	var results []bulkRowResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatal(err)
	}
	if results[0].Status != "failed" || results[0].Row != 2 || results[1].Status != "ok" {
		t.Errorf("failure isolation wrong: %+v", results)
	}
}
