package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/featuresettings"
)

type featureSettingsListInput struct {
	Name     string `json:"name,omitempty" jsonschema:"Exact feature name, e.g. mcp (AI Gateway), user_mcp (JumpCloud MCP Server for Users), ai_admin_assistant, identity_risk_detection. Omit for all features."`
	Category string `json:"category,omitempty" jsonschema:"Exact category, e.g. ai, itdr, login_ui. Omit for all categories."`
}

func (s *Server) registerFeatureSettingsTools() {
	addTypedTool(s, "org_feature_settings_list", "The organization's feature settings, one object per feature with its settings as name/value pairs. Answers questions such as whether the AI Gateway is enabled (feature mcp, setting enabled) and how many days its sessions last (session_auto_revoke_days), or whether the user MCP server (user_mcp) is on. Only exact name/category matches are supported. Read-only: this tool cannot change a setting.",
		func(ctx context.Context, req *mcp.CallToolRequest, args featureSettingsListInput) (*mcp.CallToolResult, any, error) {
			if args.Name != "" && !featuresettings.ValidName(args.Name) {
				return errorResult(featuresettings.ErrInvalidName("name", args.Name).Error()), nil, nil
			}
			if args.Category != "" && !featuresettings.ValidName(args.Category) {
				return errorResult(featuresettings.ErrInvalidName("category", args.Category).Error()), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, featuresettings.Path(args.Name, args.Category))
			if err != nil {
				return errorResult(fmt.Sprintf("listing feature settings: %v", err)), nil, nil
			}
			features, _, err := featuresettings.Parse(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			total := len(features)
			return listEnvelope(features, &total)
		},
	)
}
