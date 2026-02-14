package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

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
		Use:     "insights",
		Aliases: []string{"i"},
		Short:   "Query JumpCloud Directory Insights events",
		Long: `Query Directory Insights for audit and activity events.

Supported services: all, sso, radius, ldap, user_portal, admin, mdm, directory, software, systems, password_manager.

Time ranges can be specified as:
  --last 24h          Last 24 hours
  --last 7d           Last 7 days
  --last 30d          Last 30 days
  --last 1m           Last 1 month
  --start 2026-02-01  Absolute date
  --start 2026-02-01T00:00:00Z  RFC 3339 datetime

Aliases: i, insights`,
	}

	cmd.AddCommand(newInsightsQueryCmd())
	cmd.AddCommand(newInsightsCountCmd())
	cmd.AddCommand(newInsightsDistinctCmd())
	cmd.AddCommand(newInsightsSaveCmd())
	cmd.AddCommand(newInsightsSavedCmd())
	cmd.AddCommand(newInsightsRunCmd())

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

func newInsightsCountCmd() *cobra.Command {
	var (
		serviceFlag   string
		lastFlag      string
		startFlag     string
		endFlag       string
		eventTypeFlag string
	)

	cmd := &cobra.Command{
		Use:   "count",
		Short: "Count Directory Insights events",
		Long: `Count Directory Insights events matching the given filters.

Returns a single count value without retrieving full event records.

Examples:
  jc insights count --service sso --last 7d
  jc insights count --service sso --event-type sso_auth_failed --last 24h
  jc insights count --service all --start 2026-02-01 --end 2026-02-13`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInsightsCount(cmd, serviceFlag, lastFlag, startFlag, endFlag, eventTypeFlag)
		},
	}

	cmd.Flags().StringVar(&serviceFlag, "service", "", "Service to query (e.g. sso, ldap, all; comma-separated for multiple)")
	cmd.Flags().StringVar(&lastFlag, "last", "", "Time range shortcut (e.g. 24h, 7d, 30d, 1m)")
	cmd.Flags().StringVar(&startFlag, "start", "", "Start time (date or RFC 3339)")
	cmd.Flags().StringVar(&endFlag, "end", "", "End time (date or RFC 3339)")
	cmd.Flags().StringVar(&eventTypeFlag, "event-type", "", "Filter by event type (e.g. sso_auth_failed)")

	_ = cmd.MarkFlagRequired("service")

	return cmd
}

func runInsightsCount(cmd *cobra.Command, service, last, start, end, eventType string) error {
	if err := api.ValidateService(service); err != nil {
		return err
	}

	startTime, endTime, err := resolveInsightsTimeRange(last, start, end)
	if err != nil {
		return err
	}

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

	count, err := client.CountEvents(cmd.Context(), query)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	if opts.Quiet {
		return nil
	}

	data, err := json.Marshal(map[string]int{"count": count})
	if err != nil {
		return err
	}

	return output.WriteSingle(cmd.OutOrStdout(), json.RawMessage(data), opts)
}

func newInsightsDistinctCmd() *cobra.Command {
	var (
		serviceFlag   string
		lastFlag      string
		startFlag     string
		endFlag       string
		eventTypeFlag string
		fieldFlag     string
	)

	cmd := &cobra.Command{
		Use:   "distinct",
		Short: "Get distinct field values from Directory Insights events",
		Long: `Get distinct (unique) values for a specific field from Directory Insights events.

Useful for finding unique actors, IP addresses, event types, etc.

Examples:
  jc insights distinct --service sso --field initiated_by.username --last 30d
  jc insights distinct --service sso --field client_ip --last 7d
  jc insights distinct --service sso --field event_type --last 24h`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInsightsDistinct(cmd, serviceFlag, lastFlag, startFlag, endFlag, eventTypeFlag, fieldFlag)
		},
	}

	cmd.Flags().StringVar(&serviceFlag, "service", "", "Service to query (e.g. sso, ldap, all; comma-separated for multiple)")
	cmd.Flags().StringVar(&lastFlag, "last", "", "Time range shortcut (e.g. 24h, 7d, 30d, 1m)")
	cmd.Flags().StringVar(&startFlag, "start", "", "Start time (date or RFC 3339)")
	cmd.Flags().StringVar(&endFlag, "end", "", "End time (date or RFC 3339)")
	cmd.Flags().StringVar(&eventTypeFlag, "event-type", "", "Filter by event type (e.g. sso_auth_failed)")
	cmd.Flags().StringVar(&fieldFlag, "field", "", "Field to get distinct values for (e.g. initiated_by.username)")

	_ = cmd.MarkFlagRequired("service")
	_ = cmd.MarkFlagRequired("field")

	return cmd
}

