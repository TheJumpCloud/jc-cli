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

// office365DefaultFields is the default field subset shown for Office 365 integration output.
var office365DefaultFields = []string{"id", "name", "defaultDomain"}

// office365TranslationRuleDefaultFields is the default field subset for translation rules.
var office365TranslationRuleDefaultFields = []string{"id", "builtIn"}

// office365ImportUserDefaultFields is the default field subset for importable users.
var office365ImportUserDefaultFields = []string{"email", "firstname", "lastname", "status"}

// resolveOffice365 resolves an Office 365 instance name or ID to a JumpCloud Office 365 ID.
func resolveOffice365(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.Office365Config)
}

func newOffice365Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "office365",
		Short: "Manage JumpCloud Office 365 integrations",
		Long:  "List, get, and inspect JumpCloud Office 365 directory integrations.",
	}

	cmd.AddCommand(newOffice365ListCmd())
	cmd.AddCommand(newOffice365GetCmd())
	cmd.AddCommand(newOffice365TranslationRulesCmd())
	cmd.AddCommand(newOffice365ImportUsersCmd())

	return cmd
}

func newOffice365ListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all Office 365 integrations",
		Long: `List all JumpCloud Office 365 integrations.

Default fields: id, name, defaultDomain.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=Contoso'     Exact match`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOffice365List(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=Contoso')")

	return cmd
}

func runOffice365List(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/office365s", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = office365DefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newOffice365GetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get an Office 365 integration by name or ID",
		Long: `Get a single JumpCloud Office 365 integration by name or ID.

Accepts an Office 365 integration name (e.g., "Contoso") or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.Office365Config),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOffice365Get(cmd, args[0])
		},
	}

	return cmd
}

func runOffice365Get(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveOffice365(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/office365s/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newOffice365TranslationRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "translation-rules <name-or-id>",
		Short: "List translation rules for an Office 365 integration",
		Long: `List translation rules for a JumpCloud Office 365 integration.

Accepts an Office 365 integration name or 24-character hex ID.
Default fields: id, builtIn.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.Office365Config),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOffice365TranslationRules(cmd, args[0])
		},
	}

	return cmd
}

func runOffice365TranslationRules(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveOffice365(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/office365s/"+id+"/translationrules", api.V2ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = office365TranslationRuleDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newOffice365ImportUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import-users <name-or-id>",
		Short: "List importable users from an Office 365 integration",
		Long: `List users that can be imported from a JumpCloud Office 365 integration.

Accepts an Office 365 integration name or 24-character hex ID.
Default fields: email, firstname, lastname, status.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.Office365Config),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOffice365ImportUsers(cmd, args[0])
		},
	}

	return cmd
}

func runOffice365ImportUsers(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveOffice365(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/office365s/"+id+"/import/users", api.V2ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = office365ImportUserDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}
