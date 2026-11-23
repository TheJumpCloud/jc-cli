package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/identityrisk"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// newIdentityRiskSettingsCmd builds `jc identity-risk settings`: read the
// Access Risk Detection settings and change one.
func newIdentityRiskSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "settings",
		Aliases: []string{"setting"},
		Short:   "Access Risk Detection settings",
		Long: `Read the organization's Access Risk Detection settings and change one.

These settings control a detection feature: switching "enabled" off or
widening "trusted_ip_lists" weakens the organization's protection, so "set"
previews with --plan, confirms, and reads the setting back to prove it changed.`,
	}
	cmd.AddCommand(newIdentityRiskSettingsListCmd(), newIdentityRiskSettingsSetCmd())
	return cmd
}

func newIdentityRiskSettingsListCmd() *cobra.Command {
	var filters []string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List Access Risk Detection settings",
		Example: `  jc identity-risk settings list -t
  jc identity-risk settings list --filter setting.name:eq:trusted_ip_lists`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := fetchRiskSettings(cmd, filters)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"name", "value", "category"}
			out := make([]json.RawMessage, 0, len(rows))
			for _, r := range rows {
				out = append(out, mustJSON(r))
			}
			return output.WriteList(cmd.OutOrStdout(), out, opts)
		},
	}
	cmd.Flags().StringArrayVar(&filters, "filter", nil,
		"Server-side filter, e.g. setting.name:eq:enabled (repeatable)")
	return cmd
}

// fetchRiskSettings GETs the settings, passing each filter as a filter param.
func fetchRiskSettings(cmd *cobra.Command, filters []string) ([]identityrisk.Setting, error) {
	v := url.Values{}
	for _, f := range filters {
		v.Add("filter", f)
	}
	raw, err := riskGet(cmd.Context(), withQuery(identityrisk.SettingsEndpoint, v))
	if err != nil {
		return nil, err
	}
	return identityrisk.ParseSettings(raw)
}

func usageErr(format string, a ...any) error {
	return NewCLIError(ErrCodeUsageError, fmt.Sprintf(format, a...), "")
}

func newIdentityRiskSettingsSetCmd() *cobra.Command {
	var value, valueFile string
	cmd := &cobra.Command{
		Use:   "set <name> (--value <json> | --value-file <path>)",
		Short: "Change one Access Risk Detection setting",
		Long: `Change one setting. The value is JSON and must have the same JSON type as the
current value. The name must be one the server returns.

Turning "enabled" off switches Access Risk Detection off for the whole
organization. After the write jc reads the setting back and fails if the
value did not change.`,
		Example: `  jc identity-risk settings set trusted_ip_lists --value '["203.0.113.0/24"]' --plan
  jc identity-risk settings set enabled --value true --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			hasValue, hasFile := cmd.Flags().Changed("value"), cmd.Flags().Changed("value-file")
			if hasValue == hasFile {
				return usageErr("pass the new value with exactly one of --value or --value-file")
			}
			text := value
			if hasFile {
				b, err := os.ReadFile(valueFile)
				if err != nil {
					return usageErr("reading --value-file: %v", err)
				}
				text = string(b)
			}
			next, err := identityrisk.ParseValue(text)
			if err != nil {
				return usageErr("%v", err)
			}

			rows, err := fetchRiskSettings(cmd, nil)
			if err != nil {
				return err
			}
			cur, err := identityrisk.FindSetting(rows, name)
			if err != nil {
				return usageErr("%v", err)
			}
			if err := identityrisk.CheckType(name, cur.Value, next); err != nil {
				return usageErr("%v", err)
			}

			disabling := identityrisk.IsDisabling(name, next)
			if viper.GetBool("plan") {
				effects := []string{"current: " + string(cur.Value), "new: " + string(next)}
				if disabling {
					effects = append(effects, "switches detection off org-wide")
				}
				return renderPlan(cmd, &plan.Plan{
					Action:     "set",
					Resource:   "identity-risk setting",
					Target:     name,
					Effects:    effects,
					Reversible: true,
				})
			}

			if mustAbortWithoutTTY() {
				return fmt.Errorf("changing setting %q requires --force or --non-interactive "+
					"(or preview with --plan first)", name)
			}
			if shouldConfirm() {
				if disabling {
					fmt.Fprintln(cmd.ErrOrStderr(), identityrisk.DisableWarning)
				}
				ok, err := askYesNo(cmd, fmt.Sprintf("Change %s from %s to %s?", name, cur.Value, next))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
					return nil
				}
			}

			client, err := newV2Client()
			if err != nil {
				return err
			}
			if _, err := client.Update(cmd.Context(), identityrisk.SettingEndpoint(name),
				map[string]json.RawMessage{"value": next}); err != nil {
				return err
			}

			after, err := fetchRiskSettings(cmd, []string{"setting.name:eq:" + name})
			if err != nil {
				return fmt.Errorf("setting %q was written but could not be read back: %w", name, err)
			}
			got, err := identityrisk.FindSetting(after, name)
			if err != nil {
				return fmt.Errorf("setting %q was written but is missing on read-back: %w", name, err)
			}
			if !identityrisk.SameValue(got.Value, next) {
				return fmt.Errorf("setting %q was NOT changed: sent %s but the server now reports %s",
					name, next, got.Value)
			}
			return output.WriteSingle(cmd.OutOrStdout(), mustJSON(got), output.CurrentOptions())
		},
	}
	cmd.Flags().StringVar(&value, "value", "", "New value as JSON, e.g. true or '[\"203.0.113.0/24\"]'")
	cmd.Flags().StringVar(&valueFile, "value-file", "", "Read the new JSON value from a file")
	return cmd
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
