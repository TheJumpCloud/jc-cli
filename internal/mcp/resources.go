package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/schema"
	"github.com/klaassen-consulting/jc/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// jsonResource is a helper to create a JSON MCP resource result.
func jsonResource(uri string, v any) (*mcp.ReadResourceResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: "application/json",
			Text:     string(data),
		}},
	}, nil
}

// registerResources adds MCP resources to the server.
func (s *Server) registerResources() {
	s.registerFoundationResources()
	s.registerSchemaResources()
	s.registerRecipeResources()
	s.registerAppResources()
}

// registerFoundationResources adds server info and config profile resources.
func (s *Server) registerFoundationResources() {
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
			return jsonResource(req.Params.URI, info)
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
			result := map[string]any{
				"active_profile": config.ActiveProfile(),
				"profiles":       config.ProfileNames(),
			}
			return jsonResource(req.Params.URI, result)
		},
	)
}

// registerSchemaResources adds resource schema and command manifest resources.
func (s *Server) registerSchemaResources() {
	// jc://schema/resources — list of all resource types.
	s.mcpServer.AddResource(
		&mcp.Resource{
			URI:         "jc://schema/resources",
			Name:        "Resource Types",
			Description: "List of all JumpCloud resource types with API version, verbs, and default fields",
			MIMEType:    "application/json",
		},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return jsonResource(req.Params.URI, schema.AllResources())
		},
	)

	// jc://schema/{resource} — individual resource schema.
	for _, name := range schema.ResourceNames() {
		name := name
		rs := schema.Resources[name]
		s.mcpServer.AddResource(
			&mcp.Resource{
				URI:         fmt.Sprintf("jc://schema/%s", name),
				Name:        fmt.Sprintf("%s Schema", strings.ToUpper(name[:1])+name[1:]),
				Description: fmt.Sprintf("Field schema for JumpCloud %s resource", name),
				MIMEType:    "application/json",
			},
			func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return jsonResource(req.Params.URI, rs)
			},
		)
	}

	// jc://schema/commands — full CLI command manifest.
	s.mcpServer.AddResource(
		&mcp.Resource{
			URI:         "jc://schema/commands",
			Name:        "Command Manifest",
			Description: "Full CLI command manifest with all commands, subcommands, flags, and descriptions",
			MIMEType:    "application/json",
		},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			manifest := schema.BuildCommandManifest()
			return jsonResource(req.Params.URI, manifest)
		},
	)
}

// registerRecipeResources adds recipe list and individual recipe resources.
func (s *Server) registerRecipeResources() {
	// jc://recipes/list — available recipes with descriptions and parameters.
	s.mcpServer.AddResource(
		&mcp.Resource{
			URI:         "jc://recipes/list",
			Name:        "Available Recipes",
			Description: "List of available recipes (built-in and user-defined) with descriptions and parameters",
			MIMEType:    "application/json",
		},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			recipes, err := recipe.LoadAll()
			if err != nil {
				return nil, fmt.Errorf("loading recipes: %w", err)
			}
			summaries := make([]map[string]any, 0, len(recipes))
			for _, r := range recipes {
				params := make([]map[string]string, 0, len(r.Parameters))
				for _, p := range r.Parameters {
					pm := map[string]string{
						"name":        p.Name,
						"description": p.Description,
						"type":        p.Type,
					}
					if p.Required {
						pm["required"] = "true"
					}
					if p.Default != "" {
						pm["default"] = p.Default
					}
					params = append(params, pm)
				}
				summaries = append(summaries, map[string]any{
					"name":        r.Name,
					"description": r.Description,
					"version":     r.Version,
					"tags":        r.Tags,
					"parameters":  params,
					"steps":       len(r.Steps),
				})
			}
			return jsonResource(req.Params.URI, summaries)
		},
	)

	// jc://recipes/{name} — individual recipe YAML definition.
	// Load recipes at registration time to get the list of names for static resources.
	// The handler re-loads at read time so updates are reflected.
	builtinRecipes, _ := recipe.LoadBuiltIn()
	for _, r := range builtinRecipes {
		recipeName := r.Name
		s.mcpServer.AddResource(
			&mcp.Resource{
				URI:         fmt.Sprintf("jc://recipes/%s", recipeName),
				Name:        fmt.Sprintf("Recipe: %s", recipeName),
				Description: r.Description,
				MIMEType:    "application/json",
			},
			func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				recipes, err := recipe.LoadAll()
				if err != nil {
					return nil, fmt.Errorf("loading recipes: %w", err)
				}
				found := recipe.FindByName(recipes, recipeName)
				if found == nil {
					return nil, fmt.Errorf("recipe %q not found", recipeName)
				}
				return jsonResource(req.Params.URI, found)
			},
		)
	}
}