func runInsightsDistinct(cmd *cobra.Command, service, last, start, end, eventType, field string) error {
	if err := api.ValidateService(service); err != nil {
		return err
	}

	startTime, endTime, err := resolveInsightsTimeRange(last, start, end)
	if err != nil {
		return err
	}

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

	items, err := client.DistinctEvents(cmd.Context(), query, field)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()

	// For table/CSV/human formats, wrap scalar values into {"value": X} objects
	// since the output engine expects JSON objects for structured formats.
	displayItems := items
	if opts.Format == output.FormatTable || opts.Format == output.FormatCSV || opts.Format == output.FormatHuman {
		displayItems = wrapScalarValues(items)
	}

	if err := output.WriteList(cmd.OutOrStdout(), displayItems, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(items))
	}

	return nil
}

// wrapScalarValues wraps bare JSON scalars (strings, numbers, booleans) into
// {"value": X} objects for display in table/CSV/human formats.
func wrapScalarValues(items []json.RawMessage) []json.RawMessage {
	result := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		// Check if the item is already an object.
		var m map[string]json.RawMessage
		if json.Unmarshal(item, &m) == nil {
			result = append(result, item)
			continue
		}
		// Wrap scalar value.
		wrapped, err := json.Marshal(map[string]json.RawMessage{"value": item})
		if err != nil {
			result = append(result, item)
			continue
		}
		result = append(result, wrapped)
	}
	return result
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

// savedSearch represents a saved insights query stored in config.
type savedSearch struct {
	Service   string `json:"service" mapstructure:"service"`
	Last      string `json:"last,omitempty" mapstructure:"last"`
	Start     string `json:"start,omitempty" mapstructure:"start"`
	End       string `json:"end,omitempty" mapstructure:"end"`
	EventType string `json:"event_type,omitempty" mapstructure:"event_type"`
	Limit     int    `json:"limit,omitempty" mapstructure:"limit"`
	Sort      string `json:"sort,omitempty" mapstructure:"sort"`
}

// getSavedSearches returns the map of saved searches from config.
func getSavedSearches() map[string]savedSearch {
	raw := viper.GetStringMap("insights.saved_searches")
	result := make(map[string]savedSearch, len(raw))
	for name, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		var s savedSearch
		if sv, ok := m["service"].(string); ok {
			s.Service = sv
		}
		if sv, ok := m["last"].(string); ok {
			s.Last = sv
		}
		if sv, ok := m["start"].(string); ok {
			s.Start = sv
		}
		if sv, ok := m["end"].(string); ok {
			s.End = sv
		}
		if sv, ok := m["event_type"].(string); ok {
			s.EventType = sv
		}
		if sv, ok := m["limit"].(int); ok {
			s.Limit = sv
		} else if sv, ok := m["limit"].(float64); ok {
			s.Limit = int(sv)
		}
		if sv, ok := m["sort"].(string); ok {
			s.Sort = sv
		}
		result[name] = s
	}
	return result
}

