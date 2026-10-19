package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/agents"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/config"
)

// AI Agent Identities tools. Nine, matching the CLI's nine reads one for one.
//
// Every one of them reads. The area's writes are not here yet, and the three
// that create, change or delete an agent's OAuth credentials will NOT be:
// they ship CLI-only, by the same reasoning that keeps Password Vault's
// secret reads out of MCP. A credential creation returns a client secret
// exactly once, and a tool that returned it would put it in a transcript
// while passing every mutation-based gate, since the call that harms is not
// the call that mutates.
//
// agents_credentials_list IS here, and that is a judgement about the
// endpoint rather than about the word "credentials": it returns a client id,
// a type, a status and three timestamps, and no secret — verified against
// every agent on a live tenant. Knowing an agent has an active credential
// expiring in March is operational information; the secret is the part that
// must not travel.

type agentsListInput struct {
	Filter []string `json:"filter,omitempty" jsonschema:"Filters as field:operator:value, combined with AND. The fields are name, email and state; the operators are eq and ne. THERE IS NO SUBSTRING MATCH: an agent cannot be found by part of its name, so use the exact name or list them all. An unsupported field is rejected."`
	Limit  int      `json:"limit,omitempty" jsonschema:"Return one page of this size instead of every agent. The total in the response is the server's match count, which is larger than the rows returned when this is set."`
}

type agentGetInput struct {
	AgentID string `json:"agent_id" jsonschema:"The agent's JumpCloud 24-character object id, taken from agents_list. There is no lookup by name."`
}

type agentGroupInput struct {
	GroupID string `json:"group_id" jsonschema:"The agent group's JumpCloud 24-character object id, taken from agent_groups_list."`
}

type agentGroupsListInput struct {
	Filter []string `json:"filter,omitempty" jsonschema:"Filters as field:operator:value, combined with AND. The fields are name and type; the operators are eq and ne. Note these differ from the agents listing, which filters on name, email and state."`
	Limit  int      `json:"limit,omitempty" jsonschema:"Maximum groups to return."`
	Sort   string   `json:"sort,omitempty" jsonschema:"Sort field; prefix with - for descending. Works here, unlike on the agents listing."`
}

