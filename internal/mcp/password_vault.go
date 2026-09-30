package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/passwordvault"
)

// Password Vault tools — the tenant scope. Credentials, folders and websites
// follow in their own PRs, which is also where the secret-retrieval endpoints
// land; those will be CLI-only.
//
// Password Vault replaces Password Manager, and on a migrated org every
// /passwordmanager endpoint returns 404. Both tool sets are kept while
// customers move across.
//
// Every tool but the status gate checks activation first, so a model asking
// about an unactivated org gets one sentence naming the gate instead of a 404
// from whichever endpoint it happened to try.

type pwvGroupInput struct {
	GroupID int `json:"group_id" jsonschema:"The group's Password Vault id. It is an INTEGER here, not a JumpCloud 24-character object id and not the UUID Password Manager uses. Take it from password_vault_groups_list."`
}

type pwvAssignableInput struct {
	GroupID  int    `json:"group_id" jsonschema:"The group's integer Password Vault id"`
	Category string `json:"category" jsonschema:"Resource category to list, e.g. CREDENTIAL or WEBSITE. Required — the API rejects the call without one — but NOT validated: an unrecognised category returns an empty list rather than an error."`
}

type pwvTagsInput struct {
	TargetKind string `json:"target_kind" jsonschema:"Which tags to list: CREDENTIAL or RESOURCE. Required; this one the API does validate."`
}

