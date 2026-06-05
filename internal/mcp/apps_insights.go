package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
)

//go:embed apps_html/insights.html
var insightsHTML string

const (
	insightsResourceURI = "ui://jc/insights"

	// Upper bound on events fetched per invocation. Directory Insights windows
	// can hold millions of events; we sample with this cap to keep the payload
	// sendable and the aggregation responsive. The tool's Total field always
	// reflects the true count so clients can detect truncation.
	insightsMaxFetchEvents = 10000

	// Max number of events returned in the preview array (for drill-down).
	insightsPreviewLimit = 20

	// Max number of users returned in the top-users ranking.
	insightsTopUsers = 10
)

// insightsViewArgs is the tool input. Mirrors the shape of `jc insights query`
// flags so LLMs familiar with the CLI can invoke the tool naturally.
type insightsViewArgs struct {
	// Service to query. Accepts "all", "sso", "ldap", "radius", "directory",
	// or comma-separated combinations. Defaults to "all".
	Service string `json:"service,omitempty" jsonschema:"Service to query (sso, ldap, radius, directory, all). Accepts comma-separated values."`
	// EventType filters by a specific event type (e.g. sso_auth_failed).
	EventType string `json:"event_type,omitempty" jsonschema:"Filter by event type (e.g. sso_auth_failed, password_change)."`
	// Last is a time-range shortcut: 1h / 24h / 7d / 30d / 1m. Ignored when
	// Start or End is set. Defaults to 24h.
	Last string `json:"last,omitempty" jsonschema:"Time range shortcut: 1h, 24h, 7d, 30d, 1m. Defaults to 24h. Ignored if start/end provided."`
	// Start is an RFC3339 timestamp or YYYY-MM-DD date.
	Start string `json:"start,omitempty" jsonschema:"Start time (RFC3339 or YYYY-MM-DD)."`
	// End is an RFC3339 timestamp or YYYY-MM-DD date. Defaults to now.
	End string `json:"end,omitempty" jsonschema:"End time (RFC3339 or YYYY-MM-DD). Defaults to now."`
	// User filters events whose initiated_by.username matches.
	User string `json:"user,omitempty" jsonschema:"Filter by initiated_by.username."`
}

// insightsViewData is the JSON payload the tool returns to the app iframe.
type insightsViewData struct {
	Service    string            `json:"service"`
	EventType  string            `json:"event_type,omitempty"`
	User       string            `json:"user,omitempty"`
	Start      string            `json:"start"`
	End        string            `json:"end"`
	BucketSize string            `json:"bucket_size"`
	Total      int               `json:"total"`    // true count from CountEvents
	Sampled    int               `json:"sampled"`  // number of events actually aggregated
	EventTypes []string          `json:"event_types"`
	Bins       []insightsBin     `json:"bins"`
	TopUsers   []insightsUserCnt `json:"top_users"`
	Preview    []json.RawMessage `json:"preview"`
	Warnings   []string          `json:"warnings,omitempty"`
}

// insightsBin is one time bucket with a per-event-type count.
type insightsBin struct {
	// Bucket is the RFC3339 start timestamp of the bucket.
	Bucket string `json:"bucket"`
	// Counts maps event type → count within this bucket.
	Counts map[string]int `json:"counts"`
}

// insightsUserCnt is one entry in the top-users ranking.
type insightsUserCnt struct {
	Username string `json:"username"`
	Count    int    `json:"count"`
}

