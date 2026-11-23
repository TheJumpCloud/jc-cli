package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheJumpCloud/jc-cli/internal/api"
	"github.com/TheJumpCloud/jc-cli/internal/direports"
	"github.com/TheJumpCloud/jc-cli/internal/emailsubs"
	"github.com/TheJumpCloud/jc-cli/internal/gcep"
	"github.com/TheJumpCloud/jc-cli/internal/resolve"
	"github.com/TheJumpCloud/jc-cli/internal/risksettings"
	"github.com/TheJumpCloud/jc-cli/internal/siem"
)

// Tools for the last six API areas (KLA-485): Directory Insights interval
// counts and reports, the SIEM pull API, email subscriptions, Access Risk
// settings and GCEP. Contracts live in each area's package.
//
// Report DOWNLOAD is deliberately CLI-only: a report is a file of usernames,
// emails and account state, and tool output is model context.

type insightsIntervalInput struct {
	Service  string `json:"service,omitempty" jsonschema:"Event service: sso, ldap, radius, directory, systems, all (default) — comma-separated for several"`
	Last     string `json:"last,omitempty" jsonschema:"Time range ending now, e.g. 24h, 7d (default 7d)"`
	Interval string `json:"interval,omitempty" jsonschema:"Bucket size: a number and m, h, d or w (default 1d)"`
	Timezone string `json:"timezone,omitempty" jsonschema:"Bucket boundaries in this UTC offset, e.g. -0500"`
}

type insightsReportRequestInput struct {
	Type    string `json:"type" jsonschema:"Report type: browser-patch-policy, os-patch-policy, os-version, software-inventory, user-account-health, users-to-devices, users-to-directories, users-to-ldap-servers, users-to-radius-servers, users-to-sso-applications, users-to-user-groups"`
	Execute bool   `json:"execute,omitempty" jsonschema:"Set to true to request it. Without this the tool returns a plan."`
}

type siemEventsInput struct {
	Since    string `json:"since,omitempty" jsonschema:"Window start, RFC 3339. Omit for the hour ending hours_ago hours ago."`
	Until    string `json:"until,omitempty" jsonschema:"Window end, RFC 3339, at most one hour after since"`
	HoursAgo int    `json:"hours_ago,omitempty" jsonschema:"Without since: pull the hour ending this many hours ago (default 3; data lags about two hours)"`
	Types    string `json:"types,omitempty" jsonschema:"all (default) or up to 25 comma-separated event types, e.g. user_login_attempt"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Events per page, at most 1000 (default 100 here)"`
	Cursor   string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous result. Pass that result's since and until with it: a cursor belongs to one window"`
}

