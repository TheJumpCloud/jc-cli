package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/report"
)

// readReportFile reads a --report-file path (or "-" for stdin) and returns the
// report object to wrap in the family's {<GetKey>} envelope.
func readReportFile(cmd *cobra.Command, f report.Family, path string) (json.RawMessage, error) {
	if path == "" {
		return nil, fmt.Errorf("--report-file is required")
	}
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(cmd.InOrStdin())
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading --report-file: %w", err)
	}
	return f.ParseReportFile(raw)
}

func reportSourceLabel(path string) string {
	if path == "-" {
		return "stdin"
	}
	return path
}

func newReportCreateCmd(f report.Family) *cobra.Command {
	var reportFile string
	cmd := &cobra.Command{
		Use:   "create --report-file <file>",
		Short: "Create a " + f.Name + " report from a JSON file",
		Long: fmt.Sprintf(`Create a JumpCloud %s report.

The report definition is a large nested object (searchRequest, filters, …), so
it is supplied as raw JSON via --report-file (a {"%s": …} envelope or a bare
object; a body from 'jc reports %s get' round-trips). Use - for stdin.`,
			f.Name, f.GetKey, f.Name),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			obj, err := readReportFile(cmd, f, reportFile)
			if err != nil {
				return err
			}
			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action:   "create",
					Resource: f.Name + " report",
					Target:   reportDisplayName(obj),
					Effects:  []string{"Creates a new " + f.Name + " report from " + reportSourceLabel(reportFile)},
				})
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Create(cmd.Context(), f.ListEndpoint, f.WrapBody(obj))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), report.Unwrap(raw, f.GetKey), output.CurrentOptions())
		},
	}
	cmd.Flags().StringVar(&reportFile, "report-file", "", "Path to a JSON file with the report object (or - for stdin)")
	_ = cmd.MarkFlagRequired("report-file")
	return cmd
}

func newReportUpdateCmd(f report.Family) *cobra.Command {
	var reportFile string
	cmd := &cobra.Command{
		Use:               "update <id-or-name> --report-file <file>",
		Short:             "Update a " + f.Name + " report from a JSON file",
		Long:              "Update a JumpCloud " + f.Name + " report. The full report object is supplied as raw JSON via --report-file; a body from 'get' round-trips. Use - for stdin.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(reportResolveConfig(f)),
		RunE: func(cmd *cobra.Command, args []string) error {
			obj, err := readReportFile(cmd, f, reportFile)
			if err != nil {
				return err
			}
			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action:     "update",
					Resource:   f.Name + " report",
					Target:     args[0],
					Effects:    []string{"Replaces the report definition from " + reportSourceLabel(reportFile)},
					Reversible: true,
				})
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			id, err := resolveReport(cmd.Context(), client, f, args[0])
			if err != nil {
				return err
			}
			raw, err := client.Update(cmd.Context(), f.ListEndpoint+"/"+id, f.WrapBody(obj))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), report.Unwrap(raw, f.GetKey), output.CurrentOptions())
		},
	}
	cmd.Flags().StringVar(&reportFile, "report-file", "", "Path to a JSON file with the report object (or - for stdin)")
	_ = cmd.MarkFlagRequired("report-file")
	return cmd
}

func newReportDeleteCmd(f report.Family) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "delete <id-or-name>",
		Aliases:           []string{"rm"},
		Short:             "Delete a " + f.Name + " report",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(reportResolveConfig(f)),
		RunE:              batchRunE(f.Name+" report", "delete", reportDeleteRunE(f)),
	}
	addBatchSourceFlags(cmd)
	return cmd
}

func reportDeleteRunE(f report.Family) func(*cobra.Command, string) error {
	return func(cmd *cobra.Command, identifier string) error {
		client, err := newV2Client()
		if err != nil {
			return err
		}
		id, err := resolveReport(cmd.Context(), client, f, identifier)
		if err != nil {
			return err
		}
		name := identifier
		if raw, err := client.Get(cmd.Context(), f.ListEndpoint+"/"+id); err == nil {
			var r struct {
				DisplayName string `json:"displayName"`
			}
			if json.Unmarshal(report.Unwrap(raw, f.GetKey), &r) == nil && r.DisplayName != "" {
				name = r.DisplayName
			}
		}

		if viper.GetBool("plan") {
			return renderPlan(cmd, &plan.Plan{
				Action:   "delete",
				Resource: f.Name + " report",
				Target:   fmt.Sprintf("%s (%s)", name, id),
				Effects:  []string{"Removes the " + f.Name + " report"},
			})
		}
		if mustAbortWithoutTTY() {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
			return nil
		}
		if shouldConfirm() {
			fmt.Fprintf(cmd.ErrOrStderr(), "Delete %s report %q? [y/N] ", f.Name, name)
			reader := getConfirmReader()
			answer, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading confirmation: %w", err)
			}
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
				return nil
			}
		}
		if _, err := client.Delete(cmd.Context(), f.ListEndpoint+"/"+id); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s report %q deleted successfully.\n", f.Name, name)
		return nil
	}
}