func (s *Server) registerPasswordVaultTools() {
	addTypedTool(s, "password_vault_status", "Whether Password Vault is active on this organization, and the console URL for it. This is the ONLY Password Vault endpoint that answers before activation — every other one returns 404 until the product is enabled — so it is how \"not activated\" is told apart from \"not entitled\" or \"wrong path\". Password Vault is JumpCloud's replacement for Password Manager; on an organization that has migrated, every password_manager_* tool returns 404 and this one reports active.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := pwvGetMCPRaw(ctx, passwordvault.StatusEndpoint, nil)
			if err != nil {
				return errorResult(fmt.Sprintf("reading the Password Vault status: %v", err)), nil, nil
			}
			if _, perr := passwordvault.ParseStatus(raw); perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "password_vault_settings", "The organization-wide Password Vault settings: whether users may store private secrets, whether credential and website export are permitted, browser-extension and mobile access, and the security-alert notification switches. These govern what every vault user in the org can do, so they are the place to look before concluding a user is blocked by their own permissions.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := pwvGetMCP(ctx, passwordvault.TenantSettingsEndpoint, nil)
			if err != nil {
				return errorResult(fmt.Sprintf("reading the Password Vault settings: %v", err)), nil, nil
			}
			inner, perr := passwordvault.ParseWrapped(raw, "values", "tenant settings")
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return textResult(string(inner)), nil, nil
		},
	)

	addTypedTool(s, "password_vault_default_permissions", "The permission defaults applied to newly created resources, per category — websites, computers, devices, databases, cloud services, social networks and credentials — plus whether users are allowed to remove them. On a freshly activated organization every value is an empty string, which means \"no default set\", not \"could not be read\".",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := pwvGetMCP(ctx, passwordvault.DefaultPermsEndpoint, nil)
			if err != nil {
				return errorResult(fmt.Sprintf("reading the default permissions: %v", err)), nil, nil
			}
			inner, perr := passwordvault.ParseWrapped(raw, "defaultPermissions", "default permissions")
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return textResult(string(inner)), nil, nil
		},
	)

	addTypedTool(s, "password_vault_overview", "The Password Vault dashboard summary: credential and resource counts by type, org and per-user password-health scores, and the credentials that are weak, expiring, expired or unused. Start here when asked how an organization's password hygiene looks.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := pwvGetMCP(ctx, passwordvault.DashboardOverviewEndpoint, nil)
			if err != nil {
				return errorResult(fmt.Sprintf("reading the Password Vault overview: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "password_vault_users_list", "The users enrolled in Password Vault. IMPORTANT: the API includes the CALLING user in the results whether or not they match, while the count excludes them when they do not — so a search matching nobody returns one row with a count of zero, and the number of rows can exceed the count. Read the count, not the length of the list. The list is fetched unpaginated because this area ignores sort and its paging duplicates and omits records.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := pwvGetMCP(ctx, passwordvault.UsersEndpoint, nil)
			if err != nil {
				return errorResult(fmt.Sprintf("listing Password Vault users: %v", err)), nil, nil
			}
			rows, total, perr := passwordvault.ParseList(raw, "Password Vault users")
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "password_vault_user_self", "The Password Vault record for the identity this API key belongs to, including its integer vault id — which is the id other vault objects reference, and is not the JumpCloud user object id.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := pwvGetMCP(ctx, passwordvault.UsersSelfEndpoint, nil)
			if err != nil {
				return errorResult(fmt.Sprintf("reading the vault user record: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "password_vault_groups_list", "The groups defined in Password Vault, each with its INTEGER id — the id every other group tool takes. Password Vault groups are numbered; they do not use JumpCloud 24-character object ids or the UUIDs Password Manager uses.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := pwvGetMCP(ctx, passwordvault.GroupsEndpoint, nil)
			if err != nil {
				return errorResult(fmt.Sprintf("listing Password Vault groups: %v", err)), nil, nil
			}
			rows, total, perr := passwordvault.ParseList(raw, "Password Vault groups")
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "password_vault_group_members", "The users in one Password Vault group. WARNING: a group id that does not exist returns an EMPTY LIST rather than an error, so an empty result is not evidence the group is real — confirm the id against password_vault_groups_list before reporting a group as empty.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwvGroupInput) (*mcp.CallToolResult, any, error) {
			return pwvGroupList(ctx, args.GroupID, passwordvault.GroupMembersEndpoint, "group members")
		},
	)

	addTypedTool(s, "password_vault_group_resources", "The credentials, websites and other resources one Password Vault group has been granted access to. Same warning as the members tool: a group id that does not exist returns an empty list rather than an error.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwvGroupInput) (*mcp.CallToolResult, any, error) {
			return pwvGroupList(ctx, args.GroupID, passwordvault.GroupResourcesEndpoint, "group resources")
		},
	)

	addTypedTool(s, "password_vault_group_assignable_resources", "The resources of one category that could be assigned to a group but are not yet. A category is REQUIRED — the API rejects the call without one — but it is NOT validated: an unrecognised category returns an empty list rather than an error, so an empty result here does not establish that the group has no assignable resources of that kind. State which category was asked for when reporting the answer.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwvAssignableInput) (*mcp.CallToolResult, any, error) {
			v, err := passwordvault.AssignableQuery(args.Category)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, gerr := pwvGetMCP(ctx, passwordvault.GroupAssignableEndpoint(args.GroupID), v)
			if gerr != nil {
				return errorResult(fmt.Sprintf("listing assignable resources: %v", gerr)), nil, nil
			}
			rows, total, perr := passwordvault.ParseList(raw, "assignable resources")
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "password_vault_tags_list", "The tags defined in the vault for one target kind. A target kind is REQUIRED — CREDENTIAL or RESOURCE — and unlike the assignable-resources category this one IS validated, so a wrong value is an error rather than an empty list. The API's own error names a bare targetKind query parameter that does not work; jc sends the form the server actually accepts.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwvTagsInput) (*mcp.CallToolResult, any, error) {
			v, err := passwordvault.TagsQuery(args.TargetKind)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, gerr := pwvGetMCP(ctx, passwordvault.TagsEndpoint, v)
			if gerr != nil {
				return errorResult(fmt.Sprintf("listing Password Vault tags: %v", gerr)), nil, nil
			}
			rows, perr := passwordvault.ParseResultsOnly(raw, "Password Vault tags")
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			total := len(rows)
			return listEnvelope(rows, &total)
		},
	)
}

// pwvGroupList is the shared body of the two group sub-listings.
func pwvGroupList(ctx context.Context, groupID int, endpoint func(int) string, what string) (*mcp.CallToolResult, any, error) {
	raw, err := pwvGetMCP(ctx, endpoint(groupID), nil)
	if err != nil {
		return errorResult(fmt.Sprintf("listing %s: %v", what, err)), nil, nil
	}
	rows, total, perr := passwordvault.ParseList(raw, what)
	if perr != nil {
		return errorResult(perr.Error()), nil, nil
	}
	return listEnvelope(rows, &total)
}

// pwvGetMCP reads through the activation gate.
func pwvGetMCP(ctx context.Context, endpoint string, v url.Values) (json.RawMessage, error) {
	raw, err := pwvGetMCPRaw(ctx, passwordvault.StatusEndpoint, nil)
	if err != nil {
		return nil, err
	}
	status, err := passwordvault.ParseStatus(raw)
	if err != nil {
		return nil, err
	}
	if !status.IsActive {
		return nil, passwordvault.ErrNotActivated()
	}
	return pwvGetMCPRaw(ctx, endpoint, v)
}

// pwvGetMCPRaw skips the gate — the status tool uses it.
func pwvGetMCPRaw(ctx context.Context, endpoint string, v url.Values) (json.RawMessage, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return nil, err
	}
	if len(v) > 0 {
		endpoint += "?" + v.Encode()
	}
	return client.Get(ctx, endpoint)
}
