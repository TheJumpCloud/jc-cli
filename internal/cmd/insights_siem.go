package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
)

func newInsightsSiemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "siem",
		Short: "Test and preview the SIEM export (Directory Insights data export)",
		Long: `Check the SIEM pull connection and pull a window of exported events.

These use the Insights host for your region (--region, JC_INSIGHTS_BASE_URL).`,
	}
	cmd.AddCommand(newInsightsSiemTestCmd())
	cmd.AddCommand(newInsightsSiemEventsCmd())
	return cmd
}

func newInsightsSiemTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Test the SIEM pull connection",
		Long: `POST /data-export/v2/siem/connection-test. Prints ok and org_id and exits
non-zero when ok is false. The endpoint returns no event payloads.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newInsightsClient()
			if err != nil {
				return err
			}
			res, err := client.SIEMConnectionTest(cmd.Context())
			if err != nil {
				return siemAPIError(err)
			}
			raw, _ := json.Marshal(res)
			if err := output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions()); err != nil {
				return err
			}
			if !res.OK {
				return NewCLIError(ErrCodeGeneral, "SIEM connection test failed (ok is false)", "Check the org binding and that the API key may read SIEM exports")
			}
			return nil
		},
	}
}

func newInsightsSiemEventsCmd() *cobra.Command {
	var (
		sinceFlag, untilFlag, lastFlag string
		eventTypes                     []string
		limitFlag                      int
		ocsfFlag                       bool
	)
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Pull exported SIEM events for a window of at most one hour",
		Long: `Pull events exactly as a SIEM would receive them, following next_cursor.

The window (until - since) may not exceed one hour, and until may not be later
than 15 minutes ago. --event-type takes up to 25 slugs (repeatable or
comma-separated) or "all". Use -o ndjson to stream one event per line.

Examples:
  jc insights siem events --last 30m --event-type all -o ndjson
  jc insights siem events --since 2026-01-01T00:00:00Z --until 2026-01-01T00:30:00Z --event-type sso_auth,user_login_attempt
  jc insights siem events --last 1h --event-type all --ocsf --limit 100`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := buildSIEMQuery(sinceFlag, untilFlag, lastFlag, eventTypes, limitFlag, ocsfFlag, time.Now())
			if err != nil {
				return err
			}
			client, err := newInsightsClient()
			if err != nil {
				return err
			}
			events, err := client.SIEMEvents(cmd.Context(), *q, limitFlag)
			if err != nil {
				return siemAPIError(err)
			}
			return output.WriteList(cmd.OutOrStdout(), events, output.CurrentOptions())
		},
	}
	cmd.Flags().StringVar(&sinceFlag, "since", "", "Window start (RFC 3339)")
	cmd.Flags().StringVar(&untilFlag, "until", "", "Window end (RFC 3339, at most 1h after --since, at most now-15m)")
	cmd.Flags().StringVar(&lastFlag, "last", "", "Window ending 15 minutes ago and this long (at most 1h, e.g. 30m)")
	cmd.Flags().StringSliceVar(&eventTypes, "event-type", nil, "Event type slug (repeatable or comma-separated, at most 25) or \"all\"")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of events to output (0 = all)")
	cmd.Flags().BoolVar(&ocsfFlag, "ocsf", false, "Request OCSF format (Accept: application/ocsf+json)")
	return cmd
}

func siemUsage(format string, a ...any) error {
	return NewCLIError(ErrCodeUsageError, fmt.Sprintf(format, a...), "")
}

// buildSIEMQuery validates the flags locally; nothing is sent on error.
func buildSIEMQuery(since, until, last string, eventTypes []string, limit int, ocsf bool, now time.Time) (*api.SIEMEventsQuery, error) {
	var s, u time.Time
	var err error
	switch {
	case last != "" && (since != "" || until != ""):
		return nil, siemUsage("--last cannot be combined with --since or --until")
	case last != "":
		d, perr := time.ParseDuration(last)
		if perr != nil || d <= 0 {
			return nil, siemUsage("invalid --last %q: use a duration such as 30m or 1h", last)
		}
		u = now.UTC().Add(-15 * time.Minute).Truncate(time.Second)
		s = u.Add(-d)
	case since == "" || until == "":
		return nil, siemUsage("give --last, or both --since and --until")
	default:
		if s, err = time.Parse(time.RFC3339, since); err != nil {
			return nil, siemUsage("invalid --since %q: use RFC 3339", since)
		}
		if u, err = time.Parse(time.RFC3339, until); err != nil {
			return nil, siemUsage("invalid --until %q: use RFC 3339", until)
		}
	}
	if !u.After(s) {
		return nil, siemUsage("--until must be after --since")
	}
	if u.Sub(s) > api.SIEMMaxWindow {
		return nil, siemUsage("the window may not exceed one hour (got %s)", u.Sub(s))
	}

	var types []string
	for _, t := range eventTypes {
		if t = strings.TrimSpace(t); t != "" {
			types = append(types, t)
		}
	}
	if len(types) == 0 {
		return nil, siemUsage("at least one --event-type is required (or \"all\")")
	}
	if len(types) > api.SIEMMaxEventTypes {
		return nil, siemUsage("at most %d event types may be given (got %d)", api.SIEMMaxEventTypes, len(types))
	}
	if limit < 0 {
		return nil, siemUsage("--limit must not be negative")
	}
	pageSize := limit
	if pageSize > api.SIEMMaxPageSize {
		pageSize = api.SIEMMaxPageSize
	}
	return &api.SIEMEventsQuery{Since: s, Until: u, EventTypes: types, Limit: pageSize, OCSF: ocsf}, nil
}

// siemAPIError adds the guidance specific to the data-export endpoints.
func siemAPIError(err error) error {
	var un *api.SIEMUnavailableError
	if errors.As(err, &un) {
		e := CLIErrorFromAPI(un.APIError)
		e.Message = fmt.Sprintf("%s (events are available until %s)", e.Message, un.AvailableUntil)
		e.Suggestion = fmt.Sprintf("Retry with --until %s or earlier", un.AvailableUntil)
		return e
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		e := CLIErrorFromAPI(apiErr)
		switch apiErr.StatusCode {
		case http.StatusPaymentRequired:
			e.Suggestion = "SIEM export needs the Directory Insights Premium entitlement"
		case http.StatusForbidden:
			e.Suggestion = "The API key lacks the scope required for SIEM export"
		}
		return e
	}
	return err
}