// reportDisplayName extracts a report's displayName for plan output.
func reportDisplayName(obj json.RawMessage) string {
	var r struct {
		DisplayName string `json:"displayName"`
	}
	if json.Unmarshal(obj, &r) == nil && r.DisplayName != "" {
		return r.DisplayName
	}
	return "(unnamed report)"
}

func newReportScheduledTriggerCmd(f report.Family) *cobra.Command {
	return &cobra.Command{
		Use:               "trigger <id-or-name>",
		Short:             "Run a scheduled report now (emails its recipients)",
		Long:              "Trigger a scheduled report immediately (\"Run Now\"). This generates a run and emails the report's configured recipients.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(reportResolveConfig(f)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action:   "trigger",
					Resource: "scheduled report",
					Target:   args[0],
					Effects:  []string{"Runs the report now and emails its recipients"},
				})
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			id, err := resolveReport(cmd.Context(), client, f, args[0])
			if err != nil {
				return err
			}
			if _, err := client.Create(cmd.Context(), f.ListEndpoint+"/"+id+"/trigger", map[string]any{}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Scheduled report %q triggered.\n", args[0])
			return nil
		},
	}
}

// newReportsExportCmd is registered directly under `jc reports`.
func newReportsExportCmd() *cobra.Command {
	var (
		fromTemplate      string
		searchRequestFile string
		reportName        string
		exportType        string
		notifyByEmail     bool
	)
	cmd := &cobra.Command{
		Use:   "export --name <name> (--from-template <id> | --search-request-file <file>)",
		Short: "Export a report to a downloadable file",
		Long: `Export a report and get a download URL.

Provide the report's searchRequest either from a built-in template
(--from-template <id-or-name>) or from a JSON file (--search-request-file).
Returns a presigned downloadUrl. Pass --notify-by-email to also email it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if reportName == "" {
				return fmt.Errorf("--name is required")
			}
			if (fromTemplate == "") == (searchRequestFile == "") {
				return fmt.Errorf("provide exactly one of --from-template or --search-request-file")
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			var searchRequest json.RawMessage
			if fromTemplate != "" {
				tf := report.Families["templates"]
				id, err := resolveReport(cmd.Context(), client, tf, fromTemplate)
				if err != nil {
					return err
				}
				raw, err := client.Get(cmd.Context(), tf.ListEndpoint+"/"+id)
				if err != nil {
					return err
				}
				var t struct {
					SearchRequest json.RawMessage `json:"searchRequest"`
				}
				if err := json.Unmarshal(report.Unwrap(raw, tf.GetKey), &t); err != nil || len(t.SearchRequest) == 0 {
					return fmt.Errorf("template %q has no searchRequest", fromTemplate)
				}
				searchRequest = t.SearchRequest
			} else {
				b, err := os.ReadFile(searchRequestFile)
				if err != nil {
					return fmt.Errorf("reading --search-request-file: %w", err)
				}
				searchRequest = json.RawMessage(b)
			}

			if viper.GetBool("plan") {
				effects := []string{"Exports report as " + exportTypeOrDefault(exportType)}
				if notifyByEmail {
					effects = append(effects, "emails the download to the account")
				}
				return renderPlan(cmd, &plan.Plan{
					Action:   "export",
					Resource: "report",
					Target:   reportName,
					Effects:  effects,
				})
			}
			raw, err := client.Create(cmd.Context(), "/reports/export", report.ExportBody(reportName, exportType, notifyByEmail, searchRequest))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	cmd.Flags().StringVar(&fromTemplate, "from-template", "", "Built-in template id/name to export (pulls its searchRequest)")
	cmd.Flags().StringVar(&searchRequestFile, "search-request-file", "", "JSON file with a searchRequest object")
	cmd.Flags().StringVar(&reportName, "name", "", "Name for the exported report (required)")
	cmd.Flags().StringVar(&exportType, "type", "csv", "Export format (e.g. csv)")
	cmd.Flags().BoolVar(&notifyByEmail, "notify-by-email", false, "Also email the download link to the account")
	return cmd
}

func exportTypeOrDefault(t string) string {
	if t == "" {
		return "csv"
	}
	return t
}
