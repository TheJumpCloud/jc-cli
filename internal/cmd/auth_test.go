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

	expected := []string{"login", "status", "logout", "switch"}
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

func TestAuthStatus_ShowsProfileRoleWhenSet(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: reporting
profiles:
  reporting:
    api_key: ""
    auth_profile_role: read_only
`)
	viper.Set("defaults.output", "table")

	ts := startMockJCServer(t, "org-ro", "Reporting Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	viper.Set("api_key", "readonly-key-9876")

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))

	if err := runAuthStatus(cmd, nil); err != nil {
		t.Fatalf("runAuthStatus() error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Profile Role:  read_only") {
		t.Errorf("expected 'Profile Role:  read_only' in output, got %q", got)
	}
}

func TestAuthStatus_OmitsProfileRoleWhenUnset(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)
	viper.Set("defaults.output", "table")

	ts := startMockJCServer(t, "org-norole", "No Role Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	viper.Set("api_key", "test-key-norole-1234")

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))

	if err := runAuthStatus(cmd, nil); err != nil {
		t.Fatalf("runAuthStatus() error: %v", err)
	}

	got := stdout.String()
	if strings.Contains(got, "Profile Role:") {
		t.Errorf("did not expect 'Profile Role:' line when role is unset; got %q", got)
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

func TestAuthStatus_Quiet_Authenticated(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)
	viper.Set("defaults.output", "table")

	ts := startMockJCServer(t, "org-quiet", "Quiet Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	viper.Set("api_key", "quiet-test-key-1234")
	viper.Set("quiet", true)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthStatus(cmd, nil)
	if err != nil {
		t.Fatalf("runAuthStatus() error: %v", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("expected no output in quiet mode, got %q", stdout.String())
	}
}

func TestAuthStatus_Quiet_NotAuthenticated(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)
	viper.Set("defaults.output", "table")
	viper.Set("quiet", true)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthStatus(cmd, nil)
	if err == nil {
		t.Fatal("expected ExitError for unauthenticated quiet status")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitCodeAuthFailed {
		t.Errorf("exit code = %d, want %d", exitErr.Code, ExitCodeAuthFailed)
	}

	if stdout.Len() != 0 {
		t.Errorf("expected no output in quiet mode, got %q", stdout.String())
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

// --- Auth Switch Tests (US-035) ---

func TestAuthSwitch_ExplicitProfile(t *testing.T) {
	keyring.MockInit()
	cfgPath := setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: "key-default"
  production:
    api_key: "key-prod"
    org_id: "org-prod"
`)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthSwitch(cmd, []string{"production"}, &mockInput{})
	if err != nil {
		t.Fatalf("runAuthSwitch() error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "production") {
		t.Errorf("expected output to mention 'production', got %q", got)
	}

	// Verify config file was updated.
	data, _ := os.ReadFile(cfgPath)
	cfgStr := string(data)
	if !strings.Contains(cfgStr, "production") {
		t.Errorf("config should set active_profile to production, got:\n%s", cfgStr)
	}

	// Verify viper state was updated.
	if got := config.ActiveProfile(); got != "production" {
		t.Errorf("ActiveProfile() = %q, want %q", got, "production")
	}
}

func TestAuthSwitch_NonExistentProfile(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthSwitch(cmd, []string{"nonexistent"}, &mockInput{})
	if err == nil {
		t.Fatal("expected error for non-existent profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
	if !strings.Contains(err.Error(), "default") {
		t.Errorf("error should list available profiles, got: %v", err)
	}
}

func TestAuthSwitch_InteractivePicker(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: "key-default"
  production:
    api_key: "key-prod"
  staging:
    api_key: "key-staging"
`)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	// Select option 2 (profiles are sorted: default, production, staging).
	input := &mockInput{line: "2"}

	err := runAuthSwitch(cmd, nil, input)
	if err != nil {
		t.Fatalf("runAuthSwitch() error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "production") {
		t.Errorf("expected output to mention 'production', got %q", got)
	}

	// Verify the picker was shown on stderr.
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "Available profiles") {
		t.Errorf("expected 'Available profiles' in stderr, got %q", stderrStr)
	}
	if !strings.Contains(stderrStr, "1) default") {
		t.Errorf("expected '1) default' in stderr, got %q", stderrStr)
	}
	if !strings.Contains(stderrStr, "2) production") {
		t.Errorf("expected '2) production' in stderr, got %q", stderrStr)
	}
	if !strings.Contains(stderrStr, "3) staging") {
		t.Errorf("expected '3) staging' in stderr, got %q", stderrStr)
	}
}

func TestAuthSwitch_InteractivePickerShowsActiveMarker(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: staging
profiles:
  default:
    api_key: ""
  staging:
    api_key: ""
`)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	input := &mockInput{line: "1"}

	err := runAuthSwitch(cmd, nil, input)
	if err != nil {
		t.Fatalf("runAuthSwitch() error: %v", err)
	}

	// Active profile should have asterisk marker.
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "* ") {
		t.Errorf("expected active profile marker '*' in stderr, got %q", stderrStr)
	}
}

