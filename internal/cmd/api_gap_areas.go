package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/direports"
	"github.com/klaassen-consulting/jc/internal/emailsubs"
	"github.com/klaassen-consulting/jc/internal/gcep"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/risksettings"
	"github.com/klaassen-consulting/jc/internal/siem"
)

// The last six API areas without a command group (KLA-485): Directory
// Insights interval counts and reports, the SIEM pull API, email
// subscriptions, Access Risk settings and GCEP. Each area's contract is in
// its package.

// insightsRaw sends one request to the insights host.
func insightsRaw(cmd *cobra.Command, method, path string, body any) ([]byte, error) {
	c, err := newInsightsClient()
	if err != nil {
		return nil, err
	}
	return c.Raw(cmd.Context(), method, c.BaseURL, path, body)
}

// dataExportRaw sends one request to the SIEM pull API, on the same regional
// host as insights.
func dataExportRaw(cmd *cobra.Command, method, path string, body any) ([]byte, error) {
	c, err := newInsightsClient()
	if err != nil {
		return nil, err
	}
	base, err := api.DataExportBaseURL(c.BaseURL)
	if err != nil {
		return nil, err
	}
	return c.Raw(cmd.Context(), method, base, path, body)
}

// --- Directory Insights: interval counts ------------------------------------

func newInsightsIntervalCmd() *cobra.Command {
	var service, last, start, end, interval, timezone string
	cmd := &cobra.Command{
		Use:   "interval",
		Short: "Count Directory Insights events per time bucket",
		Long: `Count Directory Insights events in fixed time buckets — a histogram of activity.

--interval is a number and a unit: m (minutes), h (hours), d (days) or w
(weeks), e.g. 1h or 1d. Each row is one bucket: its start (key_as_string) and
its event count (doc_count).`,
		Example: `  jc insights interval --service all --last 7d --interval 1d -t
  jc insights interval --service sso --last 24h --interval 1h`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			startTime, endTime, err := resolveInsightsTimeRange(last, start, end)
			if err != nil {
				return err
			}
			body, err := api.IntervalBody(service, startTime, endTime, interval, timezone)
			if err != nil {
				return err
			}
			c, err := newInsightsClient()
			if err != nil {
				return err
			}
			rows, err := c.IntervalEvents(cmd.Context(), body)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"key_as_string", "doc_count"}
			return output.WriteList(cmd.OutOrStdout(), rows, opts)
		},
	}
	cmd.Flags().StringVar(&service, "service", "all", "Service (e.g. sso, ldap, all; comma-separated for several)")
	cmd.Flags().StringVar(&last, "last", "", "Time range shortcut (e.g. 24h, 7d, 30d)")
	cmd.Flags().StringVar(&start, "start", "", "Start time (date or RFC 3339)")
	cmd.Flags().StringVar(&end, "end", "", "End time (date or RFC 3339); defaults to now")
	cmd.Flags().StringVar(&interval, "interval", "1d", "Bucket size: a number and m, h, d or w")
	cmd.Flags().StringVar(&timezone, "timezone", "", "Bucket boundaries in this UTC offset, e.g. -0500")
	return cmd
}

// --- Directory Insights: reports --------------------------------------------

