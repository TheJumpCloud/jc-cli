package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/pwm"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// Password Manager tools. READS ONLY, and deliberately so: the API can
// CREATE a shared folder and offers no way to delete one, so a write here
// cannot be undone. Six further operations — product deactivation, a
// reinvite that emails a real person, and four cloud-backup encryption-key
// writes — are held back for the same reason.
//
// Descriptions here avoid naming sibling tools. A cross-reference reads
// well to a human and moves the referenced tool's queries onto the
// referring one: a verification pass found a lock tool winning "remove a
// user who left" purely because its description mentioned deletion.

type pwmUserInput struct {
	Identifier string `json:"identifier" jsonschema:"The person: a Password Manager display name, a directory username or email, a Password Manager UUID, or a JumpCloud 24-character user ID. jc bridges the two id systems through externalId."`
}

type pwmFolderInput struct {
	Identifier string `json:"identifier" jsonschema:"Shared folder name, or its Password Manager UUID"`
}

type pwmPoliciesInput struct {
	Scope string `json:"scope,omitempty" jsonschema:"Which settings to read: 'company' for the org-wide vault settings including whether export is disabled, or 'folders' for the shared-folder rules. Defaults to company."`
}

// pwmFetcher adapts an API client to the contract package's Fetcher.
func pwmFetcher(client *api.V2Client) pwm.Fetcher {
	return func(ctx context.Context, endpoint string) (json.RawMessage, error) {
		return client.Get(ctx, endpoint)
	}
}

// pwmDirectoryResolver crosses to the directory. Users are a V1 resource,
// so this is the V1 resolver.
func pwmDirectoryResolver(ctx context.Context, name string) (string, error) {
	v1, err := newV1ClientFunc()
	if err != nil {
		return "", err
	}
	return resolve.NewResolver(v1).Resolve(ctx, name, resolve.UserConfig)
}

// pwmRead is the shared open-fetch-return path for the no-argument tools
// that return a bare object rather than a list.
func pwmRead(ctx context.Context, endpoint, what string) (*mcp.CallToolResult, any, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
	}
	raw, err := client.Get(ctx, endpoint)
	if err != nil {
		return errorResult(fmt.Sprintf("reading %s: %v", what, err)), nil, nil
	}
	return textResult(string(raw)), nil, nil
}

// pwmReadList fetches a list endpoint and returns the standard envelope.
func pwmReadList(ctx context.Context, endpoint, what string) (*mcp.CallToolResult, any, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
	}
	raw, err := client.Get(ctx, endpoint)
	if err != nil {
		return errorResult(fmt.Sprintf("listing %s: %v", what, err)), nil, nil
	}
	rows, total, err := pwm.ParseList(raw)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return listEnvelope(rows, &total)
}

// pwmResolveUser resolves an identifier to a Password Manager user through
// the shared bridge, so this server and the CLI answer "ada" identically.
func pwmResolveUser(ctx context.Context, identifier string) (pwm.User, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return pwm.User{}, err
	}
	return pwm.ResolveUser(ctx, pwmFetcher(client), pwmDirectoryResolver, identifier)
}

func pwmResolveFolder(ctx context.Context, identifier string) (string, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return "", err
	}
	return pwm.ResolveFolder(ctx, pwmFetcher(client), identifier)
}

