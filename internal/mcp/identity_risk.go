package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/identityrisk"
)

// Identity Risk tools. Ten, matching the CLI one for one.
//
// Nine read. The tenth resolves a detection and CANNOT BE UNDONE — the API
// refuses every later change to the same detection, including reopening it —
// so it is plan-by-default like every other write tool, and its description
// says the irreversibility out loud rather than leaving a model to infer it
// from the word "resolve".

// riskWindowInput is shared by every tool that takes a time window. Which
// tools require one is not obvious and not documented: the aggregates do, the
// two record reads do not.
type riskWindowInput struct {
	StartTime string `json:"start_time,omitempty" jsonschema:"Window start as RFC3339, e.g. 2026-09-01T00:00:00Z. Required for the aggregate tools (stats, identities_list, login_types, geolocations, timeline) and optional for the rest."`
	EndTime   string `json:"end_time,omitempty" jsonschema:"Window end as RFC3339. Defaults to now when start_time is given."`
}

// values turns the window into query parameters, defaulting the end to now.
// required mirrors identityrisk.RequiresWindow so the failure names the
// arguments this tool actually takes rather than the server's field names.
func (w riskWindowInput) values(required bool) (url.Values, error) {
	v := url.Values{}
	if w.StartTime == "" {
		if required {
			return nil, fmt.Errorf("start_time is required for this tool: the API rejects it without a time window")
		}
		return v, nil
	}
	if _, err := time.Parse(time.RFC3339, w.StartTime); err != nil {
		return nil, fmt.Errorf("start_time %q is not RFC3339: %w", w.StartTime, err)
	}
	v.Set("start_time", w.StartTime)
	end := w.EndTime
	if end == "" {
		end = time.Now().UTC().Format(time.RFC3339)
	} else if _, err := time.Parse(time.RFC3339, end); err != nil {
		return nil, fmt.Errorf("end_time %q is not RFC3339: %w", end, err)
	}
	v.Set("end_time", end)
	return v, nil
}

type riskEventsListInput struct {
	riskWindowInput
	Level  string `json:"level,omitempty" jsonschema:"Only this risk level: low, medium, high or critical"`
	Search string `json:"search,omitempty" jsonschema:"Free-text search across detections"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum rows to return. The count in the response is the server's total match count, which is larger than the rows shown when this is set."`
	Skip   int    `json:"skip,omitempty" jsonschema:"Rows to skip, for paging"`
}

type riskEventGetInput struct {
	ObjectID string `json:"object_id" jsonschema:"The detection's JumpCloud 24-character object id, taken from the events listing. There is no lookup by name."`
}

type riskIdentityInput struct {
	riskWindowInput
	ObjectID string `json:"object_id" jsonschema:"The identity's JumpCloud 24-character object id, taken from a detection's identityObjectId or from the identities listing."`
}

type riskEventResolveInput struct {
	ObjectID string `json:"object_id" jsonschema:"The detection's JumpCloud 24-character object id"`
	State    string `json:"state" jsonschema:"Whether the access was legitimate: safe or unsafe. Required — the API rejects a resolution without it."`
	Status   string `json:"status,omitempty" jsonschema:"How it is being closed: resolved (default), dismissed or mfa-resolved"`
	Notes    string `json:"notes,omitempty" jsonschema:"Why. This is the only record of the judgement made, and it cannot be edited afterwards."`
	Execute  bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan. THIS CANNOT BE UNDONE once applied."`
}

type riskTimelineInput struct {
	riskWindowInput
	RiskFactorTypes []string `json:"risk_factor_types,omitempty" jsonschema:"Restrict to these RISK_FACTOR_TYPE_* values. Omit for all of them."`
}

