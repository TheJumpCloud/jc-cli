package agents

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// The write half of the area. Ten operations, and the same pattern as the
// reads: the two halves of this feature do not agree with each other, so the
// disagreements are written down once here.
//
// Probed live on 2026-10-19 by creating throwaway agents and groups,
// exercising every write against them, and deleting them again.
//
// THE TWO UPDATE VERBS HAVE OPPOSITE SEMANTICS. This is the finding that
// matters most, because getting it wrong destroys data rather than failing:
//
//	PATCH /agents/{id}       partial. Sending {"name": "x"} preserves
//	                         description, email, ownerId and
//	                         allowedAudiences. PUT answers 501.
//	PATCH /agentgroups/{id}  FULL REPLACE wearing PATCH's name. `name` is
//	                         required, and every field omitted is CLEARED —
//	                         sending {"name": "x"} wipes the description.
//	                         PUT answers 404.
//
// So an agent update sends only what changed, and a group update must read
// the group first and merge. Doing it the other way round silently erases a
// description an operator never mentioned.

// Endpoints for the state transitions. State is NOT settable through an
// update: PATCH /agents/{id} with {"state": "suspended"} is refused with
// "state cannot be changed with PATCH; use suspend or activate".
func AgentSuspendEndpoint(id string) string  { return AgentEndpoint(id) + "/suspend" }
func AgentActivateEndpoint(id string) string { return AgentEndpoint(id) + "/activate" }

// --- Creating an agent ----------------------------------------------------

// AgentCreate is the body of POST /agents.
//
// Name, Email and OwnerID are all required — established one at a time, each
// from its own refusal, because the spec marks none of them so.
type AgentCreate struct {
	Name             string   `json:"name"`
	Email            string   `json:"email"`
	OwnerID          string   `json:"ownerId"`
	Description      string   `json:"description,omitempty"`
	AllowedAudiences []string `json:"allowedAudiences,omitempty"`
}

// emailish is a deliberately loose check. The server's own validation is
// weaker than it looks — it rejected "not-an-email" with
// "email cannot be empty or whitespace-only", a message about a different
// problem entirely — so checking locally produces a better error than
// forwarding one that describes the wrong fault.
var emailish = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// BuildAgentCreate validates a creation locally and returns the body.
//
// Every check here corresponds to a refusal observed live, and names the flag
// rather than the field: the server says "ownerId cannot be cleared" when
// ownerId is simply absent from a create, which is not a sentence an operator
// can act on.
func BuildAgentCreate(c AgentCreate) (json.RawMessage, error) {
	c.Name = strings.TrimSpace(c.Name)
	c.Email = strings.TrimSpace(c.Email)
	c.OwnerID = strings.TrimSpace(c.OwnerID)

	if c.Name == "" {
		return nil, fmt.Errorf("an agent needs a name: pass --name")
	}
	if c.Email == "" {
		return nil, fmt.Errorf("an agent needs an email: pass --email. It identifies the agent "+
			"and must not already belong to another agent in %s", "this organization")
	}
	if !emailish.MatchString(c.Email) {
		return nil, fmt.Errorf("%q does not look like an email address — the server rejects a "+
			"malformed one with a message about it being empty, which is not what is wrong", c.Email)
	}
	if c.OwnerID == "" {
		return nil, fmt.Errorf("an agent needs an owner: pass --owner with the JumpCloud user id " +
			"of the admin responsible for it")
	}
	if !IsObjectID(c.OwnerID) {
		return nil, fmt.Errorf("--owner %q is not a JumpCloud object id: it must be the "+
			"24-character hex id of an ACTIVATED user in this organization", c.OwnerID)
	}
	return json.Marshal(c)
}

// EmailConflict recognises the server's refusal of an email already in use,
// so a surface can say which field collided rather than reporting a generic
// 4xx. An agent's email is its identifier and is unique per organization.
func EmailConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "email is already in use")
}

// InvalidOwner recognises an ownerId that is well-formed but not an activated
// user here. Worth separating from a malformed id: the fix is different —
// one is a typo, the other is the wrong person or an unactivated account.
func InvalidOwner(err error) bool {
	return err != nil && strings.Contains(err.Error(), "must be an activated SystemUser")
}

// --- Updating an agent ----------------------------------------------------

// agentMutableFields are the fields PATCH /agents/{id} accepts. state is
// absent deliberately; see AgentStateIsNotPatchable.
var agentMutableFields = []string{"allowedAudiences", "description", "email", "name", "ownerId"}

// AgentMutableFields is for help text and MCP schemas.
func AgentMutableFields() []string { return append([]string(nil), agentMutableFields...) }

// AgentStateIsNotPatchable records the refusal, and why no surface offers a
// --state flag on update.
const AgentStateIsNotPatchable = "an agent's state is changed with suspend or activate, not by " +
	"updating the state field — the API refuses that with \"state cannot be changed with PATCH\""

// AgentPatchIsPartial records that an agent update sends only what changed.
// Verified: patching name alone left description, email, ownerId and
// allowedAudiences untouched.
const AgentPatchIsPartial = "an agent update changes only the fields you pass; the rest are left alone"