type emailSubscriptionSetInput struct {
	EmailKey   string `json:"email_key" jsonschema:"The subscription's emailKey, from email_subscriptions_list"`
	Subscribed bool   `json:"subscribed" jsonschema:"true to receive the email, false to stop"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type riskSettingSetInput struct {
	Name    string `json:"name" jsonschema:"Setting name, from identity_risk_settings_list (e.g. dormant_account_threshold_days; enabled is the master switch)"`
	Value   string `json:"value" jsonschema:"New value as text, read as the setting's current type: true/false, a whole number, text, or a list (JSON array or comma-separated)"`
	Execute bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

// The step-up prompt names what each write changes.
func (a insightsReportRequestInput) stepUpTarget() string { return "report " + a.Type }
func (a emailSubscriptionSetInput) stepUpTarget() string {
	return fmt.Sprintf("%s subscribed=%v", a.EmailKey, a.Subscribed)
}
func (a riskSettingSetInput) stepUpTarget() string { return a.Name + "=" + a.Value }

type gcepDeviceInput struct {
	Device string `json:"device" jsonschema:"Device hostname, display name or 24-character id"`
}

func insightsRawMCP(ctx context.Context, method, path string, body any) ([]byte, error) {
	c, err := newInsightsClientFunc()
	if err != nil {
		return nil, err
	}
	return c.Raw(ctx, method, c.BaseURL, path, body)
}

func dataExportRawMCP(ctx context.Context, method, path string, body any) ([]byte, error) {
	c, err := newInsightsClientFunc()
	if err != nil {
		return nil, err
	}
	base, err := api.DataExportBaseURL(c.BaseURL)
	if err != nil {
		return nil, err
	}
	return c.Raw(ctx, method, base, path, body)
}

func readRiskSettingsMCP(ctx context.Context) ([]risksettings.Setting, error) {
	c, err := newV2ClientFunc()
	if err != nil {
		return nil, err
	}
	raw, err := c.Get(ctx, risksettings.Path)
	if err != nil {
		return nil, err
	}
	return risksettings.Parse(raw)
}

func jsonOrError(v any) (*mcp.CallToolResult, any, error) {
	res, err := jsonResult(v)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return res, nil, nil
}

func (s *Server) registerAPIGapAreaTools() {
	addTypedTool(s, "insights_event_histogram", "Directory Insights event counts per time bucket — an activity histogram (e.g. logins per day over the last week). Each row is a bucket start and its event count. For the events themselves use the insights query tools.",
		func(ctx context.Context, req *mcp.CallToolRequest, args insightsIntervalInput) (*mcp.CallToolResult, any, error) {
			service, last, interval := args.Service, args.Last, args.Interval
			if service == "" {
				service = "all"
			}
			if last == "" {
				last = "7d"
			}
			if interval == "" {
				interval = "1d"
			}
			t, err := api.ParseTimeRange(last)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body, err := api.IntervalBody(service, t.UTC().Format(time.RFC3339), "", interval, args.Timezone)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			c, err := newInsightsClientFunc()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			rows, err := c.IntervalEvents(ctx, body)
			if err != nil {
				return errorResult(fmt.Sprintf("querying event counts: %v", err)), nil, nil
			}
			return listEnvelope(rows, nil)
		},
	)

	addTypedTool(s, "insights_reports_list", "List the Directory Insights reports requested on this org (user-to-group, users-to-devices, software inventory, ...) with their status: PENDING, COMPLETED or FAILED. Reports expire after 7 days. Their content is downloaded with the CLI, not here: it is a file of personal data.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := insightsRawMCP(ctx, http.MethodGet, direports.ListPath, nil)
			if err != nil {
				return errorResult(fmt.Sprintf("listing reports: %v", err)), nil, nil
			}
			rows, err := direports.ParseList(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return listEnvelope(rows, nil)
		},
	)

	addTypedTool(s, "insights_report_request", "Request a Directory Insights report, generated in the background within about a minute (it can also fail). Poll insights_reports_list for its status. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args insightsReportRequestInput) (*mcp.CallToolResult, any, error) {
			if res := s.refuseReadOnly("insights_report_request", args.Execute); res != nil {
				return res, nil, nil
			}
			path, err := direports.RequestPath(args.Type)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("request", "Directory Insights report", args.Type, args.Type,
					map[string]any{"kept_for": "7 days"})
			}
			raw, err := insightsRawMCP(ctx, http.MethodPost, path, map[string]any{})
			if err != nil {
				return errorResult(fmt.Sprintf("requesting report: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "siem_events", "Pull one page of events from the SIEM export API — the feed a SIEM polls. One window of at most an hour per call; data lags real time by about two hours, so by default the hour ending three hours ago. Returns events, the since/until window it covered and, when more remain, next_cursor — pass all three back for the next page.",
		func(ctx context.Context, req *mcp.CallToolRequest, args siemEventsInput) (*mcp.CallToolResult, any, error) {
			hours := args.HoursAgo
			if hours == 0 {
				hours = 3
			}
			types, limit := args.Types, args.Limit
			if types == "" {
				types = "all"
			}
			if limit == 0 {
				limit = 100
			}
			if args.Cursor != "" && args.Since == "" {
				return errorResult("a cursor continues one window: pass the since and until returned with it"), nil, nil
			}
			q, err := siem.NewQuery(args.Since, args.Until, time.Duration(hours)*time.Hour, types, limit, args.Cursor)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			path, err := q.Path()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := dataExportRawMCP(ctx, http.MethodGet, path, nil)
			if err != nil {
				var apiErr *api.APIError
				if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnprocessableEntity {
					return errorResult(fmt.Sprintf("pulling events: %v — %s", err, siem.LagHint)), nil, nil
				}
				return errorResult(fmt.Sprintf("pulling events: %v", err)), nil, nil
			}
			page, err := siem.ParsePage(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if page.Events == nil {
				page.Events = []json.RawMessage{}
			}
			return jsonOrError(map[string]any{"events": page.Events, "count": len(page.Events), "next_cursor": page.NextCursor,
				"since": q.Since.UTC().Format(time.RFC3339), "until": q.Until.UTC().Format(time.RFC3339)})
		},
	)

	addTypedTool(s, "siem_connection_test", "Check that the SIEM export API is reachable for this organization (the first-time validation a SIEM runs). Returns ok and the org id; no events.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := dataExportRawMCP(ctx, http.MethodPost, siem.ConnectionTestPath, map[string]any{})
			if err != nil {
				return errorResult(fmt.Sprintf("testing the SIEM connection: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "email_subscriptions_list", "The notification emails JumpCloud sends to the calling admin (e.g. user account locked, new device online) and whether each is on. These are the caller's own inbox preferences, not org-wide settings.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			c, err := newV2ClientFunc()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := c.Get(ctx, emailsubs.Path)
			if err != nil {
				return errorResult(fmt.Sprintf("reading email subscriptions: %v", err)), nil, nil
			}
			rows, _, err := emailsubs.Parse(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return listEnvelope(rows, nil)
		},
	)

	addTypedTool(s, "email_subscription_set", "Turn one of the calling admin's own notification emails on or off. Only that key changes; the change is read back. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args emailSubscriptionSetInput) (*mcp.CallToolResult, any, error) {
			if res := s.refuseReadOnly("email_subscription_set", args.Execute); res != nil {
				return res, nil, nil
			}
			if strings.TrimSpace(args.EmailKey) == "" {
				return errorResult("email_key is required"), nil, nil
			}
			if !args.Execute {
				return planResult("set", "email subscription", args.EmailKey, args.EmailKey,
					map[string]any{"subscribed": args.Subscribed, "scope": "your own inbox only"})
			}
			c, err := newV2ClientFunc()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if _, err := c.Update(ctx, emailsubs.Path, emailsubs.Body(args.EmailKey, args.Subscribed)); err != nil {
				return errorResult(fmt.Sprintf("updating email subscription: %v", err)), nil, nil
			}
			raw, err := c.Get(ctx, emailsubs.Path)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			_, subs, err := emailsubs.Parse(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			got, err := emailsubs.State(subs, args.EmailKey)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if got != args.Subscribed {
				return errorResult(fmt.Sprintf("the API accepted the change, but %s still reads subscribed=%v", args.EmailKey, got)), nil, nil
			}
			return jsonOrError(map[string]any{"emailKey": args.EmailKey, "subscribed": got, "confirmed": true})
		},
	)

	addTypedTool(s, "identity_risk_settings_list", "The Access Risk (identity risk detection) settings: which detectors run (impossible travel, new device, dormant account, unusual geolocation, ...), their thresholds, trusted IP and ASN lists, notification, and enabled — the master switch for the feature.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			settings, err := readRiskSettingsMCP(ctx)
			if err != nil {
				return errorResult(fmt.Sprintf("reading access risk settings: %v", err)), nil, nil
			}
			rows := make([]json.RawMessage, len(settings))
			for i, st := range settings {
				rows[i], _ = json.Marshal(st)
			}
			return listEnvelope(rows, nil)
		},
	)

	addTypedTool(s, "identity_risk_setting_set", "Change one Access Risk setting. The value is converted to the setting's current type first (the API rejects a wrong type) and read back afterwards. Setting enabled to false turns identity risk detection off for the whole organization. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args riskSettingSetInput) (*mcp.CallToolResult, any, error) {
			if res := s.refuseReadOnly("identity_risk_setting_set", args.Execute); res != nil {
				return res, nil, nil
			}
			settings, err := readRiskSettingsMCP(ctx)
			if err != nil {
				return errorResult(fmt.Sprintf("reading access risk settings: %v", err)), nil, nil
			}
			cur, err := risksettings.Find(settings, args.Name)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			val, err := risksettings.Coerce(cur.Value, args.Value)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("set", "access risk setting", args.Name, args.Name,
					map[string]any{"from": cur.Value, "to": val})
			}
			c, err := newV2ClientFunc()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if _, err := c.Update(ctx, risksettings.SettingPath(args.Name), map[string]any{"value": val}); err != nil {
				return errorResult(fmt.Sprintf("updating %s: %v", args.Name, err)), nil, nil
			}
			after, err := readRiskSettingsMCP(ctx)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			got, err := risksettings.Find(after, args.Name)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !risksettings.Equal(got.Value, val) {
				return errorResult(fmt.Sprintf("the API accepted the change, but %s reads back as %v", args.Name, got.Value)), nil, nil
			}
			return jsonOrError(map[string]any{"name": args.Name, "value": got.Value, "confirmed": true})
		},
	)

	addTypedTool(s, "gcep_configuration", "The Google Chrome Enterprise Premium (GCEP) device trust integration's configuration. Reports when the integration is not configured on this organization.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return gcepReadMCP(ctx, gcep.ConfigurationPath, gcep.NotConfigured())
		},
	)

	addTypedTool(s, "gcep_device_state", "One device's trust state in the Google Chrome Enterprise Premium (GCEP) device trust integration.",
		func(ctx context.Context, req *mcp.CallToolRequest, args gcepDeviceInput) (*mcp.CallToolResult, any, error) {
			v1, err := newV1ClientFunc()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			id, err := resolve.NewResolver(v1).Resolve(ctx, args.Device, resolve.DeviceConfig)
			if err != nil {
				return errorResult(fmt.Sprintf("resolving device: %v", err)), nil, nil
			}
			return gcepReadMCP(ctx, gcep.DeviceStatePath(id), gcep.NoDeviceState(args.Device))
		},
	)
}

func gcepReadMCP(ctx context.Context, path string, notFound error) (*mcp.CallToolResult, any, error) {
	c, err := newV2ClientFunc()
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	raw, err := c.Get(ctx, path)
	if err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return errorResult(notFound.Error()), nil, nil
		}
		return errorResult(err.Error()), nil, nil
	}
	return textResult(string(raw)), nil, nil
}
