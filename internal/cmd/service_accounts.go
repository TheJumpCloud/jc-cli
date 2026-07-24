package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// serviceAccountDefaultFields is the default field subset shown for service
// account list/get output. Secrets (apiKey/clientSecret) live under
// authConfigList and surface only with -a / on create.
var serviceAccountDefaultFields = []string{"objectId", "name", "roleName", "status", "expiresAt"}

// validAuthTypes maps the friendly CLI value to the API enum.
var validAuthTypes = map[string]string{"api_key": "API_KEY", "client_secret": "CLIENT_SECRET"}

// validLifetimes is the set the API accepts for an API key / client secret.
var validLifetimes = map[string]bool{"30 Days": true, "60 Days": true, "90 Days": true, "365 Days": true}

func resolveServiceAccount(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.ServiceAccountConfig)
}

// unwrapField returns raw[key] when the response is a single-key envelope
// (the live API wraps get in {serviceAccount:…}, create in {serviceAccount:…},
// and rotate in {authConfig:…} despite the spec showing the bare object).
// Falls back to raw untouched if the key is absent or the body isn't an object.
func unwrapField(raw json.RawMessage, key string) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	if inner, ok := obj[key]; ok {
		return inner
	}
	return raw
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
	opts.DefaultFields = serviceAccountDefaultFields
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
	return output.WriteSingle(cmd.OutOrStdout(), unwrapField(result, "serviceAccount"), opts)
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

// buildAuthConfig validates the auth-type/lifetime pair and returns the
// authConfig body fragment the API expects.
func buildAuthConfig(authType, lifetime string) (map[string]any, error) {
	apiType, ok := validAuthTypes[strings.ToLower(strings.TrimSpace(authType))]
	if !ok {
		return nil, fmt.Errorf("invalid --auth-type %q: must be api_key or client_secret", authType)
	}
	if !validLifetimes[lifetime] {
		return nil, fmt.Errorf(`invalid --lifetime %q: must be one of "30 Days", "60 Days", "90 Days", "365 Days"`, lifetime)
	}
	cfg := map[string]any{"authType": apiType}
	if apiType == "API_KEY" {
		cfg["apiKeyConfig"] = map[string]any{"lifetime": lifetime}
	} else {
		cfg["clientSecretConfig"] = map[string]any{"lifetime": lifetime}
	}
	return cfg, nil
}

func runServiceAccountsCreate(cmd *cobra.Command, name, role, authType, lifetime string) error {
	authConfig, err := buildAuthConfig(authType, lifetime)
	if err != nil {
		return err
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
	return output.WriteSingle(cmd.OutOrStdout(), unwrapField(result, "serviceAccount"), opts)
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
	_ = json.Unmarshal(unwrapField(raw, "serviceAccount"), &sa)

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
	authConfig, err := buildAuthConfig(authType, lifetime)
	if err != nil {
		return err
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
	return output.WriteSingle(cmd.OutOrStdout(), unwrapField(result, "authConfig"), opts)
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
