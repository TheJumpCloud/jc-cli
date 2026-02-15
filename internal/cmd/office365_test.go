package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startOffice365Server creates a mock JumpCloud V2 server that handles /office365s endpoints.
func startOffice365Server(t *testing.T, instances []map[string]any) *httptest.Server {
	t.Helper()

	translationRules := []map[string]any{
		{"id": "aabbccddee112233aabb7101", "builtIn": true},
		{"id": "aabbccddee112233aabb7102", "builtIn": false},
	}

	importUsers := []map[string]any{
		{"email": "alice@contoso.onmicrosoft.com", "firstname": "Alice", "lastname": "Smith", "status": "new"},
		{"email": "bob@contoso.onmicrosoft.com", "firstname": "Bob", "lastname": "Jones", "status": "new"},
		{"email": "carol@contoso.onmicrosoft.com", "firstname": "Carol", "lastname": "Lee", "status": "existing"},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /office365s — list endpoint.
		if r.URL.Path == "/office365s" && r.Method == http.MethodGet {
			// Check for filter query param for simple name filtering.
			filters := r.URL.Query()["filter"]
			if len(filters) > 0 {
				var filtered []map[string]any
				for _, inst := range instances {
					for _, f := range filters {
						// V2 filter format: name:eq:Value
						parts := strings.SplitN(f, ":", 3)
						if len(parts) == 3 && parts[0] == "name" && parts[1] == "eq" {
							if inst["name"] == parts[2] {
								filtered = append(filtered, inst)
							}
						}
					}
				}
				json.NewEncoder(w).Encode(filtered)
				return
			}
			json.NewEncoder(w).Encode(instances)
			return
		}

		// Routes under /office365s/{id}...
		if strings.HasPrefix(r.URL.Path, "/office365s/") {
			rest := strings.TrimPrefix(r.URL.Path, "/office365s/")

			// Check for sub-resource paths: {id}/translationrules, {id}/import/users
			parts := strings.SplitN(rest, "/", 2)
			id := parts[0]

			// Find the instance.
			var found map[string]any
			for _, inst := range instances {
				if inst["id"] == id {
					found = inst
					break
				}
			}

			if found == nil {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}

			// Sub-resource routes.
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
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}

			// GET /office365s/{id} — get by ID.
			if r.Method == http.MethodGet {
				json.NewEncoder(w).Encode(found)
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleOffice365s() []map[string]any {
	return []map[string]any{
		{"id": "aabbccddee112233aabb7001", "name": "Contoso", "defaultDomain": "contoso.onmicrosoft.com"},
		{"id": "aabbccddee112233aabb7002", "name": "Fabrikam", "defaultDomain": "fabrikam.onmicrosoft.com"},
	}
}

// --- List Tests ---

func TestOffice365List_JSON(t *testing.T) {
	setupUsersTest(t)
	instances := sampleOffice365s()
	ts := startOffice365Server(t, instances)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"office365", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d Office 365 instances, want 2", len(result))
	}
}

func TestOffice365List_Limit(t *testing.T) {
	setupUsersTest(t)
	instances := sampleOffice365s()
	ts := startOffice365Server(t, instances)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"office365", "list", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	// The V2 client applies limit after fetching; mock returns all items but client truncates.
	if len(result) != 1 {
		t.Errorf("got %d Office 365 instances, want 1", len(result))
	}
}

func TestOffice365List_Filter(t *testing.T) {
	setupUsersTest(t)
	instances := sampleOffice365s()
	ts := startOffice365Server(t, instances)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"office365", "list", "--filter", "name=Contoso"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d Office 365 instances, want 1", len(result))
	}
	if result[0]["name"] != "Contoso" {
		t.Errorf("name = %q, want 'Contoso'", result[0]["name"])
	}
}

// --- Get Tests ---

func TestOffice365Get(t *testing.T) {
	setupUsersTest(t)
	instances := sampleOffice365s()
	ts := startOffice365Server(t, instances)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"office365", "get", "aabbccddee112233aabb7001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "Contoso" {
		t.Errorf("name = %q, want 'Contoso'", result["name"])
	}
}

func TestOffice365Get_ByName(t *testing.T) {
	setupUsersTest(t)
	instances := sampleOffice365s()
	ts := startOffice365Server(t, instances)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"office365", "get", "Contoso"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb7001" {
		t.Errorf("id = %q, want 'aabbccddee112233aabb7001'", result["id"])
	}
}

func TestOffice365Get_NotFound(t *testing.T) {
	setupUsersTest(t)
	instances := sampleOffice365s()
	ts := startOffice365Server(t, instances)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"office365", "get", "aabbccddee112233aabb9999"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found Office 365 instance, got nil")
	}
}

// --- Translation Rules Tests ---

func TestOffice365TranslationRules(t *testing.T) {
	setupUsersTest(t)
	instances := sampleOffice365s()
	ts := startOffice365Server(t, instances)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"office365", "translation-rules", "aabbccddee112233aabb7001"})

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

func TestOffice365ImportUsers(t *testing.T) {
	setupUsersTest(t)
	instances := sampleOffice365s()
	ts := startOffice365Server(t, instances)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"office365", "import-users", "aabbccddee112233aabb7001"})

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
