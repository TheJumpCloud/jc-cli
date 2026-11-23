package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// insightsReportDefaultFields is the default column set for `insights reports list`.
var insightsReportDefaultFields = []string{"id", "type", "status", "created_at", "expiration", "formats"}

// reportPollInterval is how often `request --wait` polls. Tests override it.
var reportPollInterval = 3 * time.Second

// reportWaitDefault bounds --wait when no --timeout is given.
const reportWaitDefault = 10 * time.Minute

func newInsightsReportsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reports",
		Short: "Request and download Directory Insights reports",
		Long: `Directory Insights' own report family (users to devices, OS versions,
software inventory and others). This is not the Console Reports API; for that
see 'jc reports'.

There is no delete: a requested report stays until its expiration.`,
	}
	cmd.AddCommand(newInsightsReportsListCmd())
	cmd.AddCommand(newInsightsReportsRequestCmd())
	cmd.AddCommand(newInsightsReportsDownloadCmd())
	return cmd
}

func newInsightsReportsListCmd() *cobra.Command {
	var sortFlag, typeFlag, statusFlag string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Directory Insights reports",
		Long: `List requested Directory Insights reports and their status.

--sort is passed to the API: CREATED_AT, EXPIRATION, REQUESTER_EMAIL, STATUS,
TYPE or UPDATED_AT. Per the API spec the default is descending and a "-"
prefix sorts ascending. --type and --status filter locally.

Examples:
  jc insights reports list
  jc insights reports list --sort -CREATED_AT
  jc insights reports list --type users-to-devices --status COMPLETED`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newInsightsClient()
			if err != nil {
				return err
			}
			items, err := client.ListReports(cmd.Context(), sortFlag)
			if err != nil {
				return err
			}
			rows := make([]json.RawMessage, 0, len(items))
			for _, raw := range items {
				var m map[string]any
				if err := json.Unmarshal(raw, &m); err != nil {
					return fmt.Errorf("parsing report: %w", err)
				}
				if typeFlag != "" && fmt.Sprint(m["type"]) != typeFlag {
					continue
				}
				if statusFlag != "" && !strings.EqualFold(fmt.Sprint(m["status"]), statusFlag) {
					continue
				}
				m["formats"] = reportFormats(raw)
				b, err := json.Marshal(m)
				if err != nil {
					return err
				}
				rows = append(rows, b)
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = insightsReportDefaultFields
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			writeNounFooter(cmd, len(rows), "report", "reports")
			return nil
		},
	}
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (CREATED_AT, EXPIRATION, REQUESTER_EMAIL, STATUS, TYPE, UPDATED_AT; a - prefix sorts ascending)")
	cmd.Flags().StringVar(&typeFlag, "type", "", "Only reports of this type (local filter)")
	cmd.Flags().StringVar(&statusFlag, "status", "", "Only reports with this status, e.g. COMPLETED (local filter)")
	return cmd
}

// reportFormats returns the artifact formats of a report as "CSV,JSON".
func reportFormats(raw json.RawMessage) string {
	var r api.InsightsReport
	if json.Unmarshal(raw, &r) != nil {
		return ""
	}
	fs := make([]string, 0, len(r.Artifacts))
	for _, a := range r.Artifacts {
		fs = append(fs, a.Format)
	}
	return strings.Join(fs, ",")
}

func newInsightsReportsRequestCmd() *cobra.Command {
	var wait bool
	cmd := &cobra.Command{
		Use:   "request <type>",
		Short: "Request a Directory Insights report",
		Long: `Request a report. The API answers 202 with the pending report's metadata.

Types: ` + strings.Join(api.InsightsReportTypes, ", ") + `

--wait polls until the report is COMPLETED or FAILED, bounded by --timeout
(10m when unset). Use --plan to preview the request.

Examples:
  jc insights reports request users-to-devices --plan
  jc insights reports request os-version --wait --timeout 5m`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return api.InsightsReportTypes, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInsightsReportsRequest(cmd, args[0], wait)
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", false, "Poll until the report is COMPLETED or FAILED")
	return cmd
}

func runInsightsReportsRequest(cmd *cobra.Command, reportType string, wait bool) error {
	if err := api.ValidateInsightsReportType(reportType); err != nil {
		return NewCLIError(ErrCodeUsageError, err.Error(), "")
	}
	if viper.GetBool("plan") {
		// The plan box wraps long lines, which would split the path; print the
		// request unbroken first so it can be read and grepped.
		fmt.Fprintf(cmd.ErrOrStderr(), "POST /insights/directory/v1/reports/%s\n", reportType)
		return renderPlan(cmd, &plan.Plan{
			Action:     "request",
			Resource:   "Directory Insights report",
			Target:     reportType,
			Effects:    []string{"POST /insights/directory/v1/reports/" + reportType},
			Reversible: false,
		})
	}
	client, err := newInsightsClient()
	if err != nil {
		return err
	}
	raw, err := client.RequestReport(cmd.Context(), reportType)
	if err != nil {
		return err
	}
	if wait {
		raw, err = waitForReport(cmd.Context(), client, raw)
		if err != nil {
			return err
		}
	}
	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}

// waitForReport polls the report list until the report in pending is
// COMPLETED (returning its metadata) or FAILED/DELETED (an error).
func waitForReport(ctx context.Context, client *api.InsightsClient, pending json.RawMessage) (json.RawMessage, error) {
	var rep api.InsightsReport
	if err := json.Unmarshal(pending, &rep); err != nil || rep.ID == "" {
		return nil, fmt.Errorf("cannot wait: the request response has no report id")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, reportWaitDefault)
		defer cancel()
	}
	last := rep.Status
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for report %s (last status %s); check it with: jc insights reports list", rep.ID, last)
		case <-time.After(reportPollInterval):
		}
		items, err := client.ListReports(ctx, "")
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			return nil, err
		}
		for _, raw := range items {
			var r api.InsightsReport
			if json.Unmarshal(raw, &r) != nil || r.ID != rep.ID {
				continue
			}
			last = r.Status
			switch r.Status {
			case api.ReportStatusCompleted:
				return raw, nil
			case api.ReportStatusFailed, api.ReportStatusDeleted:
				return nil, fmt.Errorf("report %s ended with status %s", rep.ID, r.Status)
			}
		}
	}
}

