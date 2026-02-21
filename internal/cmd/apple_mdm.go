package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

var appleMDMDefaultFields = []string{"id", "name", "orgName", "defaultIosUserEnrollmentDeviceGroupID"}

func resolveAppleMDM(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.AppleMDMConfig)
}

func newAppleMDMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apple-mdm",
		Short: "Manage Apple MDM configurations",
		Long:  "List, get, create, update, and delete JumpCloud Apple MDM configurations. View enrollment profiles and managed devices.",
	}

	cmd.AddCommand(newAppleMDMListCmd())
	cmd.AddCommand(newAppleMDMGetCmd())
	cmd.AddCommand(newAppleMDMCreateCmd())
	cmd.AddCommand(newAppleMDMUpdateCmd())
	cmd.AddCommand(newAppleMDMDeleteCmd())
	cmd.AddCommand(newAppleMDMEnrollmentProfilesCmd())
	cmd.AddCommand(newAppleMDMDevicesCmd())

	return cmd
}

func newAppleMDMListCmd() *cobra.Command {
	var limitFlag int

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all Apple MDM configurations",
		Long: `List all JumpCloud Apple MDM configurations.

Default fields: id, name, orgName, defaultIosUserEnrollmentDeviceGroupID.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppleMDMList(cmd, limitFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")

	return cmd
}

func runAppleMDMList(cmd *cobra.Command, limit int) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/applemdms", api.V2ListOptions{Limit: limit})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = appleMDMDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newAppleMDMGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get an Apple MDM configuration by name or ID",
		Long: `Get a single JumpCloud Apple MDM configuration by name or ID.

Accepts a configuration name or a 24-character hex ID.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppleMDMGet(cmd, args[0])
		},
	}
}

func runAppleMDMGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAppleMDM(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/applemdms/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAppleMDMCreateCmd() *cobra.Command {
	var (
		name    string
		orgName string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an Apple MDM configuration",
		Long: `Create a new JumpCloud Apple MDM configuration.

Required fields: --name.
The newly created Apple MDM configuration object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppleMDMCreate(cmd, name, orgName)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "MDM configuration name (required)")
	cmd.Flags().StringVar(&orgName, "org-name", "", "Organization name for the MDM certificate")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runAppleMDMCreate(cmd *cobra.Command, name, orgName string) error {
	if viper.GetBool("plan") {
		effects := []string{"name: " + name}
		if orgName != "" {
			effects = append(effects, "orgName: "+orgName)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "Apple MDM configuration",
			Target:     name,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	body := map[string]any{
		"name": name,
	}
	if orgName != "" {
		body["orgName"] = orgName
	}

	result, err := client.Create(cmd.Context(), "/applemdms", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAppleMDMUpdateCmd() *cobra.Command {
	var (
		name    string
		orgName string
	)

	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update an Apple MDM configuration",
		Long: `Update an existing JumpCloud Apple MDM configuration.

Accepts a configuration name or 24-character hex ID.
Specify only the fields you want to change.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppleMDMUpdate(cmd, args[0], name, orgName)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "MDM configuration name")
	cmd.Flags().StringVar(&orgName, "org-name", "", "Organization name")

	return cmd
}

func runAppleMDMUpdate(cmd *cobra.Command, identifier, name, orgName string) error {
	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("org-name") {
		body["orgName"] = orgName
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --org-name)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "Apple MDM configuration",
			Target:     identifier,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAppleMDM(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/applemdms/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAppleMDMDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete an Apple MDM configuration",
		Long: `Delete a JumpCloud Apple MDM configuration.

Accepts a configuration name or 24-character hex ID.
Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppleMDMDelete(cmd, args[0])
		},
	}
}

func runAppleMDMDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAppleMDM(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	mdmData, err := client.Get(cmd.Context(), "/applemdms/"+id)
	if err != nil {
		return err
	}

	var mdm struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(mdmData, &mdm); err != nil {
		return fmt.Errorf("parsing Apple MDM data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "Apple MDM configuration",
			Target:   fmt.Sprintf("%s (%s)", mdm.Name, id),
			Effects:  []string{"Remove Apple MDM configuration and enrollment profiles"},
		}
		return renderPlan(cmd, p)
	}

	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete Apple MDM configuration %q? [y/N] ", mdm.Name)
		reader := getConfirmReader()
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return nil
		}
	}

	_, err = client.Delete(cmd.Context(), "/applemdms/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Apple MDM configuration %q deleted successfully.\n", mdm.Name)
	return nil
}

func newAppleMDMEnrollmentProfilesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enrollment-profiles <name-or-id>",
		Short: "List enrollment profiles for an Apple MDM configuration",
		Long:  "List all enrollment profiles associated with a JumpCloud Apple MDM configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppleMDMEnrollmentProfiles(cmd, args[0])
		},
	}
}

func runAppleMDMEnrollmentProfiles(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAppleMDM(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/applemdms/"+id+"/enrollmentprofiles", api.V2ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newAppleMDMDevicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "devices <name-or-id>",
		Short: "List managed devices for an Apple MDM configuration",
		Long:  "List all devices managed by a JumpCloud Apple MDM configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppleMDMDevices(cmd, args[0])
		},
	}
}

func runAppleMDMDevices(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAppleMDM(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/applemdms/"+id+"/devices", api.V2ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}
