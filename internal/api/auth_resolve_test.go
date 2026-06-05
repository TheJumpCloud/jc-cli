package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/config"
)

// resolveAuthFixture writes a config file and re-initializes config so
// ResolveActiveAuth sees the expected state. Returns the path written.
// Mirrors the existing client_test.go setup pattern.
func resolveAuthFixture(t *testing.T, body string) string {
	t.Helper()
	resetViper()
	t.Cleanup(resetViper)

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(cfgPath, []byte(body), 0o600)
	if err := config.Init(); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	return cfgPath
}

// --- Fingerprint --------------------------------------------------------

func TestFingerprint(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "(unset)"},
		{"abcd", "****"},
		{"abcdefgh", "****efgh"},
		{"sk-ant-1234567890abcd", "****abcd"},
	}
	for _, tc := range cases {
		if got := Fingerprint(tc.in); got != tc.want {
			t.Errorf("Fingerprint(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- Service-account branches -------------------------------------------

func TestResolveActiveAuth_ServiceAccountOAuth(t *testing.T) {
	resolveAuthFixture(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
    client_id: "test-client-id-abcd"
    client_secret: "test-secret"
`)
	t.Setenv("JC_API_KEY", "")

	r := ResolveActiveAuth(Hint{})
	if r.Method != "service_account" {
		t.Errorf("Method = %q, want service_account", r.Method)
	}
	if r.Source != "service_account (OAuth)" {
		t.Errorf("Source = %q, want 'service_account (OAuth)'", r.Source)
	}
	if r.Fingerprint != "****abcd" {
		t.Errorf("Fingerprint = %q, want '****abcd'", r.Fingerprint)
	}
	if r.TokenCache() == nil {
		t.Error("TokenCache should be populated for OAuth path")
	}
}

// Pre-fix (PR #42 Bugbot #3): a stray JC_API_KEY would mislabel an
// otherwise-OAuth profile. The architectural fix prevents that.
func TestResolveActiveAuth_ServiceAccountStrayEnvKeyDoesntOverride(t *testing.T) {
	resolveAuthFixture(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
    client_id: "id-aaaa"
    client_secret: "sec"
`)
	t.Setenv("JC_API_KEY", "stray-env-key")

	r := ResolveActiveAuth(Hint{})
	if r.Source != "service_account (OAuth)" {
		t.Errorf("Source = %q, want OAuth (stray env must not override)", r.Source)
	}
}

func TestResolveActiveAuth_ServiceAccountFallsBackToAPIKey(t *testing.T) {
	resolveAuthFixture(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
    api_key: "fallback-key-1234"
`)
	t.Setenv("JC_API_KEY", "")

	r := ResolveActiveAuth(Hint{})
	if !strings.Contains(r.Method, "api_key") {
		t.Errorf("Method = %q, want fallback label", r.Method)
	}
	if !strings.Contains(r.Method, "fallback") {
		t.Errorf("Method = %q, want explicit fallback label", r.Method)
	}
	if r.Fingerprint != "****1234" {
		t.Errorf("Fingerprint = %q, want '****1234'", r.Fingerprint)
	}
	if r.APIKey() == "" {
		t.Error("APIKey should be populated on fallback path")
	}
}

func TestResolveActiveAuth_ServiceAccountNoCredsAtAll(t *testing.T) {
	resolveAuthFixture(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
`)
	t.Setenv("JC_API_KEY", "")

	r := ResolveActiveAuth(Hint{})
	if r.Source != "service_account (no client credentials)" {
		t.Errorf("Source = %q, want 'service_account (no client credentials)'", r.Source)
	}
	if r.APIKey() != "" || r.TokenCache() != nil {
		t.Error("no-creds case should produce neither APIKey nor TokenCache")
	}
}

// --- API-key branches ---------------------------------------------------

func TestResolveActiveAuth_FlagBeatsEnvWhenEqual(t *testing.T) {
	resolveAuthFixture(t, "active_profile: default\n")
	t.Setenv("JC_API_KEY", "same-value-1234")

	r := ResolveActiveAuth(Hint{APIKeyFlagChanged: true})
	if r.Source != "--api-key flag" {
		t.Errorf("Source = %q, want '--api-key flag' when flag is set", r.Source)
	}
}

func TestResolveActiveAuth_JCAPIKeyEnv(t *testing.T) {
	resolveAuthFixture(t, "active_profile: default\n")
	t.Setenv("JC_API_KEY", "env-key-9876")

	r := ResolveActiveAuth(Hint{})
	if r.Source != "JC_API_KEY env" {
		t.Errorf("Source = %q, want 'JC_API_KEY env'", r.Source)
	}
	if r.Fingerprint != "****9876" {
		t.Errorf("Fingerprint = %q, want '****9876'", r.Fingerprint)
	}
}

func TestResolveActiveAuth_PlaintextProfile(t *testing.T) {
	resolveAuthFixture(t, `
active_profile: default
profiles:
  default:
    api_key: "plain-abcd"
`)
	t.Setenv("JC_API_KEY", "")

	r := ResolveActiveAuth(Hint{})
	if r.Source != "profile config (plaintext)" {
		t.Errorf("Source = %q, want 'profile config (plaintext)'", r.Source)
	}
	if r.Fingerprint != "****abcd" {
		t.Errorf("Fingerprint = %q, want '****abcd'", r.Fingerprint)
	}
}

func TestResolveActiveAuth_Unset(t *testing.T) {
	resolveAuthFixture(t, "active_profile: default\n")
	t.Setenv("JC_API_KEY", "")

	r := ResolveActiveAuth(Hint{})
	if r.Source != "(unset)" {
		t.Errorf("Source = %q, want '(unset)'", r.Source)
	}
}

// --- Bugbot #13 fixture (the architectural-fix proof point) -------------

// When a service_account profile has a `client_secret` keychain ref
// that won't resolve AND `api_key` is also a keychain ref that won't
// resolve, the doctor's old hasAPIKey peek (raw non-empty) would have
// said "fallback to api_key" while NewClient would return
// ErrNoAPIKey. With the shared resolver, both layers see the same
// resolution: no auth available, ErrNoAPIKey, and Source surfaces the
// actual cause.
//
// Reference: PR #42 Bugbot finding #13 (BUGBOT_BUG_ID fef17827).
func TestResolveActiveAuth_KeychainRefAPIKeyDoesntCountAsFallback(t *testing.T) {
	resolveAuthFixture(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
    api_key: "keychain://jc/nonexistent-key"
`)
	t.Setenv("JC_API_KEY", "")

	r := ResolveActiveAuth(Hint{})
	// Must NOT claim api_key fallback — the keychain ref doesn't
	// resolve, so there's no real api_key available.
	if strings.Contains(r.Method, "fallback") {
		t.Errorf("Method = %q, want NO fallback (keychain doesn't resolve to a real key)", r.Method)
	}
	if r.APIKey() != "" {
		t.Errorf("APIKey = %q, want empty (keychain miss)", r.APIKey())
	}
	if r.TokenCache() != nil {
		t.Error("TokenCache should be nil (no client_secret)")
	}
}

// And the matching NewClient assertion — proves the two layers agree.
// This is the cross-package parity test that wouldn't have existed in
// the duplicated-precedence world.
func TestNewClient_KeychainRefAPIKeyReturnsErrNoAPIKey(t *testing.T) {
	resolveAuthFixture(t, `
active_profile: default
profiles:
  default:
    auth_method: service_account
    api_key: "keychain://jc/nonexistent-key"
`)
	t.Setenv("JC_API_KEY", "")

	_, err := NewClient()
	if err == nil {
		t.Fatal("expected ErrNoAPIKey (keychain doesn't resolve to a real key), got nil")
	}
	if err != ErrNoAPIKey {
		t.Errorf("got %v, want ErrNoAPIKey", err)
	}
}

// --- Org ID precedence --------------------------------------------------

func TestResolveActiveAuth_OrgIDTopLevelBeatsProfile(t *testing.T) {
	resolveAuthFixture(t, `
active_profile: default
org_id: top-level-7777
profiles:
  default:
    api_key: "test-1234"
    org_id: profile-9999
`)
	t.Setenv("JC_ORG_ID", "")
	t.Setenv("JC_API_KEY", "")

	r := ResolveActiveAuth(Hint{})
	if r.OrgID != "top-level-7777" {
		t.Errorf("OrgID = %q, want 'top-level-7777' (top-level beats profile)", r.OrgID)
	}
	if r.OrgIDSource != "top-level config" {
		t.Errorf("OrgIDSource = %q, want 'top-level config'", r.OrgIDSource)
	}
}

func TestResolveActiveAuth_OrgIDEnvWins(t *testing.T) {
	resolveAuthFixture(t, `
active_profile: default
org_id: top-level-7777
profiles:
  default:
    api_key: "test-1234"
`)
	t.Setenv("JC_ORG_ID", "env-1111")
	t.Setenv("JC_API_KEY", "")
	// Re-init viper to pick up the env-binding.
	_ = viper.BindEnv("org_id", "JC_ORG_ID")

	r := ResolveActiveAuth(Hint{})
	if r.OrgIDSource != "JC_ORG_ID env" {
		t.Errorf("OrgIDSource = %q, want 'JC_ORG_ID env'", r.OrgIDSource)
	}
}

func TestResolveActiveAuth_OrgIDProfileFallback(t *testing.T) {
	resolveAuthFixture(t, `
active_profile: default
profiles:
  default:
    api_key: "test-1234"
    org_id: only-profile-2222
`)
	t.Setenv("JC_ORG_ID", "")
	t.Setenv("JC_API_KEY", "")

	r := ResolveActiveAuth(Hint{})
	if r.OrgID != "only-profile-2222" {
		t.Errorf("OrgID = %q, want 'only-profile-2222'", r.OrgID)
	}
	if r.OrgIDSource != "profile config" {
		t.Errorf("OrgIDSource = %q, want 'profile config'", r.OrgIDSource)
	}
}