// savedSearchNames returns sorted names of all saved searches.
func savedSearchNames() []string {
	searches := getSavedSearches()
	names := make([]string, 0, len(searches))
	for name := range searches {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func newInsightsSaveCmd() *cobra.Command {
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
		Use:   "save <name>",
		Short: "Save an insights query for later re-use",
		Long: `Save a named insights query that can be re-run later with 'jc insights run'.

Time-relative searches (--last 24h) are recalculated from current time on each run.

Examples:
  jc insights save "failed-sso-24h" --service sso --event-type sso_auth_failed --last 24h
  jc insights save "all-events-7d" --service all --last 7d
  jc insights save "ldap-range" --service ldap --start 2026-02-01 --end 2026-02-13`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInsightsSave(cmd, args[0], serviceFlag, lastFlag, startFlag, endFlag, eventTypeFlag, limitFlag, sortFlag)
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

func runInsightsSave(cmd *cobra.Command, name, service, last, start, end, eventType string, limit int, sortVal string) error {
	// Validate service.
	if err := api.ValidateService(service); err != nil {
		return err
	}

	// Validate time range flags.
	if last == "" && start == "" {
		return fmt.Errorf("either --last or --start is required")
	}
	if last != "" && start != "" {
		return fmt.Errorf("--last and --start are mutually exclusive")
	}

	// Validate the time format (even though we store the raw string).
	if last != "" {
		if _, err := api.ParseTimeRange(last); err != nil {
			return err
		}
	}
	if start != "" {
		if _, err := api.ParseTimeRange(start); err != nil {
			return fmt.Errorf("invalid --start: %w", err)
		}
	}
	if end != "" {
		if _, err := api.ParseTimeRange(end); err != nil {
			return fmt.Errorf("invalid --end: %w", err)
		}
	}

	// Build the saved search entry.
	s := map[string]any{
		"service": service,
	}
	if last != "" {
		s["last"] = last
	}
	if start != "" {
		s["start"] = start
	}
	if end != "" {
		s["end"] = end
	}
	if eventType != "" {
		s["event_type"] = eventType
	}
	if limit > 0 {
		s["limit"] = limit
	}
	if sortVal != "" {
		s["sort"] = sortVal
	}

	// Store in config under insights.saved_searches.<name>.
	viper.Set("insights.saved_searches."+name, s)
	if err := writeInsightsConfig(); err != nil {
		return fmt.Errorf("failed to save search: %w", err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Saved search %q\n", name)
	return nil
}

// writeInsightsConfig writes the Viper config to disk.
var writeInsightsConfig = func() error {
	return viper.WriteConfig()
}

func newInsightsSavedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "saved",
		Short: "List all saved insight searches",
		Long: `List all saved insight searches with their names and query parameters.

Examples:
  jc insights saved
  jc insights saved --output table
  jc insights saved --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInsightsSaved(cmd)
		},
	}
	return cmd
}

func runInsightsSaved(cmd *cobra.Command) error {
	searches := getSavedSearches()
	names := savedSearchNames()

	if len(names) == 0 {
		opts := output.CurrentOptions()
		return output.WriteList(cmd.OutOrStdout(), []json.RawMessage{}, opts)
	}

	items := make([]json.RawMessage, 0, len(names))
	for _, name := range names {
		s := searches[name]
		entry := map[string]any{
			"name":    name,
			"service": s.Service,
		}
		if s.Last != "" {
			entry["last"] = s.Last
		}
		if s.Start != "" {
			entry["start"] = s.Start
		}
		if s.End != "" {
			entry["end"] = s.End
		}
		if s.EventType != "" {
			entry["event_type"] = s.EventType
		}
		if s.Limit > 0 {
			entry["limit"] = s.Limit
		}
		if s.Sort != "" {
			entry["sort"] = s.Sort
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		items = append(items, data)
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = []string{"name", "service", "last", "start", "end", "event_type"}

	if err := output.WriteList(cmd.OutOrStdout(), items, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d saved searches ──\n", len(items))
	}

	return nil
}

func newInsightsRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Run a saved insight search",
		Long: `Execute a previously saved insight search by name.

Time-relative searches (--last 24h) are recalculated from current time.

Examples:
  jc insights run "failed-sso-24h"
  jc insights run "all-events-7d" --output table
  jc insights run "all-events-7d" --limit 10`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return savedSearchNames(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInsightsRun(cmd, args[0])
		},
	}
	return cmd
}

func runInsightsRun(cmd *cobra.Command, name string) error {
	searches := getSavedSearches()
	s, ok := searches[name]
	if !ok {
		names := savedSearchNames()
		if len(names) == 0 {
			return fmt.Errorf("saved search %q not found (no saved searches exist)", name)
		}
		return fmt.Errorf("saved search %q not found; available: %s", name, strings.Join(names, ", "))
	}

	// Run the saved query using the same logic as 'insights query'.
	return runInsightsQuery(cmd, s.Service, s.Last, s.Start, s.End, s.EventType, s.Limit, s.Sort)
}