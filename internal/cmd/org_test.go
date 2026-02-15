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

// startOrgServer creates a mock JumpCloud server that handles /organizations endpoints.
func startOrgServer(t *testing.T, orgs []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /organizations — list endpoint.
		if r.URL.Path == "/organizations" && r.Method == http.MethodGet {
			resp := map[string]any{
				"results":    orgs,
				"totalCount": len(orgs),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// GET /organizations/{id} — get endpoint.
		if strings.HasPrefix(r.URL.Path, "/organizations/") && r.Method == http.MethodGet {
			id := strings.TrimPrefix(r.URL.Path, "/organizations/")
			for _, o := range orgs {
				if o["_id"] == id {
					json.NewEncoder(w).Encode(o)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}

		// PUT /organizations/{id} — update endpoint.
		if strings.HasPrefix(r.URL.Path, "/organizations/") && r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			w.Write(body)
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleOrgs() []map[string]any {
	return []map[string]any{
		{
			"_id":         "aabbccddee112233aabb8001",
			"id":          "aabbccddee112233aabb8001",
			"displayName": "Klaassen Consulting",
			"created":     "2023-01-01T00:00:00Z",
			"logoUrl":     "",
		},
	}
}

func TestOrgList_JSON(t *testing.T) {
	setupUsersTest(t)
	orgs := sampleOrgs()
	ts := startOrgServer(t, orgs)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"org", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d orgs, want 1", len(result))
	}

	// Default fields should include _id, displayName, created.
	org := result[0]
	if org["_id"] != "aabbccddee112233aabb8001" {
		t.Errorf("_id = %q, want 'aabbccddee112233aabb8001'", org["_id"])
	}
	if org["displayName"] != "Klaassen Consulting" {
		t.Errorf("displayName = %q, want 'Klaassen Consulting'", org["displayName"])
	}
	if org["created"] != "2023-01-01T00:00:00Z" {
		t.Errorf("created = %q, want '2023-01-01T00:00:00Z'", org["created"])
	}
}

func TestOrgGet(t *testing.T) {
	setupUsersTest(t)
	orgs := sampleOrgs()
	ts := startOrgServer(t, orgs)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"org", "get", "aabbccddee112233aabb8001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["displayName"] != "Klaassen Consulting" {
		t.Errorf("displayName = %q, want 'Klaassen Consulting'", result["displayName"])
	}
	if result["_id"] != "aabbccddee112233aabb8001" {
		t.Errorf("_id = %q, want 'aabbccddee112233aabb8001'", result["_id"])
	}
}

func TestOrgGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	orgs := sampleOrgs()
	ts := startOrgServer(t, orgs)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"org", "get", "000000000000000000000000"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found org, got nil")
	}
}

func TestOrgSettings(t *testing.T) {
	setupUsersTest(t)
	orgs := []map[string]any{
		{"_id": "aabb0011223344556677aa01", "displayName": "My Org", "settings": map[string]any{"passwordPolicy": map[string]any{"minLength": 8}}},
	}
	ts := startOrgServer(t, orgs)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"org", "settings", "aabb0011223344556677aa01"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "My Org") {
		t.Errorf("expected org data in output")
	}
}

func TestOrgUpdate(t *testing.T) {
	setupUsersTest(t)
	orgs := []map[string]any{
		{"_id": "aabb0011223344556677aa01", "displayName": "My Org"},
	}
	ts := startOrgServer(t, orgs)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"org", "update", "aabb0011223344556677aa01", "--name", "New Name"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOrgUpdatePlan(t *testing.T) {
	setupUsersTest(t)
	viper.Set("plan", true)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"org", "update", "aabb0011223344556677aa01", "--name", "New Name"})
	err := cmd.Execute()
	// Plan mode returns ExitError with code 10.
	if err == nil {
		t.Fatal("expected plan exit error")
	}
}
