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

var saasAppDefaultFields = []string{"id", "catalog_app_id", "status", "discovered_at"}

var saasAccountDefaultFields = []string{"id", "email", "user_id"}

var saasLicenseDefaultFields = []string{"id", "contract_term", "currency", "renewal_date"}

func resolveSaaSApp(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.SaaSManagementConfig)
}

func newSaaSManagementCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "saas-management",
		Aliases: []string{"saas"},
		Short:   "Manage JumpCloud SaaS Management applications",
		Long:    "List, get, create, update, and delete JumpCloud SaaS Management applications and their accounts, usage, and licenses.",
	}

	cmd.AddCommand(newSaaSListCmd())
	cmd.AddCommand(newSaaSGetCmd())
	cmd.AddCommand(newSaaSCreateCmd())
	cmd.AddCommand(newSaaSUpdateCmd())
	cmd.AddCommand(newSaaSDeleteCmd())
	cmd.AddCommand(newSaaSAccountsCmd())
	cmd.AddCommand(newSaaSAccountGetCmd())
	cmd.AddCommand(newSaaSAccountDeleteCmd())
	cmd.AddCommand(newSaaSUsageCmd())
	cmd.AddCommand(newSaaSLicensesCmd())
	cmd.AddCommand(newSaaSCatalogGetCmd())

	return cmd
}

func newSaaSListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all SaaS applications",
		Long: `List all JumpCloud SaaS Management applications.

Default fields: id, catalog_app_id, status, discovered_at.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'status:eq:APPROVED'     Approved apps only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaaSList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'status:eq:APPROVED')")

	return cmd
}

func runSaaSList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/saas-management/applications", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = saasAppDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newSaaSGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <catalog-app-id-or-id>",
		Short: "Get a SaaS application by catalog app ID or ID",
		Long: `Get a single JumpCloud SaaS Management application by catalog app ID or hex ID.

Accepts a catalog app ID (e.g., "jumpcloud") or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SaaSManagementConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaaSGet(cmd, args[0])
		},
	}

	return cmd
}

func runSaaSGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveSaaSApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/saas-management/applications/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newSaaSCreateCmd() *cobra.Command {
	var (
		catalogAppID    string
		status          string
		accessRestriction string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new SaaS application",
		Long: `Create a new JumpCloud SaaS Management application.

Required fields: --catalog-app-id.
The newly created SaaS application object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaaSCreate(cmd, catalogAppID, status, accessRestriction)
		},
	}

	cmd.Flags().StringVar(&catalogAppID, "catalog-app-id", "", "Catalog application ID (required)")
	cmd.Flags().StringVar(&status, "status", "", "Application status (APPROVED, UNAPPROVED, IGNORED)")
	cmd.Flags().StringVar(&accessRestriction, "access-restriction", "", "Access restriction (DEFAULT_ACTION, NO_ACTION, BLOCK, DISMISSIBLE_WARNING)")
	_ = cmd.MarkFlagRequired("catalog-app-id")

	return cmd
}

func runSaaSCreate(cmd *cobra.Command, catalogAppID, status, accessRestriction string) error {
	if viper.GetBool("plan") {
		effects := []string{"catalog_app_id: " + catalogAppID}
		if status != "" {
			effects = append(effects, "status: "+status)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "SaaS application",
			Target:     catalogAppID,
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
		"catalog_app_id": catalogAppID,
	}
	if status != "" {
		body["status"] = status
	}
	if accessRestriction != "" {
		body["access_restriction"] = accessRestriction
	}

	result, err := client.Create(cmd.Context(), "/saas-management/applications", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newSaaSUpdateCmd() *cobra.Command {
	var (
		status          string
		accessRestriction string
	)

	cmd := &cobra.Command{
		Use:   "update <catalog-app-id-or-id>",
		Short: "Update a SaaS application",
		Long: `Update an existing JumpCloud SaaS Management application.

Accepts a catalog app ID or 24-character hex ID.
Specify only the fields you want to change.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SaaSManagementConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaaSUpdate(cmd, args[0], status, accessRestriction)
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "Application status (APPROVED, UNAPPROVED, IGNORED)")
	cmd.Flags().StringVar(&accessRestriction, "access-restriction", "", "Access restriction (DEFAULT_ACTION, NO_ACTION, BLOCK, DISMISSIBLE_WARNING)")

	return cmd
}

