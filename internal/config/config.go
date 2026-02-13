package config

import (
	"fmt"
	"os"
	"path/filepath"

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

// Init sets up Viper with defaults, creates the config file if missing,
// and reads it. Returns an error only for invalid YAML; a missing file
// is handled by auto-creation.
func Init() error {
	setDefaults()

	viper.SetEnvPrefix("JC")
	viper.AutomaticEnv()

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