// registerInsightsView wires the insights_view MCP App: typed tool + ui://
// resource. Lives outside appSpecs because the tool takes typed input.
//
// Note: pre-KLA-419 the error path here was "fetching insights: <err>"
// rather than "insights_view: <err>". The shared registerTypedAppTool
// standardizes on the tool name for all typed apps so AI clients
// receiving an error can correlate to the calling tool. Minor wording
// change; no programmatic consumer should be matching on the prefix.
func (s *Server) registerInsightsView() {
	registerTypedAppTool(s, typedAppSpec[insightsViewArgs]{
		Name: "insights_view",
		Description: "Directory Insights event explorer: stacked time-series chart by event type with top-users ranking and event preview. " +
			"Parameters mirror `jc insights query` (service, event_type, last, start, end, user). " +
			"Renders as an interactive dashboard in MCP App-capable hosts.",
		ResourceURI:         insightsResourceURI,
		ResourceName:        "Insights Explorer",
		ResourceDescription: "Interactive Directory Insights time-series and top-users view",
		HTML:                insightsHTML,
		Handler: func(ctx context.Context, args insightsViewArgs) (any, error) {
			return fetchInsightsViewData(ctx, args)
		},
	})
}

// resolveInsightsWindow turns args into a concrete [start, end] pair in UTC,
// applying defaults (service=all, last=24h if nothing else set).
func resolveInsightsWindow(args insightsViewArgs, now time.Time) (start, end time.Time, err error) {
	// End first: default to now if unspecified.
	if args.End != "" {
		end, err = parseInsightsTime(args.End)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parsing end: %w", err)
		}
	} else {
		end = now
	}

	switch {
	case args.Start != "":
		start, err = parseInsightsTime(args.Start)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parsing start: %w", err)
		}
	case args.Last != "":
		// Parse the duration shortcut locally so the start anchors to our
		// end time (which honors nowFunc in tests). api.ParseTimeRange
		// uses its own clock, which causes drift in unit tests.
		dur, err := parseLastDuration(args.Last)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parsing last: %w", err)
		}
		start = end.Add(-dur)
	default:
		// Default window: 24h back from end.
		start = end.Add(-24 * time.Hour)
	}

	if !start.Before(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("start (%s) must be before end (%s)", start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	return start.UTC(), end.UTC(), nil
}

// parseInsightsTime accepts RFC3339, YYYY-MM-DD, or duration shorthand.
func parseInsightsTime(s string) (time.Time, error) {
	return api.ParseTimeRange(s)
}

// parseLastDuration turns a "Xh" / "Xd" / "Xm" shortcut into a time.Duration.
// Mirrors api.ParseTimeRange's shortcut syntax but returns the duration itself
// so callers can anchor it against their own clock rather than time.Now().
func parseLastDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "last ")
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration shortcut %q", s)
	}
	unit := s[len(s)-1]
	var n int
	if _, err := fmt.Sscanf(s[:len(s)-1], "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid duration shortcut %q: %w", s, err)
	}
	switch unit {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'm': // months (calendar) — approximate as 30d for the purpose of the shortcut window.
		return time.Duration(n) * 30 * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("invalid duration unit %q in %q", string(unit), s)
}

