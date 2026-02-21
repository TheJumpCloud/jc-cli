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

func setupUserStatesTest(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("JC_CONFIG", dir)
	viper.Reset()
	viper.Set("api_key", "test-key")
	viper.Set("cache.directory", dir)
	return dir
}

func startUserStatesServer(t *testing.T, states []map[string]any, users []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// V1 routes (user resolution)
		if r.Method == http.MethodGet && r.URL.Path == "/systemusers" {
			resp := map[string]any{"results": users, "totalCount": len(users)}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// V2 routes
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && path == "/bulk/userstates":
			json.NewEncoder(w).Encode(states)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/bulk/userstates/"):
			id := strings.TrimPrefix(path, "/bulk/userstates/")
			for _, s := range states {
				if s["id"] == id {
					json.NewEncoder(w).Encode(s)
					return
				}
			}
			http.NotFound(w, r)
		case r.Method == http.MethodPost && path == "/bulk/userstates":
			body, _ := io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			w.Write(body)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/bulk/userstates/"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestUserStatesList(t *testing.T) {
	setupUserStatesTest(t)
	states := []map[string]any{
		{"id": "aabb0011223344556677aa01", "userId": "aabb0011223344556677cc01", "state": "suspended", "startDate": "2026-03-01"},
	}
	server := startUserStatesServer(t, states, nil)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"user-states", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "suspended") {
		t.Errorf("expected suspended in output")
	}
}

func TestUserStatesList_Limit(t *testing.T) {
	setupUserStatesTest(t)
	states := []map[string]any{
		{"id": "aabb0011223344556677aa01", "userId": "aabb0011223344556677cc01", "state": "suspended", "startDate": "2026-03-01"},
		{"id": "aabb0011223344556677aa02", "userId": "aabb0011223344556677cc02", "state": "activated", "startDate": "2026-04-01"},
		{"id": "aabb0011223344556677aa03", "userId": "aabb0011223344556677cc03", "state": "suspended", "startDate": "2026-05-01"},
	}
	server := startUserStatesServer(t, states, nil)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"user-states", "list", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("got %d states, want 2 (limit)", len(result))
	}
}

func TestUserStatesGet(t *testing.T) {
	setupUserStatesTest(t)
	states := []map[string]any{
		{"id": "aabb0011223344556677aa01", "userId": "aabb0011223344556677cc01", "state": "suspended"},
	}
	server := startUserStatesServer(t, states, nil)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"user-states", "get", "aabb0011223344556677aa01"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUserStatesCreate(t *testing.T) {
	setupUserStatesTest(t)
	users := []map[string]any{
		{"_id": "aabb0011223344556677cc01", "username": "jdoe"},
	}
	server := startUserStatesServer(t, nil, users)
	defer server.Close()
	overrideV1Client(t, server.URL)
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"user-states", "create", "--user", "aabb0011223344556677cc01", "--state", "suspended", "--start-date", "2026-03-01"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUserStatesDelete(t *testing.T) {
	setupUserStatesTest(t)
	states := []map[string]any{
		{"id": "aabb0011223344556677aa01", "userId": "aabb0011223344556677cc01", "state": "suspended", "startDate": "2026-03-01"},
	}
	server := startUserStatesServer(t, states, nil)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"user-states", "delete", "aabb0011223344556677aa01", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUserStatesInvalidState(t *testing.T) {
	setupUserStatesTest(t)
	users := []map[string]any{
		{"_id": "aabb0011223344556677cc01", "username": "jdoe"},
	}
	server := startUserStatesServer(t, nil, users)
	defer server.Close()
	overrideV1Client(t, server.URL)
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"user-states", "create", "--user", "aabb0011223344556677cc01", "--state", "invalid", "--start-date", "2026-03-01"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid state")
	}
}

func TestUserStatesHelp(t *testing.T) {
	setupUserStatesTest(t)
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"user-states", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