func (s *Server) registerPasswordManagerTools() {
	addTypedTool(s, "password_manager_overview", "Org-wide health summary of JumpCloud Password Manager, the credential vault: total counts of enrolled people and groups, shared folders and stored passwords, plus the hygiene numbers — weak, old and reused credentials and an overall score. Aggregate totals only; it names nobody. Start here for how healthy the vault is. Returns a bare object, not a list envelope.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pwmRead(ctx, pwm.OverviewEndpoint, "the Password Manager overview")
		},
	)

	addTypedTool(s, "password_manager_users_list", "Who is using the Password Manager? Lists the named people enrolled in the vault, one row each, with their stored-item count, credential-hygiene score and weak or ageing credential counts. Use this to find out who is actually on it and who is not. Enrolment is NOT the same as having a JumpCloud account — a directory user who never opened the vault does not appear here at all. Each record carries externalId, the only link back to the directory.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pwmReadList(ctx, pwm.UsersEndpoint, "Password Manager users")
		},
	)

	addTypedTool(s, "password_manager_user_get", "One person's Password Manager enrolment: their vault identity, status, stored-item and credential counts, hygiene score and the groups they belong to. Accepts a display name, a directory username or email, or either id format — Password Manager records are keyed by UUID while the directory uses 24-character hex, and jc crosses between them through externalId so you do not have to. An identifier that names a real JumpCloud person who never enrolled says so explicitly rather than reporting nothing found.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwmUserInput) (*mcp.CallToolResult, any, error) {
			u, err := pwmResolveUser(ctx, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return pwmRead(ctx, pwm.UserEndpoint(u.ID), "the Password Manager user")
		},
	)

	addTypedTool(s, "password_manager_user_folders", "Which shared folders one person can reach in the vault. This is the access question — use it to answer 'what credentials can this person see' during an offboarding review or an access audit, since folder membership is what actually grants sight of a stored credential. Takes the same flexible identifier as the rest of this area.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwmUserInput) (*mcp.CallToolResult, any, error) {
			u, err := pwmResolveUser(ctx, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return pwmReadList(ctx, pwm.UserSharedFoldersEndpoint(u.ID), "that person's shared folders")
		},
	)

	addTypedTool(s, "password_manager_user_items", "How many things one person has stored in their vault, broken down by kind. Counts only — the stored secrets themselves are never returned by this API and cannot be read through jc.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwmUserInput) (*mcp.CallToolResult, any, error) {
			u, err := pwmResolveUser(ctx, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return pwmReadList(ctx, pwm.UserItemsEndpoint(u.ID), "that person's stored items")
		},
	)

	addTypedTool(s, "password_manager_groups_list", "Groups enrolled in the Password Manager vault. LIST ONLY — JumpCloud serves no endpoint for fetching a single Password Manager group, so the row in this listing is all there is; there is no deeper detail to ask for and a request for one returns 404.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pwmReadList(ctx, pwm.GroupsEndpoint, "Password Manager groups")
		},
	)

	addTypedTool(s, "password_manager_folders_list", "Every shared folder in the org's vault — the containers through which teams share credentials with each other. A folder is the unit of shared access, so this is the inventory to start from when auditing who can see what. Note that folders visible here are the org's shared ones; a person's private vault contents are not addressable through the API at all.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pwmReadList(ctx, pwm.SharedFoldersEndpoint, "shared folders")
		},
	)

	addTypedTool(s, "password_manager_folder_get", "One shared folder's record, by folder name or UUID. Password Manager spells a folder's identity three different ways across its endpoints, so pass the name and let jc pick the right one rather than copying an id between calls.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwmFolderInput) (*mcp.CallToolResult, any, error) {
			id, err := pwmResolveFolder(ctx, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return pwmRead(ctx, pwm.FolderEndpoint(id), "the shared folder")
		},
	)

	addTypedTool(s, "password_manager_folder_users", "The people who can reach one shared folder, named by the folder. This is the reverse of asking what one person can see: given a folder holding sensitive credentials, it answers who has sight of it — the direct question for a quarterly access review.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwmFolderInput) (*mcp.CallToolResult, any, error) {
			id, err := pwmResolveFolder(ctx, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return pwmReadList(ctx, pwm.FolderUsersEndpoint(id), "the folder's users")
		},
	)

	addTypedTool(s, "password_manager_folder_groups", "The groups granted access to one shared folder, named by the folder. Group grants are the usual way access is given at scale, so a folder that looks unshared by person may still be reachable by a whole team through this list.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwmFolderInput) (*mcp.CallToolResult, any, error) {
			id, err := pwmResolveFolder(ctx, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return pwmReadList(ctx, pwm.FolderGroupsEndpoint(id), "the folder's groups")
		},
	)

	addTypedTool(s, "password_manager_items", "Stored vault entries across the whole org. This endpoint returns BOTH a results array and a separate items array, and both were empty on every tenant probed, so which one carries the records when populated is not established. The response reports both rather than picking one and being confidently wrong; if you get data in exactly one of them, that is the answer.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, pwm.ItemsEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing stored items: %v", err)), nil, nil
			}
			results, items, total, perr := pwm.ParseItems(raw)
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			res, jerr := jsonResult(map[string]any{
				"results":           results,
				"items":             items,
				"returned_results":  len(results),
				"returned_items":    len(items),
				"total":             total,
				"which_array_holds": "unestablished — both were empty on every tenant probed",
			})
			if jerr != nil {
				return errorResult(jerr.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "password_manager_policies", "The vault's own configuration: with scope 'company', the org-wide settings including whether members are allowed to export their vault; with scope 'folders', the rules governing shared folders. These are Password Manager product settings, not directory credential-complexity or rotation requirements. Returns a bare object, not a list envelope.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwmPoliciesInput) (*mcp.CallToolResult, any, error) {
			endpoint, what := pwm.CompanyPolicyEndpoint, "the company vault settings"
			switch args.Scope {
			case "", "company":
			case "folders":
				endpoint, what = pwm.FolderPolicyEndpoint, "the shared-folder settings"
			default:
				return errorResult(fmt.Sprintf("scope %q is not one of: company, folders", args.Scope)), nil, nil
			}
			return pwmRead(ctx, endpoint, what)
		},
	)

	addTypedTool(s, "password_manager_backup_keys", "The org's cloud backup key records for the vault. READ ONLY, and deliberately so: the four write operations on this family manage encryption key material, are not undoable, and are not exposed through jc at all.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pwmReadList(ctx, pwm.BackupKeysEndpoint, "cloud backup keys")
		},
	)
}