func runSaaSUpdate(cmd *cobra.Command, identifier, status, accessRestriction string) error {
	body := map[string]any{}

	if cmd.Flags().Changed("status") {
		body["status"] = status
	}
	if cmd.Flags().Changed("access-restriction") {
		body["access_restriction"] = accessRestriction
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --status, --access-restriction)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "SaaS application",
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

	id, err := resolveSaaSApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/saas-management/applications/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newSaaSDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <catalog-app-id-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a SaaS application",
		Long: `Delete a JumpCloud SaaS Management application.

Accepts a catalog app ID or 24-character hex ID.
Shows the application catalog ID before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SaaSManagementConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaaSDelete(cmd, args[0])
		},
	}

	return cmd
}

func runSaaSDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveSaaSApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	appData, err := client.Get(cmd.Context(), "/saas-management/applications/"+id)
	if err != nil {
		return err
	}

	var app struct {
		CatalogAppID string `json:"catalog_app_id"`
		Name         string `json:"name"`
	}
	if err := json.Unmarshal(appData, &app); err != nil {
		return fmt.Errorf("parsing SaaS application data: %w", err)
	}

	displayName := app.Name
	if displayName == "" {
		displayName = app.CatalogAppID
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "SaaS application",
			Target:   fmt.Sprintf("%s (%s)", displayName, id),
			Effects:  []string{"Remove SaaS application"},
		}
		return renderPlan(cmd, p)
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete SaaS application %q? [y/N] ", displayName)
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

	_, err = client.Delete(cmd.Context(), "/saas-management/applications/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "SaaS application %q deleted successfully.\n", displayName)
	return nil
}

func newSaaSAccountsCmd() *cobra.Command {
	var limitFlag int

	cmd := &cobra.Command{
		Use:   "accounts <catalog-app-id-or-id>",
		Short: "List accounts for a SaaS application",
		Long: `List all accounts for a JumpCloud SaaS Management application.

Default fields: id, email, user_id.
Use --output table for a readable ASCII table.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SaaSManagementConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaaSAccounts(cmd, args[0], limitFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")

	return cmd
}

func runSaaSAccounts(cmd *cobra.Command, identifier string, limit int) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	appID, err := resolveSaaSApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), fmt.Sprintf("/saas-management/applications/%s/accounts", appID), api.V2ListOptions{
		Limit: limit,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = saasAccountDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newSaaSAccountGetCmd() *cobra.Command {
	var accountID string

	cmd := &cobra.Command{
		Use:   "account-get <catalog-app-id-or-id>",
		Short: "Get a specific account for a SaaS application",
		Long: `Get a specific account for a JumpCloud SaaS Management application.

Requires --account-id to specify which account to retrieve.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SaaSManagementConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaaSAccountGet(cmd, args[0], accountID)
		},
	}

	cmd.Flags().StringVar(&accountID, "account-id", "", "Account ID (required)")
	_ = cmd.MarkFlagRequired("account-id")

	return cmd
}

func runSaaSAccountGet(cmd *cobra.Command, identifier, accountID string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	appID, err := resolveSaaSApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), fmt.Sprintf("/saas-management/applications/%s/accounts/%s", appID, accountID))
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newSaaSAccountDeleteCmd() *cobra.Command {
	var accountID string

	cmd := &cobra.Command{
		Use:   "account-delete <catalog-app-id-or-id>",
		Short: "Delete an account from a SaaS application",
		Long: `Delete an account from a JumpCloud SaaS Management application.

Requires --account-id to specify which account to delete.
Use --force to skip the confirmation prompt.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SaaSManagementConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaaSAccountDelete(cmd, args[0], accountID)
		},
	}

	cmd.Flags().StringVar(&accountID, "account-id", "", "Account ID (required)")
	_ = cmd.MarkFlagRequired("account-id")

	return cmd
}

func runSaaSAccountDelete(cmd *cobra.Command, identifier, accountID string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	appID, err := resolveSaaSApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "SaaS account",
			Target:   fmt.Sprintf("account %s (app: %s)", accountID, appID),
			Effects:  []string{"Remove SaaS account"},
		}
		return renderPlan(cmd, p)
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete SaaS account %q from application %q? [y/N] ", accountID, appID)
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

	_, err = client.Delete(cmd.Context(), fmt.Sprintf("/saas-management/applications/%s/accounts/%s", appID, accountID))
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "SaaS account %q deleted successfully.\n", accountID)
	return nil
}

func newSaaSUsageCmd() *cobra.Command {
	var dayCount int

	cmd := &cobra.Command{
		Use:   "usage <catalog-app-id-or-id>",
		Short: "Get usage data for a SaaS application",
		Long: `Get usage data for a JumpCloud SaaS Management application.

Returns usage records for the specified number of days (default 30).`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SaaSManagementConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaaSUsage(cmd, args[0], dayCount)
		},
	}

	cmd.Flags().IntVar(&dayCount, "day-count", 30, "Number of days of usage data to return")

	return cmd
}

func runSaaSUsage(cmd *cobra.Command, identifier string, dayCount int) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	appID, err := resolveSaaSApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), fmt.Sprintf("/saas-management/applications/%s/usage?day_count=%d", appID, dayCount), api.V2ListOptions{})
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

func newSaaSLicensesCmd() *cobra.Command {
	var limitFlag int

	cmd := &cobra.Command{
		Use:   "licenses",
		Short: "List all SaaS application licenses",
		Long: `List all JumpCloud SaaS Management application licenses.

Default fields: id, contract_term, currency, renewal_date.
Use --output table for a readable ASCII table.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaaSLicenses(cmd, limitFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")

	return cmd
}

func runSaaSLicenses(cmd *cobra.Command, limit int) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/saas-management/application-licenses", api.V2ListOptions{
		Limit: limit,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = saasLicenseDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newSaaSCatalogGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog-get <catalog-app-id>",
		Short: "Get a SaaS application catalog entry",
		Long: `Get a SaaS application catalog entry by catalog app ID.

Accepts a catalog app ID (e.g., "jumpcloud", "slack").
Returns the catalog entry with name, description, and domains.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaaSCatalogGet(cmd, args[0])
		},
	}

	return cmd
}

func runSaaSCatalogGet(cmd *cobra.Command, catalogAppID string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/saas-management/application-catalog/"+catalogAppID)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