// BuildAgentPatch assembles a partial update from the fields a caller set.
//
// A nil pointer means "not mentioned" and is omitted, which is what makes
// this partial. An empty body is refused locally because the server refuses
// it too ("patch body must include at least one mutable field") — and a
// command that made that request would have burned a round trip to be told
// the operator passed no flags.
func BuildAgentPatch(name, email, description, ownerID *string, audiences *[]string) (json.RawMessage, error) {
	body := map[string]any{}
	if name != nil {
		if strings.TrimSpace(*name) == "" {
			return nil, fmt.Errorf("--name cannot be set to an empty value: the server requires a " +
				"non-blank name, and there is no way to clear one")
		}
		body["name"] = *name
	}
	if email != nil {
		if !emailish.MatchString(strings.TrimSpace(*email)) {
			return nil, fmt.Errorf("--email %q does not look like an email address", *email)
		}
		body["email"] = *email
	}
	if description != nil {
		// An empty description IS meaningful here — it clears the field —
		// which is why this one is not blank-checked.
		body["description"] = *description
	}
	if ownerID != nil {
		if !IsObjectID(strings.TrimSpace(*ownerID)) {
			return nil, fmt.Errorf("--owner %q is not a JumpCloud object id", *ownerID)
		}
		body["ownerId"] = *ownerID
	}
	if audiences != nil {
		body["allowedAudiences"] = *audiences
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("nothing to update: pass at least one of %s",
			strings.Join([]string{"--name", "--email", "--description", "--owner", "--audience"}, ", "))
	}
	return json.Marshal(body)
}

// --- Suspending and activating --------------------------------------------

// StateTransitionsAreIdempotent records that suspending a suspended agent, or
// activating an active one, succeeds and reports the same state. Verified
// live by calling each twice.
//
// It means no surface needs to read the state first to decide whether a
// transition is legal, and that a retry after a timeout is safe.
const StateTransitionsAreIdempotent = "suspending an already-suspended agent succeeds and " +
	"changes nothing; the same is true of activating an active one"

// ParseAgentState reads the state out of an agent record, for the read-back
// that follows a transition.
func ParseAgentState(raw json.RawMessage) (string, error) {
	var rec struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return "", fmt.Errorf("decoding the agent response: %w", err)
	}
	if rec.State == "" {
		return "", fmt.Errorf("the agent response carried no state — jc cannot confirm the " +
			"transition took effect, and reporting success on an unreadable answer would be a guess")
	}
	return rec.State, nil
}

// --- Groups ---------------------------------------------------------------

// BuildGroupCreate is the body of POST /agentgroups. Only name is required,
// and no organization header is needed.
func BuildGroupCreate(name, description string) (json.RawMessage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("an agent group needs a name: pass --name")
	}
	body := map[string]any{"name": name}
	if description != "" {
		body["description"] = description
	}
	return json.Marshal(body)
}

// GroupAlreadyExists recognises the server's refusal of a duplicate group
// name. Group names are unique, which the spec does not say.
func GroupAlreadyExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Already Exists")
}

// GroupPatchIsFullReplace is the trap in this area, and the reason
// MergeGroupPatch exists.
//
// PATCH /agentgroups/{id} requires `name` and CLEARS every field it is not
// given. Verified: a group created with a description, then patched with
// {"name": "<same name>"} alone, came back with description "". Nothing in
// the request said to clear it.
const GroupPatchIsFullReplace = "an agent group update REPLACES the group: the API requires the " +
	"name and clears any field you leave out, so jc reads the group first and merges"

// MergeGroupPatch builds a group update by merging changes onto the group as
// it currently is.
//
// This is the GET-then-merge jc already does for v2 PUTs, needed here even
// though the verb is PATCH. The existing record comes from a read performed
// immediately before; a nil change means "leave as is", which only works
// because the current value is resent explicitly.
func MergeGroupPatch(existing json.RawMessage, name, description *string) (json.RawMessage, error) {
	var current struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(existing, &current); err != nil {
		return nil, fmt.Errorf("decoding the group before updating it: %w — jc will not send an "+
			"update it cannot base on the current record, because this endpoint clears what it "+
			"is not given", err)
	}
	if current.Name == "" {
		return nil, fmt.Errorf("the group record carried no name, and this endpoint requires one — " +
			"sending the update anyway would be refused, and guessing the name would rename the group")
	}

	body := map[string]any{"name": current.Name, "description": current.Description}
	if name != nil {
		if strings.TrimSpace(*name) == "" {
			return nil, fmt.Errorf("--name cannot be set to an empty value")
		}
		body["name"] = *name
	}
	if description != nil {
		body["description"] = *description
	}
	if name == nil && description == nil {
		return nil, fmt.Errorf("nothing to update: pass --name or --description")
	}
	return json.Marshal(body)
}

// GroupDeleteReturnsTheDeletedRecord records that DELETE /agentgroups/{id}
// answers with the group it removed, which is what a surface reads back to
// confirm. An agent DELETE returns an empty body instead.
const GroupDeleteReturnsTheDeletedRecord = "deleting an agent group returns the group it removed"

