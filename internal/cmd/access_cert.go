package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/accesscert"
	"github.com/klaassen-consulting/jc/internal/output"
)

var (
	accessCertCampaignFields = []string{"name", "id", "status", "enabled", "scopeType", "scheduleMode", "lastRunAt"}
	accessCertRunFields      = []string{"runId", "campaignName", "runNumber", "status", "scopeType", "reviewerType", "reviewProgressPercent", "activatedAt", "closedAt"}
	accessCertItemFields     = []string{"id", "username", "finalDecision", "remediationAction"}
	accessCertQueueFields    = []string{"id", "campaignName", "taskType", "targetType", "dueDate", "pendingDecisionCount", "reviewProgressPercent"}
)

type accessCertListFlags struct {
	limit    int
	sort     string
	filters  []string
	metadata bool
}

// add registers the list flags; a non-empty metadataNoun ("campaigns",
// "runs") also adds --metadata.
func (f *accessCertListFlags) add(cmd *cobra.Command, metadataNoun string) {
	cmd.Flags().IntVar(&f.limit, "limit", 0, "Maximum number of results to return (0 = all; pages of 100 are fetched until done)")
	cmd.Flags().StringVar(&f.sort, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&f.filters, "filter", nil, "Filter results as field:op:value (repeatable)")
	if metadataNoun != "" {
		cmd.Flags().BoolVar(&f.metadata, "metadata", false, "Return only the applicationIds and userGroupIds referenced by the matching "+metadataNoun+" (the API returns no rows in this mode)")
	}
}

func (f *accessCertListFlags) query() (accesscert.Query, error) {
	if f.limit < 0 {
		return accesscert.Query{}, fmt.Errorf("--limit must not be negative")
	}
	return accesscert.Query{Sort: f.sort, Filters: f.filters, Metadata: f.metadata, Limit: f.limit}, nil
}

// writeAccessCertPage writes rows, or the whole envelope when --metadata
// asked for metadata that a bare row list would drop.
func writeAccessCertPage(cmd *cobra.Command, p *accesscert.Page, fields []string, metadata bool) error {
	opts := output.CurrentOptions()
	opts.DefaultFields = fields
	tabular := opts.Format == output.FormatTable || opts.Format == output.FormatCSV
	if metadata && !tabular && !opts.IDsOnly {
		env, err := p.Envelope()
		if err != nil {
			return err
		}
		return output.WriteSingle(cmd.OutOrStdout(), env, opts)
	}
	return output.WriteList(cmd.OutOrStdout(), p.Results, opts)
}

// flattenItemUsername copies beforeSnapshot.username to a top-level
// "username" for table/CSV, which cannot address nested fields.
func flattenItemUsername(rows []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, len(rows))
	for i, r := range rows {
		out[i] = r
		var m map[string]json.RawMessage
		if json.Unmarshal(r, &m) != nil {
			continue
		}
		var snap struct {
			Username string `json:"username"`
		}
		if s, ok := m["beforeSnapshot"]; ok {
			_ = json.Unmarshal(s, &snap)
		}
		u, _ := json.Marshal(snap.Username)
		m["username"] = u
		if b, err := json.Marshal(m); err == nil {
			out[i] = b
		}
	}
	return out
}

func accessCertGetOne(cmd *cobra.Command, endpoint string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	raw, err := client.Get(cmd.Context(), endpoint)
	if err != nil {
		return err
	}
	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}

func accessCertIDArg(kind string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		if !accesscert.IsObjectID(args[0]) {
			return accesscert.ErrNotObjectID(kind, args[0])
		}
		return nil
	}
}

