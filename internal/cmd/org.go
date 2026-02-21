package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// orgDefaultFields is the default field subset shown for organization output.
var orgDefaultFields = []string{"_id", "displayName", "created"}

func newOrgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "View JumpCloud organization settings",
		Long:  "List and view JumpCloud organization details.",
	}

	cmd.AddCommand(newOrgListCmd())
	cmd.AddCommand(newOrgGetCmd())
	cmd.AddCommand(newOrgSettingsCmd())
	cmd.AddCommand(newOrgUpdateCmd())

	return cmd
}

func newOrgListCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List organizations",
		Long: `List all JumpCloud organizations.

Default fields: _id, displayName, created.
Most JumpCloud accounts have a single organization.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOrgList(cmd, limit)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of items to return (0 = all)")

	return cmd
}

func runOrgList(cmd *cobra.Command, limit int) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/organizations", api.ListOptions{Limit: limit})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = orgDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newOrgGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <org-id>",
		Short: "Get an organization by ID",
		Long: `Get a single JumpCloud organization by its ID.

Accepts a 24-character hex organization ID.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOrgGet(cmd, args[0])
		},
	}

	return cmd
}

func runOrgGet(cmd *cobra.Command, id string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/organizations/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newOrgSettingsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "settings <org-id>",
		Short: "View organization settings",
		Long: `View the full settings for a JumpCloud organization.

Accepts a 24-character hex organization ID.
Returns the complete organization object including all settings fields.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOrgSettings(cmd, args[0])
		},
	}
}

func runOrgSettings(cmd *cobra.Command, id string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/organizations/"+id)
	if err != nil {
		return err
	}

	// Show all fields for settings (no default field filtering).
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newOrgUpdateCmd() *cobra.Command {
	var (
		nameFlag         string
		settingsJSONFlag string
	)

	cmd := &cobra.Command{
		Use:   "update <org-id>",
		Short: "Update organization settings",
		Long: `Update a JumpCloud organization's name or settings.

Accepts a 24-character hex organization ID.
Use --name to change the display name.
Use --settings-json to pass raw JSON for complex settings fields.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOrgUpdate(cmd, args[0], nameFlag, settingsJSONFlag)
		},
	}

	cmd.Flags().StringVar(&nameFlag, "name", "", "Organization display name")
	cmd.Flags().StringVar(&settingsJSONFlag, "settings-json", "", "Raw JSON for settings fields")

	return cmd
}

func runOrgUpdate(cmd *cobra.Command, id, name, settingsJSON string) error {
	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		body["displayName"] = name
	}
	if cmd.Flags().Changed("settings-json") {
		var settings map[string]any
		if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
			return NewCLIError(ErrCodeValidationError,
				fmt.Sprintf("invalid --settings-json: %v", err),
				"Provide valid JSON, e.g. --settings-json '{\"passwordPolicy\":{\"minLength\":10}}'")
		}
		body["settings"] = settings
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --settings-json)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "organization",
			Target:     id,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/organizations/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
