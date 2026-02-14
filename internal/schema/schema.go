// Package schema provides machine-readable metadata for JumpCloud resource
// types and CLI commands. It is the single source of truth consumed by both
// the "jc schema" CLI commands and the MCP resource handlers.
package schema

import (
	"sort"

	"github.com/klaassen-consulting/jc/internal/version"
)

// FieldDef describes a single field on a JumpCloud resource.
type FieldDef struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // string, bool, int, datetime, array, object
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

// ResourceSchema describes a JumpCloud resource type.
type ResourceSchema struct {
	Resource      string     `json:"resource"`
	APIVersion    string     `json:"api_version"`
	Verbs         []string   `json:"verbs"`
	DefaultFields []string   `json:"default_fields"`
	Fields        []FieldDef `json:"fields"`
	FilterSupport bool       `json:"filter_support"`
	SortSupport   bool       `json:"sort_support"`
	SortFields    []string   `json:"sort_fields,omitempty"`
	IDField       string     `json:"id_field"`
	NameField     string     `json:"name_field"`
}

// CommandManifest describes the full CLI command tree.
type CommandManifest struct {
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description"`
	Commands    []CommandEntry `json:"commands"`
	GlobalFlags []FlagEntry    `json:"global_flags"`
	Resources   []string       `json:"resources"`
}

// CommandEntry describes a CLI command group with its subcommands and flags.
type CommandEntry struct {
	Path        string      `json:"path"`
	Description string      `json:"description"`
	Subcommands []string    `json:"subcommands,omitempty"`
	Flags       []FlagEntry `json:"flags,omitempty"`
}

// FlagEntry describes a CLI flag.
type FlagEntry struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description"`
}

