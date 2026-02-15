package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/keychain"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and manage configuration",
		Long:  "Inspect and modify jc configuration from the CLI without editing YAML manually.",
	}

	cmd.AddCommand(newConfigViewCmd())
	cmd.AddCommand(newConfigSetCmd())

	return cmd
}

func newConfigViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view",
		Short: "Display current configuration",
		Long: `Display the full jc configuration with secrets redacted.

API keys are shown as '****<last4>' to prevent accidental exposure.
The active profile is highlighted in the output.`,
		RunE: runConfigView,
	}
	return cmd
}

func runConfigView(cmd *cobra.Command, args []string) error {
	settings := viper.AllSettings()

	// Redact secrets (API keys, client secrets, ask API key).
	redactSecrets(settings)

	// Highlight active profile.
	activeProfile := config.ActiveProfile()
	if profiles, ok := settings["profiles"].(map[string]interface{}); ok {
		for name := range profiles {
			if name == activeProfile {
				// Mark the active profile by adding an annotation in the map.
				// For JSON/YAML output, we add an _active field.
				if profile, ok := profiles[name].(map[string]interface{}); ok {
					profile["_active"] = true
				}
			}
		}
	}

	outputFmt := viper.GetString("defaults.output")

	switch outputFmt {
	case "yaml":
		return writeConfigYAML(cmd, settings)
	default:
		return writeConfigJSON(cmd, settings)
	}
}

// writeConfigJSON outputs config as pretty-printed JSON.
func writeConfigJSON(cmd *cobra.Command, settings map[string]interface{}) error {
	// Sort top-level keys for consistent output.
	out, err := json.MarshalIndent(sortMapKeys(settings), "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(out))
	return nil
}

// writeConfigYAML outputs config as YAML.
func writeConfigYAML(cmd *cobra.Command, settings map[string]interface{}) error {
	out, err := yaml.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	fmt.Fprint(cmd.OutOrStdout(), string(out))
	return nil
}

// sortMapKeys returns an ordered map structure suitable for json.Marshal.
// Go's encoding/json marshals map keys in sorted order, so we just need
// to ensure nested maps are also map[string]interface{}.
func sortMapKeys(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		if nested, ok := v.(map[string]interface{}); ok {
			result[k] = sortMapKeys(nested)
		} else {
			result[k] = v
		}
	}
	return result
}

// redactSecrets walks the config settings and replaces sensitive values
// (API keys, client secrets, ask API key) with redacted versions (****<last4>).
func redactSecrets(settings map[string]interface{}) {
	// Redact profiles.*.api_key and profiles.*.client_secret.
	if profiles, ok := settings["profiles"]; ok {
		if profileMap, ok := profiles.(map[string]interface{}); ok {
			for _, profile := range profileMap {
				p, ok := profile.(map[string]interface{})
				if !ok {
					continue
				}
				for _, key := range []string{"api_key", "client_secret"} {
					if val, ok := p[key].(string); ok && val != "" {
						if keychain.IsKeychainRef(val) {
							continue
						}
						p[key] = api.RedactKey(val)
					}
				}
			}
		}
	}

	// Redact ask.api_key (top-level).
	if askCfg, ok := settings["ask"]; ok {
		if askMap, ok := askCfg.(map[string]interface{}); ok {
			if val, ok := askMap["api_key"].(string); ok && val != "" {
				askMap["api_key"] = api.RedactKey(val)
			}
		}
	}
}

func newConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value using dot notation.

Examples:
  jc config set defaults.output table
  jc config set defaults.limit 50
  jc config set defaults.confirm_destructive true
  jc config set cache.ttl 600
  jc config set active_profile production
  jc config set aliases.inactive "users list --filter 'suspended=true' -t"
  jc config set aliases.stale "devices list --sort -lastContact -t"

Valid keys (aliases.* accepts any alias name):
` + formatValidKeys(),
		Args: cobra.ExactArgs(2),
		RunE: runConfigSet,
	}
	return cmd
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]

	if !config.IsValidConfigKey(key) {
		return fmt.Errorf("unknown config key %q\n\nValid keys:\n%s", key, formatValidKeys())
	}

	if err := config.SetConfigValue(key, value); err != nil {
		return fmt.Errorf("failed to set config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %s\n", key, value)
	return nil
}

// formatValidKeys returns a formatted string listing all valid config keys.
func formatValidKeys() string {
	keys := make([]string, len(config.ValidConfigKeys))
	copy(keys, config.ValidConfigKeys)
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s\n", k)
	}
	return b.String()
}
