package cmd

import (
	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
)

// orgDefaultFields is the default field subset shown for organization output.
var orgDefaultFields = []string{"_id", "displayName", "created"}

func newOrgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "View JumpCloud organization settings",
		Long:  "List and view JumpCloud organization details.",
	}

	cmd.AddCommand(newOrgListCmd())
	cmd.AddCommand(newOrgGetCmd())

	return cmd
}

func newOrgListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List organizations",
		Long: `List all JumpCloud organizations.

Default fields: _id, displayName, created.
Most JumpCloud accounts have a single organization.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOrgList(cmd)
		},
	}

	return cmd
}

func runOrgList(cmd *cobra.Command) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/organizations", api.ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = orgDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newOrgGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <org-id>",
		Short: "Get an organization by ID",
		Long: `Get a single JumpCloud organization by its ID.

Accepts a 24-character hex organization ID.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOrgGet(cmd, args[0])
		},
	}

	return cmd
}

func runOrgGet(cmd *cobra.Command, id string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/organizations/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
