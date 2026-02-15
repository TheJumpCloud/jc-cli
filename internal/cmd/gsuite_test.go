package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startGsuiteServer creates a mock JumpCloud V2 server that handles /gsuites endpoints.
func startGsuiteServer(t *testing.T, gsuites []map[string]any) *httptest.Server {
	t.Helper()

	translationRules := []map[string]any{
		{"id": "aabbccddee112233aabb7001", "builtIn": true},
		{"id": "aabbccddee112233aabb7002", "builtIn": false},
	}

	importUsers := []map[string]any{
		{"email": "alice@acme.com", "firstname": "Alice", "lastname": "Smith", "status": "unlinked"},
		{"email": "bob@acme.com", "firstname": "Bob", "lastname": "Jones", "status": "unlinked"},
		{"email": "carol@acme.com", "firstname": "Carol", "lastname": "Lee", "status": "linked"},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /gsuites — list endpoint.
		if r.URL.Path == "/gsuites" && r.Method == http.MethodGet {
			// Check for filter query param for simple name filtering.
			filters := r.URL.Query()["filter"]
			if len(filters) > 0 {
				var filtered []map[string]any
				for _, gs := range gsuites {
					for _, f := range filters {
						// V2 filter format: name:eq:Value
						parts := strings.SplitN(f, ":", 3)
						if len(parts) == 3 && parts[0] == "name" && parts[1] == "eq" {
							if gs["name"] == parts[2] {
								filtered = append(filtered, gs)
							}
						}
					}
				}
				json.NewEncoder(w).Encode(filtered)
				return
			}
			json.NewEncoder(w).Encode(gsuites)
			return
		}

		// Routes under /gsuites/{id}...
		if strings.HasPrefix(r.URL.Path, "/gsuites/") {
			rest := strings.TrimPrefix(r.URL.Path, "/gsuites/")
			parts := strings.SplitN(rest, "/", 2)
			id := parts[0]

			var found map[string]any
			for _, gs := range gsuites {
				if gs["id"] == id {
					found = gs
					break
				}
			}

			if found == nil {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}

			// Sub-resource routes: /gsuites/{id}/translationrules and /gsuites/{id}/import/users
			if len(parts) == 2 {
				subPath := parts[1]
				switch {
				case subPath == "translationrules" && r.Method == http.MethodGet:
					json.NewEncoder(w).Encode(translationRules)
					return
				case subPath == "import/users" && r.Method == http.MethodGet:
					json.NewEncoder(w).Encode(importUsers)
					return
				}
			}

			// GET /gsuites/{id}
			if r.Method == http.MethodGet && len(parts) == 1 {
				json.NewEncoder(w).Encode(found)
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleGsuites() []map[string]any {
	return []map[string]any{
		{"id": "aabbccddee112233aabb6001", "name": "Acme Corp", "defaultDomain": "acme.com"},
		{"id": "aabbccddee112233aabb6002", "name": "Beta Inc", "defaultDomain": "beta.io"},
	}
}

// --- List Tests ---

func TestGsuiteList_JSON(t *testing.T) {
	setupUsersTest(t)
	gsuites := sampleGsuites()
	ts := startGsuiteServer(t, gsuites)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gsuite", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d G Suite instances, want 2", len(result))
	}
}

func TestGsuiteList_Limit(t *testing.T) {
	setupUsersTest(t)
	gsuites := sampleGsuites()
	ts := startGsuiteServer(t, gsuites)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gsuite", "list", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d G Suite instances, want 1", len(result))
	}
}

func TestGsuiteList_Filter(t *testing.T) {
	setupUsersTest(t)
	gsuites := sampleGsuites()
	ts := startGsuiteServer(t, gsuites)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gsuite", "list", "--filter", "name=Acme Corp"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d G Suite instances, want 1", len(result))
	}
	if result[0]["name"] != "Acme Corp" {
		t.Errorf("name = %q, want 'Acme Corp'", result[0]["name"])
	}
}

// --- Get Tests ---

func TestGsuiteGet(t *testing.T) {
	setupUsersTest(t)
	gsuites := sampleGsuites()
	ts := startGsuiteServer(t, gsuites)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gsuite", "get", "aabbccddee112233aabb6001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "Acme Corp" {
		t.Errorf("name = %q, want 'Acme Corp'", result["name"])
	}
}

func TestGsuiteGet_ByName(t *testing.T) {
	setupUsersTest(t)
	gsuites := sampleGsuites()
	ts := startGsuiteServer(t, gsuites)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gsuite", "get", "Acme Corp"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb6001" {
		t.Errorf("id = %q, want 'aabbccddee112233aabb6001'", result["id"])
	}
}

func TestGsuiteGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	gsuites := sampleGsuites()
	ts := startGsuiteServer(t, gsuites)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gsuite", "get", "aabbccddee112233aabb9999"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found G Suite instance, got nil")
	}
}

// --- Translation Rules Tests ---

func TestGsuiteTranslationRules(t *testing.T) {
	setupUsersTest(t)
	gsuites := sampleGsuites()
	ts := startGsuiteServer(t, gsuites)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gsuite", "translation-rules", "aabbccddee112233aabb6001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d translation rules, want 2", len(result))
	}
}

// --- Import Users Tests ---

func TestGsuiteImportUsers(t *testing.T) {
	setupUsersTest(t)
	gsuites := sampleGsuites()
	ts := startGsuiteServer(t, gsuites)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"gsuite", "import-users", "aabbccddee112233aabb6001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d importable users, want 3", len(result))
	}
}