func newInsightsReportsDownloadCmd() *cobra.Command {
	var format, artifactID, outputFile string
	cmd := &cobra.Command{
		Use:   "download <report-id>",
		Short: "Download a completed report's artifact",
		Long: `Download a COMPLETED report's CSV or JSON artifact, unchanged.

The artifact is the report's only one, or csv when both exist; --format picks
one and --artifact selects it by id. The body goes to stdout unless
--output-file is given; an existing file is not overwritten without --force.

Examples:
  jc insights reports download <id> --format json --output-file users.json
  jc insights reports download <id> > report.csv`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInsightsReportsDownload(cmd, args[0], format, artifactID, outputFile)
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "Artifact format: csv or json")
	cmd.Flags().StringVar(&artifactID, "artifact", "", "Artifact id (skips the format choice)")
	cmd.Flags().StringVar(&outputFile, "output-file", "", "Write the artifact to this file (default: stdout)")
	return cmd
}

func runInsightsReportsDownload(cmd *cobra.Command, reportID, format, artifactID, outputFile string) error {
	format = strings.ToLower(format)
	if format != "" && format != "csv" && format != "json" {
		return NewCLIError(ErrCodeUsageError, fmt.Sprintf("invalid --format %q; use csv or json", format), "")
	}
	if outputFile != "" && !viper.GetBool("force") {
		if _, err := os.Stat(outputFile); err == nil {
			return fmt.Errorf("%s already exists; use --force to overwrite", outputFile)
		}
	}
	client, err := newInsightsClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	if artifactID == "" {
		items, err := client.ListReports(ctx, "")
		if err != nil {
			return err
		}
		var rep *api.InsightsReport
		for _, raw := range items {
			var r api.InsightsReport
			if json.Unmarshal(raw, &r) == nil && r.ID == reportID {
				rep = &r
				break
			}
		}
		if rep == nil {
			return api.NewAPIError(http.StatusNotFound, "/reports/"+reportID, []byte(fmt.Sprintf("report %q not found", reportID)))
		}
		if rep.Status != api.ReportStatusCompleted {
			return fmt.Errorf("report %s is %s, not COMPLETED; there is nothing to download yet", reportID, rep.Status)
		}
		artifactID, err = pickReportArtifact(rep, format)
		if err != nil {
			return err
		}
	}

	body, err := client.DownloadReportArtifact(ctx, reportID, artifactID)
	if err != nil {
		return err
	}
	if outputFile == "" {
		_, err = cmd.OutOrStdout().Write(body)
		return err
	}
	return writeReportFile(outputFile, body)
}

// pickReportArtifact chooses the artifact: by format when given, else the
// only one, else csv.
func pickReportArtifact(rep *api.InsightsReport, format string) (string, error) {
	want := format
	if want == "" {
		if len(rep.Artifacts) == 1 {
			return rep.Artifacts[0].ID, nil
		}
		want = "csv"
	}
	for _, a := range rep.Artifacts {
		if strings.EqualFold(a.Format, want) {
			return a.ID, nil
		}
	}
	if len(rep.Artifacts) == 0 {
		return "", fmt.Errorf("report %s has no artifacts", rep.ID)
	}
	return "", fmt.Errorf("report %s has no %s artifact", rep.ID, want)
}

// writeReportFile writes via temp file + rename so a failure leaves no partial file.
func writeReportFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".jc-report-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return errors.New("writing " + path + ": " + err.Error())
	}
	return nil
}
