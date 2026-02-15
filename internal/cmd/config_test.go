package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/config"
)

// setupConfigTest initializes Viper and config with a custom config file.
func setupConfigTest(t *testing.T, configYAML string) string {
	t.Helper()
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	// Clear env vars that could interfere.
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_ORG_ID", "")
	t.Setenv("JC_PROFILE", "")
	t.Setenv("JC_OUTPUT", "")
	t.Setenv("JC_NO_COLOR", "")
	t.Setenv("NO_COLOR", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte(configYAML), 0600)

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	return cfgPath
}

// --- config view tests ---

func TestConfigView_JSONOutput(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
  limit: 100
  confirm_destructive: true
  color: true
  pager: ""
cache:
  enabled: true
  ttl: 300
  directory: ""
profiles:
  default:
    api_key: ""
    org_id: ""
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Output should be valid JSON.
	var result map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput: %s", err, out.String())
	}

	// Check expected keys.
	if _, ok := result["active_profile"]; !ok {
		t.Error("missing active_profile key in output")
	}
	if _, ok := result["defaults"]; !ok {
		t.Error("missing defaults key in output")
	}
	if _, ok := result["cache"]; !ok {
		t.Error("missing cache key in output")
	}
}

func TestConfigView_YAMLOutput(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: yaml
  limit: 100
cache:
  enabled: true
profiles:
  default:
    api_key: ""
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Output should contain YAML-like content.
	content := out.String()
	if !strings.Contains(content, "active_profile:") {
		t.Errorf("YAML output should contain active_profile:, got: %s", content)
	}
	if !strings.Contains(content, "defaults:") {
		t.Errorf("YAML output should contain defaults:, got: %s", content)
	}
}

func TestConfigView_RedactsPlaintextAPIKeys(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
profiles:
  default:
    api_key: "secret-api-key-12345678"
    org_id: "org-abc"
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := out.String()

	// API key should be redacted.
	if strings.Contains(content, "secret-api-key-12345678") {
		t.Error("plaintext API key should not appear in config view output")
	}
	// Should show redacted form (last 4 chars).
	if !strings.Contains(content, "****5678") {
		t.Errorf("config view should show redacted API key with last 4 chars, got: %s", content)
	}
}

func TestConfigView_KeychainRefNotRedacted(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
profiles:
  default:
    api_key: "keychain://jc/default"
    org_id: "org-abc"
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := out.String()

	// Keychain ref should be shown as-is.
	if !strings.Contains(content, "keychain://jc/default") {
		t.Errorf("keychain ref should be shown as-is, got: %s", content)
	}
}

func TestConfigView_RedactsClientSecret(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
profiles:
  default:
    api_key: "keychain://jc/default"
    client_secret: "my-super-secret-client-1234"
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := out.String()
	if strings.Contains(content, "my-super-secret-client-1234") {
		t.Error("plaintext client_secret should not appear in config view output")
	}
	if !strings.Contains(content, "****1234") {
		t.Errorf("config view should show redacted client_secret with last 4 chars, got: %s", content)
	}
}

func TestConfigView_RedactsAskAPIKey(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
profiles:
  default:
    api_key: "keychain://jc/default"
ask:
  api_key: "sk-ant-secret-key-9999"
  provider: anthropic
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := out.String()
	if strings.Contains(content, "sk-ant-secret-key-9999") {
		t.Error("plaintext ask.api_key should not appear in config view output")
	}
	if !strings.Contains(content, "****9999") {
		t.Errorf("config view should show redacted ask.api_key, got: %s", content)
	}
}

func TestConfigView_ClientSecretKeychainRefNotRedacted(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
profiles:
  default:
    api_key: "keychain://jc/default"
    client_secret: "keychain://jc/default:client_secret"
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := out.String()
	if !strings.Contains(content, "keychain://jc/default:client_secret") {
		t.Errorf("keychain ref for client_secret should be shown as-is, got: %s", content)
	}
}

func TestConfigView_ActiveProfileHighlighted(t *testing.T) {
	setupConfigTest(t, `active_profile: prod
defaults:
  output: json
profiles:
  prod:
    api_key: ""
    org_id: "org-prod"
  staging:
    api_key: ""
    org_id: "org-staging"
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// Active profile should have _active: true.
	profiles := result["profiles"].(map[string]interface{})
	prod := profiles["prod"].(map[string]interface{})
	if active, ok := prod["_active"]; !ok || active != true {
		t.Error("active profile 'prod' should have _active: true")
	}

	staging := profiles["staging"].(map[string]interface{})
	if _, ok := staging["_active"]; ok {
		t.Error("non-active profile 'staging' should not have _active field")
	}
}

func TestConfigView_EmptyAPIKeyNotRedacted(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
profiles:
  default:
    api_key: ""
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not crash or produce redacted output for empty key.
	var result map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

// --- config set tests ---

func TestConfigSet_StringValue(t *testing.T) {
	cfgPath := setupConfigTest(t, `active_profile: default
defaults:
  output: json
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "defaults.output", "table"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "Set defaults.output = table") {
		t.Errorf("expected confirmation message, got: %s", out.String())
	}

	// Verify the value was persisted to disk.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("cannot read config file: %v", err)
	}
	if !strings.Contains(string(data), "table") {
		t.Errorf("config file should contain 'table', got: %s", string(data))
	}
}

