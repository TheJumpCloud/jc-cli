package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/report"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// newReportsCmd builds the `jc reports …` group over the JumpCloud Reports API
// (GET /api/v2/reports/*). v1 of this group covers the read operations across
// every family; write operations (create/update/delete/trigger/export) are a
// tracked follow-up.
func newReportsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reports",
		Short: "Browse JumpCloud reports (built-in templates, custom, scheduled)",
		Long: `Browse JumpCloud reports.

  templates   Built-in JumpCloud report templates (read-only)
  saved       Saved report instances
  custom      Custom Reports
  builder     Report Builder reports
  scheduled   Scheduled reports and their run history

Each family supports 'list' and 'get <id-or-name>'. This group is read-only;
creating and exporting reports is a planned follow-up.`,
	}
	for _, name := range report.FamilyNames() {
		cmd.AddCommand(newReportFamilyCmd(report.Families[name]))
	}
	return cmd
}

func newReportFamilyCmd(f report.Family) *cobra.Command {
	cmd := &cobra.Command{
		Use:   f.Name,
		Short: "Browse " + f.Name + " reports",
	}
	cmd.AddCommand(newReportListCmd(f))
	cmd.AddCommand(newReportGetCmd(f))
	// The scheduled family carries run history as a sub-resource.
	if f.Name == "scheduled" {
		cmd.AddCommand(newReportScheduledRunsCmd(f))
		cmd.AddCommand(newReportScheduledRunGetCmd())
	}
	return cmd
}

func reportResolveConfig(f report.Family) resolve.ResourceConfig {
	return resolve.ResourceConfig{
		CacheKey:     "reports-" + f.Name,
		ListEndpoint: f.ListEndpoint,
		NameField:    f.NameField,
		IDField:      f.IDField,
		ResponseKey:  f.ListKey,
	}
}

func newReportListCmd(f report.Family) *cobra.Command {
	var limitFlag int
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List " + f.Name + " reports",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			result, err := client.ListAll(cmd.Context(), f.ListEndpoint, api.V2ListOptions{
				Limit:       limitFlag,
				ResponseKey: f.ListKey,
			})
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = f.DefaultFields
			if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	return cmd
}

func newReportGetCmd(f report.Family) *cobra.Command {
	return &cobra.Command{
		Use:               "get <id-or-name>",
		Short:             "Get a " + f.Name + " report by ID or name",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(reportResolveConfig(f)),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			id, err := resolveReport(cmd.Context(), client, f, args[0])
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), f.ListEndpoint+"/"+id)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), report.Unwrap(raw, f.GetKey), output.CurrentOptions())
		},
	}
}

func resolveReport(ctx context.Context, client *api.V2Client, f report.Family, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, reportResolveConfig(f))
}

// newReportScheduledRunsCmd lists scheduled-report runs: all runs, or the runs
// of a specific schedule when a schedule id/name is given.
func newReportScheduledRunsCmd(f report.Family) *cobra.Command {
	var limitFlag int
	cmd := &cobra.Command{
		Use:   "runs [schedule-id-or-name]",
		Short: "List scheduled-report runs (all, or for one schedule)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			endpoint := "/reports/scheduled/runs"
			if len(args) == 1 {
				id, err := resolveReport(cmd.Context(), client, f, args[0])
				if err != nil {
					return err
				}
				endpoint = "/reports/scheduled/" + id + "/runs"
			}
			result, err := client.ListAll(cmd.Context(), endpoint, api.V2ListOptions{
				Limit:       limitFlag,
				ResponseKey: report.RunsListKey,
			})
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = report.RunsDefaultFields
			if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	return cmd
}

func newReportScheduledRunGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <run-id>",
		Short: "Get a single scheduled-report run by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), "/reports/scheduled/runs/"+args[0])
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), report.Unwrap(raw, report.RunsGetKey), output.CurrentOptions())
		},
	}
}
