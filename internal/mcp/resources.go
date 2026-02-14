package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resourceSchema describes a JumpCloud resource type for LLM consumption.
type resourceSchema struct {
	Resource      string   `json:"resource"`
	APIVersion    string   `json:"api_version"`
	Verbs         []string `json:"verbs"`
	DefaultFields []string `json:"default_fields"`
	FilterSupport bool     `json:"filter_support"`
	SortSupport   bool     `json:"sort_support"`
	IDField       string   `json:"id_field"`
	NameField     string   `json:"name_field"`
}

// schemas defines metadata for all JumpCloud resource types.
var schemas = map[string]resourceSchema{
	"users": {
		Resource:      "users",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "create", "update", "delete", "search", "lock", "unlock", "reset-mfa", "reset-password"},
		DefaultFields: []string{"username", "email", "firstname", "lastname", "activated", "suspended"},
		FilterSupport: true,
		SortSupport:   true,
		IDField:       "_id",
		NameField:     "username",
	},
	"devices": {
		Resource:      "devices",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "delete", "lock", "restart", "erase"},
		DefaultFields: []string{"displayName", "hostname", "os", "osVersion", "lastContact", "agentVersion"},
		FilterSupport: true,
		SortSupport:   true,
		IDField:       "_id",
		NameField:     "hostname",
	},
	"groups": {
		Resource:      "groups",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete", "add-member", "remove-member"},
		DefaultFields: []string{"id", "name", "description", "type"},
		FilterSupport: true,
		SortSupport:   true,
		IDField:       "id",
		NameField:     "name",
	},
	"commands": {
		Resource:      "commands",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "create", "update", "delete", "run", "results"},
		DefaultFields: []string{"name", "commandType", "command", "schedule", "scheduleRepeatType"},
		FilterSupport: true,
		SortSupport:   true,
		IDField:       "_id",
		NameField:     "name",
	},
	"policies": {
		Resource:      "policies",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "results"},
		DefaultFields: []string{"id", "name", "template", "os"},
		FilterSupport: true,
		SortSupport:   true,
		IDField:       "id",
		NameField:     "name",
	},
	"apps": {
		Resource:      "apps",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get"},
		DefaultFields: []string{"_id", "name", "displayLabel", "ssoType", "status"},
		FilterSupport: true,
		SortSupport:   true,
		IDField:       "_id",
		NameField:     "name",
	},
	"admins": {
		Resource:      "admins",
		APIVersion:    "v2",
		Verbs:         []string{"list"},
		DefaultFields: []string{"id", "email", "role", "enableMultiFactor"},
		FilterSupport: true,
		SortSupport:   true,
		IDField:       "id",
		NameField:     "email",
	},
	"insights": {
		Resource:      "insights",
		APIVersion:    "insights/v1",
		Verbs:         []string{"query", "count", "distinct"},
		DefaultFields: []string{"timestamp", "event_type", "initiated_by", "client_ip", "success"},
		FilterSupport: false,
		SortSupport:   true,
		IDField:       "",
		NameField:     "",
	},
}

// validSchemaResources returns the sorted list of valid schema resource names.
func validSchemaResources() []string {
	resources := make([]string, 0, len(schemas))
	for name := range schemas {
		resources = append(resources, name)
	}
	// Sort for deterministic output.
	for i := 0; i < len(resources); i++ {
		for j := i + 1; j < len(resources); j++ {
			if resources[i] > resources[j] {
				resources[i], resources[j] = resources[j], resources[i]
			}
		}
	}
	return resources
}

// commandManifest describes the full CLI command tree for LLM consumption.
type commandManifest struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Commands    []commandEntry    `json:"commands"`
	GlobalFlags []flagEntry       `json:"global_flags"`
	Resources   []string          `json:"resources"`
}

// commandEntry describes a CLI command with its subcommands and flags.
type commandEntry struct {
	Path        string       `json:"path"`
	Description string       `json:"description"`
	Subcommands []string     `json:"subcommands,omitempty"`
	Flags       []flagEntry  `json:"flags,omitempty"`
}

