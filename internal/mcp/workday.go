package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/workday"
)

// Workday import tools. Two, not four — the workers listing and the import
// results endpoint have never been observed with data and are not implemented
// on a guess. See internal/workday.Unprobed.

type workdayGetInput struct {
	ID string `json:"id" jsonschema:"The integration's JumpCloud 24-character object id. There is no lookup by name: the record has never been observed with data in it, so which field holds a display name is not established."`
}

func (s *Server) registerWorkdayTools() {
	addTypedTool(s, "workday_integrations_list", "Every Workday HR-import integration this organization has configured, one row each. Workday import is how employee records flow from HR into the directory, so an org with none has no HR-driven provisioning at all — and that is the common case. The response is a BARE JSON ARRAY rather than the envelope the rest of this API uses, so there is no count field to read; an org with no integration returns an empty array.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, workday.Endpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing Workday integrations: %v", err)), nil, nil
			}
			rows, perr := workday.ParseList(raw)
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			total := len(rows)
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "workday_integration_get", "One Workday HR-import integration in full, by its JumpCloud object id. Take the id from the integration listing: this area accepts no name, and an id of the wrong shape is rejected locally so a typo reads as a typo rather than as a server error.",
		func(ctx context.Context, req *mcp.CallToolRequest, args workdayGetInput) (*mcp.CallToolResult, any, error) {
			if !workday.IsObjectID(args.ID) {
				return errorResult(workday.ErrNotObjectID(args.ID).Error()), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, workday.IntegrationEndpoint(args.ID))
			if err != nil {
				return errorResult(fmt.Sprintf("getting the Workday integration: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)
}