// newAccessCertCmd builds the read-only `jc access-cert` group. The write
// operations of the Access Certification API are deliberately not exposed.
func newAccessCertCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "access-cert",
		Aliases: []string{"access-certification"},
		Short:   "Read Access Certification settings, campaigns, runs and audit reports",
		Long: `Read Access Certification state: settings, campaign templates, campaign runs,
review items, the admin queue and audit-report metadata.

READ-ONLY. Every command here only issues GET requests; creating, changing or
deleting campaigns, activating, extending or ending runs, decisions,
reassignment and audit-report sign-off are not available.

Ids are 24-character hex object ids; there is no lookup by name.`,
	}

	settings := &cobra.Command{Use: "settings", Short: "Access Certification settings"}
	settings.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Get the Access Certification settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return accessCertGetOne(cmd, accesscert.SettingsEndpoint)
		},
	})
	root.AddCommand(settings)

	campaigns := &cobra.Command{Use: "campaigns", Short: "Campaign templates"}
	var cf accessCertListFlags
	campaignsList := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List campaign templates",
		Long: `List campaign templates (flat summary rows), paging through all of them
unless --limit is given. The single-template shape from 'campaigns get' differs.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := cf.query()
			if err != nil {
				return err
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			p, err := accesscert.ListAll(cmd.Context(), client, accesscert.CampaignsEndpoint, q)
			if err != nil {
				return err
			}
			return writeAccessCertPage(cmd, p, accessCertCampaignFields, cf.metadata)
		},
	}
	cf.add(campaignsList, "campaigns")
	campaigns.AddCommand(campaignsList)
	campaigns.AddCommand(&cobra.Command{
		Use:   "get <campaignId>",
		Short: "Get one campaign template",
		Args:  accessCertIDArg("campaign"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return accessCertGetOne(cmd, accesscert.CampaignEndpoint(args[0]))
		},
	})
	root.AddCommand(campaigns)

	runs := &cobra.Command{Use: "runs", Short: "Campaign runs, review items and audit reports"}
	var rf accessCertListFlags
	var status string
	runsList := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List campaign runs",
		Long: `List campaign runs, paging through all of them unless --limit is given.

The API returns ACTIVE runs only unless asked otherwise. Closed runs need
--status closed or --status all. --status all makes one query per status
(active, closed, canceled) and concatenates the results.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := accesscert.StatusFilters(status); err != nil {
				return err
			}
			q, err := rf.query()
			if err != nil {
				return err
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			p, err := accesscert.ListRuns(cmd.Context(), client, status, q)
			if err != nil {
				return err
			}
			return writeAccessCertPage(cmd, p, accessCertRunFields, rf.metadata)
		},
	}
	rf.add(runsList, "runs")
	runsList.Flags().StringVar(&status, "status", "active", "Run status: active|closed|canceled|all (closed runs need closed or all)")
	runs.AddCommand(runsList)

	runs.AddCommand(&cobra.Command{
		Use:   "get <runId>",
		Short: "Get one campaign run",
		Args:  accessCertIDArg("campaign run"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return accessCertGetOne(cmd, accesscert.RunEndpoint(args[0]))
		},
	})

	var itf accessCertListFlags
	items := &cobra.Command{
		Use:   "items <runId>",
		Short: "List the review items of a campaign run",
		Args:  accessCertIDArg("campaign run"),
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := itf.query()
			if err != nil {
				return err
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			p, err := accesscert.ListAll(cmd.Context(), client, accesscert.ReviewItemsEndpoint(args[0]), q)
			if err != nil {
				return err
			}
			if f := output.CurrentOptions().Format; f == output.FormatTable || f == output.FormatCSV {
				p.Results = flattenItemUsername(p.Results)
			}
			return writeAccessCertPage(cmd, p, accessCertItemFields, false)
		},
	}
	itf.add(items, "")
	runs.AddCommand(items)

	var noURLs bool
	auditReport := &cobra.Command{
		Use:   "audit-report <runId>",
		Short: "Get the audit-report metadata of a closed campaign run",
		Long: `Get the audit report of a CLOSED campaign run. An active run returns the
server's 409 "audit report is only available for closed campaign runs".

The artifacts (csv, json, pdf) carry pre-signed download URLs valid for about
5 minutes. They are bearer-style: treat them like credentials. --no-urls
removes them and keeps each artifact's expiresAt.`,
		Args: accessCertIDArg("campaign run"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), accesscert.AuditReportEndpoint(args[0]))
			if err != nil {
				return err
			}
			if noURLs {
				if raw, err = accesscert.StripURLs(raw); err != nil {
					return err
				}
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	auditReport.Flags().BoolVar(&noURLs, "no-urls", false, "Strip the pre-signed download URLs from the output")
	runs.AddCommand(auditReport)
	root.AddCommand(runs)

	var qf accessCertListFlags
	queue := &cobra.Command{
		Use:   "admin-queue",
		Short: "List the admin review queue",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := qf.query()
			if err != nil {
				return err
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			p, err := accesscert.ListAll(cmd.Context(), client, accesscert.AdminQueueEndpoint, q)
			if err != nil {
				return err
			}
			return writeAccessCertPage(cmd, p, accessCertQueueFields, false)
		},
	}
	qf.add(queue, "")
	root.AddCommand(queue)

	return root
}