func TestAuthSwitch_InteractiveInvalidSelection(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
  production:
    api_key: ""
`)

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	input := &mockInput{line: "99"}

	err := runAuthSwitch(cmd, nil, input)
	if err == nil {
		t.Fatal("expected error for invalid selection")
	}
	if !strings.Contains(err.Error(), "invalid selection") {
		t.Errorf("error should mention 'invalid selection', got: %v", err)
	}
}

func TestAuthSwitch_InteractiveNonNumericSelection(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	input := &mockInput{line: "abc"}

	err := runAuthSwitch(cmd, nil, input)
	if err == nil {
		t.Fatal("expected error for non-numeric selection")
	}
	if !strings.Contains(err.Error(), "invalid selection") {
		t.Errorf("error should mention 'invalid selection', got: %v", err)
	}
}

func TestAuthSwitch_InteractiveNonInteractiveMode(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	viper.Set("non-interactive", true)
	defer viper.Set("non-interactive", false)

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthSwitch(cmd, nil, &mockInput{})
	if err == nil {
		t.Fatal("expected error in non-interactive mode without profile arg")
	}
	if !strings.Contains(err.Error(), "non-interactive") {
		t.Errorf("error should mention 'non-interactive', got: %v", err)
	}
}

func TestAuthSwitch_NoProfiles(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
`)

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthSwitch(cmd, []string{"something"}, &mockInput{})
	if err == nil {
		t.Fatal("expected error when no profiles configured")
	}
	if !strings.Contains(err.Error(), "no profiles") {
		t.Errorf("error should mention 'no profiles', got: %v", err)
	}
}

func TestAuthSwitch_ViaRootCommand(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
  staging:
    api_key: ""
`)

	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"auth", "switch", "staging"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "staging") {
		t.Errorf("expected 'staging' in output, got %q", got)
	}
}

// --- --org Flag Tests (US-035) ---

func TestOrgFlag_OverridesActiveProfile(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: "key-default"
  production:
    api_key: "key-prod"
    org_id: "org-prod"
`)

	ts := startMockJCServer(t, "org-prod", "Prod Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"--org", "production", "auth", "status"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "production") {
		t.Errorf("expected output to mention profile 'production', got %q", got)
	}
}

func TestOrgFlag_NonExistentProfile(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	rootCmd := NewRootCmd()
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"--org", "nonexistent", "auth", "status"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent --org profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestOrgFlag_DoesNotPersistToConfig(t *testing.T) {
	keyring.MockInit()
	cfgPath := setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: "key-default"
  staging:
    api_key: "key-staging"
`)

	ts := startMockJCServer(t, "org-staging", "Staging Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	rootCmd := NewRootCmd()
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"--org", "staging", "auth", "status"})

	_ = rootCmd.Execute()

	// Config file should still have default as active profile.
	data, _ := os.ReadFile(cfgPath)
	cfgStr := string(data)
	// The active_profile in the file should not have changed.
	if strings.Contains(cfgStr, "active_profile: staging") {
		t.Errorf("--org should not persist to config file, got:\n%s", cfgStr)
	}
}

// --- Auth Switch Help Tests ---

func TestAuthSwitchHelp(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"auth", "switch", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "Switch the active profile") {
		t.Errorf("help should mention 'Switch the active profile', got %q", got)
	}
	if !strings.Contains(got, "interactive") {
		t.Errorf("help should mention 'interactive', got %q", got)
	}
}

// --- Tab Completion Tests (US-035) ---

func TestAuthSwitch_TabCompletion(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
  production:
    api_key: ""
  staging:
    api_key: ""
`)

	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"__complete", "auth", "switch", ""})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	for _, profile := range []string{"default", "production", "staging"} {
		if !strings.Contains(got, profile) {
			t.Errorf("completion should include profile %q, got %q", profile, got)
		}
	}
}