// Resources is the canonical map of all JumpCloud resource schemas.
var Resources = map[string]ResourceSchema{
	"users": {
		Resource:      "users",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "create", "update", "delete", "search", "lock", "unlock", "reset-mfa", "reset-password"},
		DefaultFields: []string{"username", "email", "firstname", "lastname", "activated", "suspended"},
		Fields: []FieldDef{
			{Name: "_id", Type: "string", Description: "Unique user identifier"},
			{Name: "username", Type: "string", Description: "System username (unique)", Required: true},
			{Name: "email", Type: "string", Description: "Email address", Required: true},
			{Name: "firstname", Type: "string", Description: "First name"},
			{Name: "lastname", Type: "string", Description: "Last name"},
			{Name: "displayname", Type: "string", Description: "Display name"},
			{Name: "department", Type: "string", Description: "Department"},
			{Name: "jobTitle", Type: "string", Description: "Job title"},
			{Name: "activated", Type: "bool", Description: "Whether the account is activated"},
			{Name: "suspended", Type: "bool", Description: "Whether the account is suspended"},
			{Name: "account_locked", Type: "bool", Description: "Whether the account is locked"},
			{Name: "totp_enabled", Type: "bool", Description: "Whether TOTP MFA is enabled"},
			{Name: "enable_user_portal_multifactor", Type: "bool", Description: "User portal MFA enabled"},
			{Name: "password_date", Type: "datetime", Description: "Last password change timestamp"},
			{Name: "created", Type: "datetime", Description: "Account creation timestamp"},
			{Name: "lastLogin", Type: "datetime", Description: "Last login timestamp"},
			{Name: "state", Type: "string", Description: "Account state (e.g. ACTIVATED, STAGED)"},
			{Name: "description", Type: "string", Description: "User description"},
			{Name: "company", Type: "string", Description: "Company name"},
			{Name: "location", Type: "string", Description: "Location"},
			{Name: "costCenter", Type: "string", Description: "Cost center"},
			{Name: "employeeType", Type: "string", Description: "Employee type"},
			{Name: "employeeIdentifier", Type: "string", Description: "Employee identifier"},
			{Name: "mfa", Type: "object", Description: "MFA configuration details"},
			{Name: "addresses", Type: "array", Description: "Physical addresses"},
			{Name: "phoneNumbers", Type: "array", Description: "Phone numbers"},
			{Name: "attributes", Type: "array", Description: "Custom attributes"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"username", "email", "firstname", "lastname", "created", "activated", "suspended"},
		IDField:       "_id",
		NameField:     "username",
	},
	"devices": {
		Resource:      "devices",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "delete", "lock", "restart", "erase"},
		DefaultFields: []string{"displayName", "hostname", "os", "osVersion", "lastContact", "agentVersion"},
		Fields: []FieldDef{
			{Name: "_id", Type: "string", Description: "Unique device identifier"},
			{Name: "displayName", Type: "string", Description: "Display name"},
			{Name: "hostname", Type: "string", Description: "Device hostname"},
			{Name: "os", Type: "string", Description: "Operating system (e.g. Mac OS X, Windows, Linux)"},
			{Name: "osVersion", Type: "string", Description: "OS version string"},
			{Name: "lastContact", Type: "datetime", Description: "Last agent check-in timestamp"},
			{Name: "agentVersion", Type: "string", Description: "JumpCloud agent version"},
			{Name: "active", Type: "bool", Description: "Whether the device is active"},
			{Name: "allowMultiFactorAuthentication", Type: "bool", Description: "MFA allowed on device"},
			{Name: "allowPublicKeyAuthentication", Type: "bool", Description: "Public key auth allowed"},
			{Name: "allowSshPasswordAuthentication", Type: "bool", Description: "SSH password auth allowed"},
			{Name: "arch", Type: "string", Description: "CPU architecture"},
			{Name: "created", Type: "datetime", Description: "Registration timestamp"},
			{Name: "serialNumber", Type: "string", Description: "Device serial number"},
			{Name: "systemTimezone", Type: "int", Description: "System timezone offset"},
			{Name: "remoteIP", Type: "string", Description: "Remote IP address"},
			{Name: "networkInterfaces", Type: "array", Description: "Network interface details"},
			{Name: "tags", Type: "array", Description: "Assigned tags"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"displayName", "hostname", "os", "osVersion", "lastContact", "created", "active"},
		IDField:       "_id",
		NameField:     "hostname",
	},
	"groups": {
		Resource:      "groups",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete", "add-member", "remove-member"},
		DefaultFields: []string{"id", "name", "description", "type"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique group identifier"},
			{Name: "name", Type: "string", Description: "Group name", Required: true},
			{Name: "description", Type: "string", Description: "Group description"},
			{Name: "type", Type: "string", Description: "Group type (user_group or system_group)"},
			{Name: "email", Type: "string", Description: "Group email address"},
			{Name: "attributes", Type: "object", Description: "Group attributes"},
			{Name: "memberQuery", Type: "object", Description: "Dynamic membership query"},
			{Name: "memberQueryExemptions", Type: "array", Description: "Members exempt from dynamic query"},
			{Name: "memberSuggestionsNotify", Type: "bool", Description: "Notify on member suggestions"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name", "description", "type"},
		IDField:       "id",
		NameField:     "name",
	},
	"commands": {
		Resource:      "commands",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "create", "update", "delete", "run", "results"},
		DefaultFields: []string{"name", "commandType", "command", "schedule", "scheduleRepeatType"},
		Fields: []FieldDef{
			{Name: "_id", Type: "string", Description: "Unique command identifier"},
			{Name: "name", Type: "string", Description: "Command name", Required: true},
			{Name: "command", Type: "string", Description: "Command body (script)", Required: true},
			{Name: "commandType", Type: "string", Description: "Target OS: linux, mac, windows", Required: true},
			{Name: "user", Type: "string", Description: "Run-as user (e.g. root)"},
			{Name: "schedule", Type: "string", Description: "Cron schedule expression"},
			{Name: "scheduleRepeatType", Type: "string", Description: "Repeat type"},
			{Name: "timeout", Type: "string", Description: "Execution timeout"},
			{Name: "shell", Type: "string", Description: "Shell to use for execution"},
			{Name: "launchType", Type: "string", Description: "Launch type (trigger, manual, repeated)"},
			{Name: "trigger", Type: "string", Description: "Trigger name"},
			{Name: "files", Type: "array", Description: "Attached files"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name", "commandType", "schedule"},
		IDField:       "_id",
		NameField:     "name",
	},
	"policies": {
		Resource:      "policies",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "results"},
		DefaultFields: []string{"id", "name", "template", "os"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique policy identifier"},
			{Name: "name", Type: "string", Description: "Policy name"},
			{Name: "template", Type: "object", Description: "Policy template details"},
			{Name: "os", Type: "string", Description: "Target operating system"},
			{Name: "values", Type: "array", Description: "Policy configuration values"},
			{Name: "configuredFields", Type: "array", Description: "Configured policy fields"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name", "os"},
		IDField:       "id",
		NameField:     "name",
	},
	"apps": {
		Resource:      "apps",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get"},
		DefaultFields: []string{"_id", "name", "displayLabel", "ssoType", "status"},
		Fields: []FieldDef{
			{Name: "_id", Type: "string", Description: "Unique application identifier"},
			{Name: "name", Type: "string", Description: "Application name"},
			{Name: "displayLabel", Type: "string", Description: "Display label shown in user portal"},
			{Name: "ssoType", Type: "string", Description: "SSO type (saml, oidc, bookmark)"},
			{Name: "status", Type: "string", Description: "Application status"},
			{Name: "organization", Type: "string", Description: "Organization ID"},
			{Name: "config", Type: "object", Description: "SSO configuration details"},
			{Name: "beta", Type: "bool", Description: "Whether the app is in beta"},
			{Name: "learnMore", Type: "string", Description: "Documentation URL"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name", "displayLabel", "ssoType", "status"},
		IDField:       "_id",
		NameField:     "name",
	},
	"admins": {
		Resource:      "admins",
		APIVersion:    "v2",
		Verbs:         []string{"list"},
		DefaultFields: []string{"id", "email", "role", "enableMultiFactor"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique administrator identifier"},
			{Name: "email", Type: "string", Description: "Administrator email"},
			{Name: "role", Type: "string", Description: "Admin role (Administrator, Manager, Read Only, etc.)"},
			{Name: "enableMultiFactor", Type: "bool", Description: "Whether MFA is enabled for admin"},
			{Name: "firstname", Type: "string", Description: "First name"},
			{Name: "lastname", Type: "string", Description: "Last name"},
			{Name: "totpEnrolled", Type: "bool", Description: "Whether TOTP is enrolled"},
			{Name: "created", Type: "datetime", Description: "Account creation timestamp"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"email", "role"},
		IDField:       "id",
		NameField:     "email",
	},
	"insights": {
		Resource:      "insights",
		APIVersion:    "insights/v1",
		Verbs:         []string{"query", "count", "distinct"},
		DefaultFields: []string{"timestamp", "event_type", "initiated_by", "client_ip", "success"},
		Fields: []FieldDef{
			{Name: "timestamp", Type: "datetime", Description: "Event timestamp"},
			{Name: "event_type", Type: "string", Description: "Type of event (e.g. sso_auth, admin_login)"},
			{Name: "initiated_by", Type: "object", Description: "Who initiated the event (type, id, email)"},
			{Name: "client_ip", Type: "string", Description: "Client IP address"},
			{Name: "success", Type: "bool", Description: "Whether the event was successful"},
			{Name: "service", Type: "string", Description: "Event service category"},
			{Name: "organization", Type: "string", Description: "Organization ID"},
			{Name: "geoip", Type: "object", Description: "Geographic IP data"},
			{Name: "useragent", Type: "object", Description: "User agent details"},
			{Name: "changes", Type: "array", Description: "Fields changed by this event"},
			{Name: "resource", Type: "object", Description: "Affected resource details"},
		},
		FilterSupport: false,
		SortSupport:   true,
		SortFields:    []string{"timestamp"},
		IDField:       "",
		NameField:     "",
	},
}

// ResourceNames returns the sorted list of all resource type names.
func ResourceNames() []string {
	names := make([]string, 0, len(Resources))
	for name := range Resources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetResource returns the schema for a named resource, or nil if not found.
func GetResource(name string) *ResourceSchema {
	s, ok := Resources[name]
	if !ok {
		return nil
	}
	return &s
}

// AllResources returns all resource schemas sorted by name.
func AllResources() []ResourceSchema {
	names := ResourceNames()
	all := make([]ResourceSchema, 0, len(names))
	for _, name := range names {
		all = append(all, Resources[name])
	}
	return all
}

// BuildCommandManifest generates a machine-readable manifest of all CLI commands.
func BuildCommandManifest() CommandManifest {
	return CommandManifest{
		Name:        "jc",
		Version:     version.Number,
		Description: "JumpCloud CLI — manage users, devices, groups, policies, commands, insights, and more",
		Resources:   ResourceNames(),
		GlobalFlags: []FlagEntry{
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
		Commands: []CommandEntry{
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
				Flags: []FlagEntry{
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
				Flags: []FlagEntry{
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
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc commands",
				Description: "Manage JumpCloud commands",
				Subcommands: []string{"list", "get", "create", "update", "delete", "run", "results"},
				Flags: []FlagEntry{
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
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc apps",
				Description: "Manage SSO applications",
				Subcommands: []string{"list", "get"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc admins",
				Description: "List JumpCloud administrators",
				Subcommands: []string{"list"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc insights",
				Description: "Query Directory Insights audit events",
				Subcommands: []string{"query", "count", "distinct", "save", "run", "saved"},
				Flags: []FlagEntry{
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
				Flags: []FlagEntry{
					{Name: "from", Type: "string", Description: "Source: type:identifier (e.g. user:jdoe)"},
					{Name: "to", Type: "string", Description: "Target type: user, system, user_group, system_group, application, policy, command"},
				},
			},
			{
				Path:        "jc bulk",
				Description: "Bulk operations from CSV files",
				Subcommands: []string{"users"},
				Flags: []FlagEntry{
					{Name: "file", Type: "string", Description: "Path to CSV file"},
				},
			},
			{
				Path:        "jc recipe",
				Description: "Manage and run automation recipes",
				Subcommands: []string{"list", "show", "run", "validate", "create", "import", "export"},
				Flags: []FlagEntry{
					{Name: "param", Type: "string[]", Description: "Recipe parameters as key=value (run)"},
					{Name: "file", Type: "string", Description: "Output file path (export)"},
				},
			},
			{
				Path:        "jc mcp",
				Description: "MCP server for AI agent integration",
				Subcommands: []string{"serve"},
				Flags: []FlagEntry{
					{Name: "rate-limit", Type: "int", Default: "60", Description: "Maximum tool calls per minute"},
					{Name: "read-only", Type: "bool", Description: "Disable all mutation tools"},
				},
			},
			{
				Path:        "jc schema",
				Description: "Machine-readable schema and command manifest",
				Subcommands: []string{"resources", "commands"},
			},
			{
				Path:        "jc ask",
				Description: "Translate natural language queries into jc CLI commands",
			},
			{
				Path:        "jc explain",
				Description: "Explain what a command would do without executing",
			},
		},
	}
}
