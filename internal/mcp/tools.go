package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/klaassen-consulting/jc/internal/simulator"
	"github.com/klaassen-consulting/jc/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Client factory vars for test injection.
var (
	newV1ClientFunc       = func() (*api.V1Client, error) { return api.NewV1Client() }
	newV2ClientFunc       = func() (*api.V2Client, error) { return api.NewV2Client() }
	newInsightsClientFunc = func() (*api.InsightsClient, error) { return api.NewInsightsClient() }
)

// --- Input types for typed tools ---

type listInput struct {
	Limit  int      `json:"limit,omitempty" jsonschema:"Maximum number of results to return (0 = all)"`
	Sort   string   `json:"sort,omitempty" jsonschema:"Field to sort by. Prefix with - for descending (e.g. -created)"`
	Filter []string `json:"filter,omitempty" jsonschema:"Filter expressions (e.g. field=value)"`
}

type getInput struct {
	Identifier string `json:"identifier" jsonschema:"Name or ID of the resource"`
}

type userCreateInput struct {
	Username  string `json:"username" jsonschema:"Username"`
	Email     string `json:"email" jsonschema:"Email address"`
	Firstname string `json:"firstname,omitempty" jsonschema:"First name"`
	Lastname  string `json:"lastname,omitempty" jsonschema:"Last name"`
}

type userUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Username or ID of the user to update"`
	Email      string `json:"email,omitempty" jsonschema:"New email address"`
	Firstname  string `json:"firstname,omitempty" jsonschema:"New first name"`
	Lastname   string `json:"lastname,omitempty" jsonschema:"New last name"`
	Department string `json:"department,omitempty" jsonschema:"New department"`
	JobTitle   string `json:"jobTitle,omitempty" jsonschema:"New job title"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute the update. Without this the tool returns a plan."`
}

