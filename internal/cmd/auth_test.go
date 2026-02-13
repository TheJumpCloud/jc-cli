package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/config"
)

// mockInput implements InputReader for tests.
type mockInput struct {
	apiKey string
	line   string
	err    error
}

func (m *mockInput) ReadAPIKey() (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.apiKey, nil
}

func (m *mockInput) ReadLine() (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.line, nil
}

// setupTestConfig creates an isolated config environment for testing.
func setupTestConfig(t *testing.T, yamlContent string) string {
	t.Helper()
	viper.Reset()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_ORG_ID", "")
	t.Setenv("JC_PROFILE", "")

	_ = os.MkdirAll(dir, 0700)
	if yamlContent != "" {
		_ = os.WriteFile(cfgPath, []byte(yamlContent), 0600)
	}

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	return cfgPath
}

// startMockJCServer returns an httptest.Server that simulates the JumpCloud API.
func startMockJCServer(t *testing.T, orgID, orgName string, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/organizations" {
			if statusCode != http.StatusOK {
				w.WriteHeader(statusCode)
				w.Write([]byte(`{"message":"error"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []map[string]interface{}{
					{"_id": orgID, "displayName": orgName},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// overrideAPIClient overrides newAPIClient to create clients pointing at the test server.
func overrideAPIClient(t *testing.T, serverURL string) {
	t.Helper()
	orig := newAPIClient
	t.Cleanup(func() { newAPIClient = orig })
	newAPIClient = func(key string) *api.Client {
		c := api.NewClientWithKey(key)
		c.BaseURL = serverURL
		return c
	}
}

// --- Auth Command Registration Tests ---

func TestAuthCommandRegistered(t *testing.T) {
	rootCmd := NewRootCmd()
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "auth" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'auth' command to be registered on root")
	}
}

func TestAuthSubcommands(t *testing.T) {
	rootCmd := NewRootCmd()
	var authCmd *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Use == "auth" {
			authCmd = c
			break
		}
	}
	if authCmd == nil {
		t.Fatal("auth command not found")
	}

	expected := []string{"login", "status", "logout"}
	for _, name := range expected {
		found := false
		for _, sub := range authCmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected subcommand %q under 'auth'", name)
		}
	}
}

// --- Auth Login Tests ---

func TestAuthLogin_Success(t *testing.T) {
	keyring.MockInit()
	cfgPath := setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	ts := startMockJCServer(t, "org-123", "Test Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	input := &mockInput{apiKey: "test-valid-key-1234"}

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	err := runAuthLogin(cmd, "", input)
	if err != nil {
		t.Fatalf("runAuthLogin() error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Test Org") {
		t.Errorf("expected output to contain org name 'Test Org', got %q", got)
	}
	if !strings.Contains(got, "profile: default") {
		t.Errorf("expected output to contain 'profile: default', got %q", got)
	}

	// Verify key was stored in keychain.
	stored, err := keyring.Get("jc", "default")
	if err != nil {
		t.Fatalf("expected key in keychain: %v", err)
	}
	if stored != "test-valid-key-1234" {
		t.Errorf("keychain value = %q, want %q", stored, "test-valid-key-1234")
	}

	// Verify config was updated with keychain reference.
	cfgData, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgData), "keychain://jc/default") {
		t.Errorf("config should contain keychain ref, got:\n%s", cfgData)
	}
}

func TestAuthLogin_WithProfile(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	ts := startMockJCServer(t, "org-456", "Acme Inc", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	input := &mockInput{apiKey: "acme-key-5678"}

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	err := runAuthLogin(cmd, "production", input)
	if err != nil {
		t.Fatalf("runAuthLogin() error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "profile: production") {
		t.Errorf("expected output to mention 'profile: production', got %q", got)
	}

	// Verify keychain stores under the named profile.
	stored, err := keyring.Get("jc", "production")
	if err != nil {
		t.Fatalf("expected key in keychain for profile 'production': %v", err)
	}
	if stored != "acme-key-5678" {
		t.Errorf("keychain value = %q, want %q", stored, "acme-key-5678")
	}
}

func TestAuthLogin_EmptyKey(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	input := &mockInput{apiKey: ""}

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthLogin(cmd, "", input)
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
	if !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("error should mention 'cannot be empty', got: %v", err)
	}
}

func TestAuthLogin_InvalidKey(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	ts := startMockJCServer(t, "", "", http.StatusUnauthorized)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	input := &mockInput{apiKey: "bad-key-invalid"}

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthLogin(cmd, "", input)
	if err == nil {
		t.Fatal("expected error for invalid API key")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error should mention 'authentication failed', got: %v", err)
	}
}

func TestAuthLogin_ReadError(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	input := &mockInput{err: fmt.Errorf("terminal read error")}

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthLogin(cmd, "", input)
	if err == nil {
		t.Fatal("expected error for input read failure")
	}
	if !strings.Contains(err.Error(), "terminal read error") {
		t.Errorf("error should contain 'terminal read error', got: %v", err)
	}
}

func TestAuthLogin_NonInteractive(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	viper.Set("non-interactive", true)
	defer viper.Set("non-interactive", false)

	input := &mockInput{apiKey: "some-key"}

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthLogin(cmd, "", input)
	if err == nil {
		t.Fatal("expected error in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "interactive input") {
		t.Errorf("error should mention 'interactive input', got: %v", err)
	}
}

func TestAuthLogin_SetsOrgID(t *testing.T) {
	keyring.MockInit()
	cfgPath := setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	ts := startMockJCServer(t, "org-set-id", "ID Test Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	input := &mockInput{apiKey: "key-for-org-id-test"}

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthLogin(cmd, "", input)
	if err != nil {
		t.Fatalf("runAuthLogin() error: %v", err)
	}

	// Verify org_id was written to config.
	cfgData, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgData), "org-set-id") {
		t.Errorf("config should contain org ID, got:\n%s", cfgData)
	}
}

func TestAuthLogin_SwitchesActiveProfile(t *testing.T) {
	keyring.MockInit()
	cfgPath := setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	ts := startMockJCServer(t, "org-switch", "Switch Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	input := &mockInput{apiKey: "switch-key-1234"}

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	// Login with a different profile name.
	err := runAuthLogin(cmd, "staging", input)
	if err != nil {
		t.Fatalf("runAuthLogin() error: %v", err)
	}

	// Verify active_profile was switched.
	cfgData, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgData), "staging") {
		t.Errorf("config should contain active_profile: staging, got:\n%s", cfgData)
	}
}

// --- Auth Status Tests ---

func TestAuthStatus_Authenticated(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)
	viper.Set("defaults.output", "table")

	ts := startMockJCServer(t, "org-status", "Status Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	viper.Set("api_key", "status-test-key-1234")

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthStatus(cmd, nil)
	if err != nil {
		t.Fatalf("runAuthStatus() error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Authenticated: yes") {
		t.Errorf("expected 'Authenticated: yes' in output, got %q", got)
	}
	if !strings.Contains(got, "Status Org") {
		t.Errorf("expected org name in output, got %q", got)
	}
	if !strings.Contains(got, "org-status") {
		t.Errorf("expected org ID in output, got %q", got)
	}
	if !strings.Contains(got, "****1234") {
		t.Errorf("expected redacted API key in output, got %q", got)
	}
}

func TestAuthStatus_NotAuthenticated_ExitCode3(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)
	viper.Set("defaults.output", "table")

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthStatus(cmd, nil)
	if err == nil {
		t.Fatal("expected ExitError for unauthenticated status")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitCodeAuthFailed {
		t.Errorf("exit code = %d, want %d", exitErr.Code, ExitCodeAuthFailed)
	}

	got := stdout.String()
	if !strings.Contains(got, "Authenticated: no") {
		t.Errorf("expected 'Authenticated: no' in output, got %q", got)
	}
	if !strings.Contains(got, "jc auth login") {
		t.Errorf("expected suggestion to run 'jc auth login', got %q", got)
	}
}

func TestAuthStatus_JSONOutput_Authenticated(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: testprofile
profiles:
  testprofile:
    api_key: ""
`)

	ts := startMockJCServer(t, "org-json", "JSON Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	viper.Set("api_key", "json-test-key-5678")
	viper.Set("defaults.output", "json")

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthStatus(cmd, nil)
	if err != nil {
		t.Fatalf("runAuthStatus() error: %v", err)
	}

	var status authStatusInfo
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, stdout.String())
	}

	if !status.Authenticated {
		t.Error("expected authenticated=true in JSON output")
	}
	if status.Profile != "testprofile" {
		t.Errorf("profile = %q, want %q", status.Profile, "testprofile")
	}
	if status.OrgName != "JSON Org" {
		t.Errorf("org_name = %q, want %q", status.OrgName, "JSON Org")
	}
	if status.OrgID != "org-json" {
		t.Errorf("org_id = %q, want %q", status.OrgID, "org-json")
	}
	if status.APIKeyRedacted != "****5678" {
		t.Errorf("api_key = %q, want %q", status.APIKeyRedacted, "****5678")
	}
}

func TestAuthStatus_JSONOutput_NotAuthenticated(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)
	viper.Set("defaults.output", "json")

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))

	// JSON mode returns after encoding, no ExitError.
	err := runAuthStatus(cmd, nil)
	if err != nil {
		t.Logf("Note: got error in JSON not-authenticated mode: %v", err)
	}

	var status authStatusInfo
	if jsonErr := json.Unmarshal(stdout.Bytes(), &status); jsonErr != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", jsonErr, stdout.String())
	}
	if status.Authenticated {
		t.Error("expected authenticated=false")
	}
	if status.Profile != "default" {
		t.Errorf("profile = %q, want %q", status.Profile, "default")
	}
}

