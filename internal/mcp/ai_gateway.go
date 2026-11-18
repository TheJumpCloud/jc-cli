package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/aigateway"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

type aiGatewayServersListInput struct {
	App string `json:"app,omitempty" jsonschema:"Only servers linked to this SSO application (title, connector key, or ID). Omit for all servers."`
}

type aiGatewayServerGetInput struct {
	Server string `json:"server" jsonschema:"AI Gateway server name or 24-character ID"`
}

type aiGatewayResolveInput struct {
	App string `json:"app" jsonschema:"SSO application title, connector key, or ID"`
}

type aiGatewayConnectorInput struct {
	ConnectorID string `json:"connector_id" jsonschema:"Catalog connector id, e.g. salesforce (as reported by ai_gateway_catalog_resolve)"`
}

const aiGatewayPreviewNote = " PREVIEW: an undocumented JumpCloud API; responses in an unexpected shape are reported as errors."

func (s *Server) registerAIGatewayTools() {
	addTypedTool(s, "ai_gateway_servers_list", "List the MCP servers the JumpCloud AI Gateway fronts: name, prefix, target_url, the linked SSO application (application_object_id) and the auth kind. Credentials are never returned: auth_config shows only safe values and [redacted] for everything else. Read-only."+aiGatewayPreviewNote,
		func(ctx context.Context, req *mcp.CallToolRequest, args aiGatewayServersListInput) (*mcp.CallToolResult, any, error) {
			var appID string
			if args.App != "" {
				v1, err := newV1ClientFunc()
				if err != nil {
					return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
				}
				id, err := resolveV1(ctx, v1, args.App, resolve.ApplicationConfig)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				appID = id
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			servers, err := aigateway.ListServers(ctx, client)
			if err != nil {
				return errorResult(fmt.Sprintf("listing AI Gateway servers: %v", err)), nil, nil
			}
			if appID != "" {
				servers = aigateway.FilterByApp(servers, appID)
			}
			total := len(servers)
			return listEnvelope(servers, &total)
		},
	)

	addTypedTool(s, "ai_gateway_server_get", "Get one AI Gateway MCP server by name or ID. Credentials are never returned. Read-only."+aiGatewayPreviewNote,
		func(ctx context.Context, req *mcp.CallToolRequest, args aiGatewayServerGetInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			var server json.RawMessage
			if aigateway.ValidID(args.Server) {
				server, err = aigateway.GetServer(ctx, client, args.Server)
			} else {
				var all []json.RawMessage
				all, err = aigateway.ListServers(ctx, client)
				if err == nil {
					server, err = aigateway.FindServer(all, args.Server)
				}
			}
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return textResult(string(server)), nil, nil
		},
	)

	addTypedTool(s, "ai_gateway_redirect_uris_list", "List the OAuth redirect URIs the AI Gateway accepts from MCP clients (e.g. Claude, Cursor, VS Code callbacks). Read-only."+aiGatewayPreviewNote,
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			uris, err := aigateway.ListRedirectURIs(ctx, client)
			if err != nil {
				return errorResult(fmt.Sprintf("listing redirect URIs: %v", err)), nil, nil
			}
			total := len(uris)
			return listEnvelope(uris, &total)
		},
	)

	addTypedTool(s, "ai_gateway_catalog_resolve", "Whether an SSO application is linked to an AI Gateway catalog connector, and which one (connector_id). Read-only."+aiGatewayPreviewNote,
		func(ctx context.Context, req *mcp.CallToolRequest, args aiGatewayResolveInput) (*mcp.CallToolResult, any, error) {
			v1, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			appID, err := resolveV1(ctx, v1, args.App, resolve.ApplicationConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := aigateway.Resolve(ctx, client, appID)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "ai_gateway_catalog_connector_get", "Get an AI Gateway catalog connector's definition by id: display name, documentation URL, endpoint template, supported auth types and scopes. Read-only."+aiGatewayPreviewNote,
		func(ctx context.Context, req *mcp.CallToolRequest, args aiGatewayConnectorInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := aigateway.GetConnector(ctx, client, args.ConnectorID)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)
}
