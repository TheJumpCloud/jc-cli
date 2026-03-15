package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"
)

// wizardInput implements InputReader with separate queues for ReadLine and ReadAPIKey.
// This supports the wizard's interleaved calls to both methods.
type wizardInput struct {
	lines     []string
	masked    []string
	lineIdx   int
	maskedIdx int
}

func (w *wizardInput) ReadLine() (string, error) {
	if w.lineIdx >= len(w.lines) {
		return "", fmt.Errorf("wizardInput: no more lines (consumed %d)", w.lineIdx)
	}
	val := w.lines[w.lineIdx]
	w.lineIdx++
	return val, nil
}

func (w *wizardInput) ReadAPIKey() (string, error) {
	if w.maskedIdx >= len(w.masked) {
		return "", fmt.Errorf("wizardInput: no more masked values (consumed %d)", w.maskedIdx)
	}
	val := w.masked[w.maskedIdx]
	w.maskedIdx++
	return val, nil
}

// overrideSetupInput sets setupInputReader for the test and restores on cleanup.
func overrideSetupInput(t *testing.T, input InputReader) {
	t.Helper()
	orig := setupInputReader
	t.Cleanup(func() { setupInputReader = orig })
	setupInputReader = input
}

func TestSetup_FullFreshFlow(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	ts := startMockJCServer(t, "org-setup-123", "Setup Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	// Wizard steps:
	// 1. Profile: Enter (keep "default")
	// 2. Auth method: "1" (API key)
	// 3. Org ID: Enter (keep auto-detected)
	// 4. Output format: "table"
	// 5. Color: Enter (keep yes)
	// 6. Limit: "50"
	input := &wizardInput{
		lines:  []string{"", "1", "", "table", "", "50"},
		masked: []string{"test-setup-key-1234"},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:   cmd,
		input: input,
		w:     stderr,
		out:   stdout,
	}

	err := wiz.run()
	if err != nil {
		t.Fatalf("setup wizard error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Setup complete!") {
		t.Errorf("expected 'Setup complete!' in output, got %q", got)
	}
	if !strings.Contains(got, "Profile:       default") {
		t.Errorf("expected profile default in summary, got %q", got)
	}
	if !strings.Contains(got, "Output:        table") {
		t.Errorf("expected output table in summary, got %q", got)
	}
	if !strings.Contains(got, "Limit:         50") {
		t.Errorf("expected limit 50 in summary, got %q", got)
	}
	if !strings.Contains(got, "****1234") {
		t.Errorf("expected redacted API key in summary, got %q", got)
	}
}

func TestSetup_KeepAllDefaults(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
defaults:
  output: yaml
  color: true
  limit: 200
profiles:
  default:
    api_key: "keychain://jc/default"
    org_id: "org-existing"
`)
	_ = keyring.Set("jc", "default", "existing-key-5678")

	ts := startMockJCServer(t, "org-existing", "Existing Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	// All Enter keys = keep everything.
	// Steps: profile, keep-creds, org-id, output, color, limit
	input := &wizardInput{
		lines:  []string{"", "", "", "", "", ""},
		masked: []string{},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:   cmd,
		input: input,
		w:     stderr,
		out:   stdout,
	}

	err := wiz.run()
	if err != nil {
		t.Fatalf("setup wizard error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Setup complete!") {
		t.Errorf("expected 'Setup complete!' in output, got %q", got)
	}
	if !strings.Contains(got, "Org ID:        org-existing") {
		t.Errorf("expected org-existing in summary, got %q", got)
	}
}

func TestSetup_ReconfigureAuth(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: "keychain://jc/default"
    org_id: "old-org"
`)
	_ = keyring.Set("jc", "default", "old-key-1234")

	ts := startMockJCServer(t, "new-org-456", "New Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	// Steps: profile(keep), keep-creds(no), auth-method(1), org(keep), output(keep), color(keep), limit(keep)
	input := &wizardInput{
		lines:  []string{"", "n", "1", "", "", "", ""},
		masked: []string{"new-api-key-9999"},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:   cmd,
		input: input,
		w:     stderr,
		out:   stdout,
	}

	err := wiz.run()
	if err != nil {
		t.Fatalf("setup wizard error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "****9999") {
		t.Errorf("expected new redacted key in summary, got %q", got)
	}

	// Verify new key stored in keychain.
	stored, err := keyring.Get("jc", "default")
	if err != nil {
		t.Fatalf("expected key in keychain: %v", err)
	}
	if stored != "new-api-key-9999" {
		t.Errorf("keychain = %q, want %q", stored, "new-api-key-9999")
	}
}

func TestSetup_ServiceAccountFlow(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	oauthURL, jcURL := startMockOAuthAndJCServer(t, "org-sa-setup", "SA Setup Org")
	overrideOAuthURL(t, oauthURL)
	overrideOAuthClient(t, jcURL)

	// Steps: profile(keep), auth-method(2), org(keep), output(keep), color(keep), limit(keep)
	input := &wizardInput{
		lines:  []string{"", "2", "test-sa-client-id", "", "", "", ""},
		masked: []string{"test-sa-secret"},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:   cmd,
		input: input,
		w:     stderr,
		out:   stdout,
	}

	err := wiz.run()
	if err != nil {
		t.Fatalf("setup wizard error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Auth Method:   service_account") {
		t.Errorf("expected service_account auth method in summary, got %q", got)
	}
	if !strings.Contains(got, "Client ID:     test-sa-client-id") {
		t.Errorf("expected client ID in summary, got %q", got)
	}
}

func TestSetup_InvalidAPIKey(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	ts := startMockJCServer(t, "", "", http.StatusUnauthorized)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	// Steps: profile(keep), auth-method(1)
	input := &wizardInput{
		lines:  []string{"", "1"},
		masked: []string{"bad-key-invalid"},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:   cmd,
		input: input,
		w:     stderr,
		out:   stdout,
	}

	err := wiz.run()
	if err == nil {
		t.Fatal("expected error for invalid API key")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error should mention 'authentication failed', got: %v", err)
	}
}

func TestSetup_NewProfileCreation(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	ts := startMockJCServer(t, "org-new-prof", "New Profile Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	// Steps: profile(production), auth-method(1), org(keep), output(keep), color(keep), limit(keep)
	input := &wizardInput{
		lines:  []string{"production", "1", "", "", "", ""},
		masked: []string{"production-key-1234"},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:   cmd,
		input: input,
		w:     stderr,
		out:   stdout,
	}

	err := wiz.run()
	if err != nil {
		t.Fatalf("setup wizard error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Profile:       production") {
		t.Errorf("expected profile 'production' in summary, got %q", got)
	}
}

func TestSetup_OutputFormatValidation(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: "keychain://jc/default"
    org_id: "org-123"
`)
	_ = keyring.Set("jc", "default", "existing-key-5678")

	ts := startMockJCServer(t, "org-123", "Test Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	// Steps: profile(keep), keep-creds(yes), org(keep), output("invalid")
	input := &wizardInput{
		lines:  []string{"", "", "", "invalid"},
		masked: []string{},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:   cmd,
		input: input,
		w:     stderr,
		out:   stdout,
	}

	err := wiz.run()
	if err == nil {
		t.Fatal("expected error for invalid output format")
	}
	if !strings.Contains(err.Error(), "invalid output format") {
		t.Errorf("error should mention 'invalid output format', got: %v", err)
	}
}

func TestSetup_CustomOrgID(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	ts := startMockJCServer(t, "auto-org-id", "Auto Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	// Steps: profile(keep), auth-method(1), org("custom-org-999"), output(keep), color(keep), limit(keep)
	input := &wizardInput{
		lines:  []string{"", "1", "custom-org-999", "", "", ""},
		masked: []string{"custom-org-key"},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:   cmd,
		input: input,
		w:     stderr,
		out:   stdout,
	}

	err := wiz.run()
	if err != nil {
		t.Fatalf("setup wizard error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Org ID:        custom-org-999") {
		t.Errorf("expected custom org ID in summary, got %q", got)
	}
}

func TestSetup_NonInteractiveRejected(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	viper.Set("non-interactive", true)
	defer viper.Set("non-interactive", false)

	rootCmd := NewRootCmd()
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"setup"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "interactive input") {
		t.Errorf("error should mention 'interactive input', got: %v", err)
	}
}

func TestSetup_StaleCredentials(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: "keychain://jc/default"
    org_id: "old-org"
`)
	// Stale key that will fail validation.
	_ = keyring.Set("jc", "default", "stale-key-dead")

	// Mock server: returns 401 for stale key, 200 for fresh key.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-api-key")
		if key == "stale-key-dead" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"Unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"_id": "new-org-fresh", "displayName": "Fresh Org"},
			},
		})
	}))
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	// Steps: profile(keep), [validation fails], auth-method(1), org(keep), output(keep), color(keep), limit(keep)
	input := &wizardInput{
		lines:  []string{"", "1", "", "", "", ""},
		masked: []string{"fresh-key-1234"},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:   cmd,
		input: input,
		w:     stderr,
		out:   stdout,
	}

	err := wiz.run()
	if err != nil {
		t.Fatalf("setup wizard error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "****1234") {
		t.Errorf("expected new redacted key in summary, got %q", got)
	}

	// Verify stderr showed the failure message.
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "FAILED") {
		t.Errorf("expected 'FAILED' message in stderr, got %q", stderrStr)
	}
}

