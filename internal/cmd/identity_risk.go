package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/identityrisk"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// newIdentityRiskCmd builds the `jc identity-risk` group.
//
// Ten commands for ten operations. Nine read; the tenth resolves a detection
// and is irreversible, which is why it is classed destructive rather than
// mutating and why its confirmation says so out loud.
func newIdentityRiskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "identity-risk",
		Aliases: []string{"risk", "idrisk"},
		Short:   "Inspect and resolve JumpCloud identity risk detections",
		Long: `Read JumpCloud's identity risk detections: which identities are being
flagged, what triggered each detection, where the access came from, and how
the org's risk is trending.

TIME WINDOWS ARE NOT UNIFORM. The aggregates — stats, identities, login-types,
geolocations, timeline — reject a call that carries no window, so those
commands require --last or --start. The two record reads, "events list" and
"identities get", accept a window but do not need one. That split is not
documented; it was established by probing.

RESOLVING CANNOT BE UNDONE. The API advertises a status that would reopen a
detection and then refuses every attempt to use it: once resolved, a detection
stays resolved. "events resolve" therefore confirms like a delete, and says so.`,
		Example: `  # What is open right now.
  jc identity-risk events list --level high -t

  # Everything in the last 30 days, and where it came from.
  jc identity-risk stats --last 30d
  jc identity-risk geolocations --last 30d -t

  # Close one detection off, having looked into it.
  jc identity-risk events resolve 6aa825580af05c00013a37a4 \
      --state safe --notes "known travel, confirmed with the user"`,
	}
	cmd.AddCommand(
		newIdentityRiskEventsCmd(),
		newIdentityRiskIdentitiesCmd(),
		newIdentityRiskStatsCmd(),
		newIdentityRiskLoginTypesCmd(),
		newIdentityRiskGeolocationsCmd(),
		newIdentityRiskTimelineCmd(),
	)
	return cmd
}

// riskWindow resolves --last/--start/--end into the query parameters this area
// wants. It reuses the Directory Insights parser so "30d", "24h" and an
// RFC3339 timestamp all mean here what they mean there.
//
// required says whether the endpoint rejects a call with no window; when it
// does, the error names the flags rather than forwarding the server's
// snake_case field names, which no flag is called.
func riskWindow(last, start, end string, required bool) (url.Values, error) {
	if last == "" && start == "" {
		if required {
			return nil, fmt.Errorf("this command needs a time window: pass --last (e.g. --last 30d) or --start")
		}
		return url.Values{}, nil
	}
	if last != "" && start != "" {
		return nil, fmt.Errorf("--last and --start are mutually exclusive")
	}

	v := url.Values{}
	if last != "" {
		t, err := api.ParseTimeRange(last)
		if err != nil {
			return nil, fmt.Errorf("invalid --last: %w", err)
		}
		v.Set("start_time", t.UTC().Format(time.RFC3339))
		// The server has no "now" default for these endpoints — it wants both
		// bounds — so --last supplies the end itself.
		v.Set("end_time", time.Now().UTC().Format(time.RFC3339))
		return v, nil
	}

	t, err := api.ParseTimeRange(start)
	if err != nil {
		return nil, fmt.Errorf("invalid --start: %w", err)
	}
	v.Set("start_time", t.UTC().Format(time.RFC3339))
	if end != "" {
		te, err := api.ParseTimeRange(end)
		if err != nil {
			return nil, fmt.Errorf("invalid --end: %w", err)
		}
		v.Set("end_time", te.UTC().Format(time.RFC3339))
	} else {
		v.Set("end_time", time.Now().UTC().Format(time.RFC3339))
	}
	return v, nil
}

// withQuery appends query parameters to an endpoint.
func withQuery(endpoint string, v url.Values) string {
	if len(v) == 0 {
		return endpoint
	}
	return endpoint + "?" + v.Encode()
}

// riskGet is the shared read path.
func riskGet(ctx context.Context, endpoint string) (json.RawMessage, error) {
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}
	return client.Get(ctx, endpoint)
}

// addWindowFlags registers the window flags on a command.
func addWindowFlags(cmd *cobra.Command, last, start, end *string) {
	cmd.Flags().StringVar(last, "last", "", "Relative window, e.g. 24h, 7d, 30d")
	cmd.Flags().StringVar(start, "start", "", "Window start (RFC3339, or relative like 30d)")
	cmd.Flags().StringVar(end, "end", "", "Window end (RFC3339); defaults to now")
}

