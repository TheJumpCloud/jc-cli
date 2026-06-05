package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
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

	a := collectAuth(false) // flag not set
	if a.Source != "JC_API_KEY env" {
		t.Errorf("auth.source = %q, want 'JC_API_KEY env'", a.Source)
	}
	if a.Fingerprint != "****9999" {
		t.Errorf("auth.fingerprint = %q, want '****9999'", a.Fingerprint)
	}
}

// Bugbot finding (Low) on PR #42: when both --api-key flag and
// JC_API_KEY env are set to the *same value*, the original code
// misattributed to env because it compared resolved == env. Cobra
// flag precedence treats flag as the override; honor that.
func TestCollectAuth_FlagBeatsEnvEvenWhenEqual(t *testing.T) {
	withTempConfig(t, "active_profile: default\n")
	t.Setenv("JC_API_KEY", "same-value-1234")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth(true) // flag IS set
	if a.Source != "--api-key flag" {
		t.Errorf("auth.source = %q, want '--api-key flag' when flag is set (even if env matches)", a.Source)
	}
}

// Bugbot finding (Medium) on PR #42: service_account profiles don't
// carry an api_key — they use OAuth client credentials. The original
// code reported "(unset)" which scared operators into thinking auth
// was broken when actually the probe and every other jc command
// worked fine.
func TestCollectAuth_ServiceAccountReportsOAuth(t *testing.T) {
	withTempConfig(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
    client_id: "test-client-id-abcd"
    client_secret: "test-secret"
`)
	t.Setenv("JC_API_KEY", "")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth(false)
	if a.Method != "service_account" {
		t.Errorf("auth.method = %q, want 'service_account'", a.Method)
	}
	if a.Source != "service_account (OAuth)" {
		t.Errorf("auth.source = %q, want 'service_account (OAuth)' (not '(unset)')", a.Source)
	}
	if a.Fingerprint != "****abcd" {
		t.Errorf("auth.fingerprint = %q, want '****abcd' (client_id last 4)", a.Fingerprint)
	}
}

// Bugbot finding #3 (Medium) on PR #42: when the active profile uses
// service_account but JC_API_KEY *also* happens to be in the env (e.g.
// left over from a different profile or session), the original code
// reported "JC_API_KEY env" — but api.NewClient() short-circuits to
// OAuth Bearer when AuthMethod() == service_account with valid client
// creds, so the reported source didn't match what jc actually uses.
// Fix: short-circuit collectAuth on service_account first.
func TestCollectAuth_ServiceAccountWinsOverStrayEnvKey(t *testing.T) {
	withTempConfig(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
    client_id: "test-client-id-1234"
    client_secret: "test-secret"
`)
	t.Setenv("JC_API_KEY", "stray-env-key-from-elsewhere")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth(false)
	if a.Source != "service_account (OAuth)" {
		t.Errorf("auth.source = %q, want 'service_account (OAuth)' "+
			"(stray JC_API_KEY env must not override service_account OAuth)", a.Source)
	}
	if a.Fingerprint == "****eere" { // last 4 of "elsewhere"
		t.Errorf("auth.fingerprint = %q, leaked stray env key into report", a.Fingerprint)
	}
}

// Companion: service_account without valid client credentials AND
// without any api_key fallback — neither auth path will work.
func TestCollectAuth_ServiceAccountMissingClientCreds(t *testing.T) {
	withTempConfig(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
`)
	t.Setenv("JC_API_KEY", "")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth(false)
	if a.Source != "service_account (no client credentials)" {
		t.Errorf("auth.source = %q, want 'service_account (no client credentials)'", a.Source)
	}
}

// Bugbot finding #5 (Medium) on PR #42: when client_secret is a
// keychain:// reference but keychain resolution fails (locked,
// deleted, permission denied), config.ClientSecret() returns "" and
// my code falsely reported "no client credentials" — same status as
// "never configured." The operator chasing a "missing credentials"
// message would re-enter their client_secret instead of fixing the
// keychain. Fix: peek at the raw config value to distinguish
// keychain miss from never-configured.
func TestCollectAuth_ServiceAccountKeychainSecretMissing(t *testing.T) {
	withTempConfig(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
    client_id: "test-client-id-9876"
    client_secret: "keychain://jc/default-secret"
`)
	t.Setenv("JC_API_KEY", "")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth(false)
	// Expected: a status that names the keychain failure specifically
	// — NOT "no client credentials" (which would point at the wrong
	// remediation).
	if !strings.Contains(a.Source, "keychain unavailable") {
		t.Errorf("auth.source = %q, want a 'keychain unavailable' message", a.Source)
	}
	if strings.Contains(a.Source, "no client credentials") {
		t.Errorf("auth.source = %q, keychain failure must NOT be reported as 'no client credentials'", a.Source)
	}
	// client_id is still good — fingerprint it so the operator can
	// confirm the right service account is configured even when the
	// secret is unreadable.
	if a.Fingerprint != "****9876" {
		t.Errorf("auth.fingerprint = %q, want '****9876' (client_id last 4)", a.Fingerprint)
	}
}

