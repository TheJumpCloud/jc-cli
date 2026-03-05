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

// policyDefaultFields is the default field subset shown for policy list/table output.
var policyDefaultFields = []string{"id", "name", "template", "os"}

// policyResultDefaultFields is the default field subset shown for policy results output.
var policyResultDefaultFields = []string{"id", "policyID", "systemID", "status", "startedAt", "endedAt"}

// resolvePolicy resolves a policy name or ID to a JumpCloud policy ID.
func resolvePolicy(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.PolicyConfig)
}

func newPoliciesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policies",
		Short: "Manage JumpCloud policies",
		Long:  "List, get, create, update, and delete JumpCloud policies, and view policy application results.",
	}

	cmd.AddCommand(newPoliciesListCmd())
	cmd.AddCommand(newPoliciesGetCmd())
	cmd.AddCommand(newPoliciesCreateCmd())
	cmd.AddCommand(newPoliciesUpdateCmd())
	cmd.AddCommand(newPoliciesDeleteCmd())
	cmd.AddCommand(newPoliciesResultsCmd())

	return cmd
}

func newPoliciesListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all policies",
		Long: `List all JumpCloud policies.

Default fields: id, name, template, os.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=Disk Encryption'     Exact match
  --filter 'os=darwin'                Filter by OS target`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPoliciesList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=Disk Encryption')")

	return cmd
}

func runPoliciesList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/policies", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = policyDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newPoliciesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <policy-name-or-id>",
		Short: "Get a policy by name or ID",
		Long: `Get a single JumpCloud policy by name or ID.

Accepts a policy name (e.g., "Disk Encryption") or a 24-character hex policy ID.
Policy names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args:               cobra.ExactArgs(1),
		ValidArgsFunction:  completeResourceNames(resolve.PolicyConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPoliciesGet(cmd, args[0])
		},
	}

	return cmd
}

func runPoliciesGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolvePolicy(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/policies/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newPoliciesResultsCmd() *cobra.Command {
	var (
		limitFlag int
		sortFlag  string
	)

	cmd := &cobra.Command{
		Use:   "results <policy-name-or-id>",
		Short: "List policy application results per device",
		Long: `List policy application results (policystatuses) for a JumpCloud policy.

Accepts a policy name or 24-character hex policy ID.
Results show the device, status (applied/pending/failed), and timestamp for each application.

Default fields: id, policyID, systemID, status, startedAt, endedAt.
Use --output table for quick scanning of results.`,
		Args:               cobra.ExactArgs(1),
		ValidArgsFunction:  completeResourceNames(resolve.PolicyConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPoliciesResults(cmd, args[0], limitFlag, sortFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -startedAt)")

	return cmd
}

func runPoliciesResults(cmd *cobra.Command, identifier string, limit int, sort string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	policyID, err := resolvePolicy(ctx, client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(ctx, "/policies/"+policyID+"/policystatuses", api.V2ListOptions{
		Limit: limit,
		Sort:  sort,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = policyResultDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newPoliciesCreateCmd() *cobra.Command {
	var (
		name       string
		templateID string
		values     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new policy",
		Long: `Create a new JumpCloud policy.

Required fields: --name, --template-id.
Use --values to pass template-specific configuration as a JSON string.
The newly created policy object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPoliciesCreate(cmd, name, templateID, values)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Policy name (required)")
	cmd.Flags().StringVar(&templateID, "template-id", "", "Policy template ID (required)")
	cmd.Flags().StringVar(&values, "values", "", "Template-specific config as JSON")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("template-id")

	return cmd
}

func runPoliciesCreate(cmd *cobra.Command, name, templateID, values string) error {
	if viper.GetBool("plan") {
		effects := []string{"name: " + name, "template: " + templateID}
		if values != "" {
			effects = append(effects, "values: (custom JSON)")
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "policy",
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
		"name":     name,
		"template": map[string]any{"id": templateID},
	}
	if values != "" {
		var v map[string]any
		if err := json.Unmarshal([]byte(values), &v); err != nil {
			return fmt.Errorf("invalid --values JSON: %w", err)
		}
		body["values"] = v
	}

	result, err := client.Create(cmd.Context(), "/policies", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newPoliciesUpdateCmd() *cobra.Command {
	var (
		name   string
		values string
	)

	cmd := &cobra.Command{
		Use:   "update <policy-name-or-id>",
		Short: "Update a policy",
		Long: `Update an existing JumpCloud policy.

Accepts a policy name or 24-character hex ID.
Specify only the fields you want to change. The updated policy object is returned.`,
		Args:               cobra.ExactArgs(1),
		ValidArgsFunction:  completeResourceNames(resolve.PolicyConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPoliciesUpdate(cmd, args[0], name, values)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Policy name")
	cmd.Flags().StringVar(&values, "values", "", "Template-specific config as JSON")

	return cmd
}

func runPoliciesUpdate(cmd *cobra.Command, identifier, name, values string) error {
	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("values") {
		var v map[string]any
		if err := json.Unmarshal([]byte(values), &v); err != nil {
			return fmt.Errorf("invalid --values JSON: %w", err)
		}
		body["values"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --values)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "policy",
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

	id, err := resolvePolicy(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/policies/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newPoliciesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <policy-name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a policy",
		Long: `Delete a JumpCloud policy.

Accepts a policy name or 24-character hex ID.
Shows the policy name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args:               cobra.ExactArgs(1),
		ValidArgsFunction:  completeResourceNames(resolve.PolicyConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPoliciesDelete(cmd, args[0])
		},
	}

	return cmd
}

func runPoliciesDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolvePolicy(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the policy first so we can show details in the confirmation/plan.
	policyData, err := client.Get(cmd.Context(), "/policies/"+id)
	if err != nil {
		return err
	}

	var policy struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(policyData, &policy); err != nil {
		return fmt.Errorf("parsing policy data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "policy",
			Target:   fmt.Sprintf("%s (%s)", policy.Name, id),
			Effects:  []string{"Remove policy permanently"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete policy %q? [y/N] ", policy.Name)
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

	_, err = client.Delete(cmd.Context(), "/policies/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Policy %q deleted successfully.\n", policy.Name)
	return nil
}
