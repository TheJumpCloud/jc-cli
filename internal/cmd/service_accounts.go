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
	"github.com/klaassen-consulting/jc/internal/serviceaccount"
)

func resolveServiceAccount(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.ServiceAccountConfig)
}

func newServiceAccountsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "service-accounts",
		Aliases: []string{"service-account", "svc-accounts"},
		Short:   "Manage JumpCloud service accounts (API credentials for automation)",
		Long: `List, get, create, delete, and rotate credentials for JumpCloud service accounts.

A service account is a non-human identity that authenticates to the JumpCloud
API with an API key or OAuth client secret, scoped by a role. Creating a
service account (or rotating its credential) returns the secret ONCE — capture
it, it is not retrievable later.`,
	}
	cmd.AddCommand(newServiceAccountsListCmd())
	cmd.AddCommand(newServiceAccountsGetCmd())
	cmd.AddCommand(newServiceAccountsCreateCmd())
	cmd.AddCommand(newServiceAccountsDeleteCmd())
	cmd.AddCommand(newServiceAccountsRotateCmd())
	cmd.AddCommand(newServiceAccountsRevokeCmd())
	return cmd
}

func newServiceAccountsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all service accounts",
		Long: `List all JumpCloud service accounts.

Default fields: objectId, name, roleName, status, expiresAt.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceAccountsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'status=ACTIVE')")
	return cmd
}

func runServiceAccountsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.ListAll(cmd.Context(), "/service-accounts", api.V2ListOptions{
		Limit:       limit,
		Sort:        sort,
		Filter:      filter.ToV2Queries(exprs),
		ResponseKey: "results",
	})
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	opts.DefaultFields = serviceaccount.DefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}
	return nil
}

func newServiceAccountsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "get <name-or-id>",
		Short:             "Get a service account by name or ID",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.ServiceAccountConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceAccountsGet(cmd, args[0])
		},
	}
	return cmd
}

func runServiceAccountsGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveServiceAccount(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	result, err := client.Get(cmd.Context(), "/service-accounts/"+id)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), serviceaccount.Unwrap(result, "serviceAccount"), opts)
}

func newServiceAccountsCreateCmd() *cobra.Command {
	var (
		name     string
		role     string
		authType string
		lifetime string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a service account (returns the credential once)",
		Long: `Create a new JumpCloud service account.

The response includes the freshly minted credential (API key or client
secret) exactly once — capture it now, it cannot be retrieved later.

Required: --name, --role, --auth-type. --lifetime defaults to "90 Days".
Supported auth types: api_key, client_secret.
Supported lifetimes: "30 Days", "60 Days", "90 Days", "365 Days".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceAccountsCreate(cmd, name, role, authType, lifetime)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Service account name (required)")
	cmd.Flags().StringVar(&role, "role", "", "Role name or ID to scope the account (required)")
	cmd.Flags().StringVar(&authType, "auth-type", "", "Credential type: api_key or client_secret (required)")
	cmd.Flags().StringVar(&lifetime, "lifetime", "90 Days", `Credential lifetime: "30 Days", "60 Days", "90 Days", "365 Days"`)
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("role")
	_ = cmd.MarkFlagRequired("auth-type")
	return cmd
}

func runServiceAccountsCreate(cmd *cobra.Command, name, role, authType, lifetime string) error {
	authConfig, err := serviceaccount.BuildAuthConfig(authType, lifetime)
	if err != nil {
		return err
	}
	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:     "create",
			Resource:   "service account",
			Target:     name,
			Effects:    []string{"role: " + role, "auth-type: " + authType, "lifetime: " + lifetime, "mints a credential (shown once)"},
			Reversible: true,
		})
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	roleID, err := resolveRole(cmd.Context(), client, role)
	if err != nil {
		return err
	}
	body := map[string]any{
		"name":       name,
		"roleId":     roleID,
		"authConfig": authConfig,
	}
	result, err := client.Create(cmd.Context(), "/service-accounts", body)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), serviceaccount.Unwrap(result, "serviceAccount"), opts)
}

func newServiceAccountsDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "delete <name-or-id>",
		Aliases:           []string{"rm"},
		Short:             "Delete a service account",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.ServiceAccountConfig),
		RunE:              batchRunE("service account", "delete", runServiceAccountsDelete),
	}
	addBatchSourceFlags(cmd)
	return cmd
}

func runServiceAccountsDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveServiceAccount(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	// Fetch for the confirmation prompt.
	raw, err := client.Get(cmd.Context(), "/service-accounts/"+id)
	if err != nil {
		return err
	}
	var sa struct {
		Name string `json:"name"`
	}
	// The live GET is wrapped in {serviceAccount:…}; unwrap so the
	// confirmation/success messages show the real name, not "".
	_ = json.Unmarshal(serviceaccount.Unwrap(raw, "serviceAccount"), &sa)

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "delete",
			Resource: "service account",
			Target:   fmt.Sprintf("%s (%s)", sa.Name, id),
			Effects:  []string{"Revokes all of its credentials immediately"},
		})
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete service account %q? Its credentials stop working immediately. [y/N] ", sa.Name)
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
	if _, err := client.Delete(cmd.Context(), "/service-accounts/"+id); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Service account %q deleted successfully.\n", sa.Name)
	return nil
}

func newServiceAccountsRotateCmd() *cobra.Command {
	var (
		authType string
		lifetime string
	)
	cmd := &cobra.Command{
		Use:   "rotate <name-or-id>",
		Short: "Add a new credential to a service account (returns it once)",
		Long: `Add a new authentication config (API key or client secret) to an existing
service account. The response includes the new credential exactly once.

The old credential keeps working until you revoke it — rotate, update your
automation, then revoke the old auth-config id.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.ServiceAccountConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceAccountsRotate(cmd, args[0], authType, lifetime)
		},
	}
	cmd.Flags().StringVar(&authType, "auth-type", "", "Credential type: api_key or client_secret (required)")
	cmd.Flags().StringVar(&lifetime, "lifetime", "90 Days", `Credential lifetime: "30 Days", "60 Days", "90 Days", "365 Days"`)
	_ = cmd.MarkFlagRequired("auth-type")
	return cmd
}

func runServiceAccountsRotate(cmd *cobra.Command, identifier, authType, lifetime string) error {
	authConfig, err := serviceaccount.BuildAuthConfig(authType, lifetime)
	if err != nil {
		return err
	}
	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:     "rotate credential",
			Resource:   "service account",
			Target:     identifier,
			Effects:    []string{"auth-type: " + authType, "lifetime: " + lifetime, "mints a new credential; the old one keeps working until revoked"},
			Reversible: true,
		})
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveServiceAccount(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	result, err := client.Create(cmd.Context(), "/service-accounts/"+id+"/auth-config", authConfig)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), serviceaccount.Unwrap(result, "authConfig"), opts)
}

func newServiceAccountsRevokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <name-or-id> <auth-config-id>",
		Short: "Revoke a specific credential (auth-config) of a service account",
		Long: `Delete one authentication config from a service account, immediately
invalidating that credential. Find the auth-config ids in the account's
authConfigList (jc service-accounts get <name> -a).`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.ServiceAccountConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceAccountsRevoke(cmd, args[0], args[1])
		},
	}
	return cmd
}

func runServiceAccountsRevoke(cmd *cobra.Command, identifier, authConfigID string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveServiceAccount(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "revoke credential",
			Resource: "service account",
			Target:   identifier,
			Effects:  []string{"auth-config " + authConfigID + " stops working immediately"},
		})
	}
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Revoke credential %s? It stops working immediately. [y/N] ", authConfigID)
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
	if _, err := client.Delete(cmd.Context(), "/service-accounts/"+id+"/auth-config/"+authConfigID); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Credential %s revoked.\n", authConfigID)
	return nil
}

// resolveRole resolves a role name or ID to a role ID via the /roles endpoint.
func resolveRole(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.RoleConfig)
}
