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

// ldapDefaultFields is the default field subset shown for LDAP server output.
var ldapDefaultFields = []string{"id", "name", "userLockoutAction", "userPasswordExpirationAction"}

// resolveLDAP resolves an LDAP server name or ID to a JumpCloud LDAP server ID.
func resolveLDAP(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.LDAPServerConfig)
}

func newLDAPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ldap",
		Short: "Manage JumpCloud LDAP servers",
		Long:  "List, get, create, update, and delete JumpCloud LDAP server integrations.",
	}

	cmd.AddCommand(newLDAPListCmd())
	cmd.AddCommand(newLDAPGetCmd())
	cmd.AddCommand(newLDAPCreateCmd())
	cmd.AddCommand(newLDAPUpdateCmd())
	cmd.AddCommand(newLDAPDeleteCmd())

	return cmd
}

func newLDAPListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all LDAP servers",
		Long: `List all JumpCloud LDAP servers.

Default fields: id, name, userLockoutAction, userPasswordExpirationAction.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=jumpcloud'     Exact match`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLDAPList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=jumpcloud')")

	return cmd
}

func runLDAPList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/ldapservers", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = ldapDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newLDAPGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get an LDAP server by name or ID",
		Long: `Get a single JumpCloud LDAP server by name or ID.

Accepts an LDAP server name (e.g., "jumpcloud") or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLDAPGet(cmd, args[0])
		},
	}

	return cmd
}

func runLDAPGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveLDAP(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/ldapservers/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newLDAPCreateCmd() *cobra.Command {
	var (
		name                         string
		userLockoutAction            string
		userPasswordExpirationAction string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new LDAP server",
		Long: `Create a new JumpCloud LDAP server.

Required fields: --name.
The newly created LDAP server object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLDAPCreate(cmd, name, userLockoutAction, userPasswordExpirationAction)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "LDAP server name (required)")
	cmd.Flags().StringVar(&userLockoutAction, "user-lockout-action", "", "User lockout action (e.g. maintain, disable)")
	cmd.Flags().StringVar(&userPasswordExpirationAction, "user-password-expiration-action", "", "User password expiration action (e.g. maintain, disable)")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runLDAPCreate(cmd *cobra.Command, name, userLockoutAction, userPasswordExpirationAction string) error {
	if viper.GetBool("plan") {
		effects := []string{"name: " + name}
		if userLockoutAction != "" {
			effects = append(effects, "userLockoutAction: "+userLockoutAction)
		}
		if userPasswordExpirationAction != "" {
			effects = append(effects, "userPasswordExpirationAction: "+userPasswordExpirationAction)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "LDAP server",
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
	if userLockoutAction != "" {
		body["userLockoutAction"] = userLockoutAction
	}
	if userPasswordExpirationAction != "" {
		body["userPasswordExpirationAction"] = userPasswordExpirationAction
	}

	result, err := client.Create(cmd.Context(), "/ldapservers", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newLDAPUpdateCmd() *cobra.Command {
	var (
		name                         string
		userLockoutAction            string
		userPasswordExpirationAction string
	)

	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update an LDAP server",
		Long: `Update an existing JumpCloud LDAP server.

Accepts an LDAP server name or 24-character hex ID.
Specify only the fields you want to change. The updated LDAP server object is returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLDAPUpdate(cmd, args[0], name, userLockoutAction, userPasswordExpirationAction)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "LDAP server name")
	cmd.Flags().StringVar(&userLockoutAction, "user-lockout-action", "", "User lockout action (e.g. maintain, disable)")
	cmd.Flags().StringVar(&userPasswordExpirationAction, "user-password-expiration-action", "", "User password expiration action (e.g. maintain, disable)")

	return cmd
}

func runLDAPUpdate(cmd *cobra.Command, identifier, name, userLockoutAction, userPasswordExpirationAction string) error {
	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("user-lockout-action") {
		body["userLockoutAction"] = userLockoutAction
	}
	if cmd.Flags().Changed("user-password-expiration-action") {
		body["userPasswordExpirationAction"] = userPasswordExpirationAction
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --user-lockout-action, --user-password-expiration-action)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "LDAP server",
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

	id, err := resolveLDAP(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/ldapservers/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newLDAPDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete an LDAP server",
		Long: `Delete a JumpCloud LDAP server.

Accepts an LDAP server name or 24-character hex ID.
Shows the LDAP server name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLDAPDelete(cmd, args[0])
		},
	}

	return cmd
}

func runLDAPDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveLDAP(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the LDAP server first so we can show details in the confirmation/plan.
	ldapData, err := client.Get(cmd.Context(), "/ldapservers/"+id)
	if err != nil {
		return err
	}

	var ldapServer struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(ldapData, &ldapServer); err != nil {
		return fmt.Errorf("parsing LDAP server data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "LDAP server",
			Target:   fmt.Sprintf("%s (%s)", ldapServer.Name, id),
			Effects:  []string{"Remove LDAP server integration"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete LDAP server %q? [y/N] ", ldapServer.Name)
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

	_, err = client.Delete(cmd.Context(), "/ldapservers/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "LDAP server %q deleted successfully.\n", ldapServer.Name)
	return nil
}
