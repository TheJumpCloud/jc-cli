package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/viper"
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
