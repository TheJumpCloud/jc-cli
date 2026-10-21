package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeBody(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	return m
}

// --- Creating an agent ----------------------------------------------------

func TestBuildAgentCreate(t *testing.T) {
	raw, err := BuildAgentCreate(AgentCreate{
		Name:    "my-agent",
		Email:   "my-agent@example.com",
		OwnerID: "5ec7221c4d224c0e309577e7",
	})
	if err != nil {
		t.Fatalf("BuildAgentCreate: %v", err)
	}
	body := decodeBody(t, raw)
	for _, k := range []string{"name", "email", "ownerId"} {
		if body[k] == nil {
			t.Errorf("body is missing %q, which the server requires", k)
		}
	}
	// Optional fields must not appear as empty values; the server treats an
	// empty ownerId on a create as "cleared" and refuses it.
	if _, ok := body["description"]; ok {
		t.Error("an unset description was sent as an empty value")
	}
}

// Each of these corresponds to a refusal observed live, and the point of
// checking locally is that the server's message names the wrong problem.
func TestBuildAgentCreate_RequiredFields(t *testing.T) {
	ok := AgentCreate{Name: "a", Email: "a@example.com", OwnerID: "5ec7221c4d224c0e309577e7"}

	cases := []struct {
		name  string
		edit  func(*AgentCreate)
		wants string
	}{
		{"no name", func(c *AgentCreate) { c.Name = "" }, "--name"},
		{"blank name", func(c *AgentCreate) { c.Name = "   " }, "--name"},
		{"no email", func(c *AgentCreate) { c.Email = "" }, "--email"},
		{"no owner", func(c *AgentCreate) { c.OwnerID = "" }, "--owner"},
		{"malformed owner", func(c *AgentCreate) { c.OwnerID = "nope" }, "24-character hex"},
	}
	for _, c := range cases {
		in := ok
		c.edit(&in)
		_, err := BuildAgentCreate(in)
		if err == nil {
			t.Errorf("%s was accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.wants) {
			t.Errorf("%s: error does not mention %q: %v", c.name, c.wants, err)
		}
	}
}

// The server rejects "not-an-email" with "email cannot be empty or
// whitespace-only" — a message about a different problem. Checking locally
// is the only way an operator learns what is actually wrong.
func TestBuildAgentCreate_MalformedEmail(t *testing.T) {
	_, err := BuildAgentCreate(AgentCreate{
		Name: "a", Email: "not-an-email", OwnerID: "5ec7221c4d224c0e309577e7",
	})
	if err == nil {
		t.Fatal("a malformed email was accepted")
	}
	if !strings.Contains(err.Error(), "not what is wrong") {
		t.Errorf("the error does not explain that the server's message misdescribes it: %v", err)
	}
}

func TestEmailConflictAndInvalidOwner(t *testing.T) {
	if !EmailConflict(errString(`{"message":"email is already in use","status":"emailConflict"}`)) {
		t.Error("EmailConflict did not recognise the duplicate-email refusal")
	}
	if EmailConflict(errString(`{"message":"ownerId cannot be cleared"}`)) {
		t.Error("EmailConflict matched an unrelated error")
	}
	if !InvalidOwner(errString(`{"message":"ownerId must be an activated SystemUser in this organization"}`)) {
		t.Error("InvalidOwner did not recognise the unactivated-owner refusal")
	}
	// A malformed id is a different fault with a different fix, and must not
	// be conflated with a well-formed id that is not an activated user.
	if InvalidOwner(errString(`{"message":"ownerId must be a 24-character hexadecimal object id"}`)) {
		t.Error("InvalidOwner matched a malformed id, which is a typo rather than the wrong person")
	}
}

// --- Updating an agent: PARTIAL -------------------------------------------

func TestBuildAgentPatch_SendsOnlyWhatChanged(t *testing.T) {
	name := "renamed"
	raw, err := BuildAgentPatch(&name, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildAgentPatch: %v", err)
	}
	body := decodeBody(t, raw)
	if len(body) != 1 || body["name"] != "renamed" {
		t.Errorf("body = %v, want only the name — this endpoint is partial, and sending more "+
			"would overwrite fields the operator never mentioned", body)
	}
}

func TestBuildAgentPatch_EmptyIsRefusedLocally(t *testing.T) {
	// The server refuses this too ("patch body must include at least one
	// mutable field"); failing here saves a round trip to be told the
	// operator passed no flags.
	_, err := BuildAgentPatch(nil, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("an empty patch was accepted")
	}
	if !strings.Contains(err.Error(), "--name") {
		t.Errorf("the error does not list the flags that would work: %v", err)
	}
}

// An empty description CLEARS the field and is a legitimate request, unlike
// an empty name, which the server will not accept at all.
func TestBuildAgentPatch_EmptyDescriptionIsMeaningful(t *testing.T) {
	empty := ""
	raw, err := BuildAgentPatch(nil, nil, &empty, nil, nil)
	if err != nil {
		t.Fatalf("clearing a description was refused: %v", err)
	}
	body := decodeBody(t, raw)
	if v, ok := body["description"]; !ok || v != "" {
		t.Errorf("body = %v, want an explicit empty description", body)
	}
}

func TestBuildAgentPatch_EmptyNameIsRefused(t *testing.T) {
	empty := ""
	if _, err := BuildAgentPatch(&empty, nil, nil, nil, nil); err == nil {
		t.Error("an empty name was accepted; the server requires a non-blank one and there is " +
			"no way to clear it")
	}
}

func TestBuildAgentPatch_AudiencesCanBeCleared(t *testing.T) {
	none := []string{}
	raw, err := BuildAgentPatch(nil, nil, nil, nil, &none)
	if err != nil {
		t.Fatalf("BuildAgentPatch: %v", err)
	}
	body := decodeBody(t, raw)
	got, ok := body["allowedAudiences"].([]any)
	if !ok || len(got) != 0 {
		t.Errorf("body = %v, want an explicit empty audience list", body)
	}
}

func TestBuildAgentPatch_RejectsBadValues(t *testing.T) {
	bad := "not-an-email"
	if _, err := BuildAgentPatch(nil, &bad, nil, nil, nil); err == nil {
		t.Error("a malformed email was accepted on update")
	}
	owner := "nope"
	if _, err := BuildAgentPatch(nil, nil, nil, &owner, nil); err == nil {
		t.Error("a malformed owner id was accepted on update")
	}
}

// state is not in the mutable set, and no surface offers a flag for it.
func TestAgentMutableFields_ExcludeState(t *testing.T) {
	for _, f := range AgentMutableFields() {
		if f == "state" {
			t.Error("state is listed as mutable, but PATCH refuses it with \"state cannot be " +
				"changed with PATCH; use suspend or activate\"")
		}
	}
}

// --- State ----------------------------------------------------------------

func TestParseAgentState(t *testing.T) {
	got, err := ParseAgentState(json.RawMessage(`{"id":"a","state":"suspended"}`))
	if err != nil {
		t.Fatalf("ParseAgentState: %v", err)
	}
	if got != "suspended" {
		t.Errorf("state = %q, want suspended", got)
	}
}

// Absent data is not evidence of a negative: a response with no state must
// not read as a successful transition.
func TestParseAgentState_MissingStateIsAnError(t *testing.T) {
	if _, err := ParseAgentState(json.RawMessage(`{"id":"a"}`)); err == nil {
		t.Error("a response with no state was accepted, which would report a transition jc " +
			"could not confirm")
	}
	if _, err := ParseAgentState(json.RawMessage(`not json`)); err == nil {
		t.Error("an unreadable response was accepted")
	}
}

// --- Groups: FULL REPLACE -------------------------------------------------

func TestBuildGroupCreate(t *testing.T) {
	raw, err := BuildGroupCreate("my-group", "")
	if err != nil {
		t.Fatalf("BuildGroupCreate: %v", err)
	}
	body := decodeBody(t, raw)
	if body["name"] != "my-group" {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["description"]; ok {
		t.Error("an unset description was sent")
	}
	if _, err := BuildGroupCreate("  ", ""); err == nil {
		t.Error("a blank group name was accepted")
	}
}

// The trap. A group update that sent only the changed field would clear the
// description, which is why MergeGroupPatch resends the current values.
func TestMergeGroupPatch_PreservesWhatWasNotMentioned(t *testing.T) {
	existing := json.RawMessage(`{"id":"g1","name":"my-group","description":"important note","email":""}`)
	newName := "renamed"

	raw, err := MergeGroupPatch(existing, &newName, nil)
	if err != nil {
		t.Fatalf("MergeGroupPatch: %v", err)
	}
	body := decodeBody(t, raw)
	if body["name"] != "renamed" {
		t.Errorf("name = %v, want renamed", body["name"])
	}
	if body["description"] != "important note" {
		t.Errorf("description = %v, want it preserved — this endpoint CLEARS what it is not "+
			"given, so a rename would otherwise destroy the description", body["description"])
	}
}

func TestMergeGroupPatch_AlwaysSendsTheName(t *testing.T) {
	// name is required on every group update, even one that only changes the
	// description.
	existing := json.RawMessage(`{"id":"g1","name":"my-group","description":"old"}`)
	newDesc := "new"
	raw, err := MergeGroupPatch(existing, nil, &newDesc)
	if err != nil {
		t.Fatalf("MergeGroupPatch: %v", err)
	}
	body := decodeBody(t, raw)
	if body["name"] != "my-group" {
		t.Errorf("name = %v, want the current name resent — the server rejects an update "+
			"without one", body["name"])
	}
	if body["description"] != "new" {
		t.Errorf("description = %v, want new", body["description"])
	}
}

func TestMergeGroupPatch_CanClearADescription(t *testing.T) {
	existing := json.RawMessage(`{"id":"g1","name":"g","description":"old"}`)
	empty := ""
	raw, err := MergeGroupPatch(existing, nil, &empty)
	if err != nil {
		t.Fatalf("MergeGroupPatch: %v", err)
	}
	if decodeBody(t, raw)["description"] != "" {
		t.Error("an explicit empty description did not clear the field")
	}
}

func TestMergeGroupPatch_RefusesToGuess(t *testing.T) {
	// A record jc cannot read is not a base it can merge onto, and sending
	// the update anyway would clear every field.
	if _, err := MergeGroupPatch(json.RawMessage(`not json`), nil, nil); err == nil {
		t.Error("an unreadable group record was accepted as a merge base")
	}
	// A record with no name would force jc to either send a refused request
	// or invent a name, and inventing one renames the group.
	name := "x"
	if _, err := MergeGroupPatch(json.RawMessage(`{"id":"g1","description":"d"}`), &name, nil); err == nil {
		t.Error("a group record with no name was accepted as a merge base")
	}
	if _, err := MergeGroupPatch(json.RawMessage(`{"id":"g1","name":"g"}`), nil, nil); err == nil {
		t.Error("an update with no changes was accepted")
	}
}

func TestGroupAlreadyExists(t *testing.T) {
	if !GroupAlreadyExists(errString(`{"message":"Already Exists"}`)) {
		t.Error("GroupAlreadyExists did not recognise the duplicate-name refusal")
	}
}

// --- Membership -----------------------------------------------------------

func TestMembershipBody(t *testing.T) {
	raw, err := MembershipBody(OpAdd, "6aa85171a36f7d16b20d9b60", MemberTypeAgent)
	if err != nil {
		t.Fatalf("MembershipBody: %v", err)
	}
	body := decodeBody(t, raw)
	// op is required; without it the server answers "missing required
	// property: op".
	if body["op"] != "add" {
		t.Errorf("op = %v, want add", body["op"])
	}
	if body["type"] != "agent" || body["id"] == nil {
		t.Errorf("body = %v", body)
	}
}

func TestMembershipBody_ValidatesTheOp(t *testing.T) {
	// The server enum-validates this, so a typo is a 400 rather than a
	// silent no-op — but failing locally names the two that work.
	_, err := MembershipBody("replace", "6aa85171a36f7d16b20d9b60", MemberTypeAgent)
	if err == nil {
		t.Fatal("an invalid op was accepted")
	}
	if !strings.Contains(err.Error(), "add") || !strings.Contains(err.Error(), "remove") {
		t.Errorf("the error does not name the valid ops: %v", err)
	}
}

func TestMembershipBody_ValidatesTheID(t *testing.T) {
	if _, err := MembershipBody(OpAdd, "not-an-id", MemberTypeAgent); err == nil {
		t.Error("a malformed member id was accepted")
	}
}

// Each case here is a message observed live. The first version of this test
// was wrong in the same way the code was: it asserted that removing a
// non-member produces "failed to get graph connection", which is what you
// see only if you probe with a fake id. A real agent that is genuinely not
// in the group gets a bare 404.
func TestExplainMembershipError(t *testing.T) {
	const gid = "6aa85141bda774000189bca6"
	const aid = "6aa85171a36f7d16b20d9b60"

	t.Run("remove when the link is absent", func(t *testing.T) {
		err := errString(`JumpCloud API error (HTTP 404) /agentgroups/x/members: {"message":"Not Found"}`)
		got := ExplainMembershipError(err, OpRemove, "agent", aid, gid)
		if !strings.Contains(got.Error(), "is not in group") {
			t.Errorf("not explained: %v", got)
		}
		if !strings.Contains(got.Error(), "nothing to remove") {
			t.Errorf("does not say the request was a no-op: %v", got)
		}
	})

	t.Run("the group itself is absent", func(t *testing.T) {
		// Arrives wrapped in the same graph message as a missing target, so
		// it has to be checked first.
		err := errString(`{"message":"failed to get graph connection (org=5ec7,from=agent_group:000): failed to get graph connection: agent_group not found"}`)
		got := ExplainMembershipError(err, OpRemove, "agent", aid, gid)
		if !strings.Contains(got.Error(), "does not exist") || !strings.Contains(got.Error(), "agent group") {
			t.Errorf("a missing group was not identified as such: %v", got)
		}
		if strings.Contains(got.Error(), "is not in group") {
			t.Errorf("a missing group was reported as a missing membership: %v", got)
		}
	})

	t.Run("the target is absent", func(t *testing.T) {
		err := errString(`{"message":"failed to insert graph connection (org=5ec7,from=agent_group:6aa8,to=agent:000)"}`)
		got := ExplainMembershipError(err, OpAdd, "agent", aid, gid)
		if !strings.Contains(got.Error(), "does not exist in this organization") {
			t.Errorf("a missing target was not explained: %v", got)
		}
		// The server's internal message must not be the whole answer.
		if strings.Contains(got.Error(), "org=5ec7") {
			t.Errorf("the internal graph message was forwarded verbatim: %v", got)
		}
	})

	t.Run("duplicate add", func(t *testing.T) {
		got := ExplainMembershipError(errString(`{"message":"Already Exists"}`), OpAdd, "agent", aid, gid)
		if !strings.Contains(got.Error(), "already in group") {
			t.Errorf("not explained: %v", got)
		}
		// The non-idempotence matters: a retry can report this for a change
		// that already worked.
		if !strings.Contains(got.Error(), "retried") {
			t.Errorf("does not warn that a retry can report this falsely: %v", got)
		}
	})

	t.Run("anything else passes through", func(t *testing.T) {
		if ExplainMembershipError(errString("connection refused"), OpAdd, "agent", aid, gid).Error() != "connection refused" {
			t.Error("an unrelated error was rewritten")
		}
		if ExplainMembershipError(nil, OpAdd, "agent", aid, gid) != nil {
			t.Error("nil became an error")
		}
	})

	// A 404 is only a missing membership on a REMOVE. Attributing it to an
	// add would be inventing a cause.
	t.Run("a 404 on an add is not claimed to be a missing membership", func(t *testing.T) {
		err := errString(`JumpCloud API error (HTTP 404) /x: {"message":"Not Found"}`)
		got := ExplainMembershipError(err, OpAdd, "agent", aid, gid)
		if strings.Contains(got.Error(), "nothing to remove") {
			t.Errorf("a 404 on an add was explained as a remove: %v", got)
		}
	})
}

func TestMembershipOps_AreOnlyAddAndRemove(t *testing.T) {
	// There is no DELETE on these endpoints — DELETE /members answers 404 —
	// so remove has to go through the op field.
	if len(MembershipOps) != 2 {
		t.Errorf("MembershipOps = %v, want exactly add and remove", MembershipOps)
	}
}

func TestStateAndEndpointSpellings(t *testing.T) {
	const id = "aaa"
	if got := AgentSuspendEndpoint(id); got != "/agents/aaa/suspend" {
		t.Errorf("suspend endpoint = %q", got)
	}
	if got := AgentActivateEndpoint(id); got != "/agents/aaa/activate" {
		t.Errorf("activate endpoint = %q", got)
	}
}
