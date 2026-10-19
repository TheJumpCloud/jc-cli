package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/agents"
)

// The lifecycle and membership write tools for AI Agent Identities. Twelve,
// matching the CLI's twelve write commands, which cover ten API operations —
// the two membership endpoints each serve both directions through an "op"
// field.
//
// All of them are plan-by-default: without execute: true they describe what
// they would do and change nothing.
//
// The three CREDENTIAL writes are deliberately absent and will stay absent;
// see TestNoCredentialIssuingTools. Creating a credential is the only call in
// this API that ever returns a client secret, and it classifies as mutating —
// so a plan and a confirmation both pass it while the successful call puts a
// live secret into the transcript.
//
// Worth stating plainly because this server is itself an agent context: these
// tools let an agent suspend or delete another agent. That is legitimate
// administration and the destructive classification covers it, but the
// descriptions say what each one costs rather than leaving a model to infer
// it from a verb.

type agentRegisterInput struct {
	Name        string   `json:"name" jsonschema:"Display name for the agent. Required."`
	Email       string   `json:"email" jsonschema:"Email identifying the agent. Required, and must not already belong to another agent in this organization."`
	OwnerID     string   `json:"owner_id" jsonschema:"JumpCloud 24-character user id of the ACTIVATED admin responsible for the agent. Required. This is a user id, not an email and not an admin id."`
	Description string   `json:"description,omitempty" jsonschema:"Free-text description."`
	Audiences   []string `json:"allowed_audiences,omitempty" jsonschema:"OAuth audiences the agent's tokens may target."`
	Execute     bool     `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type agentUpdateInput struct {
	AgentID     string    `json:"agent_id" jsonschema:"The agent's JumpCloud 24-character object id."`
	Name        *string   `json:"name,omitempty" jsonschema:"New display name. Omit to leave unchanged; it cannot be set to an empty value."`
	Email       *string   `json:"email,omitempty" jsonschema:"New email. Omit to leave unchanged."`
	Description *string   `json:"description,omitempty" jsonschema:"New description. Omit to leave unchanged; pass an empty string to clear it."`
	OwnerID     *string   `json:"owner_id,omitempty" jsonschema:"New owning user's JumpCloud id. Omit to leave unchanged."`
	Audiences   *[]string `json:"allowed_audiences,omitempty" jsonschema:"Replace the allowed audiences. Omit to leave unchanged; pass an empty list to clear."`
	Execute     bool      `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type agentActionInput struct {
	AgentID string `json:"agent_id" jsonschema:"The agent's JumpCloud 24-character object id."`
	Execute bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type agentGroupCreateInput struct {
	Name        string `json:"name" jsonschema:"Display name for the group. Required, and unique within the organization."`
	Description string `json:"description,omitempty" jsonschema:"Free-text description."`
	Execute     bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type agentGroupUpdateInput struct {
	GroupID     string  `json:"group_id" jsonschema:"The group's JumpCloud 24-character object id."`
	Name        *string `json:"name,omitempty" jsonschema:"New display name. Omit to leave unchanged."`
	Description *string `json:"description,omitempty" jsonschema:"New description. Omit to leave unchanged; pass an empty string to clear it."`
	Execute     bool    `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type agentGroupActionInput struct {
	GroupID string `json:"group_id" jsonschema:"The group's JumpCloud 24-character object id."`
	Execute bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type agentMembershipInput struct {
	GroupID string `json:"group_id" jsonschema:"The agent group's JumpCloud 24-character object id."`
	AgentID string `json:"agent_id" jsonschema:"The agent's JumpCloud 24-character object id."`
	Execute bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type agentAppAssignInput struct {
	GroupID       string `json:"group_id" jsonschema:"The agent group's JumpCloud 24-character object id."`
	ApplicationID string `json:"application_id" jsonschema:"The application's JumpCloud 24-character object id, from apps_list. Applications are the only thing an agent group can be associated with."`
	Execute       bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

func (s *Server) registerAgentsWriteTools() {
	addTypedTool(s, "agent_register", "Create an AI agent identity. Requires a name, an email that no other agent in the organization uses, and the JumpCloud USER id of an activated admin to own it — the API's own spec marks none of the three as required, but all three are. A new agent starts active, belongs to no group, and cannot authenticate until a credential is created for it (which the CLI does; there is no tool for it). Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentRegisterInput) (*mcp.CallToolResult, any, error) {
			body, err := agents.BuildAgentCreate(agents.AgentCreate{
				Name: args.Name, Email: args.Email, OwnerID: args.OwnerID,
				Description: args.Description, AllowedAudiences: args.Audiences,
			})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return agentPlan("create", "AI agent", args.Name, body, true)
			}
			return agentWrite(ctx, "POST", agents.AgentsEndpoint, agents.AgentsEndpoint, body, "creating the agent")
		},
	)

	addTypedTool(s, "agent_update", "Change an AI agent's name, email, owner, description or allowed audiences. ONLY THE FIELDS YOU PASS ARE CHANGED — this endpoint is genuinely partial, unlike agent_group_update, which replaces the group. An agent's STATE cannot be changed here: use agent_suspend or agent_activate. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentUpdateInput) (*mcp.CallToolResult, any, error) {
			if !agents.IsObjectID(args.AgentID) {
				return errorResult(agents.ErrNotObjectID("agent", args.AgentID).Error()), nil, nil
			}
			body, err := agents.BuildAgentPatch(args.Name, args.Email, args.Description, args.OwnerID, args.Audiences)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return agentPlan("update", "AI agent", args.AgentID, body, true)
			}
			return agentWrite(ctx, "PATCH", agents.AgentEndpoint(args.AgentID),
				agents.AgentEndpoint(args.AgentID), body, "updating the agent")
		},
	)

	addTypedTool(s, "agent_suspend", "Suspend an AI agent so its credentials stop working immediately. This is the kill switch for a misbehaving agent: anything it is doing stops. It is REVERSIBLE with agent_activate, and suspending an already-suspended agent changes nothing, so a retry is safe. Prefer this over agent_delete when the goal is to stop an agent rather than to remove it. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentActionInput) (*mcp.CallToolResult, any, error) {
			return agentStateTool(ctx, args, "suspend", "suspended")
		},
	)

	addTypedTool(s, "agent_activate", "Return a suspended AI agent to service, so its credentials work again. Activating an already-active agent changes nothing. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentActionInput) (*mcp.CallToolResult, any, error) {
			return agentStateTool(ctx, args, "activate", "active")
		},
	)

	addTypedTool(s, "agent_delete", "Delete an AI agent identity. THIS CANNOT BE UNDONE: the agent, its credentials and its group memberships all go. If the goal is to stop the agent working, agent_suspend does that reversibly and is almost always the right choice instead. The agent's groups and the applications they grant are not affected — only this agent's membership in them. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentActionInput) (*mcp.CallToolResult, any, error) {
			if !agents.IsObjectID(args.AgentID) {
				return errorResult(agents.ErrNotObjectID("agent", args.AgentID).Error()), nil, nil
			}
			if !args.Execute {
				return agentPlan("delete", "AI agent", args.AgentID,
					json.RawMessage(`{"irreversible":"the agent, its credentials and its memberships are removed"}`), false)
			}
			return agentWrite(ctx, "DELETE", agents.AgentEndpoint(args.AgentID),
				agents.AgentEndpoint(args.AgentID), nil, "deleting the agent")
		},
	)

	addTypedTool(s, "agent_group_create", "Create an AI agent group — the collection through which agents are granted access to applications. Only a name is required, and group names are unique within the organization. A new group has no members and grants nothing until it is associated with an application. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentGroupCreateInput) (*mcp.CallToolResult, any, error) {
			body, err := agents.BuildGroupCreate(args.Name, args.Description)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return agentPlan("create", "AI agent group", args.Name, body, true)
			}
			return agentWrite(ctx, "POST", agents.GroupsEndpoint, agents.GroupsEndpoint, body, "creating the agent group")
		},
	)

	addTypedTool(s, "agent_group_update", "Rename an AI agent group or change its description. THIS ENDPOINT REPLACES THE GROUP: the API requires the name on every update and clears any field it is not given, so this tool reads the group first and merges the change onto what is already there. That is the opposite of agent_update, which is partial — the difference is the server's. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentGroupUpdateInput) (*mcp.CallToolResult, any, error) {
			if !agents.IsObjectID(args.GroupID) {
				return errorResult(agents.ErrNotObjectID("agent group", args.GroupID).Error()), nil, nil
			}
			if args.Name == nil && args.Description == nil {
				return errorResult("nothing to update: pass name or description"), nil, nil
			}
			client, err := agentsClientMCP(agents.GroupEndpoint(args.GroupID))
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			existing, err := client.Get(ctx, agents.GroupEndpoint(args.GroupID))
			if err != nil {
				return errorResult(fmt.Sprintf("reading the group before updating it: %v", err)), nil, nil
			}
			body, err := agents.MergeGroupPatch(existing, args.Name, args.Description)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return agentPlan("update", "AI agent group", args.GroupID, body, true)
			}
			raw, err := client.Patch(ctx, agents.GroupEndpoint(args.GroupID), body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating the agent group: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "agent_group_delete", "Delete an AI agent group. THIS CANNOT BE UNDONE, and every agent in the group loses the access the group granted. The agents themselves are NOT deleted — nothing cascades. Read agent_group_members first to see who is affected. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentGroupActionInput) (*mcp.CallToolResult, any, error) {
			if !agents.IsObjectID(args.GroupID) {
				return errorResult(agents.ErrNotObjectID("agent group", args.GroupID).Error()), nil, nil
			}
			if !args.Execute {
				return agentPlan("delete", "AI agent group", args.GroupID,
					json.RawMessage(`{"irreversible":"its agents lose the access this group granted; the agents are not deleted"}`), false)
			}
			return agentWrite(ctx, "DELETE", agents.GroupEndpoint(args.GroupID),
				agents.GroupEndpoint(args.GroupID), nil, "deleting the agent group")
		},
	)

	addTypedTool(s, "agent_group_add_member", "Add an AI agent to a group, granting it everything the group is associated with. Adding an agent that is already in the group is REFUSED rather than ignored, so this is not idempotent and a retry after a timeout can report a failure for a change that already took effect. The membership is read back afterwards, because the write itself returns an empty body. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentMembershipInput) (*mcp.CallToolResult, any, error) {
			return agentMembershipTool(ctx, args.GroupID, args.AgentID, agents.MemberTypeAgent, agents.OpAdd, args.Execute)
		},
	)

	addTypedTool(s, "agent_group_remove_member", "Remove an AI agent from a group, revoking everything the group granted it. The agent is not deleted and keeps any access it has through other groups. The membership is read back afterwards. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentMembershipInput) (*mcp.CallToolResult, any, error) {
			return agentMembershipTool(ctx, args.GroupID, args.AgentID, agents.MemberTypeAgent, agents.OpRemove, args.Execute)
		},
	)

	addTypedTool(s, "agent_group_assign_app", "Associate an AI agent group with an application, granting every agent in the group access to it. Applications are the only thing an agent group can be associated with. The association is read back afterwards. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentAppAssignInput) (*mcp.CallToolResult, any, error) {
			return agentMembershipTool(ctx, args.GroupID, args.ApplicationID, agents.AssociationTargetApplication, agents.OpAdd, args.Execute)
		},
	)

	addTypedTool(s, "agent_group_unassign_app", "Remove an AI agent group's association with an application, revoking it for every agent in the group. The association is read back afterwards. Returns a plan unless execute is true.",
		func(ctx context.Context, req *mcp.CallToolRequest, args agentAppAssignInput) (*mcp.CallToolResult, any, error) {
			return agentMembershipTool(ctx, args.GroupID, args.ApplicationID, agents.AssociationTargetApplication, agents.OpRemove, args.Execute)
		},
	)
}

// agentStateTool is the shared body of suspend and activate. Both read the
// resulting state back rather than reporting the one that was requested.
func agentStateTool(ctx context.Context, args agentActionInput, verb, wantState string) (*mcp.CallToolResult, any, error) {
	if !agents.IsObjectID(args.AgentID) {
		return errorResult(agents.ErrNotObjectID("agent", args.AgentID).Error()), nil, nil
	}
	endpoint := agents.AgentSuspendEndpoint(args.AgentID)
	if verb == "activate" {
		endpoint = agents.AgentActivateEndpoint(args.AgentID)
	}
	if !args.Execute {
		return agentPlan(verb, "AI agent", args.AgentID,
			json.RawMessage(`{"state":"`+wantState+`"}`), verb == "suspend")
	}

	client, err := agentsClientMCP(endpoint)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	raw, err := client.Create(ctx, endpoint, nil)
	if err != nil {
		return errorResult(fmt.Sprintf("%s the agent: %v", verb, err)), nil, nil
	}
	got, err := agents.ParseAgentState(raw)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	if got != wantState {
		return errorResult(fmt.Sprintf("asked to %s agent %s, and the API reports its state as "+
			"%q rather than %q — the change may not have taken effect", verb, args.AgentID, got, wantState)), nil, nil
	}
	return textResult(string(raw)), nil, nil
}

// agentMembershipTool is the shared body of the four membership writes, all
// of which are one POST with an op field.
func agentMembershipTool(ctx context.Context, groupID, targetID, objType, op string, execute bool) (*mcp.CallToolResult, any, error) {
	if !agents.IsObjectID(groupID) {
		return errorResult(agents.ErrNotObjectID("agent group", groupID).Error()), nil, nil
	}
	body, err := agents.MembershipBody(op, targetID, objType)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	if !execute {
		return agentPlan(op+" "+objType, "AI agent group membership", groupID, body, op == agents.OpAdd)
	}

	endpoint := agents.GroupMembersEndpoint(groupID)
	isApp := objType == agents.AssociationTargetApplication
	if isApp {
		endpoint = agents.GroupAssociationsEndpoint(groupID)
	}

	client, err := agentsClientMCP(endpoint)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	if _, err := client.Create(ctx, endpoint, body); err != nil {
		return errorResult(agents.ExplainMembershipError(err, op, objType, targetID, groupID).Error()), nil, nil
	}

	// The write returns an empty body, so the only way to report a change
	// rather than a request is to go and look.
	path := endpoint
	if isApp {
		q, qErr := agents.AssociationsQuery("")
		if qErr != nil {
			return errorResult(qErr.Error()), nil, nil
		}
		path = agents.WithQuery(endpoint, q)
	}
	raw, err := client.Get(ctx, path)
	if err != nil {
		return errorResult(fmt.Sprintf("the change was accepted, but reading the membership back "+
			"to confirm it failed: %v", err)), nil, nil
	}
	refs, err := agents.ParseMembers(raw, "members")
	if err != nil {
		return errorResult(fmt.Sprintf("the change was accepted, but the membership could not be "+
			"read back to confirm it: %v", err)), nil, nil
	}

	present := false
	for _, r := range refs {
		if r.ID == targetID {
			present = true
			break
		}
	}
	if present != (op == agents.OpAdd) {
		return errorResult(fmt.Sprintf("the API accepted the change, but reading the membership "+
			"back shows %s %s is %s in group %s — the write reported success without taking effect",
			objType, targetID,
			map[bool]string{true: "still present", false: "absent"}[present], groupID)), nil, nil
	}

	named, err := agents.ResolveMembers(ctx, agents.NewCatalog(client, nil), refs)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return namedRefsEnvelope(named)
}

// agentWrite performs a write and returns the raw response, or reports the
// deletion when the response is empty.
func agentWrite(ctx context.Context, method, clientEndpoint, endpoint string, body json.RawMessage, what string) (*mcp.CallToolResult, any, error) {
	client, err := agentsClientMCP(clientEndpoint)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	var raw json.RawMessage
	switch method {
	case "POST":
		raw, err = client.Create(ctx, endpoint, body)
	case "PATCH":
		raw, err = client.Patch(ctx, endpoint, body)
	case "DELETE":
		raw, err = client.Delete(ctx, endpoint)
	default:
		return errorResult("unsupported method " + method), nil, nil
	}
	if err != nil {
		return errorResult(fmt.Sprintf("%s: %v", what, err)), nil, nil
	}
	if len(raw) == 0 {
		// A delete answers with an empty body; saying so beats returning "".
		res, jErr := jsonResult(map[string]any{"ok": true, "endpoint": endpoint})
		if jErr != nil {
			return errorResult(jErr.Error()), nil, nil
		}
		return res, nil, nil
	}
	return textResult(string(raw)), nil, nil
}

// agentPlan renders the plan a write tool returns when execute is absent.
//
// It goes through the package's shared planResult rather than building its
// own shape. The comment on PlanShapeDoc is explicit about why: the plan's
// keys were once answered differently by different tools, and a consumer
// cannot parse a shape that drifts. The only thing added here is
// reversibility, which this area needs because five of its twelve writes
// cannot be undone.
func agentPlan(action, resource, target string, body json.RawMessage, reversible bool) (*mcp.CallToolResult, any, error) {
	effects := map[string]any{"reversible": reversible}
	if len(body) > 0 {
		var fields map[string]any
		if err := json.Unmarshal(body, &fields); err == nil {
			for k, v := range fields {
				effects[k] = v
			}
		}
	}
	return planResult(action, resource, target, target, effects)
}