func newInsightsReportsCmd() *cobra.Command {
	group := &cobra.Command{
		Use:   "reports",
		Short: "Request and download Directory Insights reports",
		Long: `Directory Insights reports: generated asynchronously, kept for 7 days, each
with a CSV and a JSON artifact. Not to be confused with ` + "`jc reports`" + `, the
console's report templates and schedules.`,
	}
	group.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List requested reports and their status",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := insightsRaw(cmd, http.MethodGet, direports.ListPath, nil)
			if err != nil {
				return err
			}
			rows, err := direports.ParseList(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"id", "type", "status", "created_at", "expiration"}
			return output.WriteList(cmd.OutOrStdout(), rows, opts)
		},
	})
	group.AddCommand(&cobra.Command{
		Use:   "request <type>",
		Short: "Request a report (generated asynchronously)",
		Long: `Request a Directory Insights report. It is generated in the background, usually
within a minute; ` + "`list`" + ` shows its status and ` + "`download`" + ` fetches it once
COMPLETED. A report can also FAIL.

Types: ` + strings.Join(direports.Types, ", ") + `.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := direports.RequestPath(args[0])
			if err != nil {
				return err
			}
			ok, err := confirmChange(cmd, "request", "Directory Insights report", args[0],
				[]string{"generated in the background; kept for 7 days"}, true)
			if err != nil || !ok {
				return err
			}
			raw, err := insightsRaw(cmd, http.MethodPost, path, map[string]any{})
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})
	var outFile string
	download := &cobra.Command{
		Use:   "download <report-id> <csv|json|artifact-id>",
		Short: "Download a completed report's CSV or JSON",
		Long: `Download one artifact of a completed report, chosen by format (csv, json) or id.

Reports carry personal data (usernames, emails, account state), so the file is
written to --out (a new file, mode 0600; an existing one is not overwritten), or
to stdout only when stdout is not a terminal.`,
		Example: `  jc insights reports download 6aff3f6a255b33a99d4cabd0 csv --out groups.csv`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if outFile == "" {
				if f, ok := cmd.OutOrStdout().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
					return fmt.Errorf("a report holds personal data and is not printed to a terminal: pass --out <file> or redirect stdout")
				}
			}
			raw, err := insightsRaw(cmd, http.MethodGet, direports.ListPath, nil)
			if err != nil {
				return err
			}
			rows, err := direports.ParseList(raw)
			if err != nil {
				return err
			}
			rep, err := direports.Find(rows, args[0])
			if err != nil {
				return err
			}
			art, err := direports.PickArtifact(rep, args[1])
			if err != nil {
				return err
			}
			content, err := insightsRaw(cmd, http.MethodGet, direports.ContentPath(rep.ID, art.ID), nil)
			if err != nil {
				return err
			}
			if outFile == "" {
				_, err = cmd.OutOrStdout().Write(content)
				return err
			}
			if err := writeNewPrivateFile(outFile, content); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s (%s, %d bytes)\n", outFile, art.Format, len(content))
			return nil
		},
	}
	download.Flags().StringVar(&outFile, "out", "", "Write to this new file (mode 0600; never overwrites)")
	group.AddCommand(download)
	return group
}

// --- SIEM pull --------------------------------------------------------------

func newSIEMCmd() *cobra.Command {
	group := &cobra.Command{
		Use:   "siem",
		Short: "Pull events from the SIEM export API",
		Long: `Pull Directory Insights events from the SIEM export API — the feed a SIEM
polls. Each request covers at most one hour, and data lags real time by about
two hours. Use ` + "`jc insights`" + ` for interactive queries; this is the export path.`,
	}
	var since, until, types, cursor string
	var limit, hoursAgo int
	events := &cobra.Command{
		Use:   "events",
		Short: "Pull one page of events for a window of at most an hour",
		Long: `Pull one page of events. Give --since and --until (at most one hour apart),
or --hours-ago N to pull the hour that ended N hours ago (default 3, safely past
the export lag). --types is "all" (default) or up to 25 comma-separated event
types. When more events remain, the cursor for the next page is printed to
stderr together with the window it belongs to; pass all three back.`,
		Example: `  jc siem events
  jc siem events --since 2026-11-19T10:00:00Z --until 2026-11-19T11:00:00Z --types user_login_attempt`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cursor != "" && since == "" {
				return fmt.Errorf("--cursor continues one window: pass the --since and --until printed with it")
			}
			q, err := siem.NewQuery(since, until, time.Duration(hoursAgo)*time.Hour, types, limit, cursor)
			if err != nil {
				return err
			}
			path, err := q.Path()
			if err != nil {
				return err
			}
			raw, err := dataExportRaw(cmd, http.MethodGet, path, nil)
			if err != nil {
				return siemErr(err)
			}
			page, err := siem.ParsePage(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"timestamp", "event_type", "service", "success", "id"}
			if err := output.WriteList(cmd.OutOrStdout(), page.Events, opts); err != nil {
				return err
			}
			if page.NextCursor != "" && !opts.Quiet {
				fmt.Fprintf(cmd.ErrOrStderr(), "More: --since %s --until %s --cursor %s\n",
					q.Since.UTC().Format(time.RFC3339), q.Until.UTC().Format(time.RFC3339), page.NextCursor)
			}
			return nil
		},
	}
	events.Flags().StringVar(&since, "since", "", "Window start (RFC 3339)")
	events.Flags().StringVar(&until, "until", "", "Window end (RFC 3339), at most an hour after --since")
	events.Flags().IntVar(&hoursAgo, "hours-ago", 3, "Without --since: pull the hour ending this many hours ago")
	events.Flags().StringVar(&types, "types", "all", "\"all\" or up to 25 comma-separated event types")
	events.Flags().IntVar(&limit, "limit", 0, "Events per page (max 1000; default 1000)")
	events.Flags().StringVar(&cursor, "cursor", "", "Cursor from the previous page")
	group.AddCommand(events)
	group.AddCommand(&cobra.Command{
		Use:   "test",
		Short: "Check that the SIEM export is reachable for this org",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := dataExportRaw(cmd, http.MethodPost, siem.ConnectionTestPath, map[string]any{})
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})
	return group
}

func siemErr(err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnprocessableEntity {
		return fmt.Errorf("%w — %s", err, siem.LagHint)
	}
	return err
}

// --- Email subscriptions ----------------------------------------------------

func newEmailSubscriptionsCmd() *cobra.Command {
	group := &cobra.Command{
		Use:     "email-subscriptions",
		Aliases: []string{"email-subs"},
		Short:   "Your own admin notification email preferences",
		Long: `The notification emails JumpCloud sends to YOU, the calling admin — e.g. "User
Account Locked — Admin Notice". Changing one affects only your own inbox.`,
	}
	group.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List your email subscriptions",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), emailsubs.Path)
			if err != nil {
				return err
			}
			rows, _, err := emailsubs.Parse(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"emailKey", "title", "subscribed"}
			return output.WriteList(cmd.OutOrStdout(), rows, opts)
		},
	})
	for _, on := range []bool{true, false} {
		on := on
		use, short := "subscribe <email-key>", "Start receiving one notification email"
		if !on {
			use, short = "unsubscribe <email-key>", "Stop receiving one notification email"
		}
		group.AddCommand(&cobra.Command{
			Use:   use,
			Short: short,
			Long:  short + ". Only this key changes; the others keep their setting. The change is read back.",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				verb := map[bool]string{true: "subscribe to", false: "unsubscribe from"}[on]
				ok, err := confirmChange(cmd, verb, "email", args[0], []string{"your own inbox only"}, true)
				if err != nil || !ok {
					return err
				}
				client, err := newV2Client()
				if err != nil {
					return err
				}
				if _, err := client.Update(cmd.Context(), emailsubs.Path, emailsubs.Body(args[0], on)); err != nil {
					return err
				}
				raw, err := client.Get(cmd.Context(), emailsubs.Path)
				if err != nil {
					return err
				}
				_, subs, err := emailsubs.Parse(raw)
				if err != nil {
					return err
				}
				got, err := emailsubs.State(subs, args[0])
				if err != nil {
					return err
				}
				if got != on {
					return fmt.Errorf("the API accepted the change, but %s still reads subscribed=%v", args[0], got)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: %s subscribed=%v\n", args[0], got)
				return nil
			},
		})
	}
	return group
}

