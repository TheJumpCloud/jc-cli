package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
)

// adminDefaultFields is the default field subset shown for admins list/table output.
// Uses _id (V1 convention) since the /users endpoint is a V1-style API.
var adminDefaultFields = []string{"_id", "email", "roleName", "enableMultiFactor"}

func newAdminsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admins",
		Short: "Manage JumpCloud administrators",
		Long:  "List JumpCloud administrators to audit admin access to the organization.",
	}

	cmd.AddCommand(newAdminsListCmd())

	return cmd
}

func newAdminsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all administrators",
		Long: `List all JumpCloud administrators with email, role, and MFA status.

Default fields: _id, email, roleName, enableMultiFactor.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'roleName=Administrator'    Filter by admin role
  --filter 'email=admin@acme.com'      Filter by email`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdminsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -email)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'roleName=Administrator')")

	return cmd
}

func runAdminsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	// Admins use the V1-style /users endpoint (not V2 /administrators which doesn't exist).
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/users", api.ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV1Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = adminDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d of %d items ──\n", len(result.Data), result.TotalCount)
	}

	return nil
}