func (s *Server) registerAgentsTools() {
	addTypedTool(s, "agents_list", "AI agent identities in this organization — autonomous workloads that hold their own JumpCloud identity, their own OAuth credentials, and membership in agent groups assigned to applications. Each row carries the agent's name, email, owning admin and state (active or suspended). Filtering is narrow: name, email and state only, with eq and ne, and NO substring match. The total in the response is the server's match count, so when limit is set it is larger than the number of rows returned; read the total, not the length of the list.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentsListInput) (*mcp.CallToolResult, any, error) {
			for _, f := range args.Filter {
				if err := agents.ValidateAgentFilter(f); err != nil {
					return errorResult(err.Error()), nil, nil
				}
			}
			client, restore, err := agentsClientMCP(agents.AgentsEndpoint)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			defer restore()

			if args.Limit > 0 {
				page, perr := agents.GetAgentsPage(ctx, client, args.Filter, args.Limit, "")
				if perr != nil {
					return errorResult(fmt.Sprintf("listing agents: %v", perr)), nil, nil
				}
				return listEnvelope(page.Rows, &page.TotalSize)
			}
			rows, total, lerr := agents.ListAgents(ctx, client, args.Filter)
			if lerr != nil {
				return errorResult(fmt.Sprintf("listing agents: %v", lerr)), nil, nil
			}
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "agent_get", "One AI agent identity in full, by its object id: name, email, description, owning admin, state, and the OAuth audiences its tokens may target. Take the id from agents_list; an id of the wrong shape is rejected locally so a typo reads as a typo.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentGetInput) (*mcp.CallToolResult, any, error) {
			return agentSingleRead(ctx, args.AgentID, "agent", agents.AgentEndpoint(args.AgentID), "getting the agent")
		},
	)

	addTypedTool(s, "agent_memberof", "The agent groups one AI agent belongs to, WITH THEIR NAMES. The API returns ids and types and no names, so this joins the names in from the group catalog; a row whose name is empty is one the catalog could not resolve, not a group without a name. Use this to answer what an agent inherits before changing its access.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentGetInput) (*mcp.CallToolResult, any, error) {
			return agentGraphRead(ctx, args.AgentID, agents.AgentMemberOfEndpoint(args.AgentID), "memberof")
		},
	)

	addTypedTool(s, "agent_applications", "The applications one AI agent can reach, with their names AND the route by which it reaches each one. An agent gets to an application through a group, so each row's \"via\" names the group that granted the access; \"via\" is empty only for a direct assignment. That distinction appears nowhere else in the API's response, and it is the difference between knowing an agent has access and knowing where to remove it.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentGetInput) (*mcp.CallToolResult, any, error) {
			return agentGraphRead(ctx, args.AgentID, agents.AgentApplicationsEndpoint(args.AgentID), "applications")
		},
	)

	addTypedTool(s, "agents_credentials_list", "How one AI agent authenticates to JumpCloud: its OAuth client id, authentication type, status, and when each credential was created, last changed and expires. NO SECRET IS RETURNED — not by this tool and not by any read in this API. A client secret is returned once, by the call that creates it, and by nothing afterwards, so a lost credential is replaced rather than recovered. Use this to find expiring or inactive credentials.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentGetInput) (*mcp.CallToolResult, any, error) {
			if !agents.IsObjectID(args.AgentID) {
				return errorResult(agents.ErrNotObjectID("agent", args.AgentID).Error()), nil, nil
			}
			endpoint := agents.AgentCredentialsEndpoint(args.AgentID)
			client, restore, err := agentsClientMCP(endpoint)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			defer restore()

			raw, err := client.Get(ctx, endpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing the agent's credentials: %v", err)), nil, nil
			}
			rows, perr := agents.ParseCredentials(raw)
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return listEnvelope(rows, nil)
		},
	)

	addTypedTool(s, "agent_groups_list", "AI agent groups in this organization — the collections through which agents are granted access to applications. Filtering here accepts name and type, which differs from the agents listing; sorting works here, which it does not there. This listing returns no total count, so the number of rows is the number of groups.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentGroupsListInput) (*mcp.CallToolResult, any, error) {
			for _, f := range args.Filter {
				if err := agents.ValidateGroupFilter(f); err != nil {
					return errorResult(err.Error()), nil, nil
				}
			}
			client, restore, err := agentsClientMCP(agents.GroupsEndpoint)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			defer restore()

			rows, lerr := agents.ListGroups(ctx, client, args.Filter, args.Limit, args.Sort)
			if lerr != nil {
				return errorResult(fmt.Sprintf("listing agent groups: %v", lerr)), nil, nil
			}
			return listEnvelope(rows, nil)
		},
	)

	addTypedTool(s, "agent_group_get", "One AI agent group in full, by its object id: name, description and organization. Take the id from agent_groups_list.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentGroupInput) (*mcp.CallToolResult, any, error) {
			return agentSingleRead(ctx, args.GroupID, "agent group", agents.GroupEndpoint(args.GroupID), "getting the agent group")
		},
	)

	addTypedTool(s, "agent_group_members", "The AI agents in one group, WITH THEIR NAMES. The API returns ids and types and no names, so this joins them in from the agent catalog. Use it to see who inherits a group's application access before changing the group.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentGroupInput) (*mcp.CallToolResult, any, error) {
			return agentMembershipRead(ctx, args.GroupID, agents.GroupMembersEndpoint(args.GroupID), "", "members")
		},
	)

	addTypedTool(s, "agent_group_associations", "The applications one AI agent group grants access to, with their names. Agent groups are associated with applications and nothing else — no policies, no commands, no devices — so there is one kind of target and this tool supplies it; the API requires it and its own spec does not say so.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentGroupInput) (*mcp.CallToolResult, any, error) {
			q, err := agents.AssociationsQuery("")
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return agentMembershipRead(ctx, args.GroupID, agents.GroupAssociationsEndpoint(args.GroupID), q.Encode(), "associations")
		},
	)
}

