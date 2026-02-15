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
		Long:  "List, get, create, update, and delete JumpCloud LDAP server integrations, including Samba domain sub-resources.",
	}

	cmd.AddCommand(newLDAPListCmd())
	cmd.AddCommand(newLDAPGetCmd())
	cmd.AddCommand(newLDAPCreateCmd())
	cmd.AddCommand(newLDAPUpdateCmd())
	cmd.AddCommand(newLDAPDeleteCmd())
	cmd.AddCommand(newLDAPSambaDomainsCmd())
	cmd.AddCommand(newLDAPSambaDomainGetCmd())
	cmd.AddCommand(newLDAPSambaDomainCreateCmd())
	cmd.AddCommand(newLDAPSambaDomainUpdateCmd())
	cmd.AddCommand(newLDAPSambaDomainDeleteCmd())

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

// --- Samba Domain sub-resource commands ---

// sambaDomainDefaultFields is the default field subset shown for samba domain output.
var sambaDomainDefaultFields = []string{"id", "name", "sid"}

func newLDAPSambaDomainsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "samba-domains <ldap-name-or-id>",
		Aliases: []string{"samba-ls"},
		Short:   "List Samba domains for an LDAP server",
		Long: `List all Samba domains configured on a JumpCloud LDAP server.

Accepts an LDAP server name or 24-character hex ID.
Default fields: id, name, sid.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLDAPSambaDomainsList(cmd, args[0])
		},
	}

	return cmd
}

func runLDAPSambaDomainsList(cmd *cobra.Command, ldapIdentifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	ldapID, err := resolveLDAP(cmd.Context(), client, ldapIdentifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/ldapservers/"+ldapID+"/sambadomains", api.V2ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = sambaDomainDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newLDAPSambaDomainGetCmd() *cobra.Command {
	var domainID string

	cmd := &cobra.Command{
		Use:   "samba-domain-get <ldap-name-or-id>",
		Short: "Get a Samba domain by ID",
		Long: `Get a single Samba domain from a JumpCloud LDAP server.

Requires --domain-id to identify the specific Samba domain.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLDAPSambaDomainGet(cmd, args[0], domainID)
		},
	}

	cmd.Flags().StringVar(&domainID, "domain-id", "", "Samba domain ID (required)")
	_ = cmd.MarkFlagRequired("domain-id")

	return cmd
}

func runLDAPSambaDomainGet(cmd *cobra.Command, ldapIdentifier, domainID string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	ldapID, err := resolveLDAP(cmd.Context(), client, ldapIdentifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/ldapservers/"+ldapID+"/sambadomains/"+domainID)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = sambaDomainDefaultFields
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newLDAPSambaDomainCreateCmd() *cobra.Command {
	var (
		name string
		sid  string
	)

	cmd := &cobra.Command{
		Use:   "samba-domain-create <ldap-name-or-id>",
		Short: "Create a Samba domain on an LDAP server",
		Long: `Create a new Samba domain on a JumpCloud LDAP server.

Required flags: --name, --sid.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLDAPSambaDomainCreate(cmd, args[0], name, sid)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Samba domain workgroup name (required)")
	cmd.Flags().StringVar(&sid, "sid", "", "Samba domain security identifier (required)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("sid")

	return cmd
}

func runLDAPSambaDomainCreate(cmd *cobra.Command, ldapIdentifier, name, sid string) error {
	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:     "create",
			Resource:   "Samba domain",
			Target:     name + " on LDAP " + ldapIdentifier,
			Effects:    []string{"name: " + name, "sid: " + sid},
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	ldapID, err := resolveLDAP(cmd.Context(), client, ldapIdentifier)
	if err != nil {
		return err
	}

	body := map[string]any{
		"name": name,
		"sid":  sid,
	}

	result, err := client.Create(cmd.Context(), "/ldapservers/"+ldapID+"/sambadomains", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = sambaDomainDefaultFields
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newLDAPSambaDomainUpdateCmd() *cobra.Command {
	var (
		domainID string
		name     string
		sid      string
	)

	cmd := &cobra.Command{
		Use:   "samba-domain-update <ldap-name-or-id>",
		Short: "Update a Samba domain on an LDAP server",
		Long: `Update an existing Samba domain on a JumpCloud LDAP server.

Requires --domain-id. Specify only the fields you want to change.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLDAPSambaDomainUpdate(cmd, args[0], domainID, name, sid)
		},
	}

	cmd.Flags().StringVar(&domainID, "domain-id", "", "Samba domain ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "New workgroup name")
	cmd.Flags().StringVar(&sid, "sid", "", "New security identifier")
	_ = cmd.MarkFlagRequired("domain-id")

	return cmd
}

func runLDAPSambaDomainUpdate(cmd *cobra.Command, ldapIdentifier, domainID, name, sid string) error {
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("sid") {
		body["sid"] = sid
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --sid)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "Samba domain",
			Target:     domainID + " on LDAP " + ldapIdentifier,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	ldapID, err := resolveLDAP(cmd.Context(), client, ldapIdentifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/ldapservers/"+ldapID+"/sambadomains/"+domainID, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = sambaDomainDefaultFields
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newLDAPSambaDomainDeleteCmd() *cobra.Command {
	var domainID string

	cmd := &cobra.Command{
		Use:     "samba-domain-delete <ldap-name-or-id>",
		Aliases: []string{"samba-rm"},
		Short:   "Delete a Samba domain from an LDAP server",
		Long: `Delete a Samba domain from a JumpCloud LDAP server.

Requires --domain-id. Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLDAPSambaDomainDelete(cmd, args[0], domainID)
		},
	}

	cmd.Flags().StringVar(&domainID, "domain-id", "", "Samba domain ID (required)")
	_ = cmd.MarkFlagRequired("domain-id")

	return cmd
}

func runLDAPSambaDomainDelete(cmd *cobra.Command, ldapIdentifier, domainID string) error {
	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "Samba domain",
			Target:   domainID + " on LDAP " + ldapIdentifier,
			Effects:  []string{"Remove Samba domain"},
		}
		return renderPlan(cmd, p)
	}

	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete Samba domain %q from LDAP %q? [y/N] ", domainID, ldapIdentifier)
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

	client, err := newV2Client()
	if err != nil {
		return err
	}

	ldapID, err := resolveLDAP(cmd.Context(), client, ldapIdentifier)
	if err != nil {
		return err
	}

	_, err = client.Delete(cmd.Context(), "/ldapservers/"+ldapID+"/sambadomains/"+domainID)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Samba domain %q deleted successfully.\n", domainID)
	return nil
}
