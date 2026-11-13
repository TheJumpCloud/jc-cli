package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/accesscert"
)

// Access Certification tools: the read-only half of the area. Every handler
// only issues GETs through accesscert, which has no write path.

const accessCertDefaultLimit = 100

type accessCertListInput struct {
	Limit  int      `json:"limit,omitempty" jsonschema:"Maximum rows to return (default 100). Pages of 100 are fetched until this many rows or totalCount is reached."`
	Sort   string   `json:"sort,omitempty" jsonschema:"Sort field; prefix with - for descending."`
	Filter []string `json:"filter,omitempty" jsonschema:"Filters as field:op:value, e.g. status:eq:closed."`
}

type accessCertCampaignsListInput struct {
	accessCertListInput
	Metadata bool `json:"metadata,omitempty" jsonschema:"Also return metadata with applicationIds and userGroupIds."`
}

type accessCertRunsListInput struct {
	accessCertListInput
	Status   string `json:"status,omitempty" jsonschema:"active (default), closed, canceled or all. all queries each status and concatenates."`
	Metadata bool   `json:"metadata,omitempty" jsonschema:"Also return metadata with applicationIds and userGroupIds."`
}

type accessCertCampaignInput struct {
	CampaignID string `json:"campaignId" jsonschema:"The campaign template's 24-character hex object id."`
}

type accessCertRunInput struct {
	RunID string `json:"runId" jsonschema:"The campaign run's 24-character hex object id."`
}

type accessCertItemsInput struct {
	RunID string `json:"runId" jsonschema:"The campaign run's 24-character hex object id."`
	accessCertListInput
}

func accessCertPageResult(p *accesscert.Page, metadata bool) (*mcp.CallToolResult, any, error) {
	result := map[string]any{"data": p.Results, "returned": len(p.Results), "total": p.TotalCount}
	if p.Results == nil {
		result["data"] = []json.RawMessage{}
	}
	if metadata && p.Metadata != nil {
		result["metadata"] = p.Metadata
	}
	res, err := jsonResult(result)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return res, nil, nil
}

func (in accessCertListInput) query(metadata bool) accesscert.Query {
	limit := in.Limit
	if limit <= 0 {
		limit = accessCertDefaultLimit
	}
	return accesscert.Query{Sort: in.Sort, Filters: in.Filter, Metadata: metadata, Limit: limit}
}

func accessCertGet(ctx context.Context, what, endpoint string) (json.RawMessage, *mcp.CallToolResult) {
	client, err := newV2ClientFunc()
	if err != nil {
		return nil, errorResult(fmt.Sprintf("creating API client: %v", err))
	}
	raw, err := client.Get(ctx, endpoint)
	if err != nil {
		return nil, errorResult(fmt.Sprintf("getting %s: %v", what, err))
	}
	return raw, nil
}