func (s *Server) registerIdentityRiskTools() {
	addTypedTool(s, "identity_risk_events_list", "Risk detections raised against identities in this organization — the anomalous logins and access patterns JumpCloud has flagged. Each row carries the risk level and score, which identity and application it concerns, where the access came from, and whether it has been resolved. A time window is OPTIONAL here, unlike the aggregate tools. The count in the response is the server's total match count, so when limit is set it is larger than the number of rows returned; read the count, not the length of the list.",
		func(ctx context.Context, req *mcp.CallToolRequest, args riskEventsListInput) (*mcp.CallToolResult, any, error) {
			v, err := args.values(identityrisk.RequiresWindow(identityrisk.EventsEndpoint))
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if args.Level != "" {
				full, err := identityrisk.ExpandLevel(args.Level)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				v.Add("filter", "level:eq:"+full)
			}
			if args.Search != "" {
				v.Set("searchTerm", args.Search)
			}
			if args.Limit > 0 {
				v.Set("limit", strconv.Itoa(args.Limit))
			}
			if args.Skip > 0 {
				v.Set("skip", strconv.Itoa(args.Skip))
			}
			raw, err := riskGetMCP(ctx, identityrisk.EventsEndpoint, v)
			if err != nil {
				return errorResult(fmt.Sprintf("listing risk detections: %v", err)), nil, nil
			}
			rows, total, perr := identityrisk.ParseEvents(raw)
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "identity_risk_event_get", "One risk detection in full, by its object id, including the raw Directory Insights event that produced it — the source address, the application, the MFA outcome and the geolocation. Use this to understand WHY a detection fired before deciding what to do about it. Take the id from identity_risk_events_list; an id of the wrong shape is rejected locally so a typo reads as a typo.",
		func(ctx context.Context, req *mcp.CallToolRequest, args riskEventGetInput) (*mcp.CallToolResult, any, error) {
			if !identityrisk.IsObjectID(args.ObjectID) {
				return errorResult(identityrisk.ErrNotObjectID("detection", args.ObjectID).Error()), nil, nil
			}
			raw, err := riskGetMCP(ctx, identityrisk.EventEndpoint(args.ObjectID), nil)
			if err != nil {
				return errorResult(fmt.Sprintf("getting the risk detection: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "identity_risk_event_resolve", "Close a risk detection, recording whether the access turned out to be legitimate. THIS CANNOT BE UNDONE: once a detection is resolved the API refuses every further change to it, including reopening it, so this is closer to a delete than to an edit. Returns a plan unless execute is true. The notes are the only record of the judgement made and cannot be edited afterwards, so say why. Read the detection with identity_risk_event_get first — resolving one without looking at it destroys the signal it carries.",
		func(ctx context.Context, req *mcp.CallToolRequest, args riskEventResolveInput) (*mcp.CallToolResult, any, error) {
			if !identityrisk.IsObjectID(args.ObjectID) {
				return errorResult(identityrisk.ErrNotObjectID("detection", args.ObjectID).Error()), nil, nil
			}
			status := args.Status
			if status == "" {
				status = "resolved"
			}
			body, err := identityrisk.BuildResolve(status, args.State, args.Notes)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			if !args.Execute {
				effects := map[string]any{
					"status":  body.ResolutionStatus,
					"state":   body.ResolutionState,
					"warning": identityrisk.IrreversibleWarning,
				}
				if body.ResolutionNotes != "" {
					effects["notes"] = body.ResolutionNotes
				}
				return planResult("resolve", "identity-risk detection", args.ObjectID, args.ObjectID, effects)
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Patch(ctx, identityrisk.EventResolveEndpoint(args.ObjectID), body)
			if err != nil {
				if identityrisk.ErrAlreadyResolved(err) {
					return errorResult(fmt.Sprintf("detection %s is already resolved, and the API "+
						"allows no further change to it — including reopening", args.ObjectID)), nil, nil
				}
				return errorResult(fmt.Sprintf("resolving the risk detection: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "identity_risk_identities_list", "The identities carrying risk detections in a time window, with a count each — the answer to \"who is being flagged\". REQUIRES start_time: the API rejects this without a window. The count in the response is the organization's total number of at-risk identities.",
		func(ctx context.Context, req *mcp.CallToolRequest, args riskWindowInput) (*mcp.CallToolResult, any, error) {
			v, err := args.values(identityrisk.RequiresWindow(identityrisk.IdentitiesEndpoint))
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := riskGetMCP(ctx, identityrisk.IdentitiesEndpoint, v)
			if err != nil {
				return errorResult(fmt.Sprintf("listing at-risk identities: %v", err)), nil, nil
			}
			rows, total, perr := identityrisk.ParseIdentities(raw)
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "identity_risk_identity_get", "One identity's behavioural profile: its typical login hours, days, locations, browsers and devices, plus the aggregated data the risk scoring is derived from. This is what a detection is measured AGAINST, so it is the context for judging whether a flagged login was really unusual. A time window is optional here, unlike the identities listing.",
		func(ctx context.Context, req *mcp.CallToolRequest, args riskIdentityInput) (*mcp.CallToolResult, any, error) {
			if !identityrisk.IsObjectID(args.ObjectID) {
				return errorResult(identityrisk.ErrNotObjectID("identity", args.ObjectID).Error()), nil, nil
			}
			v, err := args.values(identityrisk.RequiresWindow(identityrisk.IdentityEndpoint(args.ObjectID)))
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := riskGetMCP(ctx, identityrisk.IdentityEndpoint(args.ObjectID), v)
			if err != nil {
				return errorResult(fmt.Sprintf("getting the identity risk profile: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "identity_risk_identity_trend", "How one identity's detection count compares with the previous equivalent period — whether this identity is getting riskier or settling down. REQUIRES start_time.",
		func(ctx context.Context, req *mcp.CallToolRequest, args riskIdentityInput) (*mcp.CallToolResult, any, error) {
			if !identityrisk.IsObjectID(args.ObjectID) {
				return errorResult(identityrisk.ErrNotObjectID("identity", args.ObjectID).Error()), nil, nil
			}
			v, err := args.values(true)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := riskGetMCP(ctx, identityrisk.IdentityTrendEndpoint(args.ObjectID), v)
			if err != nil {
				return errorResult(fmt.Sprintf("getting the identity risk trend: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "identity_risk_stats", "Organization-wide identity risk counters for a time window: open and resolved detections, how many were auto-resolved by MFA, critical-risk and privileged-identity totals — each alongside the same figure for the previous period, so the direction of travel is visible. Start here when asked how an organization's identity risk looks. REQUIRES start_time.",
		func(ctx context.Context, req *mcp.CallToolRequest, args riskWindowInput) (*mcp.CallToolResult, any, error) {
			v, err := args.values(identityrisk.RequiresWindow(identityrisk.StatsEndpoint))
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := riskGetMCP(ctx, identityrisk.StatsEndpoint, v)
			if err != nil {
				return errorResult(fmt.Sprintf("getting identity risk stats: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "identity_risk_login_types", "Which kinds of resource the risky logins were aimed at — applications, the console, directory services — with a count each. Answers whether the risk is concentrated on one class of target. REQUIRES start_time.",
		func(ctx context.Context, req *mcp.CallToolRequest, args riskWindowInput) (*mcp.CallToolResult, any, error) {
			v, err := args.values(identityrisk.RequiresWindow(identityrisk.LoginTypesEndpoint))
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := riskGetMCP(ctx, identityrisk.LoginTypesEndpoint, v)
			if err != nil {
				return errorResult(fmt.Sprintf("getting risk login types: %v", err)), nil, nil
			}
			rows, perr := identityrisk.ParseLoginTypes(raw)
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			total := len(rows)
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "identity_risk_geolocations", "Where the risky access came from, by country and city, with a count each. Use it to tell habitual travel apart from access originating somewhere the organization has no presence. REQUIRES start_time.",
		func(ctx context.Context, req *mcp.CallToolRequest, args riskWindowInput) (*mcp.CallToolResult, any, error) {
			v, err := args.values(identityrisk.RequiresWindow(identityrisk.GeolocationsEndpoint))
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := riskGetMCP(ctx, identityrisk.GeolocationsEndpoint, v)
			if err != nil {
				return errorResult(fmt.Sprintf("getting risk access geolocations: %v", err)), nil, nil
			}
			rows, perr := identityrisk.ParseGeolocations(raw)
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			total := len(rows)
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "identity_risk_timeline", "Detection counts bucketed over a time window, one series per risk-factor type — dormant-account logins, impossible travel and the rest — so a spike can be attributed to a cause. The API returns a map keyed by factor type; it is flattened here into one row per factor, name-sorted, with its buckets. This is a POST that reads: nothing is created and nothing changes. REQUIRES start_time.",
		func(ctx context.Context, req *mcp.CallToolRequest, args riskTimelineInput) (*mcp.CallToolResult, any, error) {
			v, err := args.values(true)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Create(ctx, identityrisk.FactorsTimelineEndpoint, identityrisk.TimelineRequest{
				StartTime:       v.Get("start_time"),
				EndTime:         v.Get("end_time"),
				RiskFactorTypes: args.RiskFactorTypes,
			})
			if err != nil {
				return errorResult(fmt.Sprintf("getting the risk factors timeline: %v", err)), nil, nil
			}
			series, perr := identityrisk.ParseTimeline(raw)
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			rows := make([]json.RawMessage, 0, len(series))
			for _, s := range series {
				b, merr := json.Marshal(map[string]any{
					"riskFactorType": s.RiskFactorType,
					"buckets":        len(s.Buckets),
					"series":         s.Buckets,
				})
				if merr != nil {
					return errorResult(fmt.Sprintf("encoding the timeline series: %v", merr)), nil, nil
				}
				rows = append(rows, b)
			}
			total := len(rows)
			return listEnvelope(rows, &total)
		},
	)
}

// riskGetMCP is the shared read path for the Identity Risk tools.
func riskGetMCP(ctx context.Context, endpoint string, v url.Values) (json.RawMessage, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return nil, err
	}
	if len(v) > 0 {
		endpoint += "?" + v.Encode()
	}
	return client.Get(ctx, endpoint)
}