// flagEntry describes a CLI flag.
type flagEntry struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description"`
}

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
			// Return all schemas as an array, sorted by resource name.
			names := validSchemaResources()
			all := make([]resourceSchema, 0, len(names))
			for _, name := range names {
				all = append(all, schemas[name])
			}
			return jsonResource(req.Params.URI, all)
		},
	)

	// jc://schema/{resource} — individual resource schema.
	for name, schema := range schemas {
		name := name
		schema := schema
		s.mcpServer.AddResource(
			&mcp.Resource{
				URI:         fmt.Sprintf("jc://schema/%s", name),
				Name:        fmt.Sprintf("%s Schema", strings.ToUpper(name[:1])+name[1:]),
				Description: fmt.Sprintf("Field schema for JumpCloud %s resource", name),
				MIMEType:    "application/json",
			},
			func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return jsonResource(req.Params.URI, schema)
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
			manifest := buildCommandManifest()
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

// buildCommandManifest generates a machine-readable manifest of all CLI commands.
func buildCommandManifest() commandManifest {
	manifest := commandManifest{
		Name:        "jc",
		Version:     version.Number,
		Description: "JumpCloud CLI — manage users, devices, groups, policies, commands, insights, and more",
		Resources:   validSchemaResources(),
		GlobalFlags: []flagEntry{
			{Name: "output", Shorthand: "o", Type: "string", Default: "json", Description: "Output format: json, table, csv, human, yaml, ndjson"},
			{Name: "table", Shorthand: "t", Type: "bool", Description: "Shorthand for --output table"},
			{Name: "verbose", Shorthand: "v", Type: "bool", Description: "Enable verbose HTTP logging"},
			{Name: "debug", Type: "bool", Description: "Enable debug logging"},
			{Name: "quiet", Shorthand: "q", Type: "bool", Description: "Suppress output, exit code only"},
			{Name: "force", Shorthand: "f", Type: "bool", Description: "Skip confirmation prompts"},
			{Name: "plan", Type: "bool", Description: "Preview changes without executing"},
			{Name: "ids", Type: "bool", Description: "Output one ID per line (for piping)"},
			{Name: "fields", Type: "string", Description: "Comma-separated list of fields to include"},
			{Name: "exclude", Type: "string", Description: "Comma-separated list of fields to exclude"},
			{Name: "all", Type: "bool", Description: "Include all available fields in output"},
			{Name: "org", Type: "string", Description: "Override active profile for this command"},
			{Name: "api-key", Type: "string", Description: "Override API key for this command"},
			{Name: "no-cache", Type: "bool", Description: "Bypass name-to-ID cache"},
			{Name: "no-color", Type: "bool", Description: "Disable color output"},
			{Name: "non-interactive", Type: "bool", Description: "Disable all interactive prompts"},
		},
		Commands: []commandEntry{
			{
				Path:        "jc auth",
				Description: "Authentication commands",
				Subcommands: []string{"login", "logout", "status", "switch"},
			},
			{
				Path:        "jc config",
				Description: "Configuration management",
				Subcommands: []string{"view", "set"},
			},
			{
				Path:        "jc users",
				Description: "Manage JumpCloud system users",
				Subcommands: []string{"list", "get", "create", "update", "delete", "search", "lock", "unlock", "reset-mfa", "reset-password"},
				Flags: []flagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list/search)"},
					{Name: "sort", Type: "string", Description: "Sort field, prefix - for descending (list/search)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "search", Type: "string", Description: "Full-text search term (list)"},
				},
			},
			{
				Path:        "jc devices",
				Description: "Manage JumpCloud devices (systems)",
				Subcommands: []string{"list", "get", "delete", "lock", "restart", "erase"},
				Flags: []flagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field, prefix - for descending (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "confirm-erase", Type: "bool", Description: "Required safety flag for erase command"},
				},
			},
			{
				Path:        "jc groups",
				Description: "Manage user and device groups",
				Subcommands: []string{"user list", "user get", "user create", "user update", "user delete", "device list", "device get", "device create", "device update", "device delete", "add-member", "remove-member"},
				Flags: []flagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc commands",
				Description: "Manage JumpCloud commands",
				Subcommands: []string{"list", "get", "create", "update", "delete", "run", "results"},
				Flags: []flagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "type", Type: "string", Description: "Command type filter: linux, mac, windows (list)"},
				},
			},
			{
				Path:        "jc policies",
				Description: "Manage JumpCloud policies",
				Subcommands: []string{"list", "get", "results"},
				Flags: []flagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc apps",
				Description: "Manage SSO applications",
				Subcommands: []string{"list", "get"},
				Flags: []flagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc admins",
				Description: "List JumpCloud administrators",
				Subcommands: []string{"list"},
				Flags: []flagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc insights",
				Description: "Query Directory Insights audit events",
				Subcommands: []string{"query", "count", "distinct", "save", "run", "saved"},
				Flags: []flagEntry{
					{Name: "service", Type: "string", Description: "Event service: sso, radius, ldap, user_portal, admin, mdm, directory, software, systems, password_manager, all"},
					{Name: "last", Type: "string", Description: "Time range: 24h, 7d, 30d, 1m"},
					{Name: "start", Type: "string", Description: "Start time (RFC 3339 or YYYY-MM-DD)"},
					{Name: "end", Type: "string", Description: "End time (RFC 3339 or YYYY-MM-DD)"},
					{Name: "event-type", Type: "string", Description: "Filter by event type"},
					{Name: "limit", Type: "int", Description: "Maximum events to return"},
					{Name: "sort", Type: "string", Description: "Sort field"},
				},
			},
			{
				Path:        "jc graph",
				Description: "Traverse JumpCloud resource associations",
				Subcommands: []string{"traverse"},
				Flags: []flagEntry{
					{Name: "from", Type: "string", Description: "Source: type:identifier (e.g. user:jdoe)"},
					{Name: "to", Type: "string", Description: "Target type: user, system, user_group, system_group, application, policy, command"},
				},
			},
			{
				Path:        "jc bulk",
				Description: "Bulk operations from CSV files",
				Subcommands: []string{"users"},
				Flags: []flagEntry{
					{Name: "file", Type: "string", Description: "Path to CSV file"},
				},
			},
			{
				Path:        "jc recipe",
				Description: "Manage and run automation recipes",
				Subcommands: []string{"list", "show", "run", "validate", "create", "import", "export"},
				Flags: []flagEntry{
					{Name: "param", Type: "string[]", Description: "Recipe parameters as key=value (run)"},
					{Name: "file", Type: "string", Description: "Output file path (export)"},
				},
			},
			{
				Path:        "jc mcp",
				Description: "MCP server for AI agent integration",
				Subcommands: []string{"serve"},
				Flags: []flagEntry{
					{Name: "rate-limit", Type: "int", Default: "60", Description: "Maximum tool calls per minute"},
					{Name: "read-only", Type: "bool", Description: "Disable all mutation tools"},
				},
			},
		},
	}
	return manifest
}
