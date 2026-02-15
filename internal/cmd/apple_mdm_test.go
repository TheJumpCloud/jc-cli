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

func setupAppleMDMTest(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("JC_CONFIG", dir)
	viper.Reset()
	viper.Set("api_key", "test-key")
	viper.Set("cache.directory", dir)
	return dir
}

func startAppleMDMServer(t *testing.T, configs []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/applemdms":
			json.NewEncoder(w).Encode(configs)

		case r.Method == http.MethodGet && strings.HasSuffix(path, "/enrollmentprofiles"):
			profiles := []map[string]any{{"id": "ep01", "name": "Default Profile"}}
			json.NewEncoder(w).Encode(profiles)

		case r.Method == http.MethodGet && strings.HasSuffix(path, "/devices"):
			devices := []map[string]any{{"udid": "ABC123", "serialNumber": "C123"}}
			json.NewEncoder(w).Encode(devices)

		case r.Method == http.MethodGet && strings.HasPrefix(path, "/applemdms/"):
			id := strings.TrimPrefix(path, "/applemdms/")
			for _, c := range configs {
				if c["id"] == id {
					json.NewEncoder(w).Encode(c)
					return
				}
			}
			http.NotFound(w, r)

		case r.Method == http.MethodPost && path == "/applemdms":
			body, _ := io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			w.Write(body)

		case r.Method == http.MethodPut && strings.HasPrefix(path, "/applemdms/"):
			body, _ := io.ReadAll(r.Body)
			w.Write(body)

		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/applemdms/"):
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
}

func TestAppleMDMList(t *testing.T) {
	setupAppleMDMTest(t)
	configs := []map[string]any{
		{"id": "aabb0011223344556677aa01", "name": "MDM Config 1", "orgName": "TestOrg"},
	}
	server := startAppleMDMServer(t, configs)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"apple-mdm", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "MDM Config 1") {
		t.Errorf("expected MDM Config 1 in output, got: %s", buf.String())
	}
}

func TestAppleMDMGet(t *testing.T) {
	setupAppleMDMTest(t)
	configs := []map[string]any{
		{"id": "aabb0011223344556677aa01", "name": "MDM Config 1"},
	}
	server := startAppleMDMServer(t, configs)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"apple-mdm", "get", "aabb0011223344556677aa01"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppleMDMCreate(t *testing.T) {
	setupAppleMDMTest(t)
	server := startAppleMDMServer(t, nil)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"apple-mdm", "create", "--name", "New MDM"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppleMDMDelete(t *testing.T) {
	setupAppleMDMTest(t)
	configs := []map[string]any{
		{"id": "aabb0011223344556677aa01", "name": "MDM Config 1"},
	}
	server := startAppleMDMServer(t, configs)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"apple-mdm", "delete", "aabb0011223344556677aa01", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppleMDMEnrollmentProfiles(t *testing.T) {
	setupAppleMDMTest(t)
	configs := []map[string]any{
		{"id": "aabb0011223344556677aa01", "name": "MDM Config 1"},
	}
	server := startAppleMDMServer(t, configs)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"apple-mdm", "enrollment-profiles", "aabb0011223344556677aa01"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppleMDMDevices(t *testing.T) {
	setupAppleMDMTest(t)
	configs := []map[string]any{
		{"id": "aabb0011223344556677aa01", "name": "MDM Config 1"},
	}
	server := startAppleMDMServer(t, configs)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"apple-mdm", "devices", "aabb0011223344556677aa01"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppleMDMHelp(t *testing.T) {
	setupAppleMDMTest(t)
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"apple-mdm", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
