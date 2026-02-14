package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"

	kc "github.com/klaassen-consulting/jc/internal/keychain"
)

// resetViper clears Viper's global state between tests.
func resetViper() {
	viper.Reset()
}

func TestConfigDir_Default(t *testing.T) {
	t.Setenv("JC_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	dir := ConfigDir()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "jc")
	if dir != want {
		t.Errorf("ConfigDir() = %q, want %q", dir, want)
	}
}

func TestConfigDir_XDG(t *testing.T) {
	t.Setenv("JC_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")

	dir := ConfigDir()
	want := filepath.Join("/tmp/xdg", "jc")
	if dir != want {
		t.Errorf("ConfigDir() = %q, want %q", dir, want)
	}
}

func TestConfigDir_JCConfig(t *testing.T) {
	t.Setenv("JC_CONFIG", "/custom/path/myconfig.yaml")

	dir := ConfigDir()
	want := "/custom/path"
	if dir != want {
		t.Errorf("ConfigDir() = %q, want %q", dir, want)
	}
}

func TestConfigPath_Default(t *testing.T) {
	t.Setenv("JC_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	p := ConfigPath()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "jc", "config.yaml")
	if p != want {
		t.Errorf("ConfigPath() = %q, want %q", p, want)
	}
}

func TestConfigPath_JCConfig(t *testing.T) {
	t.Setenv("JC_CONFIG", "/custom/config.yaml")

	p := ConfigPath()
	want := "/custom/config.yaml"
	if p != want {
		t.Errorf("ConfigPath() = %q, want %q", p, want)
	}
}

func TestInit_CreatesConfigFile(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Verify file was created.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("config file is empty")
	}

	// Verify content has expected sections.
	content := string(data)
	for _, section := range []string{"active_profile:", "defaults:", "cache:", "profiles:"} {
		if !strings.Contains(content, section) {
			t.Errorf("config file missing section %q", section)
		}
	}
}

func TestInit_DirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission checks not applicable on Windows")
	}
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("cannot stat config dir: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0700 {
		t.Errorf("config dir perm = %o, want 0700", perm)
	}
}

func TestInit_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission checks not applicable on Windows")
	}
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("cannot stat config file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("config file perm = %o, want 0600", perm)
	}
}

func TestInit_DoesNotOverwriteExisting(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	// Create a custom config before Init.
	_ = os.MkdirAll(dir, 0700)
	custom := []byte("active_profile: production\ndefaults:\n  output: table\n")
	_ = os.WriteFile(cfgPath, custom, 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "production") {
		t.Error("Init() overwrote existing config file")
	}
}

func TestInit_ViperReadsValues(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	// Write a config with non-default values.
	_ = os.MkdirAll(dir, 0700)
	custom := []byte("active_profile: myorg\ndefaults:\n  output: table\n  limit: 50\n  confirm_destructive: false\ncache:\n  enabled: false\n  ttl: 600\n")
	_ = os.WriteFile(cfgPath, custom, 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	tests := []struct {
		key  string
		want interface{}
	}{
		{"active_profile", "myorg"},
		{"defaults.output", "table"},
		{"defaults.limit", 50},
		{"defaults.confirm_destructive", false},
		{"cache.enabled", false},
		{"cache.ttl", 600},
	}
	for _, tt := range tests {
		got := viper.Get(tt.key)
		if got != tt.want {
			t.Errorf("viper.Get(%q) = %v (%T), want %v (%T)", tt.key, got, got, tt.want, tt.want)
		}
	}
}

func TestInit_DefaultValues(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Defaults loaded from the default config or Viper defaults.
	if got := viper.GetString("active_profile"); got != "default" {
		t.Errorf("active_profile = %q, want %q", got, "default")
	}
	if got := viper.GetString("defaults.output"); got != "json" {
		t.Errorf("defaults.output = %q, want %q", got, "json")
	}
	if got := viper.GetInt("defaults.limit"); got != 100 {
		t.Errorf("defaults.limit = %d, want 100", got)
	}
	if got := viper.GetBool("defaults.confirm_destructive"); !got {
		t.Error("defaults.confirm_destructive should be true")
	}
	if got := viper.GetBool("cache.enabled"); !got {
		t.Error("cache.enabled should be true")
	}
	if got := viper.GetInt("cache.ttl"); got != 300 {
		t.Errorf("cache.ttl = %d, want 300", got)
	}
}

func TestInit_InvalidYAML(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	// Write invalid YAML.
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("active_profile: [\ninvalid yaml\n"), 0600)

	err := Init()
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), cfgPath) {
		t.Errorf("error should include config file path, got: %v", err)
	}
}