func TestOrgFlag_TabCompletion(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
  production:
    api_key: ""
`)

	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"__complete", "--org", ""})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	for _, profile := range []string{"default", "production"} {
		if !strings.Contains(got, profile) {
			t.Errorf("--org completion should include profile %q, got %q", profile, got)
		}
	}
}

// --- Service Account Authentication Tests (US-036) ---

// startMockOAuthServer creates a mock OAuth token server + JumpCloud API server.
// The OAuth server issues tokens; the JC server validates them.
func startMockOAuthAndJCServer(t *testing.T, orgID, orgName string) (oauthURL, jcURL string) {
	t.Helper()

	// OAuth token server.
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-bearer-token-9999",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	t.Cleanup(func() { oauthServer.Close() })

	// JumpCloud API server (validates bearer tokens).
	jcServer := startMockJCServer(t, orgID, orgName, http.StatusOK)
	t.Cleanup(func() { jcServer.Close() })

	return oauthServer.URL, jcServer.URL
}

// overrideOAuthURL sets the OAuth token URL to a test server.
func overrideOAuthURL(t *testing.T, url string) {
	t.Helper()
	orig := api.SetOAuthTokenURL(url)
	t.Cleanup(func() { api.SetOAuthTokenURL(orig) })
}

// overrideOAuthClient overrides newOAuthClient to point at the test JC server.
func overrideOAuthClient(t *testing.T, serverURL string) {
	t.Helper()
	orig := newOAuthClient
	t.Cleanup(func() { newOAuthClient = orig })
	newOAuthClient = func(tc *api.TokenCache) *api.Client {
		c := api.NewClientWithToken(tc)
		c.BaseURL = serverURL
		return c
	}
}

func TestAuthLogin_ServiceAccount_Success(t *testing.T) {
	keyring.MockInit()
	cfgPath := setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	oauthURL, jcURL := startMockOAuthAndJCServer(t, "org-sa-123", "SA Test Org")
	overrideOAuthURL(t, oauthURL)
	overrideOAuthClient(t, jcURL)

	input := &mockInput{
		line:   "test-client-id-abc",
		apiKey: "test-client-secret-xyz",
	}

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	err := runAuthLoginServiceAccount(cmd, "", input)
	if err != nil {
		t.Fatalf("runAuthLoginServiceAccount() error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "SA Test Org") {
		t.Errorf("expected output to contain org name 'SA Test Org', got %q", got)
	}
	if !strings.Contains(got, "service account") {
		t.Errorf("expected output to mention 'service account', got %q", got)
	}
	if !strings.Contains(got, "profile: default") {
		t.Errorf("expected output to contain 'profile: default', got %q", got)
	}

	// Verify config was updated.
	cfgData, _ := os.ReadFile(cfgPath)
	cfgStr := string(cfgData)
	if !strings.Contains(cfgStr, "service_account") {
		t.Errorf("config should contain auth_method: service_account, got:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, "test-client-id-abc") {
		t.Errorf("config should contain client_id, got:\n%s", cfgStr)
	}

	// Verify client secret was stored in keychain.
	stored, err := keyring.Get("jc", "default:client_secret")
	if err != nil {
		t.Fatalf("expected client secret in keychain: %v", err)
	}
	if stored != "test-client-secret-xyz" {
		t.Errorf("keychain value = %q, want %q", stored, "test-client-secret-xyz")
	}
}

func TestAuthLogin_ServiceAccount_WithProfile(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	oauthURL, jcURL := startMockOAuthAndJCServer(t, "org-sa-456", "SA Profile Org")
	overrideOAuthURL(t, oauthURL)
	overrideOAuthClient(t, jcURL)

	input := &mockInput{
		line:   "profile-client-id",
		apiKey: "profile-client-secret",
	}

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthLoginServiceAccount(cmd, "automation", input)
	if err != nil {
		t.Fatalf("runAuthLoginServiceAccount() error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "profile: automation") {
		t.Errorf("expected output to mention 'profile: automation', got %q", got)
	}

	// Verify client secret stored under the named profile.
	stored, err := keyring.Get("jc", "automation:client_secret")
	if err != nil {
		t.Fatalf("expected client secret in keychain for profile 'automation': %v", err)
	}
	if stored != "profile-client-secret" {
		t.Errorf("keychain value = %q, want %q", stored, "profile-client-secret")
	}
}

func TestAuthLogin_ServiceAccount_EmptyClientID(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	input := &mockInput{line: "", apiKey: "secret"}

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthLoginServiceAccount(cmd, "", input)
	if err == nil {
		t.Fatal("expected error for empty client ID")
	}
	if !strings.Contains(err.Error(), "client ID cannot be empty") {
		t.Errorf("error should mention 'client ID cannot be empty', got: %v", err)
	}
}

func TestAuthLogin_ServiceAccount_EmptyClientSecret(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	input := &mockInput{line: "some-id", apiKey: ""}

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthLoginServiceAccount(cmd, "", input)
	if err == nil {
		t.Fatal("expected error for empty client secret")
	}
	if !strings.Contains(err.Error(), "client secret cannot be empty") {
		t.Errorf("error should mention 'client secret cannot be empty', got: %v", err)
	}
}

func TestAuthLogin_ServiceAccount_InvalidCredentials(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	// OAuth server that rejects credentials.
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer oauthServer.Close()
	overrideOAuthURL(t, oauthServer.URL)

	input := &mockInput{line: "bad-id", apiKey: "bad-secret"}

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthLoginServiceAccount(cmd, "", input)
	if err == nil {
		t.Fatal("expected error for invalid credentials")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error should mention 'authentication failed', got: %v", err)
	}
}

func TestAuthLogin_ServiceAccount_NonInteractive(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	viper.Set("non-interactive", true)
	defer viper.Set("non-interactive", false)

	input := &mockInput{line: "id", apiKey: "secret"}

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := runAuthLoginServiceAccount(cmd, "", input)
	if err == nil {
		t.Fatal("expected error in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "interactive input") {
		t.Errorf("error should mention 'interactive input', got: %v", err)
	}
}

func TestAuthLogin_ServiceAccount_OrgFetch403(t *testing.T) {
	keyring.MockInit()
	cfgPath := setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	// OAuth server that issues tokens successfully.
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-bearer-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer oauthServer.Close()
	overrideOAuthURL(t, oauthServer.URL)

	// JC server that returns 403 for /organizations.
	jcServer := startMockJCServer(t, "", "", http.StatusForbidden)
	defer jcServer.Close()
	overrideOAuthClient(t, jcServer.URL)

	input := &mockInput{
		line:   "test-client-id",
		apiKey: "test-client-secret",
	}

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	err := runAuthLoginServiceAccount(cmd, "", input)
	if err != nil {
		t.Fatalf("runAuthLoginServiceAccount() should succeed despite 403, got error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Logged in") {
		t.Errorf("expected 'Logged in' in output, got %q", got)
	}
	if !strings.Contains(got, "service account") {
		t.Errorf("expected 'service account' in output, got %q", got)
	}
	if !strings.Contains(got, "profile: default") {
		t.Errorf("expected 'profile: default' in output, got %q", got)
	}

	// Verify warning was shown on stderr.
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "skipped") {
		t.Errorf("expected 'skipped' warning on stderr, got %q", stderrStr)
	}

	// Verify credentials were saved despite 403.
	cfgData, _ := os.ReadFile(cfgPath)
	cfgStr := string(cfgData)
	if !strings.Contains(cfgStr, "service_account") {
		t.Errorf("config should contain auth_method: service_account, got:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, "test-client-id") {
		t.Errorf("config should contain client_id, got:\n%s", cfgStr)
	}

	// Verify client secret in keychain.
	stored, err := keyring.Get("jc", "default:client_secret")
	if err != nil {
		t.Fatalf("expected client secret in keychain: %v", err)
	}
	if stored != "test-client-secret" {
		t.Errorf("keychain value = %q, want %q", stored, "test-client-secret")
	}
}

func TestAuthLogin_ServiceAccountFlagInHelp(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"auth", "login", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "--service-account") {
		t.Errorf("login help should mention --service-account flag, got %q", got)
	}
}

// --- Auth Status with Service Account (US-036) ---

func TestAuthStatus_ServiceAccount_Authenticated(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    auth_method: service_account
    client_id: "sa-client-id-1234"
    client_secret: ""
    org_id: ""
`)

	oauthURL, jcURL := startMockOAuthAndJCServer(t, "org-sa-status", "SA Status Org")
	overrideOAuthURL(t, oauthURL)
	overrideOAuthClient(t, jcURL)

	// Store client secret in keychain directly.
	_ = keyring.Set("jc", "default:client_secret", "sa-secret-5678")
	viper.Set("profiles.default.client_secret", "keychain://jc/default:client_secret")

	viper.Set("defaults.output", "table")

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
		t.Errorf("expected 'Authenticated: yes', got %q", got)
	}
	if !strings.Contains(got, "service_account") {
		t.Errorf("expected 'service_account' in output, got %q", got)
	}
	if !strings.Contains(got, "sa-client-id-1234") {
		t.Errorf("expected client ID in output, got %q", got)
	}
	if !strings.Contains(got, "SA Status Org") {
		t.Errorf("expected org name in output, got %q", got)
	}
}

