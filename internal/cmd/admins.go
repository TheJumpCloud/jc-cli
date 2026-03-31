package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// adminDefaultFields is the default field subset shown for admins list/table output.
// Uses _id (V1 convention) since the /users endpoint is a V1-style API.
var adminDefaultFields = []string{"_id", "email", "roleName", "enableMultiFactor"}

// resolveAdmin resolves an admin email or ID to a JumpCloud admin user ID.
func resolveAdmin(ctx context.Context, client *api.V1Client, identifier string) (string, error) {
	r := resolve.NewResolver(client)
	return r.Resolve(ctx, identifier, resolve.AdminConfig)
}

func newAdminsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admins",
		Short: "Manage JumpCloud administrators",
		Long:  "List, get, create, update, and delete JumpCloud administrators.",
	}

	cmd.AddCommand(newAdminsListCmd())
	cmd.AddCommand(newAdminsGetCmd())
	cmd.AddCommand(newAdminsCreateCmd())
	cmd.AddCommand(newAdminsUpdateCmd())
	cmd.AddCommand(newAdminsDeleteCmd())

	return cmd
}

func newAdminsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all administrators",
		Long: `List all JumpCloud administrators with email, role, and MFA status.

Default fields: _id, email, roleName, enableMultiFactor.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'roleName=Administrator'    Filter by admin role
  --filter 'email=admin@acme.com'      Filter by email`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdminsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -email)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'roleName=Administrator')")

	return cmd
}

func runAdminsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	// Admins use the V1-style /users endpoint (not V2 /administrators which doesn't exist).
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/users", api.ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV1Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = adminDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d of %d items ──\n", len(result.Data), result.TotalCount)
	}

	return nil
}

func newAdminsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <email-or-id>",
		Short: "Get an administrator by email or ID",
		Long: `Get a single JumpCloud administrator by email or ID.

Accepts an email address (e.g., "admin@acme.com") or a 24-character hex admin ID.
Email addresses are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdminsGet(cmd, args[0])
		},
	}
	return cmd
}

func runAdminsGet(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}
	id, err := resolveAdmin(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	result, err := client.Get(cmd.Context(), "/users/"+id)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAdminsCreateCmd() *cobra.Command {
	var (
		email     string
		role      string
		enableMFA bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new administrator",
		Long: `Create a new JumpCloud administrator.

Required: --email. Optional: --role, --enable-mfa.
Sends an activation email to the new admin.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdminsCreate(cmd, email, role, enableMFA)
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Admin email address (required)")
	cmd.Flags().StringVar(&role, "role", "", "Admin role name")
	cmd.Flags().BoolVar(&enableMFA, "enable-mfa", false, "Enable multi-factor authentication")
	_ = cmd.MarkFlagRequired("email")
	return cmd
}

func runAdminsCreate(cmd *cobra.Command, email, role string, enableMFA bool) error {
	if viper.GetBool("plan") {
		effects := []string{"email: " + email}
		if role != "" {
			effects = append(effects, "role: "+role)
		}
		if enableMFA {
			effects = append(effects, "MFA: enabled")
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "admin",
			Target:     email,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	body := map[string]any{
		"email": email,
	}
	if role != "" {
		body["roleName"] = role
	}
	if enableMFA {
		body["enableMultiFactor"] = true
	}

	result, err := client.Create(cmd.Context(), "/users", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAdminsUpdateCmd() *cobra.Command {
	var (
		role       string
		enableMFA  bool
		disableMFA bool
	)
	cmd := &cobra.Command{
		Use:   "update <email-or-id>",
		Short: "Update an administrator",
		Long: `Update an existing JumpCloud administrator.

Accepts an email address or 24-character hex admin ID.
Specify only the fields you want to change.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdminsUpdate(cmd, args[0], role, enableMFA, disableMFA)
		},
	}
	cmd.Flags().StringVar(&role, "role", "", "Admin role name")
	cmd.Flags().BoolVar(&enableMFA, "enable-mfa", false, "Enable multi-factor authentication")
	cmd.Flags().BoolVar(&disableMFA, "disable-mfa", false, "Disable multi-factor authentication")
	return cmd
}

func runAdminsUpdate(cmd *cobra.Command, identifier, role string, enableMFA, disableMFA bool) error {
	body := map[string]any{}
	if cmd.Flags().Changed("role") {
		body["roleName"] = role
	}
	if cmd.Flags().Changed("enable-mfa") {
		body["enableMultiFactor"] = true
	}
	if cmd.Flags().Changed("disable-mfa") {
		body["enableMultiFactor"] = false
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --role, --enable-mfa)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "admin",
			Target:     identifier,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveAdmin(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/users/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAdminsDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <email-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete an administrator",
		Long: `Delete a JumpCloud administrator.

Accepts an email address or 24-character hex admin ID.
Shows the admin email before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdminsDelete(cmd, args[0])
		},
	}
	return cmd
}

func runAdminsDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveAdmin(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	adminData, err := client.Get(cmd.Context(), "/users/"+id)
	if err != nil {
		return err
	}

	var admin struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(adminData, &admin); err != nil {
		return fmt.Errorf("parsing admin data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "admin",
			Target:   fmt.Sprintf("%s (%s)", admin.Email, id),
			Effects:  []string{"Remove administrator permanently"},
		}
		return renderPlan(cmd, p)
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete admin %q? [y/N] ", admin.Email)
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

	_, err = client.Delete(cmd.Context(), "/users/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Admin %q deleted successfully.\n", admin.Email)
	return nil
}