func TestInit_ProfilesSection(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	_ = os.MkdirAll(dir, 0700)
	custom := []byte(`active_profile: prod
profiles:
  prod:
    api_key: "key-prod"
    org_id: "org-prod"
  staging:
    api_key: "key-staging"
    org_id: "org-staging"
`)
	_ = os.WriteFile(cfgPath, custom, 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if got := viper.GetString("profiles.prod.api_key"); got != "key-prod" {
		t.Errorf("profiles.prod.api_key = %q, want %q", got, "key-prod")
	}
	if got := viper.GetString("profiles.staging.org_id"); got != "org-staging" {
		t.Errorf("profiles.staging.org_id = %q, want %q", got, "org-staging")
	}
}

func TestInit_JCConfigEnvOverridesPath(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	customPath := filepath.Join(tmp, "custom", "my-jc-config.yaml")
	t.Setenv("JC_CONFIG", customPath)

	// Write config at the custom path.
	_ = os.MkdirAll(filepath.Dir(customPath), 0700)
	_ = os.WriteFile(customPath, []byte("active_profile: custom\ndefaults:\n  output: csv\n"), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if got := viper.GetString("active_profile"); got != "custom" {
		t.Errorf("active_profile = %q, want %q", got, "custom")
	}
	if got := viper.GetString("defaults.output"); got != "csv" {
		t.Errorf("defaults.output = %q, want %q", got, "csv")
	}
}

// --- Environment Variable Tests (US-003) ---

func TestEnv_JCOutputOverridesConfig(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_OUTPUT", "table")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("defaults:\n  output: csv\n"), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if got := Output(); got != "table" {
		t.Errorf("Output() = %q, want %q (JC_OUTPUT should override config)", got, "table")
	}
}

func TestEnv_JCAPIKeyOverridesConfig(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "env-key-1234")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("active_profile: default\nprofiles:\n  default:\n    api_key: config-key-5678\n"), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if got := APIKey(); got != "env-key-1234" {
		t.Errorf("APIKey() = %q, want %q (JC_API_KEY should override config)", got, "env-key-1234")
	}
}

func TestEnv_JCOrgIDOverridesConfig(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_ORG_ID", "env-org-abc")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("active_profile: default\nprofiles:\n  default:\n    org_id: config-org-xyz\n"), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if got := OrgID(); got != "env-org-abc" {
		t.Errorf("OrgID() = %q, want %q (JC_ORG_ID should override config)", got, "env-org-abc")
	}
}

func TestEnv_JCProfileOverridesConfig(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_PROFILE", "staging")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("active_profile: production\n"), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if got := ActiveProfile(); got != "staging" {
		t.Errorf("ActiveProfile() = %q, want %q (JC_PROFILE should override config)", got, "staging")
	}
}

func TestEnv_JCNoColorDisablesColor(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_NO_COLOR", "1")

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if !NoColor() {
		t.Error("NoColor() should return true when JC_NO_COLOR is set")
	}
}

func TestEnv_StandardNoColorEnv(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("NO_COLOR", "true")

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if !NoColor() {
		t.Error("NoColor() should return true when NO_COLOR is set")
	}
}

func TestEnv_NoColorFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		jcColor  string
		noColor  string
		wantBool bool
	}{
		{"JC_NO_COLOR set", "1", "", true},
		{"NO_COLOR set", "", "1", true},
		{"both set", "true", "true", true},
		{"neither set", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("JC_NO_COLOR", tt.jcColor)
			t.Setenv("NO_COLOR", tt.noColor)
			if got := NoColorFromEnv(); got != tt.wantBool {
				t.Errorf("NoColorFromEnv() = %v, want %v", got, tt.wantBool)
			}
		})
	}
}