// DeletingAGroupLeavesItsAgents records the blast radius of a group delete,
// established by deleting a group that still had a member: the group goes,
// the agent remains, and its memberof becomes empty. Nothing cascades.
const DeletingAGroupLeavesItsAgents = "deleting an agent group does not delete the agents in it; " +
	"they simply lose the access the group granted"

// --- Membership and associations ------------------------------------------

// Membership operations. ONE endpoint handles both directions, selected by
// the op field — the same shape as jc's user-group membership and graph
// bindings. There is no DELETE: DELETE /agentgroups/{id}/members answers 404.
const (
	OpAdd    = "add"
	OpRemove = "remove"
)

// MembershipOps is the whole enum, server-validated.
var MembershipOps = []string{OpAdd, OpRemove}

// MemberTypeAgent is the only type a group takes as a member.
const MemberTypeAgent = "agent"

// MembershipBody is the body of POST /agentgroups/{id}/members and
// .../associations.
//
// op is REQUIRED — without it the server answers "missing required property:
// op" — and both op and type are enum-validated, so a bad value is a 400
// rather than a silent no-op.
func MembershipBody(op, id, objType string) (json.RawMessage, error) {
	if !contains(MembershipOps, op) {
		return nil, fmt.Errorf("invalid membership operation %q: expected %s",
			op, strings.Join(MembershipOps, " or "))
	}
	if !IsObjectID(id) {
		return nil, ErrNotObjectID(objType, id)
	}
	if objType == "" {
		return nil, fmt.Errorf("a membership change needs a target type")
	}
	return json.Marshal(map[string]any{"op": op, "id": id, "type": objType})
}

// MembershipWriteReturnsNothing records that a successful membership change
// answers 200 with an EMPTY BODY. There is nothing in the response to
// confirm, which is why every one of these writes reads the membership back.
const MembershipWriteReturnsNothing = "a membership change returns an empty body, so jc reads the " +
	"membership back rather than reporting a success it cannot see"

// The four ways a membership change can fail, and how to tell them apart.
//
// This took two probe rounds to get right, and the first version of it was
// WRONG in a way worth recording: the obvious test for "remove something
// that is not a member" uses an id like 000000000000000000000000, which
// exercises a DIFFERENT path — the server distinguishes "this link does not
// exist" from "this object does not exist", and a probe using a fake id only
// ever sees the second. Checked again with a real agent that was genuinely
// not in the group:
//
//	remove, link absent        404 {"message":"Not Found"}
//	remove, target absent      400 "failed to get graph connection (…to=agent:X)"
//	remove/add, GROUP absent   400 "failed to … graph connection (…): agent_group not found"
//	add, already a member      "Already Exists"
//	add, target absent         400 "failed to insert graph connection (…)"
//
// The bare 404 is therefore unambiguous: a missing group says so explicitly,
// so a 404 on a remove means the group and the target both exist and the
// link between them does not.

// AlreadyAMember recognises the refusal of a duplicate add. The first add
// succeeds, a second answers "Already Exists" — so the operation is NOT
// idempotent, and a retry after a timeout can report a failure for a change
// that already took effect.
func AlreadyAMember(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Already Exists")
}

// LinkAbsent recognises the bare 404 a remove gets when the membership it
// was asked to remove is not there.
//
// Only meaningful for a remove, which is why it is consulted from
// ExplainMembershipError rather than exported as a general predicate: a 404
// on some other call means something else.
func LinkAbsent(err error) bool {
	return err != nil && strings.Contains(err.Error(), `"message":"Not Found"`)
}

// MissingGroup recognises the group itself being absent, which the server
// says explicitly inside its graph error.
func MissingGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "agent_group not found")
}

// GraphOpFailed recognises the server's internal graph messages, which leak
// its storage model and name nothing an operator typed.
func GraphOpFailed(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "failed to get graph connection") ||
		strings.Contains(err.Error(), "failed to insert graph connection")
}

// ExplainMembershipError turns each of those into a sentence about what the
// operator actually did.
func ExplainMembershipError(err error, op, memberKind, memberID, groupID string) error {
	if err == nil {
		return nil
	}
	switch {
	// Checked before the generic graph predicate, because a missing group
	// arrives wrapped in the same message as a missing target.
	case MissingGroup(err):
		return fmt.Errorf("agent group %s does not exist in this organization — "+
			"list them with: jc agent-groups list", groupID)

	case AlreadyAMember(err) && op == OpAdd:
		return fmt.Errorf("%s %s is already in group %s — nothing to do. Note this API refuses "+
			"a duplicate rather than ignoring it, so a retried add can report this for a "+
			"change that already succeeded", memberKind, memberID, groupID)

	case LinkAbsent(err) && op == OpRemove:
		return fmt.Errorf("%s %s is not in group %s, so there was nothing to remove",
			memberKind, memberID, groupID)

	case GraphOpFailed(err):
		return fmt.Errorf("could not change group %s: the API could not %s the link to %s %s, "+
			"which means that id does not exist in this organization",
			groupID, map[string]string{OpAdd: "create", OpRemove: "find"}[op], memberKind, memberID)
	}
	return err
}
