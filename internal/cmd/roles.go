package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// roleDefaultFields is the default field subset shown for role output.
var roleDefaultFields = []string{"id", "name", "description"}

func newRolesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "roles",
		Short: "Manage JumpCloud roles (RBAC scopes for admins and service accounts)",
		Long: `List, get, create, update, and delete JumpCloud roles.

A role is a named set of API scopes. Roles scope what an administrator or a
service account may do; see 'jc service-accounts create --role'.`,
	}
	cmd.AddCommand(newRolesListCmd())
	cmd.AddCommand(newRolesGetCmd())
	cmd.AddCommand(newRolesCreateCmd())
	cmd.AddCommand(newRolesUpdateCmd())
	cmd.AddCommand(newRolesDeleteCmd())
	return cmd
}

func newRolesListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all roles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRolesList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=Administrator')")
	return cmd
}

func runRolesList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.ListAll(cmd.Context(), "/roles", api.V2ListOptions{
		Limit:       limit,
		Sort:        sort,
		Filter:      filter.ToV2Queries(exprs),
		ResponseKey: "results",
	})
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	opts.DefaultFields = roleDefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}
	return nil
}

func newRolesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "get <name-or-id>",
		Short:             "Get a role by name or ID",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.RoleConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRolesGet(cmd, args[0])
		},
	}
	return cmd
}

func runRolesGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveRole(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	result, err := client.Get(cmd.Context(), "/roles/"+id)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

// splitScopes turns a comma-separated --scopes value into a trimmed,
// empty-free slice.
func splitScopes(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func newRolesCreateCmd() *cobra.Command {
	var (
		name        string
		scopes      string
		description string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a role",
		Long: `Create a new JumpCloud role.

Required: --name and --scopes (comma-separated API scopes, e.g.
"users,groups,commands"). Use 'jc roles get Administrator -a' to see the
full scope set of an existing role as a reference.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRolesCreate(cmd, name, scopes, description)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Role name (required)")
	cmd.Flags().StringVar(&scopes, "scopes", "", "Comma-separated API scopes (required)")
	cmd.Flags().StringVar(&description, "description", "", "Role description")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("scopes")
	return cmd
}

func runRolesCreate(cmd *cobra.Command, name, scopes, description string) error {
	scopeList := splitScopes(scopes)
	if len(scopeList) == 0 {
		return fmt.Errorf("--scopes must list at least one scope")
	}
	body := map[string]any{"name": name, "scopes": scopeList}
	if description != "" {
		body["description"] = description
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.Create(cmd.Context(), "/roles", body)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newRolesUpdateCmd() *cobra.Command {
	var (
		name        string
		scopes      string
		description string
	)
	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update a role",
		Long: `Update an existing JumpCloud role.

Specify only the fields you want to change; all others are preserved. The
role PUT is a full-object replace, so this command reads the current role,
applies your changes, and writes it back.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.RoleConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRolesUpdate(cmd, args[0], name, scopes, description)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New role name")
	cmd.Flags().StringVar(&scopes, "scopes", "", "New comma-separated API scopes (replaces the set)")
	cmd.Flags().StringVar(&description, "description", "", "New role description")
	return cmd
}

func runRolesUpdate(cmd *cobra.Command, identifier, name, scopes, description string) error {
	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("scopes") && !cmd.Flags().Changed("description") {
		return fmt.Errorf("no fields to update. Specify at least one of --name, --scopes, --description")
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveRole(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	// Read-modify-write: the role PUT is a full-object replace (name and
	// scopes are both required), so fetch the current role and apply only
	// the changed fields rather than clobbering unspecified ones.
	current, err := client.Get(cmd.Context(), "/roles/"+id)
	if err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(current, &obj); err != nil {
		return fmt.Errorf("parsing current role: %w", err)
	}
	if cmd.Flags().Changed("name") {
		obj["name"] = name
	}
	if cmd.Flags().Changed("scopes") {
		scopeList := splitScopes(scopes)
		if len(scopeList) == 0 {
			return fmt.Errorf("--scopes must list at least one scope")
		}
		obj["scopes"] = scopeList
	}
	if cmd.Flags().Changed("description") {
		obj["description"] = description
	}
	delete(obj, "id")
	result, err := client.Update(cmd.Context(), "/roles/"+id, obj)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newRolesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "delete <name-or-id>",
		Aliases:           []string{"rm"},
		Short:             "Delete a role",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.RoleConfig),
		RunE:              batchRunE("role", "delete", runRolesDelete),
	}
	addBatchSourceFlags(cmd)
	return cmd
}

func runRolesDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveRole(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	raw, err := client.Get(cmd.Context(), "/roles/"+id)
	if err != nil {
		return err
	}
	var role struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &role)

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete role %q? Admins or service accounts using it lose their scopes. [y/N] ", role.Name)
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
	if _, err := client.Delete(cmd.Context(), "/roles/"+id); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Role %q deleted successfully.\n", role.Name)
	return nil
}