func TestEnv_ConfigFallbackWhenNoEnv(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	// Clear all JC env vars to test config fallback.
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_ORG_ID", "")
	t.Setenv("JC_PROFILE", "")
	t.Setenv("JC_OUTPUT", "")
	t.Setenv("JC_NO_COLOR", "")
	t.Setenv("NO_COLOR", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: myprofile
defaults:
  output: csv
  color: true
profiles:
  myprofile:
    api_key: "from-config"
    org_id: "org-from-config"
`), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if got := ActiveProfile(); got != "myprofile" {
		t.Errorf("ActiveProfile() = %q, want %q", got, "myprofile")
	}
	if got := APIKey(); got != "from-config" {
		t.Errorf("APIKey() = %q, want %q", got, "from-config")
	}
	if got := OrgID(); got != "org-from-config" {
		t.Errorf("OrgID() = %q, want %q", got, "org-from-config")
	}
	if got := Output(); got != "csv" {
		t.Errorf("Output() = %q, want %q", got, "csv")
	}
	if NoColor() {
		t.Error("NoColor() should be false when no env var set and config has color: true")
	}
}

func TestEnv_DefaultsFallbackWhenNoConfigOrEnv(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	// Clear all JC env vars.
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_ORG_ID", "")
	t.Setenv("JC_PROFILE", "")
	t.Setenv("JC_OUTPUT", "")
	t.Setenv("JC_NO_COLOR", "")
	t.Setenv("NO_COLOR", "")

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Should get built-in defaults.
	if got := ActiveProfile(); got != "default" {
		t.Errorf("ActiveProfile() = %q, want %q", got, "default")
	}
	if got := Output(); got != "json" {
		t.Errorf("Output() = %q, want %q", got, "json")
	}
	if got := APIKey(); got != "" {
		t.Errorf("APIKey() = %q, want empty string", got)
	}
	if got := OrgID(); got != "" {
		t.Errorf("OrgID() = %q, want empty string", got)
	}
}

func TestEnv_PriorityChain_EnvOverridesConfig(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	// Config says csv, env says table.
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("defaults:\n  output: csv\n"), 0600)
	t.Setenv("JC_OUTPUT", "table")

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Env should win over config.
	if got := Output(); got != "table" {
		t.Errorf("Output() = %q, want %q (env should override config)", got, "table")
	}
}

func TestEnv_JCProfileSelectsCorrectAPIKey(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_PROFILE", "staging")
	t.Setenv("JC_API_KEY", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: production
profiles:
  production:
    api_key: "prod-key"
    org_id: "prod-org"
  staging:
    api_key: "staging-key"
    org_id: "staging-org"
`), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// JC_PROFILE=staging should select the staging profile's API key.
	if got := APIKey(); got != "staging-key" {
		t.Errorf("APIKey() = %q, want %q", got, "staging-key")
	}
	if got := OrgID(); got != "staging-org" {
		t.Errorf("OrgID() = %q, want %q", got, "staging-org")
	}
}

// --- Keychain Integration Tests (US-005) ---

func TestAPIKey_KeychainRef(t *testing.T) {
	resetViper()
	defer resetViper()
	keyring.MockInit()

	// Store a key in the mock keychain.
	if err := kc.Set("myprofile", "secret-from-keychain"); err != nil {
		t.Fatalf("keychain.Set() error: %v", err)
	}

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")

	// Config references keychain.
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: myprofile
profiles:
  myprofile:
    api_key: "keychain://jc/myprofile"
    org_id: "org-123"
`), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	got := APIKey()
	if got != "secret-from-keychain" {
		t.Errorf("APIKey() = %q, want %q (should resolve keychain ref)", got, "secret-from-keychain")
	}
}

func TestAPIKey_KeychainRefFallsBackGracefully(t *testing.T) {
	resetViper()
	defer resetViper()
	// Simulate keychain unavailable.
	keyring.MockInitWithError(fmt.Errorf("keychain unavailable"))

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: default
profiles:
  default:
    api_key: "keychain://jc/default"
`), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Should return empty string (graceful fallback) when keychain fails.
	got := APIKey()
	if got != "" {
		t.Errorf("APIKey() = %q, want empty string (keychain unavailable should fallback)", got)
	}
}

func TestAPIKey_PlaintextStillWorks(t *testing.T) {
	resetViper()
	defer resetViper()
	keyring.MockInit()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: default
profiles:
  default:
    api_key: "plaintext-key-abcd"
`), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	got := APIKey()
	if got != "plaintext-key-abcd" {
		t.Errorf("APIKey() = %q, want %q (plaintext key should pass through)", got, "plaintext-key-abcd")
	}
}

func TestAPIKey_EnvOverridesKeychainRef(t *testing.T) {
	resetViper()
	defer resetViper()
	keyring.MockInit()

	_ = kc.Set("default", "keychain-secret")

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "env-override-key")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: default
profiles:
  default:
    api_key: "keychain://jc/default"
`), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	got := APIKey()
	if got != "env-override-key" {
		t.Errorf("APIKey() = %q, want %q (env var should take priority over keychain ref)", got, "env-override-key")
	}
}

