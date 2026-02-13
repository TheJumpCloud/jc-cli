package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
)

// userDefaultFields is the default field subset shown for user list/table output.
var userDefaultFields = []string{"username", "email", "firstname", "lastname", "activated", "suspended"}

// newV1Client creates a V1 API client. Overridable in tests.
var newV1Client = func() (*api.V1Client, error) {
	return api.NewV1Client()
}

func newUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage JumpCloud users",
		Long:  "List, get, create, update, and delete JumpCloud system users.",
	}

	cmd.AddCommand(newUsersListCmd())
	cmd.AddCommand(newUsersGetCmd())

	return cmd
}

func newUsersListCmd() *cobra.Command {
	var (
		limitFlag int
		sortFlag  string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all users",
		Long: `List all JumpCloud system users.

Default fields: username, email, firstname, lastname, activated, suspended.
Use --output table for a readable ASCII table.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersList(cmd, limitFlag, sortFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -created)")

	return cmd
}

func runUsersList(cmd *cobra.Command, limit int, sort string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/systemusers", api.ListOptions{
		Limit: limit,
		Sort:  sort,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = userDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	// Print footer with count info (only in non-quiet, non-IDs mode).
	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newUsersGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <username-or-id>",
		Short: "Get a user by username or ID",
		Long: `Get a single JumpCloud user by ID.

Accepts a 24-character hex user ID. Name resolution (username → ID)
will be available in a future release.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersGet(cmd, args[0])
		},
	}

	return cmd
}

func runUsersGet(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/systemusers/"+identifier)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

// writeListFooter writes a "── N of TOTAL items ──" footer to stderr.
func writeListFooter(cmd *cobra.Command, count, total int) {
	if count == total {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", count)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d of %d items ──\n", count, total)
	}
}