func (s *Server) registerAccessCertTools() {
	addTypedTool(s, "access_cert_settings_get", "Access Certification settings: featureEnabled, aiSuggestionsEnabled and platforms (type, enabled, destinations). Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, errRes := accessCertGet(ctx, "the Access Certification settings", accesscert.SettingsEndpoint)
			if errRes != nil {
				return errRes, nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "access_cert_campaigns_list", "List Access Certification campaign templates as flat summary rows (id, name, enabled, status, scheduleMode, scopeType, scopeTargetId, reviewerType, lastRunAt). The shape differs from access_cert_campaign_get. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args accessCertCampaignsListInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			p, err := accesscert.ListAll(ctx, client, accesscert.CampaignsEndpoint, args.query(args.Metadata))
			if err != nil {
				return errorResult(fmt.Sprintf("listing campaigns: %v", err)), nil, nil
			}
			return accessCertPageResult(p, args.Metadata)
		},
	)

	addTypedTool(s, "access_cert_campaign_get", "One Access Certification campaign template in full (nested config: scope, schedule, reviewerChain, remediationPolicy, notificationPreferences), by its 24-character hex object id. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args accessCertCampaignInput) (*mcp.CallToolResult, any, error) {
			if !accesscert.IsObjectID(args.CampaignID) {
				return errorResult(accesscert.ErrNotObjectID("campaign", args.CampaignID).Error()), nil, nil
			}
			raw, errRes := accessCertGet(ctx, "the campaign", accesscert.CampaignEndpoint(args.CampaignID))
			if errRes != nil {
				return errRes, nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "access_cert_runs_list", "List Access Certification campaign runs. By default (status omitted or active) this returns ACTIVE runs only, because that is all the API returns without a filter; closed runs appear only with status closed or all. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args accessCertRunsListInput) (*mcp.CallToolResult, any, error) {
			if _, err := accesscert.StatusFilters(args.Status); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			p, err := accesscert.ListRuns(ctx, client, args.Status, args.query(args.Metadata))
			if err != nil {
				return errorResult(fmt.Sprintf("listing campaign runs: %v", err)), nil, nil
			}
			return accessCertPageResult(p, args.Metadata)
		},
	)

	addTypedTool(s, "access_cert_run_get", "One Access Certification campaign run in full (scope, materializationStatus, reviewItemsMaterialized, requireJustification, allowReassign, closeReason), by its 24-character hex object id. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args accessCertRunInput) (*mcp.CallToolResult, any, error) {
			if !accesscert.IsObjectID(args.RunID) {
				return errorResult(accesscert.ErrNotObjectID("campaign run", args.RunID).Error()), nil, nil
			}
			raw, errRes := accessCertGet(ctx, "the campaign run", accesscert.RunEndpoint(args.RunID))
			if errRes != nil {
				return errRes, nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "access_cert_review_items_list", "List the review items of one Access Certification campaign run (id, identityId, finalDecision, remediationAction, reviewerStages, beforeSnapshot). Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args accessCertItemsInput) (*mcp.CallToolResult, any, error) {
			if !accesscert.IsObjectID(args.RunID) {
				return errorResult(accesscert.ErrNotObjectID("campaign run", args.RunID).Error()), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			p, err := accesscert.ListAll(ctx, client, accesscert.ReviewItemsEndpoint(args.RunID), args.query(false))
			if err != nil {
				return errorResult(fmt.Sprintf("listing review items: %v", err)), nil, nil
			}
			return accessCertPageResult(p, false)
		},
	)

	addTypedTool(s, "access_cert_audit_report_get", "Audit-report metadata of a CLOSED Access Certification campaign run: status, documentHash, generatedAt and, per artifact format (csv/json/pdf), its expiresAt. The pre-signed download URLs are deliberately NOT returned; downloads are available through the CLI (jc access-cert runs audit-report). An active run returns a 409 error. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args accessCertRunInput) (*mcp.CallToolResult, any, error) {
			if !accesscert.IsObjectID(args.RunID) {
				return errorResult(accesscert.ErrNotObjectID("campaign run", args.RunID).Error()), nil, nil
			}
			raw, errRes := accessCertGet(ctx, "the audit report", accesscert.AuditReportEndpoint(args.RunID))
			if errRes != nil {
				return errRes, nil, nil
			}
			stripped, err := accesscert.StripURLs(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			var report map[string]any
			if err := json.Unmarshal(stripped, &report); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			report["note"] = "Download URLs are withheld from MCP results; download the artifacts through the CLI: jc access-cert runs audit-report " + args.RunID
			res, err := jsonResult(report)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "access_cert_admin_queue_list", "List the Access Certification admin queue: active runs awaiting review (id = runId, campaignId, campaignName, taskType, targetType, scopeSummary, startDate, dueDate, pendingDecisionCount, reviewProgressPercent). Empty when no run is active. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args accessCertListInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			p, err := accesscert.ListAll(ctx, client, accesscert.AdminQueueEndpoint, args.query(false))
			if err != nil {
				return errorResult(fmt.Sprintf("listing the admin queue: %v", err)), nil, nil
			}
			return accessCertPageResult(p, false)
		},
	)
}
