package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/privaccess"
)

// Privileged Access write tools. Twelve, matching the CLI's write commands;
// servers, databases and websites share one tool per verb with a "kind"
// argument. All are plan-by-default and refused in read-only mode on execute.
//
// The lockout that shapes every update — the API replaces the whole record,
// and a record sent without an access policy granting Manage can afterwards
// be neither read, changed nor deleted — is handled in internal/privaccess:
// updates change only named fields, send the read policy back unchanged, and
// read the record back. No tool here creates or edits a credential, and none
// reads a secret.

type pamResourceCreateInput struct {
	Kind         string `json:"kind" jsonschema:"server, database or website"`
	Name         string `json:"name" jsonschema:"Name for the new resource. Must not already be in use for this kind."`
	URI          string `json:"uri" jsonschema:"Host, address or URL"`
	Notes        string `json:"notes,omitempty" jsonschema:"Free-text notes"`
	Tags         string `json:"tags,omitempty" jsonschema:"Tags, comma-separated"`
	OS           string `json:"os,omitempty" jsonschema:"Servers only: linux or windows (default linux)"`
	Protocol     string `json:"protocol,omitempty" jsonschema:"Servers only: connection protocol such as ssh or rdp (default ssh)"`
	Port         int    `json:"port,omitempty" jsonschema:"Servers (default 22) and databases (required)"`
	Provider     string `json:"provider,omitempty" jsonschema:"Databases only, required: engine such as postgresql, mysql or sqlserver"`
	DatabaseName string `json:"database_name,omitempty" jsonschema:"Databases only: database name"`
	Execute      bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type pamResourceUpdateInput struct {
	Kind         string  `json:"kind" jsonschema:"server, database or website"`
	Identifier   string  `json:"identifier" jsonschema:"The resource's 24-character hex id or exact name"`
	Name         *string `json:"name,omitempty" jsonschema:"New name. Omit to leave unchanged."`
	URI          *string `json:"uri,omitempty" jsonschema:"New host, address or URL. Omit to leave unchanged."`
	Notes        *string `json:"notes,omitempty" jsonschema:"New notes; empty string clears. Omit to leave unchanged."`
	Tags         *string `json:"tags,omitempty" jsonschema:"New tags; empty string clears. Omit to leave unchanged."`
	OS           *string `json:"os,omitempty" jsonschema:"Servers only: linux or windows. Omit to leave unchanged."`
	Port         *int    `json:"port,omitempty" jsonschema:"Databases only: new port. Omit to leave unchanged."`
	DatabaseName *string `json:"database_name,omitempty" jsonschema:"Databases only: new database name. Omit to leave unchanged."`
	Execute      bool    `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type pamResourceActionInput struct {
	Kind       string `json:"kind" jsonschema:"server, database or website"`
	Identifier string `json:"identifier" jsonschema:"The resource's 24-character hex id or exact name"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type pamResourceArchiveInput struct {
	Kind        string   `json:"kind" jsonschema:"server, database or website"`
	Identifiers []string `json:"identifiers" jsonschema:"One or more 24-character hex ids or exact names"`
	Execute     bool     `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type pamCredentialActionInput struct {
	Identifier string `json:"identifier" jsonschema:"The credential's 24-character hex id, from privileged_access_credentials_list"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type pamCredentialArchiveInput struct {
	IDs     []string `json:"ids" jsonschema:"One or more credential 24-character hex ids"`
	Execute bool     `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type pamRuleUpdateInput struct {
	Identifier string  `json:"identifier" jsonschema:"The blocking rule's 24-character hex id or exact name"`
	Name       *string `json:"name,omitempty" jsonschema:"New name. Omit to leave unchanged."`
	Notes      *string `json:"notes,omitempty" jsonschema:"New notes; empty string clears. Omit to leave unchanged."`
	Tags       *string `json:"tags,omitempty" jsonschema:"New tags; empty string clears. Omit to leave unchanged."`
	Priority   *int    `json:"priority,omitempty" jsonschema:"New priority, 1 or more. Omit to leave unchanged."`
	Execute    bool    `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type pamRuleActionInput struct {
	Identifier string `json:"identifier" jsonschema:"The blocking rule's 24-character hex id or exact name"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

// pamWriterMCP returns the v2 client after the activation gate.
func pamWriterMCP(ctx context.Context) (privaccess.Writer, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return nil, err
	}
	if err := privaccess.RequireActive(ctx, client); err != nil {
		return nil, err
	}
	return client, nil
}

func pamDone(v map[string]any) (*mcp.CallToolResult, any, error) {
	res, err := jsonResult(v)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return res, nil, nil
}

func setIf[T any](m map[string]any, key string, v *T) {
	if v != nil {
		m[key] = *v
	}
}

func (s *Server) registerPrivilegedAccessWriteTools() {
	addTypedTool(s, "privileged_access_resource_create", "Create a privileged  server, database or website in Privileged Access. The caller is granted Manage, View Detail and Connect on it — always, because an object created without Manage can never be changed or deleted through the API. Servers take os, protocol and port; databases need provider and port; websites take only name, uri, notes and tags. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamResourceCreateInput) (*mcp.CallToolResult, any, error) {
			if res := s.refuseReadOnly("privileged_access_resource_create", args.Execute); res != nil {
				return res, nil, nil
			}
			k, err := privaccess.KindByName(args.Kind)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			spec := privaccess.ResourceSpec{Name: args.Name, URI: args.URI, Notes: args.Notes, Tags: args.Tags,
				OS: args.OS, Protocol: args.Protocol, Port: args.Port, Provider: args.Provider, DatabaseName: args.DatabaseName}
			if k.Endpoint == privaccess.ServersEndpoint {
				if spec.OS == "" {
					spec.OS = "linux"
				}
				if spec.Protocol == "" {
					spec.Protocol = "ssh"
				}
				if spec.Port == 0 {
					spec.Port = 22
				}
			}
			// Validate the body before planning, with a placeholder owner id.
			if _, err := privaccess.CreateBody(k, spec, 1); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("create", "privileged "+k.Name, args.Name, "(new)",
					map[string]any{"uri": args.URI, "grants_caller": privaccess.FullPermissions})
			}
			client, err := pamWriterMCP(ctx)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			id, err := privaccess.CreateResource(ctx, client, k, spec)
			if err != nil {
				return errorResult(fmt.Sprintf("creating %s: %v", k.Name, err)), nil, nil
			}
			return pamDone(map[string]any{"created": k.Name, "id": id, "confirmed": true})
		},
	)

	addTypedTool(s, "privileged_access_resource_update", "Change the name, uri, notes or tags of a privileged  server, database or website (also os on a server; port and database_name on a database). Only the fields given change. The API replaces the whole record on every update, and a record sent without its access policy loses every permission irrecoverably — this tool sends the policy back exactly as read, refuses if it grants Manage to nobody, and reads the record back to confirm. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamResourceUpdateInput) (*mcp.CallToolResult, any, error) {
			if res := s.refuseReadOnly("privileged_access_resource_update", args.Execute); res != nil {
				return res, nil, nil
			}
			k, err := privaccess.KindByName(args.Kind)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			changes := map[string]any{}
			setIf(changes, "name", args.Name)
			setIf(changes, "uri", args.URI)
			setIf(changes, "notes", args.Notes)
			setIf(changes, "tags", args.Tags)
			setIf(changes, "os", args.OS)
			setIf(changes, "port", args.Port)
			setIf(changes, "databaseName", args.DatabaseName)
			if err := privaccess.ApplyChanges(k, map[string]any{"accessPolicies": map[string]any{
				"users": []any{map[string]any{"permissions": []any{"Manage"}}}}}, changes); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("update", "privileged "+k.Name, args.Identifier, args.Identifier, changes)
			}
			client, err := pamWriterMCP(ctx)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := privaccess.UpdateResource(ctx, client, k, args.Identifier, changes)
			if err != nil {
				return errorResult(fmt.Sprintf("updating %s: %v", k.Name, err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "privileged_access_resource_delete", "Delete a privileged  server, database or website. PERMANENT. An archived server cannot be deleted (the API fails); unarchive it first. Archived databases and websites can be. Confirmed by reading the listings back. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamResourceActionInput) (*mcp.CallToolResult, any, error) {
			if res := s.refuseReadOnly("privileged_access_resource_delete", args.Execute); res != nil {
				return res, nil, nil
			}
			k, err := privaccess.KindByName(args.Kind)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "privileged "+k.Name, args.Identifier, args.Identifier,
					map[string]any{"reversible": false})
			}
			client, err := pamWriterMCP(ctx)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			id, err := privaccess.DeleteResource(ctx, client, k, args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting %s: %v", k.Name, err)), nil, nil
			}
			return pamDone(map[string]any{"deleted": k.Name, "id": id, "confirmed": true})
		},
	)

	for _, archive := range []bool{true, false} {
		verb, desc := "archive", "Archive one or more privileged  servers, databases or websites (all of one kind). An archived resource leaves the listing and cannot be connected to; privileged_access_resource_unarchive restores it."
		if !archive {
			verb, desc = "unarchive", "Restore one or more archived privileged  servers, databases or websites (all of one kind)."
		}
		name := "privileged_access_resource_" + verb
		addTypedTool(s, name, desc+" Confirmed by reading the archived listing back. Returns a plan unless execute is true.",
			func(ctx context.Context, req *mcp.CallToolRequest, args pamResourceArchiveInput) (*mcp.CallToolResult, any, error) {
				if res := s.refuseReadOnly(name, args.Execute); res != nil {
					return res, nil, nil
				}
				k, err := privaccess.KindByName(args.Kind)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				if len(args.Identifiers) == 0 {
					return errorResult("identifiers is required"), nil, nil
				}
				target := strings.Join(args.Identifiers, ", ")
				if !args.Execute {
					return planResult(verb, "privileged "+k.Name+"s", target, target,
						map[string]any{"count": len(args.Identifiers), "reversible": true})
				}
				client, err := pamWriterMCP(ctx)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				ids, err := privaccess.SetArchived(ctx, client, k, args.Identifiers, archive)
				if err != nil {
					return errorResult(fmt.Sprintf("%s: %v", verb, err)), nil, nil
				}
				return pamDone(map[string]any{verb + "d": ids, "confirmed": true})
			},
		)
	}

	addTypedTool(s, "privileged_access_resource_clone", "Duplicate a privileged  server, database or website. The copy is named \"<name> - Copy\" and carries the original's settings, notes and access policy. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamResourceActionInput) (*mcp.CallToolResult, any, error) {
			if res := s.refuseReadOnly("privileged_access_resource_clone", args.Execute); res != nil {
				return res, nil, nil
			}
			k, err := privaccess.KindByName(args.Kind)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("duplicate", "privileged "+k.Name, args.Identifier, args.Identifier,
					map[string]any{"creates": "<name> - Copy, with the same access policy"})
			}
			client, err := pamWriterMCP(ctx)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			id, err := privaccess.CloneResource(ctx, client, k, args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("cloning %s: %v", k.Name, err)), nil, nil
			}
			return pamDone(map[string]any{"created": k.Name + " copy", "id": id, "confirmed": true})
		},
	)

	addTypedTool(s, "privileged_access_credential_delete", "Delete a privileged credential, archived or not. PERMANENT: the secret goes with it. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamCredentialActionInput) (*mcp.CallToolResult, any, error) {
			if res := s.refuseReadOnly("privileged_access_credential_delete", args.Execute); res != nil {
				return res, nil, nil
			}
			if !privaccess.IsObjectID(args.Identifier) {
				return errorResult("identifier must be the credential's 24-character hex id"), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "privileged credential", args.Identifier, args.Identifier,
					map[string]any{"reversible": false, "secret": "deleted with it"})
			}
			client, err := pamWriterMCP(ctx)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if err := privaccess.DeleteCredential(ctx, client, args.Identifier); err != nil {
				return errorResult(fmt.Sprintf("deleting credential: %v", err)), nil, nil
			}
			return pamDone(map[string]any{"deleted": "credential", "id": args.Identifier, "confirmed": true})
		},
	)

	for _, archive := range []bool{true, false} {
		verb, desc := "archive", "Archive privileged credentials by id, in one call."
		if !archive {
			verb, desc = "unarchive", "Restore archived privileged credentials by id, in one call."
		}
		name := "privileged_access_credential_" + verb
		addTypedTool(s, name, desc+" Confirmed by reading the archived listing back. Returns a plan unless execute is true.",
			func(ctx context.Context, req *mcp.CallToolRequest, args pamCredentialArchiveInput) (*mcp.CallToolResult, any, error) {
				if res := s.refuseReadOnly(name, args.Execute); res != nil {
					return res, nil, nil
				}
				if _, err := privaccess.IDsBody(args.IDs); err != nil {
					return errorResult(err.Error()), nil, nil
				}
				target := strings.Join(args.IDs, ", ")
				if !args.Execute {
					return planResult(verb, "privileged credentials", target, target,
						map[string]any{"count": len(args.IDs), "reversible": true})
				}
				client, err := pamWriterMCP(ctx)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				if err := privaccess.SetCredentialsArchived(ctx, client, args.IDs, archive); err != nil {
					return errorResult(fmt.Sprintf("%s: %v", verb, err)), nil, nil
				}
				return pamDone(map[string]any{verb + "d": args.IDs, "confirmed": true})
			},
		)
	}

	addTypedTool(s, "privileged_access_credential_clone", "Duplicate a privileged credential, SECRET INCLUDED: the copy (\"<name> - Copy\") is one more place the secret exists. The secret is not returned. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamCredentialActionInput) (*mcp.CallToolResult, any, error) {
			if res := s.refuseReadOnly("privileged_access_credential_clone", args.Execute); res != nil {
				return res, nil, nil
			}
			if !privaccess.IsObjectID(args.Identifier) {
				return errorResult("identifier must be the credential's 24-character hex id"), nil, nil
			}
			if !args.Execute {
				return planResult("duplicate", "privileged credential", args.Identifier, args.Identifier,
					map[string]any{"copies": "the secret into a second credential"})
			}
			client, err := pamWriterMCP(ctx)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			id, err := privaccess.CloneCredential(ctx, client, args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("cloning credential: %v", err)), nil, nil
			}
			return pamDone(map[string]any{"created": "credential copy", "id": id, "confirmed": true})
		},
	)

	addTypedTool(s, "privileged_access_blocking_rule_update", "Change a blocking rule's name, notes, tags or priority. Its criteria and resources stay as they are. The whole record is sent back with its access policy unchanged, and read back to confirm. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamRuleUpdateInput) (*mcp.CallToolResult, any, error) {
			if res := s.refuseReadOnly("privileged_access_blocking_rule_update", args.Execute); res != nil {
				return res, nil, nil
			}
			changes := map[string]any{}
			setIf(changes, "name", args.Name)
			setIf(changes, "notes", args.Notes)
			setIf(changes, "tags", args.Tags)
			setIf(changes, "priority", args.Priority)
			if len(changes) == 0 {
				return errorResult("nothing to change: give name, notes, tags or priority"), nil, nil
			}
			if args.Priority != nil && *args.Priority < 1 {
				return errorResult("priority must be 1 or more"), nil, nil
			}
			if !args.Execute {
				return planResult("update", "blocking rule", args.Identifier, args.Identifier, changes)
			}
			client, err := pamWriterMCP(ctx)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := privaccess.UpdateBlockingRule(ctx, client, args.Identifier, changes)
			if err != nil {
				return errorResult(fmt.Sprintf("updating blocking rule: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "privileged_access_blocking_rule_delete", "Delete a blocking rule. PERMANENT: the commands or queries it blocked are no longer blocked. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pamRuleActionInput) (*mcp.CallToolResult, any, error) {
			if res := s.refuseReadOnly("privileged_access_blocking_rule_delete", args.Execute); res != nil {
				return res, nil, nil
			}
			if !args.Execute {
				return planResult("delete", "blocking rule", args.Identifier, args.Identifier,
					map[string]any{"reversible": false})
			}
			client, err := pamWriterMCP(ctx)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			id, err := privaccess.DeleteBlockingRule(ctx, client, args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting blocking rule: %v", err)), nil, nil
			}
			return pamDone(map[string]any{"deleted": "blocking rule", "id": id, "confirmed": true})
		},
	)
}
