package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
	keyring "github.com/zalando/go-keyring"
)

func setupPolicyTemplatesTest(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("JC_CONFIG", dir)
	viper.Reset()
	viper.Set("api_key", "test-key")
	viper.Set("cache.directory", dir)
	return dir
}

func startPolicyTemplatesServer(t *testing.T, templates []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/v2")
		switch {
		case r.Method == http.MethodGet && path == "/policytemplates":
			json.NewEncoder(w).Encode(templates)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/policytemplates/"):
			id := strings.TrimPrefix(path, "/policytemplates/")
			for _, tmpl := range templates {
				if tmpl["id"] == id {
					json.NewEncoder(w).Encode(tmpl)
					return
				}
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestPolicyTemplatesList(t *testing.T) {
	setupPolicyTemplatesTest(t)
	templates := []map[string]any{
		{"id": "aabb0011223344556677aa01", "name": "FileVault", "description": "FileVault encryption", "osMetaFamily": "darwin"},
	}
	server := startPolicyTemplatesServer(t, templates)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy-templates", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "FileVault") {
		t.Errorf("expected FileVault in output, got: %s", buf.String())
	}
}

func TestPolicyTemplatesGet(t *testing.T) {
	setupPolicyTemplatesTest(t)
	templates := []map[string]any{
		{"id": "aabb0011223344556677aa01", "name": "FileVault"},
	}
	server := startPolicyTemplatesServer(t, templates)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy-templates", "get", "aabb0011223344556677aa01"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPolicyTemplatesHelp(t *testing.T) {
	setupPolicyTemplatesTest(t)
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy-templates", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
