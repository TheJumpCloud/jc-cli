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
	keyring "github.com/zalando/go-keyring"
)

func setupPolicyGroupsTest(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("JC_CONFIG", dir)
	viper.Reset()
	viper.Set("api_key", "test-key")
	viper.Set("cache.directory", dir)
	return dir
}

func startPolicyGroupsServer(t *testing.T, groups []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/v2")
		switch {
		case r.Method == http.MethodGet && path == "/policygroups":
			json.NewEncoder(w).Encode(groups)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/policygroups/"):
			id := strings.TrimPrefix(path, "/policygroups/")
			for _, g := range groups {
				if g["id"] == id {
					json.NewEncoder(w).Encode(g)
					return
				}
			}
			http.NotFound(w, r)
		case r.Method == http.MethodPost && path == "/policygroups":
			body, _ := io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			w.Write(body)
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/policygroups/"):
			body, _ := io.ReadAll(r.Body)
			w.Write(body)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/policygroups/"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestPolicyGroupsList(t *testing.T) {
	setupPolicyGroupsTest(t)
	groups := []map[string]any{
		{"id": "aabb0011223344556677aa01", "name": "security-policies", "description": "Security policy group"},
	}
	server := startPolicyGroupsServer(t, groups)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy-groups", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "security-policies") {
		t.Errorf("expected security-policies in output")
	}
}

func TestPolicyGroupsGet(t *testing.T) {
	setupPolicyGroupsTest(t)
	groups := []map[string]any{
		{"id": "aabb0011223344556677aa01", "name": "security-policies"},
	}
	server := startPolicyGroupsServer(t, groups)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy-groups", "get", "aabb0011223344556677aa01"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPolicyGroupsCreate(t *testing.T) {
	setupPolicyGroupsTest(t)
	server := startPolicyGroupsServer(t, nil)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy-groups", "create", "--name", "new-group"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPolicyGroupsDelete(t *testing.T) {
	setupPolicyGroupsTest(t)
	groups := []map[string]any{
		{"id": "aabb0011223344556677aa01", "name": "old-group"},
	}
	server := startPolicyGroupsServer(t, groups)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy-groups", "delete", "aabb0011223344556677aa01", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPolicyGroupsHelp(t *testing.T) {
	setupPolicyGroupsTest(t)
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy-groups", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