// bucketSizeFor picks a sensible time bucket for the given window. Bigger
// windows use bigger buckets to keep the bin count manageable.
func bucketSizeFor(window time.Duration) time.Duration {
	switch {
	case window <= 6*time.Hour:
		return 5 * time.Minute
	case window <= 48*time.Hour:
		return time.Hour
	case window <= 7*24*time.Hour:
		return 6 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// formatBucketSize returns a human-readable representation of a bucket size.
func formatBucketSize(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
}

// fetchInsightsViewData runs the Directory Insights queries, aggregates events
// into time buckets, and returns a payload ready for the UI to render.
func fetchInsightsViewData(ctx context.Context, args insightsViewArgs) (*insightsViewData, error) {
	now := nowFunc().UTC()

	start, end, err := resolveInsightsWindow(args, now)
	if err != nil {
		return nil, err
	}

	service := args.Service
	if service == "" {
		service = "all"
	}

	query := api.InsightsQuery{
		Service:   service,
		StartTime: start.Format(time.RFC3339),
		EndTime:   end.Format(time.RFC3339),
	}
	filter := map[string]any{}
	if args.EventType != "" {
		filter["event_type"] = args.EventType
	}
	if args.User != "" {
		filter["initiated_by.username"] = args.User
	}
	if len(filter) > 0 {
		query.SearchTermFilter = filter
	}

	client, err := newInsightsClientFunc()
	if err != nil {
		return nil, fmt.Errorf("insights client: %w", err)
	}

	total, err := client.CountEvents(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("counting events: %w", err)
	}

	// Fetch events (bounded). If total > cap, we still aggregate what we can
	// and add a warning so the UI can flag partial data.
	fetchLimit := total
	if fetchLimit > insightsMaxFetchEvents {
		fetchLimit = insightsMaxFetchEvents
	}

	var events []json.RawMessage
	if fetchLimit > 0 {
		res, err := client.QueryEvents(ctx, query, api.InsightsQueryOptions{
			Limit: fetchLimit,
			Sort:  "-timestamp",
		})
		if err != nil {
			return nil, fmt.Errorf("querying events: %w", err)
		}
		events = res.Data
	}

	window := end.Sub(start)
	bucket := bucketSizeFor(window)

	data := &insightsViewData{
		Service:    service,
		EventType:  args.EventType,
		User:       args.User,
		Start:      start.Format(time.RFC3339),
		End:        end.Format(time.RFC3339),
		BucketSize: formatBucketSize(bucket),
		Total:      total,
		Sampled:    len(events),
		Bins:       []insightsBin{},
		TopUsers:   []insightsUserCnt{},
		Preview:    []json.RawMessage{},
	}

	if total > len(events) {
		data.Warnings = append(data.Warnings,
			fmt.Sprintf("Window contains %d events; aggregated the most recent %d (chart reflects the sample).", total, len(events)))
	}

	// Pre-seed bins for every bucket in the window so the chart shows empty
	// slots where no events occurred.
	binIndex := map[string]*insightsBin{}
	for t := start.Truncate(bucket); !t.After(end); t = t.Add(bucket) {
		key := t.UTC().Format(time.RFC3339)
		b := insightsBin{Bucket: key, Counts: map[string]int{}}
		data.Bins = append(data.Bins, b)
		binIndex[key] = &data.Bins[len(data.Bins)-1]
	}

	// Tally events into bins, track event types and top users, collect preview.
	eventTypeSet := map[string]struct{}{}
	userCounts := map[string]int{}

	for i, raw := range events {
		var evt struct {
			Timestamp   string `json:"timestamp"`
			EventType   string `json:"event_type"`
			InitiatedBy struct {
				Username string `json:"username"`
			} `json:"initiated_by"`
		}
		if err := json.Unmarshal(raw, &evt); err != nil {
			continue
		}

		evtTime, err := time.Parse(time.RFC3339, evt.Timestamp)
		if err != nil {
			continue
		}

		if evt.EventType == "" {
			evt.EventType = "unknown"
		}
		eventTypeSet[evt.EventType] = struct{}{}

		key := evtTime.UTC().Truncate(bucket).Format(time.RFC3339)
		if b, ok := binIndex[key]; ok {
			b.Counts[evt.EventType]++
		}

		if evt.InitiatedBy.Username != "" {
			userCounts[evt.InitiatedBy.Username]++
		}

		if i < insightsPreviewLimit {
			data.Preview = append(data.Preview, raw)
		}
	}

	// Sort event types for stable rendering (legend order).
	data.EventTypes = make([]string, 0, len(eventTypeSet))
	for k := range eventTypeSet {
		data.EventTypes = append(data.EventTypes, k)
	}
	sort.Strings(data.EventTypes)

	// Top-N users by count (desc), tiebreak on username asc for stability.
	for u, c := range userCounts {
		data.TopUsers = append(data.TopUsers, insightsUserCnt{Username: u, Count: c})
	}
	sort.Slice(data.TopUsers, func(i, j int) bool {
		if data.TopUsers[i].Count != data.TopUsers[j].Count {
			return data.TopUsers[i].Count > data.TopUsers[j].Count
		}
		return strings.Compare(data.TopUsers[i].Username, data.TopUsers[j].Username) < 0
	})
	if len(data.TopUsers) > insightsTopUsers {
		data.TopUsers = data.TopUsers[:insightsTopUsers]
	}

	return data, nil
}
