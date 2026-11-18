package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/featuresettings"
	"github.com/klaassen-consulting/jc/internal/output"
)

// newOrgFeaturesCmd builds `jc org features`: read-only access to the
// organization feature settings. The PUT that changes them is not exposed;
// see internal/featuresettings.
func newOrgFeaturesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "features",
		Short: "View organization feature settings (AI Gateway, user MCP, identity risk, ...)",
		Long: `View the organization's feature settings: per-feature switches and parameters,
grouped by feature, for example:

  mcp                      AI Gateway (enabled, session_auto_revoke_days)
  user_mcp                 JumpCloud MCP Server for Users
  ai_admin_assistant, nli  AI assistant features
  identity_risk_detection  Identity risk detection rules and thresholds

READ-ONLY. The API can change these settings, but jc does not: they switch
org-wide features such as the AI Gateway on and off.`,
	}

	var name, category string
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List feature settings",
		Long: `List feature settings, optionally narrowed by exact feature name or category.

JSON output keeps the API's grouping (one object per feature with its
settings). Table and CSV output show one row per setting: feature, category,
setting, value.

Only exact matches are offered: the API's other filter operators return wrong
results, and filter fields other than name and category fail with HTTP 500.`,
		Example: `  jc org features list
  jc org features list --category ai -t
  jc org features list --name mcp`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOrgFeatures(cmd, name, category, false)
		},
	}
	list.Flags().StringVar(&name, "name", "", "Exact feature name (e.g. mcp, user_mcp)")
	list.Flags().StringVar(&category, "category", "", "Exact category (e.g. ai, itdr, login_ui)")
	cmd.AddCommand(list)

	cmd.AddCommand(&cobra.Command{
		Use:   "get <feature>",
		Short: "Get one feature and its settings",
		Long: `Get one feature by its exact name, for example "mcp" for the AI Gateway.
Exits non-zero when the organization has no feature of that name.`,
		Example: `  jc org features get mcp`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOrgFeatures(cmd, args[0], "", true)
		},
	})

	return cmd
}

func runOrgFeatures(cmd *cobra.Command, name, category string, single bool) error {
	if name != "" && !featuresettings.ValidName(name) {
		return featuresettings.ErrInvalidName("name", name)
	}
	if category != "" && !featuresettings.ValidName(category) {
		return featuresettings.ErrInvalidName("category", category)
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	raw, err := client.Get(cmd.Context(), featuresettings.Path(name, category))
	if err != nil {
		return err
	}
	rawFeatures, features, err := featuresettings.Parse(raw)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	if single {
		if len(rawFeatures) == 0 {
			return fmt.Errorf("no feature named %q in this organization (see `jc org features list`)", name)
		}
		if opts.Format != output.FormatTable && opts.Format != output.FormatCSV {
			return output.WriteSingle(cmd.OutOrStdout(), rawFeatures[0], opts)
		}
	}
	if opts.Format == output.FormatTable || opts.Format == output.FormatCSV {
		rows, err := featuresettings.Flatten(features)
		if err != nil {
			return err
		}
		opts.DefaultFields = featuresettings.FlatFields
		return output.WriteList(cmd.OutOrStdout(), nonNilRows(rows), opts)
	}
	return output.WriteList(cmd.OutOrStdout(), rawFeatures, opts)
}

func nonNilRows(rows []json.RawMessage) []json.RawMessage {
	if rows == nil {
		return []json.RawMessage{}
	}
	return rows
}
