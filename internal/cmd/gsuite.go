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

// gsuiteDefaultFields is the default field subset shown for G Suite output.
var gsuiteDefaultFields = []string{"id", "name", "defaultDomain"}

// gsuiteTranslationRuleDefaultFields is the default field subset for translation rules.
var gsuiteTranslationRuleDefaultFields = []string{"id", "builtIn"}

// gsuiteImportUserDefaultFields is the default field subset for importable users.
var gsuiteImportUserDefaultFields = []string{"email", "firstname", "lastname", "status"}

// resolveGsuite resolves a G Suite instance name or ID to a JumpCloud G Suite ID.
func resolveGsuite(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.GsuiteConfig)
}

func newGsuiteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gsuite",
		Short: "Manage JumpCloud G Suite integrations",
		Long:  "List, view, and manage JumpCloud G Suite directory integrations.",
	}

	cmd.AddCommand(newGsuiteListCmd())
	cmd.AddCommand(newGsuiteGetCmd())
	cmd.AddCommand(newGsuiteTranslationRulesCmd())
	cmd.AddCommand(newGsuiteImportUsersCmd())

	return cmd
}

func newGsuiteListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all G Suite integrations",
		Long: `List all JumpCloud G Suite integrations.

Default fields: id, name, defaultDomain.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=Acme Corp'     Exact match`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGsuiteList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=Acme Corp')")

	return cmd
}

func runGsuiteList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/gsuites", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = gsuiteDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newGsuiteGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get a G Suite integration by name or ID",
		Long: `Get a single JumpCloud G Suite integration by name or ID.

Accepts a G Suite instance name (e.g., "Acme Corp") or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.GsuiteConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGsuiteGet(cmd, args[0])
		},
	}

	return cmd
}

func runGsuiteGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveGsuite(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/gsuites/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newGsuiteTranslationRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "translation-rules <name-or-id>",
		Short: "List translation rules for a G Suite integration",
		Long: `List translation rules for a JumpCloud G Suite integration.

Accepts a G Suite instance name or 24-character hex ID.
Returns the translation rules configured for the G Suite directory.

Default fields: id, builtIn.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.GsuiteConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGsuiteTranslationRules(cmd, args[0])
		},
	}

	return cmd
}

func runGsuiteTranslationRules(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveGsuite(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/gsuites/"+id+"/translationrules", api.V2ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = gsuiteTranslationRuleDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newGsuiteImportUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import-users <name-or-id>",
		Short: "List importable users from a G Suite integration",
		Long: `List users available for import from a JumpCloud G Suite integration.

Accepts a G Suite instance name or 24-character hex ID.
Returns users from the G Suite directory that can be imported into JumpCloud.

Default fields: email, firstname, lastname, status.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.GsuiteConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGsuiteImportUsers(cmd, args[0])
		},
	}

	return cmd
}

func runGsuiteImportUsers(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveGsuite(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/gsuites/"+id+"/import/users", api.V2ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = gsuiteImportUserDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}
