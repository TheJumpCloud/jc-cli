package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// withTempConfig writes a config file into a tmp dir and points
// JC_CONFIG at it, then re-initializes viper. Returns the path written.
// Mirrors the setupTest pattern in internal/mcp tests but lives here
// since cmd-package tests have their own conventions.
func withTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JC_CONFIG", path)
	viper.Reset()
	if err := viper.SafeWriteConfigAs(path); err != nil && !os.IsExist(err) {
		// SafeWriteConfigAs fails on existing files; that's fine.
	}
	viper.SetConfigFile(path)
	_ = viper.ReadInConfig()
	// Re-bind env vars the same way config.Init does so tests respect
	// JC_API_KEY / JC_PROFILE precedence rules.
	_ = viper.BindEnv("api_key", "JC_API_KEY")
	_ = viper.BindEnv("org_id", "JC_ORG_ID")
	_ = viper.BindEnv("active_profile", "JC_PROFILE")
	_ = viper.BindEnv("ask.api_key", "JC_ASK_API_KEY")
	return path
}

func TestFingerprint(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "(unset)"},
		{"abcd", "****"},          // ≤4 chars → all masked
		{"abcdefgh", "****efgh"},  // last 4 only
		{"sk-ant-1234567890abcd", "****abcd"},
	}
	for _, tc := range cases {
		if got := fingerprint(tc.in); got != tc.want {
			t.Errorf("fingerprint(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCollectBuild(t *testing.T) {
	b := collectBuild()
	if b.Version == "" {
		t.Error("build.version should not be empty")
	}
	if !strings.HasPrefix(b.GoVersion, "go") {
		t.Errorf("build.go_version = %q, want 'go...' prefix", b.GoVersion)
	}
	if !strings.Contains(b.OSArch, "/") {
		t.Errorf("build.os_arch = %q, want 'os/arch' shape", b.OSArch)
	}
}

func TestCollectProfile_JCProfileEnvOverridesConfig(t *testing.T) {
	withTempConfig(t, `
active_profile: from-config
profiles:
  from-config:
    api_key: ""
  staging:
    api_key: ""
`)
	t.Setenv("JC_PROFILE", "staging")
	// Refresh viper's env-bound view.
	_ = viper.BindEnv("active_profile", "JC_PROFILE")

	p := collectProfile()
	if p.Active != "staging" {
		t.Errorf("profile.active = %q, want 'staging' (env override)", p.Active)
	}
	if p.Source != "JC_PROFILE env" {
		t.Errorf("profile.source = %q, want 'JC_PROFILE env'", p.Source)
	}
	if len(p.Available) != 2 {
		t.Errorf("profile.available = %v, want [from-config, staging]", p.Available)
	}
}

func TestCollectConfig_ExistingFile(t *testing.T) {
	withTempConfig(t, "active_profile: default\n")
	// collectConfig reads paths from config.ConfigPath / ConfigDir,
	// which point at JC_CONFIG when set.
	c := collectConfig()
	if !c.Exists {
		t.Errorf("config.exists = false, want true (we just wrote the file)")
	}
	if c.FileMode == "" {
		t.Error("config.file_mode should be reported for existing file")
	}
}

func TestCollectAuth_JCAPIKeyEnvWins(t *testing.T) {
	withTempConfig(t, `
active_profile: default
profiles:
  default:
    api_key: "from-config-1234"
`)
	t.Setenv("JC_API_KEY", "from-env-9999")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth()
	if a.Source != "JC_API_KEY env" {
		t.Errorf("auth.source = %q, want 'JC_API_KEY env'", a.Source)
	}
	if a.Fingerprint != "****9999" {
		t.Errorf("auth.fingerprint = %q, want '****9999'", a.Fingerprint)
	}
}

func TestCollectAuth_KeychainReference(t *testing.T) {
	withTempConfig(t, `
active_profile: default
profiles:
  default:
    api_key: "keychain://jc/default"
`)
	// No JC_API_KEY env.
	t.Setenv("JC_API_KEY", "")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth()
	if !strings.HasPrefix(a.Source, "keychain") {
		t.Errorf("auth.source = %q, want 'keychain (...)' prefix", a.Source)
	}
	// Fingerprint may be "(keychain unavailable)" in a test env without
	// a real keychain entry — that's expected and a documented branch.
	if a.Fingerprint == "" {
		t.Error("auth.fingerprint should be populated even on keychain miss")
	}
}

func TestCollectAuth_PlaintextProfileConfig(t *testing.T) {
	withTempConfig(t, `
active_profile: default
profiles:
  default:
    api_key: "plain-text-abcd"
`)
	t.Setenv("JC_API_KEY", "")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth()
	if a.Source != "profile config (plaintext)" {
		t.Errorf("auth.source = %q, want 'profile config (plaintext)'", a.Source)
	}
	if a.Fingerprint != "****abcd" {
		t.Errorf("auth.fingerprint = %q, want '****abcd'", a.Fingerprint)
	}
}

func TestCollectAuth_Unset(t *testing.T) {
	withTempConfig(t, `
active_profile: default
profiles:
  default:
    api_key: ""
`)
	t.Setenv("JC_API_KEY", "")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth()
	if a.Source != "(unset)" {
		t.Errorf("auth.source = %q, want '(unset)'", a.Source)
	}
	if a.Fingerprint != "" {
		t.Errorf("auth.fingerprint = %q, want empty when unset", a.Fingerprint)
	}
}

func TestCollectLLM_EnvOverridesConfig(t *testing.T) {
	withTempConfig(t, `
ask:
  provider: anthropic
  api_key: "from-config-1234"
  model: "claude-3-5-sonnet"
`)
	t.Setenv("JC_ASK_API_KEY", "from-env-9876")
	_ = viper.BindEnv("ask.api_key", "JC_ASK_API_KEY")

	l := collectLLM()
	if l.Provider != "anthropic" {
		t.Errorf("llm.provider = %q, want 'anthropic'", l.Provider)
	}
	if l.APIKeySource != "JC_ASK_API_KEY env" {
		t.Errorf("llm.api_key_source = %q, want 'JC_ASK_API_KEY env'", l.APIKeySource)
	}
	if l.APIKey != "****9876" {
		t.Errorf("llm.api_key = %q, want '****9876'", l.APIKey)
	}
}

func TestRunAPIProbe_OK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	withTempConfig(t, `
active_profile: default
profiles:
  default:
    api_key: "test-1234"
`)

	probe := runAPIProbe(context.Background(), ts.URL, 2*time.Second)
	if probe == nil {
		t.Fatal("probe is nil")
	}
	if probe.Status != "ok" {
		t.Errorf("probe.status = %q, want 'ok' (HTTP 200)", probe.Status)
	}
	if probe.StatusCode != 200 {
		t.Errorf("probe.status_code = %d, want 200", probe.StatusCode)
	}
	if probe.LatencyMS < 0 {
		t.Errorf("probe.latency_ms = %d, want non-negative", probe.LatencyMS)
	}
}

func TestRunAPIProbe_AuthFailed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	withTempConfig(t, "")
	probe := runAPIProbe(context.Background(), ts.URL, 2*time.Second)
	if probe.Status != "auth_failed" {
		t.Errorf("probe.status = %q, want 'auth_failed' (HTTP 401)", probe.Status)
	}
}