// --- Auth Logout Tests ---

func TestAuthLogout_Success(t *testing.T) {
	keyring.MockInit()
	cfgPath := setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: "keychain://jc/default"
    org_id: "org-123"
`)

	_ = keyring.Set("jc", "default", "secret-key")

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthLogout(cmd, nil)
	if err != nil {
		t.Fatalf("runAuthLogout() error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Logged out") {
		t.Errorf("expected 'Logged out' message, got %q", got)
	}
	if !strings.Contains(got, "profile: default") {
		t.Errorf("expected profile name in output, got %q", got)
	}

	// Verify key removed from keychain.
	_, err = keyring.Get("jc", "default")
	if err == nil {
		t.Error("expected keychain entry to be removed after logout")
	}

	// Verify config api_key was cleared.
	cfgData, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(cfgData), "keychain://jc/default") {
		t.Errorf("config should not contain keychain ref after logout, got:\n%s", cfgData)
	}
}

func TestAuthLogout_NoKeychainEntry(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: "plaintext-key"
`)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	err := runAuthLogout(cmd, nil)
	if err != nil {
		t.Fatalf("runAuthLogout() error: %v", err)
	}

	if !strings.Contains(stdout.String(), "Logged out") {
		t.Errorf("expected 'Logged out' message, got %q", stdout.String())
	}
}