func TestAPIKey_KeychainRefWithProfileSwitch(t *testing.T) {
	resetViper()
	defer resetViper()
	keyring.MockInit()

	_ = kc.Set("prod", "prod-secret")
	_ = kc.Set("staging", "staging-secret")

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_PROFILE", "staging")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: prod
profiles:
  prod:
    api_key: "keychain://jc/prod"
  staging:
    api_key: "keychain://jc/staging"
`), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	got := APIKey()
	if got != "staging-secret" {
		t.Errorf("APIKey() = %q, want %q (JC_PROFILE=staging should select staging keychain entry)", got, "staging-secret")
	}
}

// --- Config Set / Validation Tests (US-022) ---

func TestIsValidConfigKey(t *testing.T) {
	validKeys := []string{
		"active_profile",
		"defaults.output",
		"defaults.limit",
		"defaults.confirm_destructive",
		"defaults.color",
		"defaults.pager",
		"cache.enabled",
		"cache.ttl",
		"cache.directory",
	}
	for _, k := range validKeys {
		if !IsValidConfigKey(k) {
			t.Errorf("IsValidConfigKey(%q) = false, want true", k)
		}
	}

	invalidKeys := []string{"invalid", "foo.bar", "profiles.default.api_key", "defaults", "cache"}
	for _, k := range invalidKeys {
		if IsValidConfigKey(k) {
			t.Errorf("IsValidConfigKey(%q) = true, want false", k)
		}
	}
}

func TestSetConfigValue_String(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if err := SetConfigValue("defaults.output", "table"); err != nil {
		t.Fatalf("SetConfigValue() error: %v", err)
	}

	if got := viper.GetString("defaults.output"); got != "table" {
		t.Errorf("defaults.output = %q, want %q", got, "table")
	}
}

func TestSetConfigValue_Int(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if err := SetConfigValue("defaults.limit", "50"); err != nil {
		t.Fatalf("SetConfigValue() error: %v", err)
	}

	if got := viper.GetInt("defaults.limit"); got != 50 {
		t.Errorf("defaults.limit = %d, want 50", got)
	}
}

func TestSetConfigValue_Bool(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if err := SetConfigValue("defaults.confirm_destructive", "false"); err != nil {
		t.Fatalf("SetConfigValue() error: %v", err)
	}

	if got := viper.GetBool("defaults.confirm_destructive"); got != false {
		t.Errorf("defaults.confirm_destructive = %v, want false", got)
	}
}

// --- Profile Functions Tests (US-035) ---

func TestProfileNames_MultipleProfiles(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_PROFILE", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: default
profiles:
  default:
    api_key: ""
  production:
    api_key: ""
  staging:
    api_key: ""
`), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	names := ProfileNames()
	if len(names) != 3 {
		t.Fatalf("ProfileNames() returned %d names, want 3: %v", len(names), names)
	}
	// Should be sorted alphabetically.
	want := []string{"default", "production", "staging"}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("ProfileNames()[%d] = %q, want %q", i, names[i], n)
		}
	}
}

func TestProfileNames_Empty(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_PROFILE", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: default
`), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	names := ProfileNames()
	if len(names) != 0 {
		t.Errorf("ProfileNames() returned %d names, want 0: %v", len(names), names)
	}
}

func TestProfileExists(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_PROFILE", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: default
profiles:
  default:
    api_key: ""
  production:
    api_key: ""
`), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if !ProfileExists("default") {
		t.Error("ProfileExists('default') = false, want true")
	}
	if !ProfileExists("production") {
		t.Error("ProfileExists('production') = false, want true")
	}
	if ProfileExists("nonexistent") {
		t.Error("ProfileExists('nonexistent') = true, want false")
	}
}

func TestOverrideActiveProfile(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_PROFILE", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(`active_profile: default
profiles:
  default:
    api_key: "default-key"
  staging:
    api_key: "staging-key"
`), 0600)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Verify initial state.
	if got := ActiveProfile(); got != "default" {
		t.Errorf("initial ActiveProfile() = %q, want %q", got, "default")
	}
	if got := APIKey(); got != "default-key" {
		t.Errorf("initial APIKey() = %q, want %q", got, "default-key")
	}

	// Override to staging.
	OverrideActiveProfile("staging")

	if got := ActiveProfile(); got != "staging" {
		t.Errorf("after override ActiveProfile() = %q, want %q", got, "staging")
	}
	if got := APIKey(); got != "staging-key" {
		t.Errorf("after override APIKey() = %q, want %q", got, "staging-key")
	}
}

func TestSetConfigValue_Persists(t *testing.T) {
	resetViper()
	defer resetViper()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "jc", "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if err := SetConfigValue("defaults.pager", "less"); err != nil {
		t.Fatalf("SetConfigValue() error: %v", err)
	}

	// Read the file to verify persistence.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("cannot read config: %v", err)
	}
	if !strings.Contains(string(data), "less") {
		t.Errorf("config file should contain 'less', got: %s", string(data))
	}
}