func TestRunAPIProbe_Unreachable(t *testing.T) {
	withTempConfig(t, "")
	// Pick a port no real server is listening on.
	probe := runAPIProbe(context.Background(), "http://127.0.0.1:1", 500*time.Millisecond)
	if probe.Status != "unreachable" {
		t.Errorf("probe.status = %q, want 'unreachable'", probe.Status)
	}
	if probe.Error == "" {
		t.Error("probe.error should be populated on connection failure")
	}
}

func TestPrintDoctorJSON_RoundTrip(t *testing.T) {
	withTempConfig(t, "active_profile: default\n")
	rep := collectDoctorReport(context.Background(), false, 0)

	var buf bytes.Buffer
	if err := printDoctorJSON(&buf, rep); err != nil {
		t.Fatalf("printDoctorJSON: %v", err)
	}
	var out doctorReport
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, buf.String())
	}
	if out.Build.Version != rep.Build.Version {
		t.Errorf("round-trip mismatch on build.version: got %q, want %q",
			out.Build.Version, rep.Build.Version)
	}
}

func TestPrintDoctorText_IncludesAllSections(t *testing.T) {
	withTempConfig(t, "active_profile: default\n")
	rep := collectDoctorReport(context.Background(), false, 0)

	var buf bytes.Buffer
	if err := printDoctorText(&buf, rep); err != nil {
		t.Fatalf("printDoctorText: %v", err)
	}
	out := buf.String()
	for _, section := range []string{"Build", "Profile", "Config", "Auth", "API", "LLM", "MCP"} {
		if !strings.Contains(out, "▸ "+section) {
			t.Errorf("text output missing section %q:\n%s", section, out)
		}
	}
	// No-probe path must surface explicitly so the operator knows.
	if !strings.Contains(out, "skipped via --no-probe") {
		t.Error("text output should note when probe was skipped")
	}
}

// TestPrintDoctorText_NeverPrintsRawSecrets is the load-bearing
// contract: no raw API key, no raw OAuth secret, no raw LLM key
// ever appears in the rendered output. We seed the config with
// distinctive plaintext values and assert their literal absence.
func TestPrintDoctorText_NeverPrintsRawSecrets(t *testing.T) {
	withTempConfig(t, `
active_profile: default
profiles:
  default:
    api_key: "RAW-SECRET-DO-NOT-LEAK-12345"
ask:
  provider: anthropic
  api_key: "ASK-SECRET-ALSO-NOT-12345"
`)

	rep := collectDoctorReport(context.Background(), false, 0)

	var buf bytes.Buffer
	if err := printDoctorText(&buf, rep); err != nil {
		t.Fatalf("printDoctorText: %v", err)
	}
	out := buf.String()
	for _, secret := range []string{
		"RAW-SECRET-DO-NOT-LEAK",
		"ASK-SECRET-ALSO-NOT",
	} {
		if strings.Contains(out, secret) {
			t.Errorf("text output leaked raw secret %q:\n%s", secret, out)
		}
	}

	var jsonBuf bytes.Buffer
	if err := printDoctorJSON(&jsonBuf, rep); err != nil {
		t.Fatalf("printDoctorJSON: %v", err)
	}
	jsonOut := jsonBuf.String()
	for _, secret := range []string{
		"RAW-SECRET-DO-NOT-LEAK",
		"ASK-SECRET-ALSO-NOT",
	} {
		if strings.Contains(jsonOut, secret) {
			t.Errorf("json output leaked raw secret %q:\n%s", secret, jsonOut)
		}
	}
}
