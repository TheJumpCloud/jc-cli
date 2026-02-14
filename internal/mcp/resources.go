package mcp

import (
	"context"
	"encoding/json"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerResources adds MCP resources to the server.
// US-046 registers foundation resources; US-048 will add schema and org resources.
func (s *Server) registerResources() {
	// jc://server/info — basic server info for clients.
	s.mcpServer.AddResource(
		&mcp.Resource{
			URI:         "jc://server/info",
			Name:        "Server Info",
			Description: "JC MCP server version and configuration",
			MIMEType:    "application/json",
		},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			info := map[string]any{
				"name":       "jc",
				"version":    version.Number,
				"profile":    config.ActiveProfile(),
				"read_only":  s.readOnly,
				"rate_limit": s.limiter.maxPerMin,
			}
			data, err := json.MarshalIndent(info, "", "  ")
			if err != nil {
				return nil, err
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      req.Params.URI,
					MIMEType: "application/json",
					Text:     string(data),
				}},
			}, nil
		},
	)

	// jc://config/profiles — available profile names (no secrets).
	s.mcpServer.AddResource(
		&mcp.Resource{
			URI:         "jc://config/profiles",
			Name:        "Available Profiles",
			Description: "List of configured JumpCloud organization profiles",
			MIMEType:    "application/json",
		},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			profiles := config.ProfileNames()
			active := config.ActiveProfile()
			result := map[string]any{
				"active_profile": active,
				"profiles":       profiles,
			}
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return nil, err
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      req.Params.URI,
					MIMEType: "application/json",
					Text:     string(data),
				}},
			}, nil
		},
	)
}
