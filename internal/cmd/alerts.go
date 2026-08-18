package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/alert"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

func resolveAlert(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.AlertConfig)
}

func newAlertsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "alerts",
		Aliases: []string{"alert"},
		Short:   "Triage JumpCloud alerts (Monitoring & Alerting)",
		Long: `List, inspect, annotate, and resolve JumpCloud alerts.

Alerts are raised by health-monitoring rules against devices, users, and other
resources. Each alert has a status (open, acknowledged, resolved), a severity,
and a stream of occurrences. Use 'status' to acknowledge or resolve an alert
and 'add-note' to leave a triage note.

Alerts are usually addressed by their objectId (from 'jc alerts list --ids');
a unique alert title also resolves.`,
	}
	cmd.AddCommand(newAlertsListCmd())
	cmd.AddCommand(newAlertsGetCmd())
	cmd.AddCommand(newAlertsStatsCmd())
	cmd.AddCommand(newAlertsOccurrencesCmd())
	cmd.AddCommand(newAlertsNotesCmd())
	cmd.AddCommand(newAlertsAddNoteCmd())
	cmd.AddCommand(newAlertsStatusCmd())
	cmd.AddCommand(newAlertsDeleteCmd())
	cmd.AddCommand(newAlertsRulesCmd())
	return cmd
}

func newAlertsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List alerts",
		Long: `List JumpCloud alerts.

Default fields: objectId, title, severity, status, sourceName, lastOccurredAt.
Filter by status/severity, e.g. --filter 'status=ALERT_STATUS_OPEN'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAlertsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'status=ALERT_STATUS_OPEN')")
	return cmd
}

func runAlertsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.ListAll(cmd.Context(), "/alerts", api.V2ListOptions{
		Limit:       limit,
		Sort:        sort,
		Filter:      filter.ToV2Queries(exprs),
		ResponseKey: "alerts",
	})
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	opts.DefaultFields = alert.DefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}
	return nil
}

func newAlertsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "get <id-or-title>",
		Short:             "Get an alert by ID or title",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.AlertConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAlertsGet(cmd, args[0])
		},
	}
	return cmd
}

func runAlertsGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveAlert(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	result, err := client.Get(cmd.Context(), "/alerts/"+id)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), alert.Unwrap(result, "alert"), opts)
}

func newAlertsStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show alert count statistics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			result, err := client.Get(cmd.Context(), "/alerts-stats")
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			return output.WriteSingle(cmd.OutOrStdout(), result, opts)
		},
	}
}

func newAlertsOccurrencesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "occurrences <id-or-title>",
		Aliases:           []string{"occ"},
		Short:             "List the occurrences of an alert",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.AlertConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAlertsSubList(cmd, args[0], "/occurrences", "alertOccurrences")
		},
	}
	return cmd
}

func newAlertsNotesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "notes <id-or-title>",
		Short:             "List the triage notes on an alert",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.AlertConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAlertsSubList(cmd, args[0], "/notes", "notes")
		},
	}
	return cmd
}

// runAlertsSubList fetches a wrapped sub-resource list ({key:[...]}) of an
// alert and writes the unwrapped array.
func runAlertsSubList(cmd *cobra.Command, identifier, subPath, key string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveAlert(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	raw, err := client.Get(cmd.Context(), "/alerts/"+id+subPath)
	if err != nil {
		return err
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}
	arr := wrap[key]
	if arr == nil {
		arr = json.RawMessage("[]")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(arr, &items); err != nil {
		return fmt.Errorf("parsing %s: %w", key, err)
	}
	opts := output.CurrentOptions()
	if err := output.WriteList(cmd.OutOrStdout(), items, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(items))
	}
	return nil
}

func newAlertsAddNoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "add-note <id-or-title> <text>",
		Short:             "Add a triage note to an alert",
		Long:              "Add a triage note to an alert. Notes are permanent — there is no delete-note endpoint.",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AlertConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAlertsAddNote(cmd, args[0], args[1])
		},
	}
	return cmd
}

func runAlertsAddNote(cmd *cobra.Command, identifier, text string) error {
	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "add note",
			Resource: "alert",
			Target:   identifier,
			Effects:  []string{"note: " + text, "notes are permanent (no delete-note endpoint)"},
		})
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveAlert(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	result, err := client.Create(cmd.Context(), "/alerts/"+id+"/notes", alert.NoteBody(text))
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), alert.Unwrap(result, "note"), opts)
}

func newAlertsStatusCmd() *cobra.Command {
	var remark string
	cmd := &cobra.Command{
		Use:               "status <id-or-title> <open|acknowledged|resolved>",
		Short:             "Change an alert's status",
		Long:              "Acknowledge, resolve, or reopen an alert. An optional --remark is recorded with the change.",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AlertConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAlertsStatus(cmd, args[0], args[1], remark)
		},
	}
	cmd.Flags().StringVar(&remark, "remark", "", "Optional remark recorded with the status change")
	return cmd
}

func runAlertsStatus(cmd *cobra.Command, identifier, status, remark string) error {
	apiStatus, err := alert.NormalizeStatus(status)
	if err != nil {
		return err
	}
	if viper.GetBool("plan") {
		effects := []string{"status → " + status}
		if remark != "" {
			effects = append(effects, "remark: "+remark)
		}
		return renderPlan(cmd, &plan.Plan{
			Action:     "set status",
			Resource:   "alert",
			Target:     identifier,
			Effects:    effects,
			Reversible: true,
		})
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveAlert(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	result, err := client.Create(cmd.Context(), "/alerts/"+id+"/status", alert.StatusBody(apiStatus, remark))
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), alert.Unwrap(result, "alert"), opts)
}

func newAlertsDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "delete <id-or-title>",
		Aliases:           []string{"rm"},
		Short:             "Delete an alert",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.AlertConfig),
		RunE:              batchRunE("alert", "delete", runAlertsDelete),
	}
	addBatchSourceFlags(cmd)
	return cmd
}

func runAlertsDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveAlert(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	// Read the title back for the confirmation/success message.
	title := identifier
	if raw, err := client.Get(cmd.Context(), "/alerts/"+id); err == nil {
		var a struct {
			Title string `json:"title"`
		}
		if json.Unmarshal(alert.Unwrap(raw, "alert"), &a) == nil && a.Title != "" {
			title = a.Title
		}
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "delete",
			Resource: "alert",
			Target:   fmt.Sprintf("%s (%s)", title, id),
			Effects:  []string{"Removes the alert and its occurrence history"},
		})
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete alert %q? [y/N] ", title)
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
	if _, err := client.Delete(cmd.Context(), "/alerts/"+id); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Alert %q deleted successfully.\n", title)
	return nil
}