// --- events ---------------------------------------------------------------

func newIdentityRiskEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "events",
		Aliases: []string{"event", "detections"},
		Short:   "Risk detections",
	}
	cmd.AddCommand(newIdentityRiskEventsListCmd(), newIdentityRiskEventGetCmd(), newIdentityRiskEventResolveCmd())
	return cmd
}

func newIdentityRiskEventsListCmd() *cobra.Command {
	var last, start, end, level, search string
	var limit, skip int

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List risk detections",
		Long: `List risk detections, newest first.

A time window is optional here, unlike the aggregate commands. The footer
reports the server's match count, which is larger than the rows shown whenever
--limit or --skip is in play.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := riskWindow(last, start, end, identityrisk.RequiresWindow(identityrisk.EventsEndpoint))
			if err != nil {
				return err
			}
			if level != "" {
				full, err := identityrisk.ExpandLevel(level)
				if err != nil {
					return err
				}
				v.Add("filter", "level:eq:"+full)
			}
			if search != "" {
				v.Set("searchTerm", search)
			}
			if limit > 0 {
				v.Set("limit", strconv.Itoa(limit))
			}
			if skip > 0 {
				v.Set("skip", strconv.Itoa(skip))
			}

			raw, err := riskGet(cmd.Context(), withQuery(identityrisk.EventsEndpoint, v))
			if err != nil {
				return err
			}
			rows, total, err := identityrisk.ParseEvents(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"objectId", "level", "score", "identityDisplayName",
				"applicationDisplayName", "resolutionStatus", "lastOccurrenceAt"}
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				writeListFooter(cmd, len(rows), total)
			}
			return nil
		},
	}
	addWindowFlags(cmd, &last, &start, &end)
	cmd.Flags().StringVar(&level, "level", "", "Only this risk level: low, medium, high, critical")
	cmd.Flags().StringVar(&search, "search", "", "Free-text search across detections")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum rows to return")
	cmd.Flags().IntVar(&skip, "skip", 0, "Rows to skip")
	return cmd
}

func newIdentityRiskEventGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <objectId>",
		Short: "One risk detection in full",
		Long: `One detection with everything the server holds, including the raw Directory
Insights event that produced it.

Detections are addressed by objectId only — there is no lookup by name, and an
id of the wrong shape is rejected here rather than sent, so a typo reads as a
typo instead of a server error.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !identityrisk.IsObjectID(args[0]) {
				return identityrisk.ErrNotObjectID("detection", args[0])
			}
			raw, err := riskGet(cmd.Context(), identityrisk.EventEndpoint(args[0]))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

func newIdentityRiskEventResolveCmd() *cobra.Command {
	var status, state, notes string

	cmd := &cobra.Command{
		Use:   "resolve <objectId>",
		Short: "Resolve a risk detection (cannot be undone)",
		Long: `Close a risk detection, recording whether the access turned out to be safe.

` + identityrisk.IrreversibleWarning + `

The API's own enum advertises a status that would reopen a detection. It does
not work: every call after the first returns "risk event is already resolved".
Treat this like a delete — preview with --plan, and note WHY in --notes, since
the note is the only record of the judgement that was made.`,
		Example: `  jc identity-risk events resolve 6aa825580af05c00013a37a4 \
      --state safe --notes "known travel, confirmed with the user"

  # Mark it as a genuine compromise instead.
  jc identity-risk events resolve 6aa825580af05c00013a37a4 --state unsafe

  # Preview only; makes no request.
  jc identity-risk events resolve 6aa825580af05c00013a37a4 --state safe --plan`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if !identityrisk.IsObjectID(id) {
				return identityrisk.ErrNotObjectID("detection", id)
			}
			body, err := identityrisk.BuildResolve(status, state, notes)
			if err != nil {
				return err
			}

			if viper.GetBool("plan") {
				effects := []string{
					"status: " + body.ResolutionStatus,
					"state: " + body.ResolutionState,
				}
				if body.ResolutionNotes != "" {
					effects = append(effects, "notes: "+body.ResolutionNotes)
				}
				// Short enough for the plan box, which does not wrap. The
				// full sentence prints on the confirmation path.
				effects = append(effects, "irreversible: cannot be reopened")
				return renderPlan(cmd, &plan.Plan{
					Action:     "resolve",
					Resource:   "identity-risk detection",
					Target:     id,
					Effects:    effects,
					Reversible: false,
				})
			}

			if mustAbortWithoutTTY() {
				return fmt.Errorf("resolving detection %s requires --force or --non-interactive "+
					"(or preview with --plan first) — %s", id, identityrisk.IrreversibleWarning)
			}
			if shouldConfirm() {
				fmt.Fprintln(cmd.ErrOrStderr(), identityrisk.IrreversibleWarning)
				ok, err := askYesNo(cmd, fmt.Sprintf("Resolve detection %s as %s?", id, state))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
					return nil
				}
			}

			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Patch(cmd.Context(), identityrisk.EventResolveEndpoint(id), body)
			if err != nil {
				if identityrisk.ErrAlreadyResolved(err) {
					// Settled state, not a transient failure. Saying so stops
					// the operator retrying something that cannot succeed.
					return fmt.Errorf("detection %s is already resolved, and the API allows no "+
						"further change to it — including reopening", id)
				}
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	cmd.Flags().StringVar(&status, "status", "resolved",
		"Resolution status: resolved, dismissed, mfa-resolved")
	cmd.Flags().StringVar(&state, "state", "",
		"Whether the access was legitimate: safe or unsafe (required)")
	cmd.Flags().StringVar(&notes, "notes", "", "Why — the only record of the judgement made")
	return cmd
}

// --- identities -----------------------------------------------------------

func newIdentityRiskIdentitiesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "identities",
		Aliases: []string{"identity"},
		Short:   "Identities carrying risk",
	}
	cmd.AddCommand(newIdentityRiskIdentitiesListCmd(), newIdentityRiskIdentityGetCmd(), newIdentityRiskIdentityTrendCmd())
	return cmd
}

