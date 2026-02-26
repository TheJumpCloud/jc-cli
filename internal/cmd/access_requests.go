package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
)

var accessRequestDefaultFields = []string{"accessId", "requestorId", "resourceId", "accessState", "expiry"}

func newAccessRequestsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "access-requests",
		Aliases: []string{"ar"},
		Short:   "Manage JumpCloud access requests",
		Long:    "List, get, create, update, and revoke JumpCloud temporary elevated device privilege requests.",
	}

	cmd.AddCommand(newAccessRequestsListCmd())
	cmd.AddCommand(newAccessRequestsGetCmd())

	return cmd
}

func newAccessRequestsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all access requests",
		Long: `List all JumpCloud access requests.

Default fields: accessId, requestorId, resourceId, accessState, expiry.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'accessState:eq:granted'     Granted requests only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessRequestsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'accessState:eq:granted')")

	return cmd
}

func runAccessRequestsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/accessrequests", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = accessRequestDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newAccessRequestsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <access-id>",
		Short: "Get an access request by ID",
		Long: `Get a single JumpCloud access request by its access ID.

Accepts the 24-character hex access ID returned when creating a request.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessRequestsGet(cmd, args[0])
		},
	}
	return cmd
}

func runAccessRequestsGet(cmd *cobra.Command, id string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.Get(cmd.Context(), "/accessrequests/"+id)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