func TestSetup_CommandRegistered(t *testing.T) {
	rootCmd := NewRootCmd()
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "setup" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'setup' command to be registered on root")
	}
}

func TestSetup_Summary(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: testprof
defaults:
  output: csv
  color: false
  limit: 25
profiles:
  testprof:
    api_key: "keychain://jc/testprof"
    org_id: "org-summary"
`)
	_ = keyring.Set("jc", "testprof", "summary-key-abcd")

	ts := startMockJCServer(t, "org-summary", "Summary Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	// Keep all defaults.
	input := &wizardInput{
		lines:  []string{"", "", "", "", "", ""},
		masked: []string{},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:   cmd,
		input: input,
		w:     stderr,
		out:   stdout,
	}

	err := wiz.run()
	if err != nil {
		t.Fatalf("setup wizard error: %v", err)
	}

	got := stdout.String()
	for _, want := range []string{
		"Setup complete!",
		"Profile:       testprof",
		"Auth Method:   api_key",
		"API Key:       ****abcd",
		"Org ID:        org-summary",
		"Output:        csv",
		"Color:         false",
		"Limit:         25",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q, got:\n%s", want, got)
		}
	}
}

func TestSetup_KeychainUnavailable_FailsWithoutFlag(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	ts := startMockJCServer(t, "org-setup-kc", "Setup KC Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	// Simulate keychain unavailable.
	orig := keychainIsAvailable
	t.Cleanup(func() { keychainIsAvailable = orig })
	keychainIsAvailable = func() bool { return false }

	// Steps: profile(keep), auth-method(1)
	input := &wizardInput{
		lines:  []string{"", "1"},
		masked: []string{"test-setup-key-1234"},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:            cmd,
		input:          input,
		w:              stderr,
		out:            stdout,
		allowPlaintext: false,
	}

	err := wiz.run()
	if err == nil {
		t.Fatal("expected error when keychain unavailable and allowPlaintext is false")
	}
	if !strings.Contains(err.Error(), "keychain unavailable") {
		t.Errorf("expected keychain unavailable error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--allow-plaintext") {
		t.Errorf("expected --allow-plaintext suggestion, got: %v", err)
	}
}

func TestSetup_KeychainUnavailable_SucceedsWithFlag(t *testing.T) {
	keyring.MockInit()
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	ts := startMockJCServer(t, "org-setup-kc", "Setup KC Org", http.StatusOK)
	defer ts.Close()
	overrideAPIClient(t, ts.URL)

	// Simulate keychain unavailable.
	orig := keychainIsAvailable
	t.Cleanup(func() { keychainIsAvailable = orig })
	keychainIsAvailable = func() bool { return false }

	// Steps: profile(keep), auth-method(1), org(keep), output(keep), color(keep), limit(keep)
	input := &wizardInput{
		lines:  []string{"", "1", "", "", "", ""},
		masked: []string{"test-setup-key-1234"},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:            cmd,
		input:          input,
		w:              stderr,
		out:            stdout,
		allowPlaintext: true,
	}

	err := wiz.run()
	if err != nil {
		t.Fatalf("expected success with allowPlaintext, got: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Setup complete!") {
		t.Errorf("expected 'Setup complete!' in output, got %q", got)
	}

	// Verify plaintext warning was shown.
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "plaintext") {
		t.Errorf("expected plaintext warning on stderr, got %q", stderrStr)
	}
}

func TestSetup_ServiceAccount_OrgFetch403(t *testing.T) {
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

	// Steps: profile(keep), auth-method(2), client-id, org(keep), output(keep), color(keep), limit(keep)
	input := &wizardInput{
		lines:  []string{"", "2", "test-sa-client-id", "", "", "", ""},
		masked: []string{"test-sa-secret"},
	}
	overrideSetupInput(t, input)

	cmd := &cobra.Command{}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	wiz := &setupWizard{
		cmd:   cmd,
		input: input,
		w:     stderr,
		out:   stdout,
	}

	err := wiz.run()
	if err != nil {
		t.Fatalf("setup wizard should succeed despite 403, got error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Auth Method:   service_account") {
		t.Errorf("expected service_account auth method in summary, got %q", got)
	}
	if !strings.Contains(got, "Client ID:     test-sa-client-id") {
		t.Errorf("expected client ID in summary, got %q", got)
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
	if !strings.Contains(cfgStr, "test-sa-client-id") {
		t.Errorf("config should contain client_id, got:\n%s", cfgStr)
	}

	// Verify client secret in keychain.
	stored, err := keyring.Get("jc", "default:client_secret")
	if err != nil {
		t.Fatalf("expected client secret in keychain: %v", err)
	}
	if stored != "test-sa-secret" {
		t.Errorf("keychain client secret = %q, want %q", stored, "test-sa-secret")
	}
}
