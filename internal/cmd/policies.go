package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
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
		Long:  "List policies, get policy details, and view policy application results.",
	}

	cmd.AddCommand(newPoliciesListCmd())
	cmd.AddCommand(newPoliciesGetCmd())
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
		Use:   "list",
		Short: "List all policies",
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
		Args: cobra.ExactArgs(1),
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
		Args: cobra.ExactArgs(1),
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