func newIdentityRiskIdentitiesListCmd() *cobra.Command {
	var last, start, end string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Identities with detections in the window",
		Long:    "Requires a time window. The footer reports totalIdentitiesWithRisk, the server's own count.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := riskWindow(last, start, end, identityrisk.RequiresWindow(identityrisk.IdentitiesEndpoint))
			if err != nil {
				return err
			}
			raw, err := riskGet(cmd.Context(), withQuery(identityrisk.IdentitiesEndpoint, v))
			if err != nil {
				return err
			}
			rows, total, err := identityrisk.ParseIdentities(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"identityObjectId", "displayName", "identifier", "count"}
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				writeListFooter(cmd, len(rows), total)
			}
			return nil
		},
	}
	addWindowFlags(cmd, &last, &start, &end)
	return cmd
}

func newIdentityRiskIdentityGetCmd() *cobra.Command {
	var last, start, end string
	cmd := &cobra.Command{
		Use:   "get <objectId>",
		Short: "One identity's risk profile",
		Long: `An identity's behavioural profile: typical login hours, days, locations and
devices, plus the raw profile data the scoring is derived from.

A time window is optional here — unlike the identities listing, this endpoint
answers without one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !identityrisk.IsObjectID(args[0]) {
				return identityrisk.ErrNotObjectID("identity", args[0])
			}
			v, err := riskWindow(last, start, end, identityrisk.RequiresWindow(identityrisk.IdentityEndpoint(args[0])))
			if err != nil {
				return err
			}
			raw, err := riskGet(cmd.Context(), withQuery(identityrisk.IdentityEndpoint(args[0]), v))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	addWindowFlags(cmd, &last, &start, &end)
	return cmd
}

func newIdentityRiskIdentityTrendCmd() *cobra.Command {
	var last, start, end string
	cmd := &cobra.Command{
		Use:   "trend <objectId>",
		Short: "One identity's detection count against the previous period",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !identityrisk.IsObjectID(args[0]) {
				return identityrisk.ErrNotObjectID("identity", args[0])
			}
			v, err := riskWindow(last, start, end, true)
			if err != nil {
				return err
			}
			raw, err := riskGet(cmd.Context(), withQuery(identityrisk.IdentityTrendEndpoint(args[0]), v))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	addWindowFlags(cmd, &last, &start, &end)
	return cmd
}

// --- aggregates -----------------------------------------------------------

// newRiskAggregateCmd builds the three single-object aggregate reads, which
// differ only in endpoint and help text.
func newRiskAggregateCmd(use, short, long, endpoint string) *cobra.Command {
	var last, start, end string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := riskWindow(last, start, end, identityrisk.RequiresWindow(endpoint))
			if err != nil {
				return err
			}
			raw, err := riskGet(cmd.Context(), withQuery(endpoint, v))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	addWindowFlags(cmd, &last, &start, &end)
	return cmd
}

func newIdentityRiskStatsCmd() *cobra.Command {
	return newRiskAggregateCmd("stats", "Org-wide risk counters for the window",
		`Open and resolved detection counts, critical-risk and privileged-identity
totals, each with the equivalent figure for the previous period.

Requires a time window.`, identityrisk.StatsEndpoint)
}

func newIdentityRiskLoginTypesCmd() *cobra.Command {
	var last, start, end string
	cmd := &cobra.Command{
		Use:     "login-types",
		Aliases: []string{"logintypes"},
		Short:   "Which kinds of resource the risky logins targeted",
		Long:    "A count per target resource type. Requires a time window.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := riskWindow(last, start, end, identityrisk.RequiresWindow(identityrisk.LoginTypesEndpoint))
			if err != nil {
				return err
			}
			raw, err := riskGet(cmd.Context(), withQuery(identityrisk.LoginTypesEndpoint, v))
			if err != nil {
				return err
			}
			rows, err := identityrisk.ParseLoginTypes(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"loginResource", "count"}
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				writeListFooter(cmd, len(rows), len(rows))
			}
			return nil
		},
	}
	addWindowFlags(cmd, &last, &start, &end)
	return cmd
}

func newIdentityRiskGeolocationsCmd() *cobra.Command {
	var last, start, end string
	cmd := &cobra.Command{
		Use:     "geolocations",
		Aliases: []string{"geo", "locations"},
		Short:   "Where the risky access came from",
		Long:    "A count per country and city. Requires a time window.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := riskWindow(last, start, end, identityrisk.RequiresWindow(identityrisk.GeolocationsEndpoint))
			if err != nil {
				return err
			}
			raw, err := riskGet(cmd.Context(), withQuery(identityrisk.GeolocationsEndpoint, v))
			if err != nil {
				return err
			}
			rows, err := identityrisk.ParseGeolocations(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"countryCode", "countryName", "city", "count"}
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				writeListFooter(cmd, len(rows), len(rows))
			}
			return nil
		},
	}
	addWindowFlags(cmd, &last, &start, &end)
	return cmd
}

func newIdentityRiskTimelineCmd() *cobra.Command {
	var last, start, end string
	var factors []string

	cmd := &cobra.Command{
		Use:     "timeline",
		Aliases: []string{"factors-timeline"},
		Short:   "Detections over time, per risk factor",
		Long: `Detection counts bucketed over the window, one series per risk-factor type.

The API returns a map keyed by factor type. That is flattened here into one
row per factor, sorted by name, so the output is a stable table rather than an
object whose keys change with the tenant.

This is a POST that reads: nothing is created and nothing changes. The body
exists only because the filter does not fit in a query string, which is why
the command needs neither --force nor confirmation.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := riskWindow(last, start, end, true)
			if err != nil {
				return err
			}
			body := identityrisk.TimelineRequest{
				StartTime:       v.Get("start_time"),
				EndTime:         v.Get("end_time"),
				RiskFactorTypes: factors,
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Create(cmd.Context(), identityrisk.FactorsTimelineEndpoint, body)
			if err != nil {
				return err
			}
			series, err := identityrisk.ParseTimeline(raw)
			if err != nil {
				return err
			}
			rows := make([]json.RawMessage, 0, len(series))
			for _, s := range series {
				b, err := json.Marshal(map[string]any{
					"riskFactorType": s.RiskFactorType,
					"buckets":        len(s.Buckets),
					"series":         s.Buckets,
				})
				if err != nil {
					return err
				}
				rows = append(rows, b)
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"riskFactorType", "buckets"}
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %d risk factors ──\n", len(rows))
			}
			return nil
		},
	}
	addWindowFlags(cmd, &last, &start, &end)
	cmd.Flags().StringArrayVar(&factors, "factor", nil,
		"Restrict to a risk factor type, e.g. RISK_FACTOR_TYPE_DORMANT_ACCOUNT_LOGIN (repeatable)")
	return cmd
}
