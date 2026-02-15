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

var policyGroupDefaultFields = []string{"id", "name", "description"}

func resolvePolicyGroup(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.PolicyGroupConfig)
}

func newPolicyGroupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy-groups",
		Short: "Manage policy groups",
		Long:  "List, get, create, update, and delete JumpCloud policy groups.",
	}

	cmd.AddCommand(newPolicyGroupsListCmd())
	cmd.AddCommand(newPolicyGroupsGetCmd())
	cmd.AddCommand(newPolicyGroupsCreateCmd())
	cmd.AddCommand(newPolicyGroupsUpdateCmd())
	cmd.AddCommand(newPolicyGroupsDeleteCmd())

	return cmd
}

func newPolicyGroupsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all policy groups",
		Long: `List all JumpCloud policy groups.

Default fields: id, name, description.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=Security Policies'     Exact match`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPolicyGroupsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=Security Policies')")

	return cmd
}

func runPolicyGroupsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/policygroups", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = policyGroupDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newPolicyGroupsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get a policy group by name or ID",
		Long: `Get a single JumpCloud policy group by name or ID.

Accepts a group name (e.g., "Security Policies") or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPolicyGroupsGet(cmd, args[0])
		},
	}
}

func runPolicyGroupsGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolvePolicyGroup(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/policygroups/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newPolicyGroupsCreateCmd() *cobra.Command {
	var (
		name        string
		description string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new policy group",
		Long: `Create a new JumpCloud policy group.

Required fields: --name.
The newly created policy group object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPolicyGroupsCreate(cmd, name, description)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Policy group name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Policy group description")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runPolicyGroupsCreate(cmd *cobra.Command, name, description string) error {
	if viper.GetBool("plan") {
		effects := []string{"name: " + name}
		if description != "" {
			effects = append(effects, "description: "+description)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "policy group",
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
	if description != "" {
		body["description"] = description
	}

	result, err := client.Create(cmd.Context(), "/policygroups", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newPolicyGroupsUpdateCmd() *cobra.Command {
	var (
		name        string
		description string
	)

	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update a policy group",
		Long: `Update an existing JumpCloud policy group.

Accepts a group name or 24-character hex ID.
Specify only the fields you want to change. The updated policy group object is returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPolicyGroupsUpdate(cmd, args[0], name, description)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Policy group name")
	cmd.Flags().StringVar(&description, "description", "", "Policy group description")

	return cmd
}

func runPolicyGroupsUpdate(cmd *cobra.Command, identifier, name, description string) error {
	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("description") {
		body["description"] = description
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --description)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "policy group",
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

	id, err := resolvePolicyGroup(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/policygroups/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newPolicyGroupsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a policy group",
		Long: `Delete a JumpCloud policy group.

Accepts a group name or 24-character hex ID.
Shows the group name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPolicyGroupsDelete(cmd, args[0])
		},
	}
}

func runPolicyGroupsDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolvePolicyGroup(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	groupData, err := client.Get(cmd.Context(), "/policygroups/"+id)
	if err != nil {
		return err
	}

	var group struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(groupData, &group); err != nil {
		return fmt.Errorf("parsing policy group data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "policy group",
			Target:   fmt.Sprintf("%s (%s)", group.Name, id),
			Effects:  []string{"Remove policy group and all policy associations"},
		}
		return renderPlan(cmd, p)
	}

	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete policy group %q? [y/N] ", group.Name)
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

	_, err = client.Delete(cmd.Context(), "/policygroups/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Policy group %q deleted successfully.\n", group.Name)
	return nil
}
