package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/search"
)

// newSearchCmd builds the `jc search …` group over the JumpCloud v1 search API
// (POST /api/search/*). Each subcommand searches one resource index and returns
// {results, totalCount}, paginated by api.V1Client.Search.
func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search JumpCloud resource indexes (systems, users, commands, command-results)",
		Long: `Search JumpCloud resource indexes via the v1 search API.

Each subcommand runs a keyword search over one resource and returns the
matching records. A bare term matches the resource's default search fields; add
--filter for structured field matching, or omit the term to match all.

  jc search systems web-01
  jc search users john --filter 'activated=true'
  jc search commands --filter 'commandType=linux'

Note: organization search is not exposed here (its filter grammar differs and
` + "`jc org list`" + ` covers the practical need).`,
	}
	for _, name := range search.ResourceNames() {
		cmd.AddCommand(newSearchResourceCmd(search.Resources[name]))
	}
	return cmd
}

func newSearchResourceCmd(r search.Resource) *cobra.Command {
	var (
		limitFlag    int
		sortFlag     string
		filterFlag   []string
		searchFields []string
	)
	cmd := &cobra.Command{
		Use:     r.Name + " [term]",
		Aliases: r.Aliases,
		Short:   "Search JumpCloud " + r.Name,
		Long: fmt.Sprintf(`Search JumpCloud %s via POST %s.

A bare term matches the default search fields (%v); override with --search-field.
Use --filter for structured matching, or omit the term to match all.`,
			r.Name, r.Endpoint, r.SearchFields),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			term := ""
			if len(args) == 1 {
				term = args[0]
			}
			return runSearch(cmd, r, term, limitFlag, sortFlag, filterFlag, searchFields)
		},
	}
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Structured filter (e.g. 'activated=true'); repeatable")
	cmd.Flags().StringArrayVar(&searchFields, "search-field", nil, "Fields the term matches against (overrides the resource default); repeatable")
	return cmd
}

func runSearch(cmd *cobra.Command, r search.Resource, term string, limit int, sort string, filters, searchFields []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}
	client, err := newV1Client()
	if err != nil {
		return err
	}
	body := r.Body(term, searchFields, filter.ToV1Queries(exprs))
	result, err := client.Search(cmd.Context(), r.Endpoint, body, api.SearchOptions{
		Limit: limit,
		Sort:  sort,
	})
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	opts.DefaultFields = r.DefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}
	return nil
}