func TestAuthStatus_ServiceAccount_JSONOutput(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    auth_method: service_account
    client_id: "json-sa-client"
    client_secret: ""
`)

	oauthURL, jcURL := startMockOAuthAndJCServer(t, "org-json-sa", "JSON SA Org")
	overrideOAuthURL(t, oauthURL)
	overrideOAuthClient(t, jcURL)

	_ = keyring.Set("jc", "default:client_secret", "json-sa-secret")
	viper.Set("profiles.default.client_secret", "keychain://jc/default:client_secret")
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
		t.Error("expected authenticated=true")
	}
	if status.AuthMethod != "service_account" {
		t.Errorf("auth_method = %q, want %q", status.AuthMethod, "service_account")
	}
	if status.ClientID != "json-sa-client" {
		t.Errorf("client_id = %q, want %q", status.ClientID, "json-sa-client")
	}
	if status.TokenExpiry == "" {
		t.Error("expected token_expiry to be set")
	}
	if status.OrgID != "org-json-sa" {
		t.Errorf("org_id = %q, want %q", status.OrgID, "org-json-sa")
	}
}

// --- Auth Logout with Service Account (US-036) ---

func TestAuthLogout_ServiceAccount(t *testing.T) {
	keyring.MockInit()
	cfgPath := setupTestConfig(t, `active_profile: default
profiles:
  default:
    auth_method: service_account
    client_id: "logout-sa-id"
    client_secret: "keychain://jc/default:client_secret"
    org_id: "org-123"
`)

	// Store client secret in keychain.
	_ = keyring.Set("jc", "default:client_secret", "logout-sa-secret")

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

	// Verify client secret removed from keychain.
	_, err = keyring.Get("jc", "default:client_secret")
	if err == nil {
		t.Error("expected client secret to be removed from keychain after logout")
	}

	// Verify config was cleared.
	cfgData, _ := os.ReadFile(cfgPath)
	cfgStr := string(cfgData)
	if strings.Contains(cfgStr, "service_account") {
		t.Errorf("config should not contain 'service_account' after logout, got:\n%s", cfgStr)
	}
	if strings.Contains(cfgStr, "logout-sa-id") {
		t.Errorf("config should not contain client_id after logout, got:\n%s", cfgStr)
	}
}

// --- Bearer Token Auth Transport Tests (US-036) ---

func TestNewClientWithToken(t *testing.T) {
	keyring.MockInit()

	// Create a mock JC server that verifies Bearer auth.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"missing bearer token"}`))
			return
		}

		if r.URL.Path == "/organizations" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []map[string]interface{}{
					{"_id": "org-bearer", "displayName": "Bearer Org"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// Create a mock token server.
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "bearer-test-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	overrideOAuthURL(t, tokenServer.URL)

	tc := api.NewTokenCache("test-id", "test-secret")
	client := api.NewClientWithToken(tc)
	client.BaseURL = ts.URL

	org, err := client.ValidateAPIKey()
	if err != nil {
		t.Fatalf("ValidateAPIKey() error: %v", err)
	}
	if org.DisplayName != "Bearer Org" {
		t.Errorf("org name = %q, want %q", org.DisplayName, "Bearer Org")
	}
}

func TestAuthStatus_APIKey_ShowsAuthMethod(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	ts := startMockJCServer(t, "org-api-method", "API Method Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	viper.Set("api_key", "api-method-key-1234")
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
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if status.AuthMethod != "api_key" {
		t.Errorf("auth_method = %q, want %q", status.AuthMethod, "api_key")
	}
}

// --- Login via Root Command (US-036) ---

func TestAuthLogin_ServiceAccount_ViaRootCommand(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"auth", "login", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "--service-account") {
		t.Errorf("auth login help should show --service-account, got %q", got)
	}
	if !strings.Contains(got, "OAuth 2.0") {
		t.Errorf("auth login help should mention OAuth 2.0, got %q", got)
	}
}

// --- Keychain unavailability tests ---

// overrideKeychainAvailable overrides keychainIsAvailable for the test.
func overrideKeychainAvailable(t *testing.T, available bool) {
	t.Helper()
	orig := keychainIsAvailable
	t.Cleanup(func() { keychainIsAvailable = orig })
	keychainIsAvailable = func() bool { return available }
}

func TestAuthLogin_KeychainUnavailable_FailsWithoutFlag(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	ts := startMockJCServer(t, "org-123", "Test Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)
	overrideKeychainAvailable(t, false)

	authCmd := newAuthCmd()
	authCmd.SetOut(new(bytes.Buffer))
	authCmd.SetErr(new(bytes.Buffer))
	loginCmd, _, _ := authCmd.Find([]string{"login"})

	err := runAuthLogin(loginCmd, "", &mockInput{apiKey: "test-key-1234"})
	if err == nil {
		t.Fatal("expected error when keychain unavailable and --allow-plaintext not set")
	}
	if !strings.Contains(err.Error(), "keychain unavailable") {
		t.Errorf("expected keychain unavailable error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--allow-plaintext") {
		t.Errorf("expected --allow-plaintext suggestion, got: %v", err)
	}
}

func TestAuthLogin_KeychainUnavailable_SucceedsWithFlag(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	ts := startMockJCServer(t, "org-123", "Test Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)
	overrideKeychainAvailable(t, false)

	authCmd := newAuthCmd()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	authCmd.SetOut(stdout)
	authCmd.SetErr(stderr)
	// Set the persistent flag on the parent (auth) command.
	authCmd.PersistentFlags().Set("allow-plaintext", "true")
	loginCmd, _, _ := authCmd.Find([]string{"login"})

	err := runAuthLogin(loginCmd, "", &mockInput{apiKey: "test-key-1234"})
	if err != nil {
		t.Fatalf("expected success with --allow-plaintext, got: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Logged in") {
		t.Errorf("expected 'Logged in' message, got: %s", got)
	}
}

func TestAuthLoginServiceAccount_KeychainUnavailable_FailsWithoutFlag(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	oauthURL, jcURL := startMockOAuthAndJCServer(t, "org-sa-123", "SA Test Org")
	overrideOAuthURL(t, oauthURL)
	overrideOAuthClient(t, jcURL)
	overrideKeychainAvailable(t, false)

	authCmd := newAuthCmd()
	authCmd.SetOut(new(bytes.Buffer))
	authCmd.SetErr(new(bytes.Buffer))
	loginCmd, _, _ := authCmd.Find([]string{"login"})

	input := &mockInput{apiKey: "test-secret", line: "test-client-id"}
	err := runAuthLoginServiceAccount(loginCmd, "", input)
	if err == nil {
		t.Fatal("expected error when keychain unavailable and --allow-plaintext not set")
	}
	if !strings.Contains(err.Error(), "keychain unavailable") {
		t.Errorf("expected keychain unavailable error, got: %v", err)
	}
}