// --- Access Risk settings (under identity-risk) -----------------------------

func newIdentityRiskSettingsCmd() *cobra.Command {
	group := &cobra.Command{
		Use:   "settings",
		Short: "Access Risk detection settings (the detectors, thresholds and switch)",
		Long: `The Access Risk detection settings: which detectors run (impossible travel,
new device, dormant account, ...), their thresholds, trusted IP and ASN lists,
notification, and "enabled", the master switch for the whole feature.`,
	}
	group.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List every setting and its value",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			settings, err := readRiskSettings(cmd)
			if err != nil {
				return err
			}
			rows := make([]json.RawMessage, len(settings))
			for i, s := range settings {
				rows[i], _ = json.Marshal(s)
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"name", "value"}
			return output.WriteList(cmd.OutOrStdout(), rows, opts)
		},
	})
	group.AddCommand(&cobra.Command{
		Use:   "set <name> <value>",
		Short: "Change one setting",
		Long: `Change one Access Risk setting. The value is read as the setting's current type:
true/false, a whole number, text, or a list (JSON array or comma-separated).
The API rejects a value of the wrong type, so jc converts it first, and reads the
setting back afterwards.

"enabled" switches the whole feature on or off.`,
		Example: `  jc identity-risk settings set dormant_account_threshold_days 90
  jc identity-risk settings set impossible_travel false`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			settings, err := readRiskSettings(cmd)
			if err != nil {
				return err
			}
			cur, err := risksettings.Find(settings, args[0])
			if err != nil {
				return err
			}
			val, err := risksettings.Coerce(cur.Value, args[1])
			if err != nil {
				return err
			}
			ok, err := confirmChange(cmd, "set", "access risk setting", args[0],
				[]string{fmt.Sprintf("%v → %v", cur.Value, val)}, true)
			if err != nil || !ok {
				return err
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			if _, err := client.Update(cmd.Context(), risksettings.SettingPath(args[0]), map[string]any{"value": val}); err != nil {
				return err
			}
			after, err := readRiskSettings(cmd)
			if err != nil {
				return err
			}
			got, err := risksettings.Find(after, args[0])
			if err != nil {
				return err
			}
			if !risksettings.Equal(got.Value, val) {
				return fmt.Errorf("the API accepted the change, but %s reads back as %v", args[0], got.Value)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: %s = %v\n", args[0], got.Value)
			return nil
		},
	})
	return group
}

