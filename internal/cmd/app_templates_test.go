package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func sampleAppTemplates() []map[string]any {
	return []map[string]any{
		{"_id": "aaa111aaa111aaa111aaa111", "name": "aws", "displayName": "AWS", "displayLabel": "Amazon Web Services", "active": true},
		{"_id": "bbb222bbb222bbb222bbb222", "name": "slack", "displayName": "Slack", "displayLabel": "Slack", "active": true},
		{"_id": "ccc333ccc333ccc333ccc333", "name": "salesforce", "displayName": "Salesforce", "displayLabel": "Salesforce", "active": false},
	}
}

// startAppTemplatesServer creates a mock JumpCloud server that handles
// GET /application-templates (list) and GET /application-templates/{id} (get).
func startAppTemplatesServer(t *testing.T, templates []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /application-templates — list endpoint with skip/limit pagination.
		if r.URL.Path == "/application-templates" && r.Method == http.MethodGet {
			skip := 0
			limit := 0
			if v := r.URL.Query().Get("skip"); v != "" {
				skip, _ = strconv.Atoi(v)
			}
			if v := r.URL.Query().Get("limit"); v != "" {
				limit, _ = strconv.Atoi(v)
			}

			total := len(templates)
			end := total
			if limit > 0 && skip+limit < total {
				end = skip + limit
			}
			var page []map[string]any
			if skip < total {
				page = templates[skip:end]
			}
			if page == nil {
				page = []map[string]any{}
			}

			resp := map[string]any{
				"results":    page,
				"totalCount": total,
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// GET /application-templates/{id} — get single template.
		if strings.HasPrefix(r.URL.Path, "/application-templates/") && r.Method == http.MethodGet {
			id := strings.TrimPrefix(r.URL.Path, "/application-templates/")
			for _, tmpl := range templates {
				if tmpl["_id"] == id {
					json.NewEncoder(w).Encode(tmpl)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func TestAppTemplatesList_JSON(t *testing.T) {
	setupUsersTest(t)
	templates := sampleAppTemplates()
	ts := startAppTemplatesServer(t, templates)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"app-templates", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d templates, want 3", len(result))
	}
}

func TestAppTemplatesList_Limit(t *testing.T) {
	setupUsersTest(t)
	templates := sampleAppTemplates()
	ts := startAppTemplatesServer(t, templates)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"app-templates", "list", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d templates, want 1", len(result))
	}
}

func TestAppTemplatesGet_ByID(t *testing.T) {
	setupUsersTest(t)
	templates := sampleAppTemplates()
	ts := startAppTemplatesServer(t, templates)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"app-templates", "get", "aaa111aaa111aaa111aaa111"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "aws" {
		t.Errorf("name = %q, want 'aws'", result["name"])
	}
	if result["displayLabel"] != "Amazon Web Services" {
		t.Errorf("displayLabel = %q, want 'Amazon Web Services'", result["displayLabel"])
	}
}

func TestAppTemplatesGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	templates := sampleAppTemplates()
	ts := startAppTemplatesServer(t, templates)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"app-templates", "get", "000000000000000000000000"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found template, got nil")
	}
}

func TestAppTemplatesHelp_Subcommands(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"app-templates", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, sub := range []string{"list", "get"} {
		if !strings.Contains(out, sub) {
			t.Errorf("help should contain subcommand %q, got:\n%s", sub, out)
		}
	}
}
