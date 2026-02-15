package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
)

var policyTemplateDefaultFields = []string{"id", "name", "description", "osMetaFamily"}

func newPolicyTemplatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy-templates",
		Short: "View policy templates",
		Long:  "List and view JumpCloud policy templates. Templates define the schema for policies.",
	}

	cmd.AddCommand(newPolicyTemplatesListCmd())
	cmd.AddCommand(newPolicyTemplatesGetCmd())

	return cmd
}

func newPolicyTemplatesListCmd() *cobra.Command {
	var (
		limitFlag  int
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all policy templates",
		Long: `List all JumpCloud policy templates.

Default fields: id, name, description, osMetaFamily.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'osMetaFamily=darwin'     macOS templates only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPolicyTemplatesList(cmd, limitFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'osMetaFamily=darwin')")

	return cmd
}

func runPolicyTemplatesList(cmd *cobra.Command, limit int, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/policytemplates", api.V2ListOptions{
		Limit:  limit,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = policyTemplateDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newPolicyTemplatesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get a policy template by ID",
		Long: `Get a single JumpCloud policy template by its ID.

Policy template IDs can be found in the output of 'jc policies get'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPolicyTemplatesGet(cmd, args[0])
		},
	}
}

func runPolicyTemplatesGet(cmd *cobra.Command, id string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/policytemplates/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
