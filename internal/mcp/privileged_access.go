package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheJumpCloud/jc-cli/internal/privaccess"
)

// Privileged Access (PAM) tools, read-only. The contract and its workarounds
// live in internal/privaccess. No tool reads a secret, and credential
// listings always drop card and identity-document fields: tool output is
// model context.

type pamIdentInput struct {
	Resource string `json:"resource" jsonschema:"The resource's 24-character hex id or its exact name, from the matching list tool"`
}

type pamCredentialInput struct {
	ID string `json:"id" jsonschema:"The credential's 24-character hex id, from privileged_access_credentials_list"`
}

type pamCredentialPageInput struct {
	ID    string `json:"id" jsonschema:"The credential's 24-character hex id, from privileged_access_credentials_list"`
	Token string `json:"continuation_token,omitempty" jsonschema:"Continuation token from the previous page, if any"`
}

type pamGroupsInput struct {
	Product string `json:"product,omitempty" jsonschema:"Only groups for this product: PAM, or PWM for Password Vault. Omit for all."`
}

// pamClientMCP returns the v2 client after the activation gate.
func pamClientMCP(ctx context.Context) (privaccess.Getter, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return nil, err
	}
	if err := privaccess.RequireActive(ctx, client); err != nil {
		return nil, err
	}
	return client, nil
}

func pamList(ctx context.Context, what string,
	read func(context.Context, privaccess.Getter) ([]json.RawMessage, int, error)) (*mcp.CallToolResult, any, error) {
	client, err := pamClientMCP(ctx)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	rows, total, err := read(ctx, client)
	if err != nil {
		return errorResult(fmt.Sprintf("listing %s: %v", what, err)), nil, nil
	}
	return listEnvelope(rows, &total)
}

func pamSingle(ctx context.Context, what string,
	read func(context.Context, privaccess.Getter) (json.RawMessage, error)) (*mcp.CallToolResult, any, error) {
	client, err := pamClientMCP(ctx)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	raw, err := read(ctx, client)
	if err != nil {
		return errorResult(fmt.Sprintf("reading %s: %v", what, err)), nil, nil
	}
	return textResult(string(raw)), nil, nil
}

func pamTokenPage(ctx context.Context, what string, args pamCredentialPageInput,
	read func(context.Context, privaccess.Getter, string, string) (privaccess.TokenPage, error)) (*mcp.CallToolResult, any, error) {
	client, err := pamClientMCP(ctx)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	p, err := read(ctx, client, args.ID, args.Token)
	if err != nil {
		return errorResult(fmt.Sprintf("reading %s: %v", what, err)), nil, nil
	}
	out := map[string]any{"items": p.Items, "count": len(p.Items)}
	if p.Next != "" {
		out["continuationToken"] = p.Next
	}
	res, jerr := jsonResult(out)
	if jerr != nil {
		return errorResult(jerr.Error()), nil, nil
	}
	return res, nil, nil
}