// Bugbot finding #6 (Medium) on PR #42: a service_account profile
// with an unresolvable client_secret keychain ref AND an api_key
// fallback (env / flag / profile) — api.NewClient() drops to the
// api_key path, but my code (after the #5 fix) returned early on the
// keychain-unavailable branch and reported method as service_account.
// Auth section disagreed with the probe + every other jc command.
// Fix: only return early on keychain failure when there's no api_key
// fallback; otherwise re-label method to surface both the fallback
// AND the keychain miss.
func TestCollectAuth_ServiceAccountKeychainFailureFallsBackToAPIKey(t *testing.T) {
	withTempConfig(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
    client_id: "test-client-id-aaaa"
    client_secret: "keychain://jc/default-secret"
    api_key: "fallback-key-bbbb"
`)
	t.Setenv("JC_API_KEY", "")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth(false)
	// Method must announce both the fallback AND the keychain miss
	// so the operator sees both facts in one line.
	if !strings.Contains(a.Method, "api_key") {
		t.Errorf("auth.method = %q, want 'api_key (...)' (service_account fell back to api_key)", a.Method)
	}
	if !strings.Contains(a.Method, "keychain unavailable") {
		t.Errorf("auth.method = %q, want it to surface the keychain miss that caused the fallback", a.Method)
	}
	// Source should describe where the api_key came from (the fallback
	// path's resolution), not the dead OAuth attempt.
	if a.Source != "profile config (plaintext)" {
		t.Errorf("auth.source = %q, want 'profile config (plaintext)' (the api_key fallback's source)", a.Source)
	}
	if a.Fingerprint != "****bbbb" {
		t.Errorf("auth.fingerprint = %q, want '****bbbb' (the api_key fallback's last 4)", a.Fingerprint)
	}
}

// Bugbot finding #4 (Medium) on PR #42: when AuthMethod is
// service_account but client credentials are missing, api.NewClient()
// silently falls through to the api_key resolution path. If an api_key
// is available (via flag/env/keychain/config), every other jc command
// uses x-api-key auth — but the doctor was reporting `method:
// service_account` with the api-key source, which was internally
// inconsistent. Fix: re-label the method as `api_key (service_account
// fallback)` so the operator sees both that their service_account
// config is broken AND that jc is actually using an API key.
func TestCollectAuth_ServiceAccountFallsBackToAPIKey(t *testing.T) {
	withTempConfig(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
    api_key: "fallback-key-9999"
`)
	t.Setenv("JC_API_KEY", "")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth(false)
	if a.Method != "api_key (service_account fallback)" {
		t.Errorf("auth.method = %q, want 'api_key (service_account fallback)'", a.Method)
	}
	// Source should still describe where the *api_key* came from —
	// in this case, the profile config.
	if a.Source != "profile config (plaintext)" {
		t.Errorf("auth.source = %q, want 'profile config (plaintext)' (the api_key fallback's actual source)", a.Source)
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
	t.Setenv("JC_API_KEY", "")
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	a := collectAuth(false)
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

	a := collectAuth(false)
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

	a := collectAuth(false)
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

// runAPIProbe now routes through api.NewV2Client (rather than a
// hand-rolled HTTP request) so it correctly exercises whichever auth
// method the active profile uses. We test the error-classification
// layer directly via classifyProbeError so we don't need to mock the
// entire HTTP client; the end-to-end "real client makes a real call"
// path is covered by the api package's own tests.

func TestClassifyProbeError_Success(t *testing.T) {
	p := classifyProbeError(nil)
	if p.Status != "ok" {
		t.Errorf("Status = %q, want 'ok'", p.Status)
	}
	if p.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", p.StatusCode)
	}
}

func TestClassifyProbeError_AuthFailed(t *testing.T) {
	cases := []int{http.StatusUnauthorized, http.StatusForbidden}
	for _, code := range cases {
		err := &api.APIError{StatusCode: code, Message: "denied", Endpoint: "/usergroups"}
		p := classifyProbeError(err)
		if p.Status != "auth_failed" {
			t.Errorf("HTTP %d → status = %q, want 'auth_failed'", code, p.Status)
		}
		if p.StatusCode != code {
			t.Errorf("HTTP %d → status_code = %d, want %d", code, p.StatusCode, code)
		}
	}
}

func TestClassifyProbeError_OtherHTTP(t *testing.T) {
	err := &api.APIError{StatusCode: 500, Message: "server error", Endpoint: "/usergroups"}
	p := classifyProbeError(err)
	if p.Status != "http_500" {
		t.Errorf("Status = %q, want 'http_500'", p.Status)
	}
}

func TestClassifyProbeError_TransportFailure(t *testing.T) {
	// Non-APIError → unreachable. A DNS-failure-style error fits the
	// real shape: it's not an HTTP error, just a wrapped transport err.
	err := fmt.Errorf("dial tcp: lookup failed: no such host")
	p := classifyProbeError(err)
	if p.Status != "unreachable" {
		t.Errorf("Status = %q, want 'unreachable'", p.Status)
	}
	if p.Error == "" {
		t.Error("Error should be populated on transport failure")
	}
}

// Bugbot finding (Medium) on PR #42: service-account profiles that
// fail at the OAuth token exchange surface their errors as plain
// `error` strings — *api.APIError isn't involved because the failure
// happens before the API call. Pre-fix those landed in the
// "unreachable" bucket and operators saw network-trouble suggestions
// instead of "check your client credentials." Pin the classification
// against the exact phrases internal/api/oauth.go emits.
func TestClassifyProbeError_OAuthInvalidClient(t *testing.T) {
	// Shape matches what bearerAuthTransport returns when oauth.go's
	// fetchToken hits HTTP 401:
	//   "failed to obtain bearer token: invalid client credentials
	//    (HTTP 401). Check your client ID and secret"
	err := fmt.Errorf("failed to obtain bearer token: invalid client credentials (HTTP 401). Check your client ID and secret")
	p := classifyProbeError(err)
	if p.Status != "auth_failed" {
		t.Errorf("Status = %q, want 'auth_failed' (OAuth 401 must NOT be unreachable)", p.Status)
	}
	if p.Error == "" {
		t.Error("Error should carry the original OAuth message for the operator")
	}
}

func TestClassifyProbeError_OAuthInsufficientScope(t *testing.T) {
	err := fmt.Errorf("failed to obtain bearer token: client credentials lack permission (HTTP 403). Verify the service account scope")
	p := classifyProbeError(err)
	if p.Status != "auth_failed" {
		t.Errorf("Status = %q, want 'auth_failed' (OAuth 403 must NOT be unreachable)", p.Status)
	}
}

// Bugbot finding (Medium) on PR #42: only "json" was honored as a
// global output format; "yaml" / "table" / "csv" / "ndjson" / "human"
// all silently fell through to text. The fix renders YAML when asked,
// renders text for "human"/"text"/empty, and surfaces a stderr note
// when an unsupported format is requested rather than silently
// downgrading.
func TestPrintDoctorYAML_RoundTrip(t *testing.T) {
	withTempConfig(t, "active_profile: default\n")
	rep := collectDoctorReport(context.Background(), false, 0, false)

	var buf bytes.Buffer
	if err := printDoctorYAML(&buf, rep); err != nil {
		t.Fatalf("printDoctorYAML: %v", err)
	}
	out := buf.String()
	// YAML output must include the section keys and not be empty.
	for _, key := range []string{"build:", "profile:", "config:", "auth:", "api:", "llm:", "mcp:"} {
		if !strings.Contains(out, key) {
			t.Errorf("YAML output missing %q:\n%s", key, out)
		}
	}
	// Same load-bearing secret contract as the JSON/text formats.
	if strings.Contains(out, "RAW-SECRET-DO-NOT-LEAK") {
		t.Error("YAML output leaked a raw secret")
	}
}

func TestPrintDoctorJSON_RoundTrip(t *testing.T) {
	withTempConfig(t, "active_profile: default\n")
	rep := collectDoctorReport(context.Background(), false, 0, false)

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
	rep := collectDoctorReport(context.Background(), false, 0, false)

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

	rep := collectDoctorReport(context.Background(), false, 0, false)

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
