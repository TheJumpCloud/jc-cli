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
