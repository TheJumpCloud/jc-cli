package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/klaassen-consulting/jc/internal/keychain"
	"github.com/spf13/viper"
)

// DefaultConfig is the YAML content written when no config file exists.
const DefaultConfig = `# jc — JumpCloud CLI configuration
# See: jc config view

active_profile: default

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
`

// ConfigDir returns the directory where the config file lives.
// Priority: JC_CONFIG env (parent dir), XDG_CONFIG_HOME/jc, ~/.config/jc.
func ConfigDir() string {
	if p := os.Getenv("JC_CONFIG"); p != "" {
		return filepath.Dir(p)
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "jc")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "jc")
}

// ConfigPath returns the full path to the config file.
func ConfigPath() string {
	if p := os.Getenv("JC_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(ConfigDir(), "config.yaml")
}

// ensureConfigFile creates the config directory and a default config file
// if they do not already exist. Directory gets 0700, file gets 0600.
func ensureConfigFile() error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create config directory %s: %w", dir, err)
	}

	path := ConfigPath()
	if _, err := os.Stat(path); err == nil {
		return nil // file already exists
	}

	if err := os.WriteFile(path, []byte(DefaultConfig), 0600); err != nil {
		return fmt.Errorf("cannot write default config to %s: %w", path, err)
	}
	return nil
}

// setDefaults registers Viper defaults so values are available even without
// a config file.
func setDefaults() {
	viper.SetDefault("active_profile", "default")
	viper.SetDefault("defaults.output", "json")
	viper.SetDefault("defaults.limit", 100)
	viper.SetDefault("defaults.confirm_destructive", true)
	viper.SetDefault("defaults.color", true)
	viper.SetDefault("defaults.pager", "")
	viper.SetDefault("cache.enabled", true)
	viper.SetDefault("cache.ttl", 300)
	viper.SetDefault("cache.directory", "")
}

// bindEnvVars registers explicit mappings from environment variable names
// to Viper config keys. This allows user-friendly env var names like
// JC_OUTPUT to map to nested keys like defaults.output, and ensures
// keys like api_key and org_id are accessible even without config file entries.
func bindEnvVars() {
	// JC_API_KEY → api_key (top-level override, highest priority for auth)
	_ = viper.BindEnv("api_key", "JC_API_KEY")

	// JC_ORG_ID → org_id (top-level override)
	_ = viper.BindEnv("org_id", "JC_ORG_ID")

	// JC_PROFILE → active_profile
	_ = viper.BindEnv("active_profile", "JC_PROFILE")

	// JC_OUTPUT → defaults.output (shortcut so users don't need JC_DEFAULTS_OUTPUT)
	_ = viper.BindEnv("defaults.output", "JC_OUTPUT")

	// JC_NO_COLOR → no_color
	_ = viper.BindEnv("no_color", "JC_NO_COLOR")
}

// Init sets up Viper with defaults, creates the config file if missing,
// and reads it. Returns an error only for invalid YAML; a missing file
// is handled by auto-creation.
func Init() error {
	setDefaults()

	viper.SetEnvPrefix("JC")
	viper.AutomaticEnv()
	bindEnvVars()

	// Create config file on first run if it doesn't exist.
	if err := ensureConfigFile(); err != nil {
		// Non-fatal: log and continue with defaults.
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	// Configure Viper to read the config file.
	cfgPath := ConfigPath()
	viper.SetConfigFile(cfgPath)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil // no config is fine, we have defaults
		}
		return fmt.Errorf("error reading config file %s: %w", cfgPath, err)
	}

	return nil
}

// ActiveProfile returns the name of the currently active profile.
// Priority: JC_PROFILE env var > config file active_profile > "default".
func ActiveProfile() string {
	return viper.GetString("active_profile")
}

// APIKey returns the API key to use for authentication.
// Priority: JC_API_KEY env var > active profile's api_key in config.
// If the config value is a keychain reference (keychain://jc/<profile>),
// the actual key is retrieved from the OS keychain transparently.
func APIKey() string {
	// JC_API_KEY (bound to "api_key") takes highest priority.
	// Env var values are always plaintext, never keychain refs.
	if key := viper.GetString("api_key"); key != "" {
		return key
	}
	// Fall back to the active profile's api_key.
	profile := ActiveProfile()
	value := viper.GetString("profiles." + profile + ".api_key")
	if value == "" {
		return ""
	}

	// Resolve keychain references transparently.
	resolved, err := keychain.Resolve(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not retrieve API key from keychain: %v\n", err)
		return ""
	}
	return resolved
}

// OrgID returns the organization ID.
// Priority: JC_ORG_ID env var > active profile's org_id in config.
func OrgID() string {
	if id := viper.GetString("org_id"); id != "" {
		return id
	}
	profile := ActiveProfile()
	return viper.GetString("profiles." + profile + ".org_id")
}

// Output returns the configured output format.
// Priority: --output flag > JC_OUTPUT env var > config defaults.output > "json".
func Output() string {
	return viper.GetString("defaults.output")
}

// NoColor returns true if color output should be disabled.
// Color is disabled if JC_NO_COLOR or NO_COLOR is set to any non-empty value,
// or if the --no-color flag is passed.
func NoColor() bool {
	// Check standard NO_COLOR env var (https://no-color.org/)
	if v := os.Getenv("NO_COLOR"); v != "" {
		return true
	}
	// Check JC_NO_COLOR (bound to "no_color" via BindEnv)
	if v := viper.GetString("no_color"); v != "" {
		return true
	}
	// Check --no-color flag (bound to "no-color" via flag binding or direct check)
	if viper.GetBool("no-color") {
		return true
	}
	// Check config defaults.color (inverted: color=false means no-color=true)
	if !viper.GetBool("defaults.color") {
		return true
	}
	return false
}

// NoColorFromEnv returns true if color is disabled via environment variables
// (JC_NO_COLOR or NO_COLOR). This is useful for checking env-only state
// before flags are parsed.
func NoColorFromEnv() bool {
	if v := os.Getenv("NO_COLOR"); v != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv("JC_NO_COLOR")) != ""
}
