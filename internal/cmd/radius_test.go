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

func setupRADIUSTest(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("JC_CONFIG", dir)
	viper.Reset()
	viper.Set("api_key", "test-key")
	viper.Set("cache.directory", dir)
	return dir
}

func startRADIUSServer(t *testing.T, servers []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		path := strings.TrimPrefix(r.URL.Path, "/api")

		switch {
		case r.Method == http.MethodGet && path == "/radiusservers":
			resp := map[string]any{"results": servers, "totalCount": len(servers)}
			json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodGet && strings.HasPrefix(path, "/radiusservers/"):
			id := strings.TrimPrefix(path, "/radiusservers/")
			for _, s := range servers {
				if s["_id"] == id {
					json.NewEncoder(w).Encode(s)
					return
				}
			}
			http.NotFound(w, r)

		case r.Method == http.MethodPost && path == "/radiusservers":
			body, _ := io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			w.Write(body)

		case r.Method == http.MethodPut && strings.HasPrefix(path, "/radiusservers/"):
			body, _ := io.ReadAll(r.Body)
			w.Write(body)

		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/radiusservers/"):
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRADIUSList(t *testing.T) {
	setupRADIUSTest(t)
	servers := []map[string]any{
		{"_id": "aabb0011223344556677aa01", "name": "radius-1", "networkSourceIp": "10.0.0.1", "authPort": 1812, "accountingPort": 1813},
	}
	server := startRADIUSServer(t, servers)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"radius", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "radius-1") {
		t.Errorf("expected radius-1 in output, got: %s", buf.String())
	}
}

func TestRADIUSGet(t *testing.T) {
	setupRADIUSTest(t)
	servers := []map[string]any{
		{"_id": "aabb0011223344556677aa01", "name": "radius-1"},
	}
	server := startRADIUSServer(t, servers)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"radius", "get", "aabb0011223344556677aa01"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "radius-1") {
		t.Errorf("expected radius-1 in output")
	}
}

func TestRADIUSCreate(t *testing.T) {
	setupRADIUSTest(t)
	server := startRADIUSServer(t, nil)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"radius", "create", "--name", "new-radius", "--shared-secret", "s3cret"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRADIUSDelete(t *testing.T) {
	setupRADIUSTest(t)
	servers := []map[string]any{
		{"_id": "aabb0011223344556677aa01", "name": "radius-1"},
	}
	server := startRADIUSServer(t, servers)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"radius", "delete", "aabb0011223344556677aa01", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRADIUSHelp(t *testing.T) {
	setupRADIUSTest(t)
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"radius", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