func TestConfigSet_IntValue(t *testing.T) {
	cfgPath := setupConfigTest(t, `active_profile: default
defaults:
  output: json
  limit: 100
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "defaults.limit", "50"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the value was written as a number (not string).
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("cannot read config file: %v", err)
	}
	content := string(data)
	// YAML should have `limit: 50` (not `limit: "50"`).
	if !strings.Contains(content, "limit: 50") {
		t.Errorf("config should contain 'limit: 50', got: %s", content)
	}
}

func TestConfigSet_BoolValue(t *testing.T) {
	cfgPath := setupConfigTest(t, `active_profile: default
defaults:
  output: json
  confirm_destructive: true
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "defaults.confirm_destructive", "false"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("cannot read config file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "confirm_destructive: false") {
		t.Errorf("config should contain 'confirm_destructive: false', got: %s", content)
	}
}

func TestConfigSet_ActiveProfile(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
profiles:
  default:
    api_key: ""
  production:
    api_key: ""
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "active_profile", "production"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "Set active_profile = production") {
		t.Errorf("expected confirmation, got: %s", out.String())
	}

	// Verify in Viper.
	if got := viper.GetString("active_profile"); got != "production" {
		t.Errorf("active_profile should be 'production', got %q", got)
	}
}

func TestConfigSet_InvalidKey(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	var errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"config", "set", "invalid.key", "value"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid config key")
	}

	if !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("error should mention 'unknown config key', got: %v", err)
	}
	if !strings.Contains(err.Error(), "Valid keys:") {
		t.Errorf("error should list valid keys, got: %v", err)
	}
}

func TestConfigSet_MissingArgs(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
`)

	rootCmd := NewRootCmd()
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "defaults.output"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestConfigSet_CacheTTL(t *testing.T) {
	setupConfigTest(t, `active_profile: default
cache:
  enabled: true
  ttl: 300
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "cache.ttl", "600"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := viper.GetInt("cache.ttl"); got != 600 {
		t.Errorf("cache.ttl should be 600, got %d", got)
	}
}

func TestConfigSet_CacheEnabled(t *testing.T) {
	setupConfigTest(t, `active_profile: default
cache:
  enabled: true
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "cache.enabled", "false"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := viper.GetBool("cache.enabled"); got != false {
		t.Errorf("cache.enabled should be false, got %v", got)
	}
}

func TestConfigSet_AtomicWrite(t *testing.T) {
	cfgPath := setupConfigTest(t, `active_profile: default
defaults:
  output: json
`)

	rootCmd := NewRootCmd()
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "defaults.pager", "less"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no temp files left behind.
	tmpPath := cfgPath + ".tmp.yaml"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful write")
	}
}

func TestConfigView_Help(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "Display the full jc configuration") {
		t.Errorf("help should include description, got: %s", out.String())
	}
}

func TestConfigSet_Help(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "set", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := out.String()
	if !strings.Contains(content, "Set a configuration value") {
		t.Errorf("help should include description, got: %s", content)
	}
	if !strings.Contains(content, "defaults.output") {
		t.Errorf("help should list valid keys, got: %s", content)
	}
}

func TestConfigHelp_ShowsSubcommands(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := out.String()
	if !strings.Contains(content, "view") {
		t.Error("config help should list 'view' subcommand")
	}
	if !strings.Contains(content, "set") {
		t.Error("config help should list 'set' subcommand")
	}
}

func TestConfigView_WithOutputFlag(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
profiles:
  default:
    api_key: ""
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view", "--output", "json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still produce valid JSON.
	var result map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput: %s", err, out.String())
	}
}

func TestConfigView_WithYAMLOutputFlag(t *testing.T) {
	setupConfigTest(t, `active_profile: default
defaults:
  output: json
profiles:
  default:
    api_key: ""
`)

	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"config", "view", "--output", "yaml"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := out.String()
	if !strings.Contains(content, "active_profile:") {
		t.Errorf("YAML output should contain active_profile:, got: %s", content)
	}
}