type destructiveInput struct {
	Identifier string `json:"identifier" jsonschema:"Name or ID of the resource"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type membershipInput struct {
	Group      string `json:"group" jsonschema:"Group name or ID"`
	Member     string `json:"member" jsonschema:"User or device name or ID to add/remove"`
	MemberType string `json:"member_type" jsonschema:"Type of member: user or device"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type insightsQueryInput struct {
	Service   string `json:"service" jsonschema:"Event service to query (sso/radius/ldap/user_portal/admin/mdm/directory/software/systems/password_manager/all)"`
	Last      string `json:"last,omitempty" jsonschema:"Time range shortcut (24h/7d/30d/1m)"`
	Start     string `json:"start,omitempty" jsonschema:"Start time (RFC 3339 or YYYY-MM-DD)"`
	End       string `json:"end,omitempty" jsonschema:"End time (RFC 3339 or YYYY-MM-DD)"`
	EventType string `json:"event_type,omitempty" jsonschema:"Filter by event type (e.g. sso_auth_failed)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum events to return (0 = all)"`
}

type insightsCountInput struct {
	Service   string `json:"service" jsonschema:"Event service to query"`
	Last      string `json:"last,omitempty" jsonschema:"Time range shortcut (24h/7d/30d/1m)"`
	Start     string `json:"start,omitempty" jsonschema:"Start time (RFC 3339 or YYYY-MM-DD)"`
	End       string `json:"end,omitempty" jsonschema:"End time (RFC 3339 or YYYY-MM-DD)"`
	EventType string `json:"event_type,omitempty" jsonschema:"Filter by event type"`
}

type commandRunInput struct {
	Command string `json:"command" jsonschema:"Command name or ID to run"`
	Target  string `json:"target" jsonschema:"Device hostname/ID or device group name/ID to run on"`
	Execute bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type recipeRunInput struct {
	Name   string            `json:"name" jsonschema:"Recipe name to run"`
	Params map[string]string `json:"params,omitempty" jsonschema:"Recipe parameters as key-value pairs"`
}

type authPolicyCreateInput struct {
	Name       string `json:"name" jsonschema:"Policy name"`
	Type       string `json:"type,omitempty" jsonschema:"Policy type (e.g. user_portal, admin)"`
	Conditions string `json:"conditions,omitempty" jsonschema:"Conditions tree as raw JSON string"`
	MFA        bool   `json:"mfa,omitempty" jsonschema:"Require MFA for this policy"`
	Disabled   bool   `json:"disabled,omitempty" jsonschema:"Create in disabled state"`
}

type authPolicyUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Policy name or ID to update"`
	Name       string `json:"name,omitempty" jsonschema:"New policy name"`
	Conditions string `json:"conditions,omitempty" jsonschema:"New conditions tree as raw JSON"`
	Disabled   *bool  `json:"disabled,omitempty" jsonschema:"Set to true to disable or false to enable"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type simulateInput struct {
	Policy   string `json:"policy" jsonschema:"Policy name or ID to simulate"`
	User     string `json:"user" jsonschema:"User name or ID"`
	IP       string `json:"ip,omitempty" jsonschema:"Source IP address"`
	Device   string `json:"device,omitempty" jsonschema:"Device name or ID"`
	Location string `json:"location,omitempty" jsonschema:"Country code (e.g. US, DE)"`
}

type blastRadiusInput struct {
	Policy string `json:"policy" jsonschema:"Policy name or ID"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of affected users to return (default 100)"`
}

type ipListCreateInput struct {
	Name        string   `json:"name" jsonschema:"IP list name"`
	Description string   `json:"description,omitempty" jsonschema:"IP list description"`
	IPs         []string `json:"ips" jsonschema:"IP entries (single IPs, CIDR ranges, IP ranges)"`
}

type ipListUpdateInput struct {
	Identifier  string   `json:"identifier" jsonschema:"IP list name or ID to update"`
	Name        string   `json:"name,omitempty" jsonschema:"New IP list name"`
	Description string   `json:"description,omitempty" jsonschema:"New description"`
	IPs         []string `json:"ips,omitempty" jsonschema:"New IP entries (replaces existing)"`
	Execute     bool     `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type softwareCreateInput struct {
	Name     string `json:"name" jsonschema:"Display name for the software app"`
	Settings string `json:"settings,omitempty" jsonschema:"Package settings as raw JSON array"`
}

type softwareUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Software app name or ID to update"`
	Name       string `json:"name,omitempty" jsonschema:"New display name"`
	Settings   string `json:"settings,omitempty" jsonschema:"New package settings as raw JSON array"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type ldapCreateInput struct {
	Name                         string `json:"name" jsonschema:"LDAP server name"`
	UserLockoutAction            string `json:"user_lockout_action,omitempty" jsonschema:"Action on user lockout"`
	UserPasswordExpirationAction string `json:"user_password_expiration_action,omitempty" jsonschema:"Action on password expiration"`
}

type ldapUpdateInput struct {
	Identifier                   string `json:"identifier" jsonschema:"LDAP server name or ID to update"`
	Name                         string `json:"name,omitempty" jsonschema:"New server name"`
	UserLockoutAction            string `json:"user_lockout_action,omitempty" jsonschema:"New lockout action"`
	UserPasswordExpirationAction string `json:"user_password_expiration_action,omitempty" jsonschema:"New password expiration action"`
	Execute                      bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type adCreateInput struct {
	Domain  string `json:"domain" jsonschema:"Active Directory domain name"`
	UseCase string `json:"use_case,omitempty" jsonschema:"Integration use case"`
}

type adUpdateInput struct {
	Identifier    string `json:"identifier" jsonschema:"AD domain or ID to update"`
	UseCase       string `json:"use_case,omitempty" jsonschema:"New use case"`
	GroupsEnabled *bool  `json:"groups_enabled,omitempty" jsonschema:"Enable or disable group sync"`
	Execute       bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type adminCreateInput struct {
	Email     string `json:"email" jsonschema:"Administrator email address"`
	Role      string `json:"role,omitempty" jsonschema:"Admin role (e.g. Administrator, Manager, Read Only)"`
	EnableMFA bool   `json:"enable_mfa,omitempty" jsonschema:"Enable multi-factor authentication"`
}

type adminUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Admin email or ID to update"`
	Role       string `json:"role,omitempty" jsonschema:"New admin role"`
	EnableMFA  *bool  `json:"enable_mfa,omitempty" jsonschema:"Set to true to enable MFA or false to disable"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type planInput struct {
	Command string `json:"command" jsonschema:"A jc command string to preview (e.g. users delete jdoe)"`
}

type explainInput struct {
	Command string `json:"command" jsonschema:"A jc command string to explain (e.g. users delete jdoe)"`
}

// registerTools adds MCP tools to the server.
func (s *Server) registerTools() {
	// ping: A simple health-check tool.
	s.addTool("jc_ping", "Check if the JC MCP server is running and authenticated",
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return textResult(fmt.Sprintf("jc MCP server v%s is running", version.Number)), nil, nil
		},
	)

	// --- Users tools ---
	s.registerUserTools()

	// --- Devices tools ---
	s.registerDeviceTools()

	// --- Groups tools ---
	s.registerGroupTools()

	// --- Insights tools ---
	s.registerInsightsTools()

	// --- Commands tools ---
	s.registerCommandTools()

	// --- Policies tools ---
	s.registerPolicyTools()

	// --- Auth Policies tools ---
	s.registerAuthPolicyTools()

	// --- IP Lists tools ---
	s.registerIPListTools()

	// --- Software tools ---
	s.registerSoftwareTools()

	// --- LDAP tools ---
	s.registerLDAPTools()

	// --- Active Directory tools ---
	s.registerADTools()

	// --- Organization tools ---
	s.registerOrgTools()

	// --- Admin tools ---
	s.registerAdminTools()

	// --- Recipe tools ---
	s.registerRecipeTools()

	// --- Plan and explain tools ---
	s.registerMetaTools()
}

func (s *Server) registerUserTools() {
	addTypedTool(s, "users_list", "List all JumpCloud users. Returns user objects with fields like username, email, firstname, lastname, activated, suspended.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV1ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/systemusers", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing users: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "users_get", "Get a single JumpCloud user by username or ID. Returns the full user object.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/systemusers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting user: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "users_create", "Create a new JumpCloud user. Requires username and email.",
		func(ctx context.Context, req *mcp.CallToolRequest, args userCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]string{
				"username": args.Username,
				"email":    args.Email,
			}
			if args.Firstname != "" {
				body["firstname"] = args.Firstname
			}
			if args.Lastname != "" {
				body["lastname"] = args.Lastname
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/systemusers", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating user: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "users_update", "Update a JumpCloud user's fields. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args userUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]string{}
			if args.Email != "" {
				body["email"] = args.Email
			}
			if args.Firstname != "" {
				body["firstname"] = args.Firstname
			}
			if args.Lastname != "" {
				body["lastname"] = args.Lastname
			}
			if args.Department != "" {
				body["department"] = args.Department
			}
			if args.JobTitle != "" {
				body["jobTitle"] = args.JobTitle
			}
			if len(body) == 0 {
				return errorResult("no fields to update — provide at least one field (email, firstname, lastname, department, jobTitle)"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("update", "user", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/systemusers/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating user: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "users_delete", "Delete a JumpCloud user. Set execute=true to delete; otherwise returns a plan. This is destructive and irreversible.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "user", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/systemusers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting user: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("User %s deleted successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "users_lock", "Lock a JumpCloud user account. Set execute=true to lock; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("lock", "user", args.Identifier, id, nil)
			}
			_, err = client.Update(ctx, "/systemusers/"+id, map[string]bool{"account_locked": true})
			if err != nil {
				return errorResult(fmt.Sprintf("locking user: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("User %s locked successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "users_unlock", "Unlock a JumpCloud user account. Set execute=true to unlock; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("unlock", "user", args.Identifier, id, nil)
			}
			_, err = client.Update(ctx, "/systemusers/"+id, map[string]bool{"account_locked": false})
			if err != nil {
				return errorResult(fmt.Sprintf("unlocking user: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("User %s unlocked successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "users_reset_mfa", "Reset MFA/TOTP enrollment for a JumpCloud user. The user will need to re-enroll. Set execute=true to reset; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("reset_mfa", "user", args.Identifier, id, nil)
			}
			_, err = client.Post(ctx, "/systemusers/"+id+"/resetmfa", map[string]bool{"exclusion": true})
			if err != nil {
				return errorResult(fmt.Sprintf("resetting MFA: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("MFA reset for user %s. User will need to re-enroll.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "users_reset_password", "Trigger a password reset email for a JumpCloud user. Set execute=true to reset; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("reset_password", "user", args.Identifier, id, nil)
			}
			_, err = client.Post(ctx, "/systemusers/"+id+"/expire", nil)
			if err != nil {
				return errorResult(fmt.Sprintf("resetting password: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Password reset email sent to user %s.", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerDeviceTools() {
	addTypedTool(s, "devices_list", "List all JumpCloud devices (systems). Returns device objects with fields like displayName, hostname, os, osVersion, lastContact, agentVersion.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV1ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/systems", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing devices: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "devices_get", "Get a single JumpCloud device by hostname or ID. Returns the full device object.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.DeviceConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/systems/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting device: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "devices_lock", "Send MDM lock command to a device. Set execute=true to lock; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			return s.runDeviceMDMTool(ctx, args, "lock")
		},
	)

	addTypedTool(s, "devices_restart", "Send MDM restart command to a device. Set execute=true to restart; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			return s.runDeviceMDMTool(ctx, args, "restart")
		},
	)

	addTypedTool(s, "devices_erase", "Send MDM erase (wipe) command to a device. THIS IS EXTREMELY DESTRUCTIVE — it will wipe all data. Set execute=true to erase; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			return s.runDeviceMDMTool(ctx, args, "erase")
		},
	)
}

func (s *Server) runDeviceMDMTool(ctx context.Context, args destructiveInput, action string) (*mcp.CallToolResult, any, error) {
	if s.readOnly {
		return errorResult("server is in read-only mode"), nil, nil
	}
	client, err := newV1ClientFunc()
	if err != nil {
		return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
	}
	id, err := resolveV1(ctx, client, args.Identifier, resolve.DeviceConfig)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	if !args.Execute {
		return planResult(action, "device", args.Identifier, id, nil)
	}
	_, err = client.Post(ctx, "/systems/"+id+"/command/builtin/"+action, nil)
	if err != nil {
		return errorResult(fmt.Sprintf("%s device: %v", action, err)), nil, nil
	}
	return textResult(fmt.Sprintf("Device %s %s command sent successfully.", args.Identifier, action)), nil, nil
}

func (s *Server) registerGroupTools() {
	addTypedTool(s, "groups_list", "List all JumpCloud user groups and device (system) groups. Returns group objects with id, name, type.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			userGroups, err := client.ListAll(ctx, "/usergroups", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing user groups: %v", err)), nil, nil
			}
			deviceGroups, err := client.ListAll(ctx, "/systemgroups", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing device groups: %v", err)), nil, nil
			}
			all := append(userGroups.Data, deviceGroups.Data...)
			return rawListResult(all, len(all))
		},
	)

	addTypedTool(s, "groups_add_member", "Add a user or device to a group. Set execute=true to add; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args membershipInput) (*mcp.CallToolResult, any, error) {
			return s.runMembershipTool(ctx, args, "add")
		},
	)

	addTypedTool(s, "groups_remove_member", "Remove a user or device from a group. Set execute=true to remove; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args membershipInput) (*mcp.CallToolResult, any, error) {
			return s.runMembershipTool(ctx, args, "remove")
		},
	)
}

func (s *Server) runMembershipTool(ctx context.Context, args membershipInput, op string) (*mcp.CallToolResult, any, error) {
	if s.readOnly {
		return errorResult("server is in read-only mode"), nil, nil
	}
	memberType := strings.ToLower(args.MemberType)
	if memberType != "user" && memberType != "device" {
		return errorResult("member_type must be 'user' or 'device'"), nil, nil
	}

	v2Client, err := newV2ClientFunc()
	if err != nil {
		return errorResult(fmt.Sprintf("creating V2 client: %v", err)), nil, nil
	}

	// Resolve group.
	var groupID string
	var groupEndpoint, memberEndpoint string
	var v2Resolver = resolve.NewV2Resolver(v2Client)
	if memberType == "user" {
		groupID, err = v2Resolver.Resolve(ctx, args.Group, resolve.UserGroupConfig)
		if err != nil {
			return errorResult(fmt.Sprintf("resolving group: %v", err)), nil, nil
		}
		groupEndpoint = "/usergroups"
		memberEndpoint = "/usergroups/" + groupID + "/members"
	} else {
		groupID, err = v2Resolver.Resolve(ctx, args.Group, resolve.DeviceGroupConfig)
		if err != nil {
			return errorResult(fmt.Sprintf("resolving group: %v", err)), nil, nil
		}
		groupEndpoint = "/systemgroups"
		memberEndpoint = "/systemgroups/" + groupID + "/membership"
	}
	_ = groupEndpoint

	// Resolve member.
	v1Client, err := newV1ClientFunc()
	if err != nil {
		return errorResult(fmt.Sprintf("creating V1 client: %v", err)), nil, nil
	}
	var memberID string
	if memberType == "user" {
		memberID, err = resolveV1(ctx, v1Client, args.Member, resolve.UserConfig)
	} else {
		memberID, err = resolveV1(ctx, v1Client, args.Member, resolve.DeviceConfig)
	}
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	if !args.Execute {
		effects := map[string]string{
			"operation":   op,
			"group":       args.Group,
			"group_id":    groupID,
			"member":      args.Member,
			"member_id":   memberID,
			"member_type": memberType,
		}
		return planResult(op+"_member", "group_membership", args.Group, groupID, effects)
	}

	apiType := memberType
	if memberType == "device" {
		apiType = "system"
	}
	body := map[string]string{
		"op":   op,
		"type": apiType,
		"id":   memberID,
	}
	_, err = v2Client.Create(ctx, memberEndpoint, body)
	if err != nil {
		return errorResult(fmt.Sprintf("%s member: %v", op, err)), nil, nil
	}
	verb := "added to"
	if op == "remove" {
		verb = "removed from"
	}
	return textResult(fmt.Sprintf("Member %s %s group %s.", args.Member, verb, args.Group)), nil, nil
}

func (s *Server) registerInsightsTools() {
	addTypedTool(s, "insights_query", "Query JumpCloud Directory Insights events. Returns audit/activity events matching the criteria.",
		func(ctx context.Context, req *mcp.CallToolRequest, args insightsQueryInput) (*mcp.CallToolResult, any, error) {
			client, err := newInsightsClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating Insights client: %v", err)), nil, nil
			}
			if err := api.ValidateService(args.Service); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			startTime, endTime, err := resolveTimeRange(args.Last, args.Start, args.End)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			query := api.InsightsQuery{
				Service:   args.Service,
				StartTime: startTime,
				EndTime:   endTime,
			}
			if args.EventType != "" {
				query.SearchTermFilter = map[string]any{"event_type": args.EventType}
			}
			result, err := client.QueryEvents(ctx, query, api.InsightsQueryOptions{
				Limit: args.Limit,
			})
			if err != nil {
				return errorResult(fmt.Sprintf("querying events: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "insights_count", "Count JumpCloud Directory Insights events matching criteria. Returns a single count number.",
		func(ctx context.Context, req *mcp.CallToolRequest, args insightsCountInput) (*mcp.CallToolResult, any, error) {
			client, err := newInsightsClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating Insights client: %v", err)), nil, nil
			}
			if err := api.ValidateService(args.Service); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			startTime, endTime, err := resolveTimeRange(args.Last, args.Start, args.End)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			query := api.InsightsQuery{
				Service:   args.Service,
				StartTime: startTime,
				EndTime:   endTime,
			}
			if args.EventType != "" {
				query.SearchTermFilter = map[string]any{"event_type": args.EventType}
			}
			count, err := client.CountEvents(ctx, query)
			if err != nil {
				return errorResult(fmt.Sprintf("counting events: %v", err)), nil, nil
			}
			res, err := jsonResult(map[string]int{"count": count})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)
}

func (s *Server) registerCommandTools() {
	addTypedTool(s, "commands_list", "List all JumpCloud commands. Returns command objects with name, commandType, command, schedule fields.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV1ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/commands", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing commands: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "commands_run", "Trigger a JumpCloud command on a device or device group. Set execute=true to run; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args commandRunInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			// Resolve command.
			cmdResolver := resolve.NewResolver(client)
			cmdID, err := cmdResolver.Resolve(ctx, args.Command, resolve.CommandConfig)
			if err != nil {
				return errorResult(fmt.Sprintf("resolving command: %v", err)), nil, nil
			}
			// Determine if target is a device or device group.
			targetID := args.Target
			isDeviceID := resolve.IsID(args.Target)
			if !isDeviceID {
				// Try resolving as device first.
				deviceResolver := resolve.NewResolver(client)
				id, deviceErr := deviceResolver.Resolve(ctx, args.Target, resolve.DeviceConfig)
				if deviceErr == nil {
					targetID = id
					isDeviceID = true
				}
			}

			if !args.Execute {
				effects := map[string]string{
					"command":    args.Command,
					"command_id": cmdID,
					"target":     args.Target,
					"target_id":  targetID,
				}
				return planResult("run", "command", args.Command, cmdID, effects)
			}

			body := map[string]any{
				"command": cmdID,
			}
			if isDeviceID {
				body["systems"] = []string{targetID}
			} else {
				body["systemGroups"] = []string{targetID}
			}
			_, err = client.Post(ctx, "/runcommand", body)
			if err != nil {
				return errorResult(fmt.Sprintf("running command: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Command %s triggered on %s.", args.Command, args.Target)), nil, nil
		},
	)
}

func (s *Server) registerPolicyTools() {
	addTypedTool(s, "policies_list", "List all JumpCloud policies. Returns policy objects with id, name, template, os fields.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/policies", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing policies: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)
}

func (s *Server) registerAuthPolicyTools() {
	addTypedTool(s, "auth_policies_list", "List all JumpCloud authentication policies for conditional access. Returns policy objects with id, name, disabled, type, conditions, effect fields.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/authn/policies", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing auth policies: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "auth_policies_get", "Get a single JumpCloud authentication policy by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/authn/policies/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting auth policy: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "auth_policies_create", "Create a new JumpCloud authentication policy. Conditions are specified as a raw JSON string.",
		func(ctx context.Context, req *mcp.CallToolRequest, args authPolicyCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name":     args.Name,
				"disabled": args.Disabled,
			}
			if args.Type != "" {
				body["type"] = args.Type
			}
			if args.Conditions != "" {
				var cond any
				if err := json.Unmarshal([]byte(args.Conditions), &cond); err != nil {
					return errorResult(fmt.Sprintf("invalid conditions JSON: %v", err)), nil, nil
				}
				body["conditions"] = cond
			}
			if args.MFA {
				body["mfa"] = map[string]any{"required": true}
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/authn/policies", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating auth policy: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "auth_policies_update", "Update a JumpCloud authentication policy. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args authPolicyUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.Conditions != "" {
				var cond any
				if err := json.Unmarshal([]byte(args.Conditions), &cond); err != nil {
					return errorResult(fmt.Sprintf("invalid conditions JSON: %v", err)), nil, nil
				}
				body["conditions"] = cond
			}
			if args.Disabled != nil {
				body["disabled"] = *args.Disabled
			}
			if !args.Execute {
				return planResult("update", "auth policy", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/authn/policies/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating auth policy: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "auth_policies_delete", "Delete a JumpCloud authentication policy. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "auth policy", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/authn/policies/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting auth policy: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Auth policy %q deleted successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "auth_policies_simulate", "Simulate whether a user would be allowed or denied by an authentication policy. Evaluates policy conditions locally against the provided context.",
		func(ctx context.Context, req *mcp.CallToolRequest, args simulateInput) (*mcp.CallToolResult, any, error) {
			v2Client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V2 client: %v", err)), nil, nil
			}
			v1Client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V1 client: %v", err)), nil, nil
			}

			// Resolve policy.
			r := resolve.NewV2Resolver(v2Client)
			policyID, err := r.Resolve(ctx, args.Policy, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			policyRaw, err := v2Client.Get(ctx, "/authn/policies/"+policyID)
			if err != nil {
				return errorResult(fmt.Sprintf("getting policy: %v", err)), nil, nil
			}

			var policy simulator.Policy
			if err := json.Unmarshal(policyRaw, &policy); err != nil {
				return errorResult(fmt.Sprintf("parsing policy: %v", err)), nil, nil
			}

			// Resolve user.
			userID, err := resolveV1(ctx, v1Client, args.User, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			// Fetch user's group memberships.
			memberResult, _ := v2Client.ListAll(ctx, "/users/"+userID+"/memberof", api.V2ListOptions{})
			var userGroups []string
			if memberResult != nil {
				for _, raw := range memberResult.Data {
					var assoc struct {
						ID   string `json:"id"`
						Type string `json:"type"`
					}
					if err := json.Unmarshal(raw, &assoc); err == nil && assoc.Type == "user_group" {
						userGroups = append(userGroups, assoc.ID)
					}
				}
			}

			simCtx := simulator.SimulationContext{
				UserID:     userID,
				UserGroups: userGroups,
				IP:         args.IP,
				Location:   args.Location,
			}

			// IP resolver.
			ipResolver := func(listID string) ([]string, error) {
				raw, err := v2Client.Get(ctx, "/iplists/"+listID)
				if err != nil {
					return nil, err
				}
				var ipList struct {
					IPs []string `json:"ips"`
				}
				if err := json.Unmarshal(raw, &ipList); err != nil {
					return nil, err
				}
				return ipList.IPs, nil
			}

			result := simulator.EvaluatePolicy(policy, simCtx, ipResolver)
			res, err := jsonResult(result)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "auth_policies_blast_radius", "Analyze which users are affected by an authentication policy. Returns the list of users in the policy's target groups.",
		func(ctx context.Context, req *mcp.CallToolRequest, args blastRadiusInput) (*mcp.CallToolResult, any, error) {
			v2Client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V2 client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(v2Client)
			policyID, err := r.Resolve(ctx, args.Policy, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			policyRaw, err := v2Client.Get(ctx, "/authn/policies/"+policyID)
			if err != nil {
				return errorResult(fmt.Sprintf("getting policy: %v", err)), nil, nil
			}

			var policy struct {
				Name    string `json:"name"`
				Targets struct {
					UserGroups []string `json:"userGroups"`
					AllUsers   bool     `json:"allUsers"`
				} `json:"targets"`
			}
			if err := json.Unmarshal(policyRaw, &policy); err != nil {
				return errorResult(fmt.Sprintf("parsing policy: %v", err)), nil, nil
			}

			limit := args.Limit
			if limit == 0 {
				limit = 100
			}

			if policy.Targets.AllUsers {
				res, err := jsonResult(map[string]any{
					"all_users": true,
					"message":   fmt.Sprintf("Policy %q targets ALL users.", policy.Name),
				})
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				return res, nil, nil
			}

			if len(policy.Targets.UserGroups) == 0 {
				res, err := jsonResult(map[string]any{
					"groups":  0,
					"members": 0,
					"message": fmt.Sprintf("Policy %q has no target user groups.", policy.Name),
				})
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				return res, nil, nil
			}

			seen := make(map[string]bool)
			var members []json.RawMessage
			for _, groupID := range policy.Targets.UserGroups {
				if len(members) >= limit {
					break
				}
				result, err := v2Client.ListAll(ctx, "/usergroups/"+groupID+"/members", api.V2ListOptions{})
				if err != nil {
					continue
				}
				for _, raw := range result.Data {
					var m struct {
						ID string `json:"id"`
					}
					if err := json.Unmarshal(raw, &m); err == nil && !seen[m.ID] {
						seen[m.ID] = true
						members = append(members, raw)
						if len(members) >= limit {
							break
						}
					}
				}
			}

			return rawListResult(members, len(members))
		},
	)
}

func (s *Server) registerIPListTools() {
	addTypedTool(s, "iplists_list", "List all JumpCloud IP lists. Returns objects with id, name, description.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/iplists", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing IP lists: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "iplists_get", "Get a single JumpCloud IP list by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.IPListConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/iplists/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting IP list: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "iplists_create", "Create a new JumpCloud IP list. IPs can be single addresses, CIDR ranges, or IP ranges.",
		func(ctx context.Context, req *mcp.CallToolRequest, args ipListCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name": args.Name,
				"ips":  args.IPs,
			}
			if args.Description != "" {
				body["description"] = args.Description
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/iplists", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating IP list: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "iplists_update", "Update a JumpCloud IP list. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args ipListUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.IPListConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.Description != "" {
				body["description"] = args.Description
			}
			if len(args.IPs) > 0 {
				body["ips"] = args.IPs
			}
			if !args.Execute {
				return planResult("update", "IP list", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/iplists/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating IP list: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "iplists_delete", "Delete a JumpCloud IP list. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.IPListConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "IP list", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/iplists/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting IP list: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("IP list %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerSoftwareTools() {
	addTypedTool(s, "software_list", "List all JumpCloud software apps. Returns objects with id, displayName, settings, createdAt, updatedAt.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/softwareapps", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing software apps: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "software_get", "Get a single JumpCloud software app by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.SoftwareAppConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/softwareapps/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting software app: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "software_create", "Create a new JumpCloud software app. Settings is a raw JSON array of package configurations.",
		func(ctx context.Context, req *mcp.CallToolRequest, args softwareCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"displayName": args.Name,
			}
			if args.Settings != "" {
				var settings any
				if err := json.Unmarshal([]byte(args.Settings), &settings); err != nil {
					return errorResult(fmt.Sprintf("invalid settings JSON: %v", err)), nil, nil
				}
				body["settings"] = settings
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/softwareapps", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating software app: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "software_update", "Update a JumpCloud software app. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args softwareUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.SoftwareAppConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["displayName"] = args.Name
			}
			if args.Settings != "" {
				var settings any
				if err := json.Unmarshal([]byte(args.Settings), &settings); err != nil {
					return errorResult(fmt.Sprintf("invalid settings JSON: %v", err)), nil, nil
				}
				body["settings"] = settings
			}
			if !args.Execute {
				return planResult("update", "software app", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/softwareapps/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating software app: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "software_delete", "Delete a JumpCloud software app. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.SoftwareAppConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "software app", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/softwareapps/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting software app: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Software app %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerLDAPTools() {
	addTypedTool(s, "ldap_list", "List all JumpCloud LDAP servers. Returns objects with id, name, userLockoutAction, userPasswordExpirationAction.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/ldapservers", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing LDAP servers: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "ldap_get", "Get a single JumpCloud LDAP server by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.LDAPServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/ldapservers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting LDAP server: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ldap_create", "Create a new JumpCloud LDAP server.",
		func(ctx context.Context, req *mcp.CallToolRequest, args ldapCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name": args.Name,
			}
			if args.UserLockoutAction != "" {
				body["userLockoutAction"] = args.UserLockoutAction
			}
			if args.UserPasswordExpirationAction != "" {
				body["userPasswordExpirationAction"] = args.UserPasswordExpirationAction
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/ldapservers", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating LDAP server: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ldap_update", "Update a JumpCloud LDAP server. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args ldapUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.LDAPServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.UserLockoutAction != "" {
				body["userLockoutAction"] = args.UserLockoutAction
			}
			if args.UserPasswordExpirationAction != "" {
				body["userPasswordExpirationAction"] = args.UserPasswordExpirationAction
			}
			if !args.Execute {
				return planResult("update", "LDAP server", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/ldapservers/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating LDAP server: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ldap_delete", "Delete a JumpCloud LDAP server. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.LDAPServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "LDAP server", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/ldapservers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting LDAP server: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("LDAP server %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerADTools() {
	addTypedTool(s, "ad_list", "List all JumpCloud Active Directory integrations. Returns objects with id, domain, useCase, groupsEnabled, delegationState.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/activedirectories", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing Active Directory integrations: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "ad_get", "Get a single JumpCloud Active Directory integration by domain or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.ActiveDirectoryConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/activedirectories/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting Active Directory: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ad_create", "Create a new JumpCloud Active Directory integration.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"domain": args.Domain,
			}
			if args.UseCase != "" {
				body["useCase"] = args.UseCase
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/activedirectories", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating Active Directory: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ad_update", "Update a JumpCloud Active Directory integration. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.ActiveDirectoryConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.UseCase != "" {
				body["useCase"] = args.UseCase
			}
			if args.GroupsEnabled != nil {
				body["groupsEnabled"] = *args.GroupsEnabled
			}
			if !args.Execute {
				return planResult("update", "Active Directory", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/activedirectories/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating Active Directory: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ad_delete", "Delete a JumpCloud Active Directory integration. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.ActiveDirectoryConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "Active Directory", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/activedirectories/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting Active Directory: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Active Directory %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerOrgTools() {
	addTypedTool(s, "org_list", "List JumpCloud organizations. Returns objects with _id, displayName, created.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			result, err := client.ListAll(ctx, "/organizations", api.ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing organizations: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "org_get", "Get a JumpCloud organization by ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Get(ctx, "/organizations/"+args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("getting organization: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)
}

func (s *Server) registerAdminTools() {
	addTypedTool(s, "admins_list", "List all JumpCloud administrators. Returns objects with id, email, role, enableMultiFactor.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV1ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/users", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing admins: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "admins_get", "Get a single JumpCloud administrator by email or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.AdminConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/users/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting admin: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "admins_create", "Create a new JumpCloud administrator.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adminCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"email": args.Email,
			}
			if args.Role != "" {
				body["roleName"] = args.Role
			}
			if args.EnableMFA {
				body["enableMultiFactor"] = true
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/users", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating admin: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "admins_update", "Update a JumpCloud administrator. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adminUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.AdminConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Role != "" {
				body["roleName"] = args.Role
			}
			if args.EnableMFA != nil {
				body["enableMultiFactor"] = *args.EnableMFA
			}
			if !args.Execute {
				return planResult("update", "admin", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/users/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating admin: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "admins_delete", "Delete a JumpCloud administrator. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.AdminConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "admin", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/users/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting admin: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Admin %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerRecipeTools() {
	addTypedTool(s, "recipe_run", "Run a named jc recipe with parameters. Recipes are multi-step automated workflows.",
		func(ctx context.Context, req *mcp.CallToolRequest, args recipeRunInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			recipes, err := recipe.LoadAll()
			if err != nil {
				return errorResult(fmt.Sprintf("loading recipes: %v", err)), nil, nil
			}
			r := recipe.FindByName(recipes, args.Name)
			if r == nil {
				names := make([]string, 0, len(recipes))
				for _, rec := range recipes {
					names = append(names, rec.Name)
				}
				return errorResult(fmt.Sprintf("recipe %q not found. Available recipes: %s", args.Name, strings.Join(names, ", "))), nil, nil
			}
			params := args.Params
			if params == nil {
				params = map[string]string{}
			}
			// Preview the recipe steps.
			plans, err := r.Plan(params)
			if err != nil {
				return errorResult(fmt.Sprintf("planning recipe: %v", err)), nil, nil
			}
			res, err := jsonResult(plans)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)
}

func (s *Server) registerMetaTools() {
	addTypedTool(s, "plan", "Preview what a jc command would do without executing it. Returns a structured plan showing the action, target, and effects.",
		func(ctx context.Context, req *mcp.CallToolRequest, args planInput) (*mcp.CallToolResult, any, error) {
			parts := strings.Fields(args.Command)
			if len(parts) == 0 {
				return errorResult("empty command string"), nil, nil
			}
			result := map[string]any{
				"command":     args.Command,
				"description": describeCommand(parts),
				"note":        "Use the specific tool with execute=true to perform this action.",
			}
			res, err := jsonResult(result)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "explain", "Explain what a jc command would do in plain English. Does not execute anything.",
		func(ctx context.Context, req *mcp.CallToolRequest, args explainInput) (*mcp.CallToolResult, any, error) {
			parts := strings.Fields(args.Command)
			if len(parts) == 0 {
				return errorResult("empty command string"), nil, nil
			}
			explanation := describeCommand(parts)
			return textResult(explanation), nil, nil
		},
	)
}

// --- Helper functions ---

func resolveV1(ctx context.Context, client *api.V1Client, identifier string, cfg resolve.ResourceConfig) (string, error) {
	r := resolve.NewResolver(client)
	return r.Resolve(ctx, identifier, cfg)
}

func buildV1ListOptions(args listInput) (api.ListOptions, error) {
	opts := api.ListOptions{
		Limit: args.Limit,
		Sort:  args.Sort,
	}
	if len(args.Filter) > 0 {
		exprs, err := filter.ParseAll(args.Filter)
		if err != nil {
			return opts, fmt.Errorf("invalid filter: %w", err)
		}
		opts.Filter = filter.ToV1Queries(exprs)
	}
	return opts, nil
}

func buildV2ListOptions(args listInput) (api.V2ListOptions, error) {
	opts := api.V2ListOptions{
		Limit: args.Limit,
		Sort:  args.Sort,
	}
	if len(args.Filter) > 0 {
		exprs, err := filter.ParseAll(args.Filter)
		if err != nil {
			return opts, fmt.Errorf("invalid filter: %w", err)
		}
		opts.Filter = filter.ToV2Queries(exprs)
	}
	return opts, nil
}

func resolveTimeRange(last, start, end string) (string, string, error) {
	if last == "" && start == "" {
		return "", "", fmt.Errorf("either last or start is required for time range")
	}
	if last != "" && start != "" {
		return "", "", fmt.Errorf("last and start are mutually exclusive")
	}
	if last != "" {
		t, err := api.ParseTimeRange(last)
		if err != nil {
			return "", "", err
		}
		return t.UTC().Format("2006-01-02T15:04:05Z"), api.InsightsNowFunc().UTC().Format("2006-01-02T15:04:05Z"), nil
	}
	startTime, err := api.ParseTimeRange(start)
	if err != nil {
		return "", "", fmt.Errorf("invalid start time: %w", err)
	}
	endStr := api.InsightsNowFunc().UTC().Format("2006-01-02T15:04:05Z")
	if end != "" {
		endTime, err := api.ParseTimeRange(end)
		if err != nil {
			return "", "", fmt.Errorf("invalid end time: %w", err)
		}
		endStr = endTime.UTC().Format("2006-01-02T15:04:05Z")
	}
	return startTime.UTC().Format("2006-01-02T15:04:05Z"), endStr, nil
}

// rawListResult creates a JSON result from raw API response items.
func rawListResult(data []json.RawMessage, total int) (*mcp.CallToolResult, any, error) {
	result := map[string]any{
		"data":  data,
		"total": total,
	}
	if data == nil {
		result["data"] = []json.RawMessage{}
	}
	res, err := jsonResult(result)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return res, nil, nil
}

// planResult creates a plan preview result for destructive operations.
func planResult(action, resource, identifier, resolvedID string, effects any) (*mcp.CallToolResult, any, error) {
	plan := map[string]any{
		"plan":        true,
		"action":      action,
		"resource":    resource,
		"target":      identifier,
		"resolved_id": resolvedID,
		"message":     fmt.Sprintf("This would %s %s %q (ID: %s). Pass execute=true to proceed.", action, resource, identifier, resolvedID),
	}
	if effects != nil {
		plan["effects"] = effects
	}
	res, err := jsonResult(plan)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return res, nil, nil
}

// describeCommand generates a plain-English description of a jc command.
func describeCommand(parts []string) string {
	if len(parts) == 0 {
		return "Empty command."
	}

	descriptions := map[string]map[string]string{
		"users": {
			"list":           "List all JumpCloud system users with optional filtering and sorting.",
			"get":            "Retrieve a single JumpCloud user by username or ID.",
			"create":         "Create a new JumpCloud user account.",
			"update":         "Update fields on an existing JumpCloud user.",
			"delete":         "Permanently delete a JumpCloud user. This removes the user from all groups and unbinds all devices. IRREVERSIBLE.",
			"search":         "Search for users by keyword across username, email, firstname, and lastname.",
			"lock":           "Lock a user account, preventing login.",
			"unlock":         "Unlock a previously locked user account.",
			"reset-mfa":      "Reset MFA/TOTP enrollment. The user will need to re-enroll their authenticator.",
			"reset-password": "Trigger a password reset email to the user.",
		},
		"devices": {
			"list":    "List all JumpCloud-managed devices (systems) with optional filtering.",
			"get":     "Retrieve a single device by hostname or ID.",
			"delete":  "Remove a device record from JumpCloud.",
			"lock":    "Send an MDM lock command to the device.",
			"restart": "Send an MDM restart command to the device.",
			"erase":   "Send an MDM erase (wipe) command to the device. EXTREMELY DESTRUCTIVE — wipes all data.",
		},
		"groups": {
			"list":          "List all user groups and device groups.",
			"add-member":    "Add a user or device to a group.",
			"remove-member": "Remove a user or device from a group.",
		},
		"insights": {
			"query": "Query Directory Insights events (audit log) for a given service and time range.",
			"count": "Count events matching criteria without returning full records.",
		},
		"commands": {
			"list": "List all JumpCloud commands.",
			"run":  "Trigger a command to run on specified devices or device groups.",
		},
		"policies": {
			"list": "List all JumpCloud policies with name, type, and OS target.",
		},
		"auth-policies": {
			"list":         "List all JumpCloud authentication policies for conditional access.",
			"get":          "Get an authentication policy by name or ID.",
			"create":       "Create a new authentication policy with conditions and targets.",
			"update":       "Update an existing authentication policy.",
			"delete":       "Delete an authentication policy. IRREVERSIBLE.",
			"enable":       "Enable a disabled authentication policy.",
			"disable":      "Disable an authentication policy.",
			"simulate":     "Simulate whether a user would be allowed/denied by a policy.",
			"blast-radius": "Show which users are affected by a policy.",
		},
		"iplists": {
			"list":   "List all JumpCloud IP lists used by authentication policies.",
			"get":    "Get an IP list by name or ID.",
			"create": "Create a new IP list with IP entries.",
			"update": "Update an existing IP list.",
			"delete": "Delete an IP list. IRREVERSIBLE.",
		},
		"software": {
			"list":   "List all JumpCloud software apps.",
			"get":    "Get a software app by name or ID.",
			"create": "Create a new software app with package settings.",
			"update": "Update an existing software app.",
			"delete": "Delete a software app. IRREVERSIBLE.",
		},
		"ldap": {
			"list":   "List all JumpCloud LDAP servers.",
			"get":    "Get an LDAP server by name or ID.",
			"create": "Create a new LDAP server.",
			"update": "Update an existing LDAP server.",
			"delete": "Delete an LDAP server. IRREVERSIBLE.",
		},
		"ad": {
			"list":   "List all JumpCloud Active Directory integrations.",
			"get":    "Get an Active Directory integration by domain or ID.",
			"create": "Create a new Active Directory integration.",
			"update": "Update an existing Active Directory integration.",
			"delete": "Delete an Active Directory integration. IRREVERSIBLE.",
		},
		"org": {
			"list": "List JumpCloud organizations.",
			"get":  "Get organization details by ID.",
		},
		"admins": {
			"list":   "List all JumpCloud administrators.",
			"get":    "Get an administrator by email or ID.",
			"create": "Create a new administrator account.",
			"update": "Update an administrator's role or settings.",
			"delete": "Delete an administrator account. IRREVERSIBLE.",
		},
		"recipe": {
			"run": "Execute a multi-step automated recipe with parameters.",
		},
	}

	resource := parts[0]
	if resourceDescs, ok := descriptions[resource]; ok {
		if len(parts) > 1 {
			verb := parts[1]
			if desc, ok := resourceDescs[verb]; ok {
				return desc
			}
		}
		return fmt.Sprintf("Manage JumpCloud %s.", resource)
	}
	return fmt.Sprintf("Run jc command: %s", strings.Join(parts, " "))
}

// --- Tool infrastructure (addTool, addTypedTool, result helpers) ---

// addTool wraps mcp.AddTool with rate limiting and audit logging.
// Tools that are filtered out by the allow/block list are not registered.
func (s *Server) addTool(name, description string, handler func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error)) {
	if !s.toolFilter.isAllowed(name) {
		return
	}

	tool := &mcp.Tool{
		Name:        name,
		Description: description,
	}

	wrappedHandler := func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		// Rate limit check.
		if err := s.limiter.allow(); err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
			return errorResult(err.Error()), nil, nil
		}

		// Execute the tool.
		result, out, err := handler(ctx, req, args)

		// Audit log.
		if err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
		} else if result != nil && result.IsError {
			errMsg := "tool error"
			if len(result.Content) > 0 {
				if tc, ok := result.Content[0].(*mcp.TextContent); ok {
					errMsg = tc.Text
				}
			}
			s.auditLog.log(name, req.Params.Arguments, false, errMsg)
		} else {
			s.auditLog.log(name, req.Params.Arguments, true, "")
		}

		return result, out, err
	}

	mcp.AddTool(s.mcpServer, tool, wrappedHandler)
	s.toolNames = append(s.toolNames, name)
}

// addTypedTool wraps mcp.AddTool with typed input args, rate limiting, and audit logging.
// Tools that are filtered out by the allow/block list are not registered.
func addTypedTool[In any](s *Server, name, description string, handler func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error)) {
	if !s.toolFilter.isAllowed(name) {
		return
	}

	tool := &mcp.Tool{
		Name:        name,
		Description: description,
	}

	wrappedHandler := func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error) {
		// Rate limit check.
		if err := s.limiter.allow(); err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
			return errorResult(err.Error()), nil, nil
		}

		// Execute the tool.
		result, out, err := handler(ctx, req, args)

		// Audit log.
		if err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
		} else if result != nil && result.IsError {
			errMsg := "tool error"
			if len(result.Content) > 0 {
				if tc, ok := result.Content[0].(*mcp.TextContent); ok {
					errMsg = tc.Text
				}
			}
			s.auditLog.log(name, req.Params.Arguments, false, errMsg)
		} else {
			s.auditLog.log(name, req.Params.Arguments, true, "")
		}

		return result, out, err
	}

	mcp.AddTool(s.mcpServer, tool, wrappedHandler)
	s.toolNames = append(s.toolNames, name)
}

// textResult creates a simple text result.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

// jsonResult creates a JSON result from a value.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}, nil
}

// errorResult creates an error result.
func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
		IsError: true,
	}
}