func readRiskSettings(cmd *cobra.Command) ([]risksettings.Setting, error) {
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}
	raw, err := client.Get(cmd.Context(), risksettings.Path)
	if err != nil {
		return nil, err
	}
	return risksettings.Parse(raw)
}

// --- GCEP -------------------------------------------------------------------

func newGCEPCmd() *cobra.Command {
	group := &cobra.Command{
		Use:   "gcep",
		Short: "Google Chrome Enterprise Premium device trust integration (read-only)",
		Long: `The Google Chrome Enterprise Premium (GCEP) device trust integration: its
configuration and the trust state it holds for a device. Read-only: deleting the
configuration disconnects the organization from Google.`,
	}
	group.AddCommand(&cobra.Command{
		Use:   "config",
		Short: "Show the GCEP integration configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return gcepRead(cmd, gcep.ConfigurationPath, gcep.NotConfigured())
		},
	})
	group.AddCommand(&cobra.Command{
		Use:   "device-state <device>",
		Short: "Show a device's GCEP trust state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v1, err := newV1Client()
			if err != nil {
				return err
			}
			id, err := resolveDevice(cmd.Context(), v1, args[0])
			if err != nil {
				return err
			}
			return gcepRead(cmd, gcep.DeviceStatePath(id), gcep.NoDeviceState(args[0]))
		},
	})
	return group
}

func gcepRead(cmd *cobra.Command, path string, notFound error) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	raw, err := client.Get(cmd.Context(), path)
	if err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return notFound
		}
		return err
	}
	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}

// writeNewPrivateFile creates path with mode 0600 and writes data. It refuses
// an existing path: os.WriteFile would keep that file's mode (a 0644 file
// stays world-readable) and follow a symlink.
func writeNewPrivateFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists, and jc will not overwrite a file of personal data — "+
				"choose another path or remove it first", path)
		}
		return fmt.Errorf("creating %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return f.Close()
}