// agentsClientMCP mirrors the CLI's agentsClient: it installs the active
// profile's organization id as a request header for the duration of the call.
//
// The header is required on most of this area's endpoints and the API spec
// marks it optional. Sending it where it is not required is harmless, and
// having one place decide that is what keeps the CLI and these tools from
// disagreeing about which calls need it.
func agentsClientMCP(endpoint string) (*api.V2Client, func(), error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return nil, nil, err
	}
	orgID := config.OrgID()
	if orgID == "" {
		if agents.RequiresOrgID(endpoint) {
			return nil, nil, agents.ErrNoOrgID()
		}
		return client, func() {}, nil
	}
	return client, api.SetExtraHeaders(http.Header{agents.OrgHeader: []string{orgID}}), nil
}

func agentSingleRead(ctx context.Context, id, kind, endpoint, what string) (*mcp.CallToolResult, any, error) {
	if !agents.IsObjectID(id) {
		return errorResult(agents.ErrNotObjectID(kind, id).Error()), nil, nil
	}
	client, restore, err := agentsClientMCP(endpoint)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	defer restore()

	raw, err := client.Get(ctx, endpoint)
	if err != nil {
		return errorResult(fmt.Sprintf("%s: %v", what, err)), nil, nil
	}
	return textResult(string(raw)), nil, nil
}

// agentGraphRead reads a /memberof or /applications response and names the
// ids in it.
func agentGraphRead(ctx context.Context, agentID, endpoint, what string) (*mcp.CallToolResult, any, error) {
	if !agents.IsObjectID(agentID) {
		return errorResult(agents.ErrNotObjectID("agent", agentID).Error()), nil, nil
	}
	client, restore, err := agentsClientMCP(endpoint)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	defer restore()

	raw, err := agents.GetBareArray(ctx, client, endpoint, nil)
	if err != nil {
		return errorResult(fmt.Sprintf("reading %s: %v", what, err)), nil, nil
	}
	refs, perr := agents.ParseGraph(raw, what)
	if perr != nil {
		return errorResult(perr.Error()), nil, nil
	}
	named, rerr := agents.ResolveGraph(ctx, agents.NewCatalog(client, nil), refs)
	if rerr != nil {
		return errorResult(rerr.Error()), nil, nil
	}
	return namedRefsEnvelope(named)
}

// agentMembershipRead reads a /members or /associations response and names
// the ids in it.
func agentMembershipRead(ctx context.Context, groupID, endpoint, rawQuery, what string) (*mcp.CallToolResult, any, error) {
	if !agents.IsObjectID(groupID) {
		return errorResult(agents.ErrNotObjectID("agent group", groupID).Error()), nil, nil
	}
	client, restore, err := agentsClientMCP(endpoint)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	defer restore()

	path := endpoint
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	raw, err := client.Get(ctx, path)
	if err != nil {
		return errorResult(fmt.Sprintf("reading %s: %v", what, err)), nil, nil
	}
	refs, perr := agents.ParseMembers(raw, what)
	if perr != nil {
		return errorResult(perr.Error()), nil, nil
	}
	named, rerr := agents.ResolveMembers(ctx, agents.NewCatalog(client, nil), refs)
	if rerr != nil {
		return errorResult(rerr.Error()), nil, nil
	}
	return namedRefsEnvelope(named)
}

func namedRefsEnvelope(refs []agents.NamedRef) (*mcp.CallToolResult, any, error) {
	rows := make([]json.RawMessage, 0, len(refs))
	for _, r := range refs {
		raw, err := json.Marshal(r)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		rows = append(rows, raw)
	}
	return listEnvelope(rows, nil)
}