func (s *Server) registerPrivilegedAccessTools() {
	addTypedTool(s, "privileged_access_status", "Whether JumpCloud Privileged Access (PAM) is active on this organization, expired, and whether Password Vault (isPwm) is on too. Every other privileged_access_* tool checks this first and reports one sentence if PAM is off.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := client.Get(ctx, privaccess.StatusEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("reading the Privileged Access status: %v", err)), nil, nil
			}
			if _, perr := privaccess.ParseStatus(raw); perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "privileged_access_dashboard", "Privileged Access at a glance: how many privileged servers, databases, websites, blocking rules, jump servers and users there are (with month-over-month change), connector health, and the most and least connected resources.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pamSingle(ctx, "the dashboard", func(ctx context.Context, c privaccess.Getter) (json.RawMessage, error) {
				raw, err := c.Get(ctx, privaccess.DashboardEndpoint)
				if err != nil {
					return nil, err
				}
				return privaccess.Redact(raw)
			})
		},
	)

	addTypedTool(s, "privileged_access_users_list", "The users enrolled in Privileged Access (the same users as Password Vault: one backend). IMPORTANT: "+privaccess.SelfInjected+".",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pamList(ctx, "users", func(ctx context.Context, c privaccess.Getter) ([]json.RawMessage, int, error) {
				return privaccess.ListResults(ctx, c, privaccess.UsersEndpoint, "users")
			})
		},
	)

	addTypedTool(s, "privileged_access_groups_list", "Groups that grant access to privileged resources, optionally only those for PAM or for Password Vault (PWM).",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamGroupsInput) (*mcp.CallToolResult, any, error) {
			endpoint, err := privaccess.GroupsEndpoint(args.Product)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return pamList(ctx, "groups", func(ctx context.Context, c privaccess.Getter) ([]json.RawMessage, int, error) {
				rows, err := privaccess.ListUncounted(ctx, c, endpoint, "results", "groups")
				return rows, len(rows), err
			})
		},
	)

	addTypedTool(s, "privileged_access_tags_list", "Tags (labels) used to organise privileged servers, databases, websites and credentials in Privileged Access (PAM). Lists the tag names only; it does not say which resources carry them.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pamList(ctx, "tags", func(ctx context.Context, c privaccess.Getter) ([]json.RawMessage, int, error) {
				rows, err := privaccess.ListUncounted(ctx, c, privaccess.TagsEndpoint, "results", "tags")
				return rows, len(rows), err
			})
		},
	)

	for _, k := range []privaccess.Kind{privaccess.Servers, privaccess.Databases, privaccess.Websites} {
		k := k
		addTypedTool(s, "privileged_access_"+k.Name+"s_list", fmt.Sprintf("The privileged %ss people connect to through a Privileged Access connector, with their address, connector, archived state and whether they are in use.", k.Name),
			func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
				return pamList(ctx, k.Name+"s", func(ctx context.Context, c privaccess.Getter) ([]json.RawMessage, int, error) {
					return privaccess.List(ctx, c, k)
				})
			},
		)
		addTypedTool(s, "privileged_access_"+k.Name+"_get", fmt.Sprintf("One privileged %s in full, by id or exact name: address, connector, access policies (who may manage, view or connect), linked credential ids and recording settings. Secret-looking values, such as a database connection string, are returned as [redacted]. This reads the API's edit view, because its plain detail endpoint refuses callers who hold the permission it asks for.", k.Name),
			func(ctx context.Context, req *mcp.CallToolRequest, args pamIdentInput) (*mcp.CallToolResult, any, error) {
				return pamSingle(ctx, "the "+k.Name, func(ctx context.Context, c privaccess.Getter) (json.RawMessage, error) {
					return privaccess.Get(ctx, c, k, args.Resource)
				})
			},
		)
	}

	addTypedTool(s, "privileged_access_credentials_list", "The privileged credentials (the accounts used to log in to privileged resources) with type, username, domain and rotation state. Never their secrets. Card and identity-document fields are removed. Credentials that are not marked privileged are in password_vault_credentials_list instead.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pamList(ctx, "credentials", func(ctx context.Context, c privaccess.Getter) ([]json.RawMessage, int, error) {
				rows, total, err := privaccess.ListCredentials(ctx, c)
				if err != nil {
					return nil, 0, err
				}
				rows, err = privaccess.StripPersonalFields(rows)
				return rows, total, err
			})
		},
	)

	addTypedTool(s, "privileged_access_credential_get", "One privileged credential's name, type, access policies and whether the caller may view its secret. Never the secret itself.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamCredentialInput) (*mcp.CallToolResult, any, error) {
			return pamSingle(ctx, "the credential", func(ctx context.Context, c privaccess.Getter) (json.RawMessage, error) {
				return privaccess.GetCredential(ctx, c, args.ID)
			})
		},
	)

	addTypedTool(s, "privileged_access_credential_managers", "Who holds management rights (owner, manager) over one privileged credential in Privileged Access: the people who can change, share or delete it. Not who may merely connect with it; that is in its access policies (privileged_access_credential_get).",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamCredentialInput) (*mcp.CallToolResult, any, error) {
			return pamList(ctx, "credential managers", func(ctx context.Context, c privaccess.Getter) ([]json.RawMessage, int, error) {
				return privaccess.CredentialManagers(ctx, c, args.ID)
			})
		},
	)

	addTypedTool(s, "privileged_access_credential_history", "When one privileged credential changed (its versions, newest first), not what it changed to. Pages with a continuation token.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamCredentialPageInput) (*mcp.CallToolResult, any, error) {
			return pamTokenPage(ctx, "the credential history", args, privaccess.CredentialHistory)
		},
	)

	addTypedTool(s, "privileged_access_credential_linked_servers", "The privileged servers one credential is linked to. Pages with a continuation token.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamCredentialPageInput) (*mcp.CallToolResult, any, error) {
			return pamTokenPage(ctx, "the linked servers", args, privaccess.CredentialLinkedServers)
		},
	)

	addTypedTool(s, "privileged_access_blocking_rules_list", "Blocking rules: what commands, queries or actions are stopped in privileged sessions, with criteria type, severity and priority.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pamList(ctx, "blocking rules", privaccess.ListBlockingRules)
		},
	)

	addTypedTool(s, "privileged_access_blocking_rule_get", "One blocking rule in full, by id or exact name: its criteria, the resources it applies to, and the users and groups exempted.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamIdentInput) (*mcp.CallToolResult, any, error) {
			return pamSingle(ctx, "the blocking rule", func(ctx context.Context, c privaccess.Getter) (json.RawMessage, error) {
				return privaccess.GetBlockingRule(ctx, c, args.Resource)
			})
		},
	)

	addTypedTool(s, "privileged_access_connectors_list", "Privileged Access connectors: the gateway agents, installed on a host in the customer network, that privileged SSH, RDP, database and web sessions run through. Lists them and their health; it does not return their install files or enrollment tokens.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pamList(ctx, "connectors", func(ctx context.Context, c privaccess.Getter) ([]json.RawMessage, int, error) {
				return privaccess.ListPaged(ctx, c, privaccess.ConnectorsEndpoint, "connectors")
			})
		},
	)

	addTypedTool(s, "privileged_access_connector_versions", "Connector releases available to install, with which one is the default and which are beta.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pamList(ctx, "connector versions", func(ctx context.Context, c privaccess.Getter) ([]json.RawMessage, int, error) {
				rows, err := privaccess.ListUncounted(ctx, c, privaccess.ConnectorVersionsEndpoint, "items", "connector versions")
				return rows, len(rows), err
			})
		},
	)

	addTypedTool(s, "privileged_access_connections_list", "The privileged session log: who connected to which resource, when and for how long.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pamList(ctx, "connections", func(ctx context.Context, c privaccess.Getter) ([]json.RawMessage, int, error) {
				return privaccess.ListPaged(ctx, c, privaccess.ConnectionsEndpoint, "connections")
			})
		},
	)

	addTypedTool(s, "privileged_access_connection_status", "Who is connected right now to one privileged server, database or website (by id or exact name). The API reports \"Available\" for an id matching nothing, so the resource is looked up first and an unknown one is an error.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamIdentInput) (*mcp.CallToolResult, any, error) {
			client, err := pamClientMCP(ctx)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, kind, err := privaccess.ResourceConnectionStatus(ctx, client, args.Resource)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			res, jerr := jsonResult(map[string]any{"resourceKind": kind, "status": json.RawMessage(raw)})
			if jerr != nil {
				return errorResult(jerr.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "privileged_access_resource_managers_list", "Resource managers: people or groups delegated to manage a set of privileged resources.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			return pamList(ctx, "resource managers", func(ctx context.Context, c privaccess.Getter) ([]json.RawMessage, int, error) {
				return privaccess.ListResults(ctx, c, privaccess.ResourceManagersEndpoint, "resource managers")
			})
		},
	)
}
