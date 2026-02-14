package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
)

// insightsDefaultFields is the default field subset shown for insights query output.
var insightsDefaultFields = []string{"timestamp", "event_type", "initiated_by", "client_ip", "success"}

// newInsightsClient is the constructor for InsightsClient. Tests override this.
var newInsightsClient = func() (*api.InsightsClient, error) {
	return api.NewInsightsClient()
}

func newInsightsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Query JumpCloud Directory Insights events",
		Long: `Query Directory Insights for audit and activity events.

Supported services: all, sso, radius, ldap, user_portal, admin, mdm, directory, software, systems, password_manager.

Time ranges can be specified as:
  --last 24h          Last 24 hours
  --last 7d           Last 7 days
  --last 30d          Last 30 days
  --last 1m           Last 1 month
  --start 2026-02-01  Absolute date
  --start 2026-02-01T00:00:00Z  RFC 3339 datetime`,
	}

	cmd.AddCommand(newInsightsQueryCmd())

	return cmd
}

func newInsightsQueryCmd() *cobra.Command {
	var (
		serviceFlag   string
		lastFlag      string
		startFlag     string
		endFlag       string
		eventTypeFlag string
		limitFlag     int
		sortFlag      string
	)

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query Directory Insights events",
		Long: `Query Directory Insights events with service, time range, and event type filters.

Examples:
  jc insights query --service sso --last 24h
  jc insights query --service sso,ldap --start 2026-02-01 --end 2026-02-13
  jc insights query --service all --last 7d
  jc insights query --service sso --event-type sso_auth_failed --last 24h
  jc insights query --service sso --last 7d --limit 100
  jc insights query --service sso --last 7d --sort -timestamp

Default fields: timestamp, event_type, initiated_by, client_ip, success.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInsightsQuery(cmd, serviceFlag, lastFlag, startFlag, endFlag, eventTypeFlag, limitFlag, sortFlag)
		},
	}

	cmd.Flags().StringVar(&serviceFlag, "service", "", "Service to query (e.g. sso, ldap, all; comma-separated for multiple)")
	cmd.Flags().StringVar(&lastFlag, "last", "", "Time range shortcut (e.g. 24h, 7d, 30d, 1m)")
	cmd.Flags().StringVar(&startFlag, "start", "", "Start time (date or RFC 3339)")
	cmd.Flags().StringVar(&endFlag, "end", "", "End time (date or RFC 3339)")
	cmd.Flags().StringVar(&eventTypeFlag, "event-type", "", "Filter by event type (e.g. sso_auth_failed)")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of events to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -timestamp)")

	_ = cmd.MarkFlagRequired("service")

	return cmd
}

func runInsightsQuery(cmd *cobra.Command, service, last, start, end, eventType string, limit int, sort string) error {
	// Validate service.
	if err := api.ValidateService(service); err != nil {
		return err
	}

	// Resolve time range.
	startTime, endTime, err := resolveInsightsTimeRange(last, start, end)
	if err != nil {
		return err
	}

	// Build query.
	query := api.InsightsQuery{
		Service:   service,
		StartTime: startTime,
	}
	if endTime != "" {
		query.EndTime = endTime
	}
	if eventType != "" {
		query.SearchTermFilter = map[string]any{
			"event_type": eventType,
		}
	}

	client, err := newInsightsClient()
	if err != nil {
		return err
	}

	result, err := client.QueryEvents(cmd.Context(), query, api.InsightsQueryOptions{
		Limit: limit,
		Sort:  sort,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = insightsDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

// resolveInsightsTimeRange resolves --last, --start, and --end flags into RFC 3339 start/end times.
func resolveInsightsTimeRange(last, start, end string) (startTime, endTime string, err error) {
	if last == "" && start == "" {
		return "", "", fmt.Errorf("either --last or --start is required")
	}
	if last != "" && start != "" {
		return "", "", fmt.Errorf("--last and --start are mutually exclusive")
	}

	if last != "" {
		t, err := api.ParseTimeRange(last)
		if err != nil {
			return "", "", err
		}
		startTime = t.UTC().Format(time.RFC3339)
		// No end time for --last; defaults to now on the server.
		return startTime, "", nil
	}

	// --start was provided.
	t, err := api.ParseTimeRange(start)
	if err != nil {
		return "", "", fmt.Errorf("invalid --start: %w", err)
	}
	startTime = t.UTC().Format(time.RFC3339)

	if end != "" {
		te, err := api.ParseTimeRange(end)
		if err != nil {
			return "", "", fmt.Errorf("invalid --end: %w", err)
		}
		endTime = te.UTC().Format(time.RFC3339)
	}

	return startTime, endTime, nil
}