// --- ExitError Tests ---

func TestExitError(t *testing.T) {
	err := &ExitError{Code: 3, Err: fmt.Errorf("not authenticated")}

	if err.Error() != "not authenticated" {
		t.Errorf("Error() = %q, want %q", err.Error(), "not authenticated")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatal("errors.As should match ExitError")
	}
	if exitErr.Code != 3 {
		t.Errorf("exit code = %d, want %d", exitErr.Code, 3)
	}
}

func TestExitError_NilErr(t *testing.T) {
	err := &ExitError{Code: 10}
	if !strings.Contains(err.Error(), "exit code 10") {
		t.Errorf("Error() = %q, expected to contain 'exit code 10'", err.Error())
	}
}

func TestExitError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("inner error")
	err := &ExitError{Code: 3, Err: inner}
	if !errors.Is(err, inner) {
		t.Error("Unwrap should return inner error")
	}
}

// --- Auth Help Tests ---

func TestAuthHelp(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"auth", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "login") {
		t.Errorf("auth help should list 'login' subcommand, got %q", got)
	}
	if !strings.Contains(got, "status") {
		t.Errorf("auth help should list 'status' subcommand, got %q", got)
	}
	if !strings.Contains(got, "logout") {
		t.Errorf("auth help should list 'logout' subcommand, got %q", got)
	}
}

func TestAuthLoginHelp(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"auth", "login", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "--profile") {
		t.Errorf("login help should mention --profile flag, got %q", got)
	}
}

// --- Config Write Tests ---

func TestConfigSetProfileField(t *testing.T) {
	keyring.MockInit()
	cfgPath := setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	if err := config.SetProfileField("default", "api_key", "new-key-value"); err != nil {
		t.Fatalf("SetProfileField() error: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "new-key-value") {
		t.Errorf("config file should contain 'new-key-value', got:\n%s", data)
	}
}

func TestConfigSetActiveProfile(t *testing.T) {
	keyring.MockInit()
	cfgPath := setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	if err := config.SetActiveProfile("production"); err != nil {
		t.Fatalf("SetActiveProfile() error: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "production") {
		t.Errorf("config file should contain 'production', got:\n%s", data)
	}
}
