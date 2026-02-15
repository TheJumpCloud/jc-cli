package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
)

var appTemplateDefaultFields = []string{"_id", "name", "displayName", "displayLabel", "active"}

func newAppTemplatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app-templates",
		Short: "View JumpCloud application templates",
		Long:  "List and view JumpCloud application templates (read-only).",
	}

	cmd.AddCommand(newAppTemplatesListCmd())
	cmd.AddCommand(newAppTemplatesGetCmd())

	return cmd
}

func newAppTemplatesListCmd() *cobra.Command {
	var (
		limitFlag int
		sortFlag  string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all application templates",
		Long: `List all JumpCloud application templates.

Default fields: _id, name, displayName, displayLabel, active.
Use --output table for a readable ASCII table.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppTemplatesList(cmd, limitFlag, sortFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")

	return cmd
}

func runAppTemplatesList(cmd *cobra.Command, limit int, sort string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/application-templates", api.ListOptions{
		Limit: limit,
		Sort:  sort,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = appTemplateDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newAppTemplatesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get an application template by ID",
		Long: `Get a single JumpCloud application template by its ID.

Application templates are provided by JumpCloud and referenced by ID.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppTemplatesGet(cmd, args[0])
		},
	}

	return cmd
}

func runAppTemplatesGet(cmd *cobra.Command, id string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), fmt.Sprintf("/application-templates/%s", id))
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
