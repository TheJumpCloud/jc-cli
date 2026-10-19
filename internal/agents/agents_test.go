package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- The x-org-id header --------------------------------------------------

// The predicate is prefix-based, and "/agents" is a prefix of "/agentgroups".
// Getting that wrong inverts the answer for all nine group operations, so
// every endpoint in the area is named here explicitly rather than pattern
// matched.
func TestRequiresOrgID(t *testing.T) {
	const agentID = "6aa85171a36f7d16b20d9b60"
	const groupID = "6aa85141bda774000189bca6"

	cases := []struct {
		endpoint string
		want     bool
	}{
		// Verified live: 400 {"message":"x-org-id is required"} without it.
		{AgentsEndpoint, true},
		{AgentEndpoint(agentID), true},
		{AgentCredentialsEndpoint(agentID), true},

		// Verified live: answers correctly with no header. These two are
		// served by the older association subsystem.
		{AgentMemberOfEndpoint(agentID), false},
		{AgentApplicationsEndpoint(agentID), false},

		// The whole group tree. None of these want the header, and every one
		// of them starts with the string "/agents".
		{GroupsEndpoint, false},
		{GroupEndpoint(groupID), false},
		{GroupMembersEndpoint(groupID), false},
		{GroupAssociationsEndpoint(groupID), false},

		// Nothing outside the area.
		{"/users", false},
		{"/systemgroups", false},
	}
	for _, c := range cases {
		if got := RequiresOrgID(c.endpoint); got != c.want {
			t.Errorf("RequiresOrgID(%q) = %v, want %v", c.endpoint, got, c.want)
		}
	}
}

// The two association reads are the whole content of this predicate, and the
// only part of it that is not derivable from the path. They are called out
// separately so a regression that drops the exception names itself rather
// than appearing as one row of a table.
func TestRequiresOrgID_TheTwoAssociationReadsAreExceptions(t *testing.T) {
	const agentID = "6aa85171a36f7d16b20d9b60"

	// Both live under /agents and both answer correctly with no header: they
	// are served by the same older association subsystem that answers
	// /users/{id}/memberof. Requiring an org id for them would make jc
	// refuse a call the server would have accepted.
	for _, endpoint := range []string{AgentMemberOfEndpoint(agentID), AgentApplicationsEndpoint(agentID)} {
		if RequiresOrgID(endpoint) {
			t.Errorf("RequiresOrgID(%q) = true, but that endpoint answers without the header", endpoint)
		}
	}
	// Their sibling under the same agent does require it, so the exception
	// has to be the suffix and cannot be the /agents prefix.
	if !RequiresOrgID(AgentCredentialsEndpoint(agentID)) {
		t.Error("RequiresOrgID said the credentials listing needs no org id, but it answers " +
			"400 \"x-org-id is required\" without one")
	}
}

// The shared prefix between the two trees is "/agent", not "/agents". A
// predicate written against the shorter one answers true for every group
// operation; this is the test that would catch it.
func TestRequiresOrgID_IsNotMatchedOnTheSharedPrefix(t *testing.T) {
	if !strings.HasPrefix(GroupsEndpoint, "/agent") {
		t.Fatalf("GroupsEndpoint = %q no longer shares a prefix with the agents tree, "+
			"so this test is measuring nothing", GroupsEndpoint)
	}
	if strings.HasPrefix(GroupsEndpoint, AgentsEndpoint) {
		t.Fatalf("%q now starts with %q, which would make RequiresOrgID's prefix test "+
			"claim every group operation needs an org id", GroupsEndpoint, AgentsEndpoint)
	}
	if RequiresOrgID(GroupsEndpoint) {
		t.Error("the agent groups listing does not take an x-org-id header, but RequiresOrgID said it needs one")
	}
}

func TestOrgMismatch(t *testing.T) {
	// The server's answer to an org id the calling key does not own. It is a
	// 404, which otherwise reads as "no such agent".
	err := errString(`JumpCloud API error (HTTP 404) /agents: {"error":"Not Found: selected organization not found"}`)
	if !OrgMismatch(err) {
		t.Error("OrgMismatch did not recognise the server's wrong-org 404")
	}
	if OrgMismatch(errString(`JumpCloud API error (HTTP 404) /agents/xxx: {"message":"Not Found"}`)) {
		t.Error("OrgMismatch matched an ordinary not-found, which would send an operator " +
			"to check their config when the agent is simply absent")
	}
	if OrgMismatch(nil) {
		t.Error("OrgMismatch(nil) = true")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// --- Ids ------------------------------------------------------------------

func TestIsObjectID(t *testing.T) {
	for _, ok := range []string{"6aa85171a36f7d16b20d9b60", "6AA85171A36F7D16B20D9B60"} {
		if !IsObjectID(ok) {
			t.Errorf("IsObjectID(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "not-an-id", "6aa85171a36f7d16b20d9b6", "6aa85171a36f7d16b20d9b600", "zzz85171a36f7d16b20d9b60"} {
		if IsObjectID(bad) {
			t.Errorf("IsObjectID(%q) = true, want false", bad)
		}
	}
}

// --- Pagination -----------------------------------------------------------

func TestAgentsPageQuery_OmitsEmptyValues(t *testing.T) {
	if got := AgentsPageQuery(0, ""); len(got) != 0 {
		t.Errorf("AgentsPageQuery(0, \"\") = %v, want empty — a pageSize of 0 means the server's default", got)
	}
	got := AgentsPageQuery(50, "tok")
	if got.Get("pageSize") != "50" || got.Get("pageToken") != "tok" {
		t.Errorf("AgentsPageQuery(50, \"tok\") = %v", got)
	}
}

func TestAgentsPageQuery_NeverSendsOffsetParams(t *testing.T) {
	// /agents ignores limit and skip in silence. If either ever appears here
	// it would read as working while doing nothing.
	got := AgentsPageQuery(50, "tok")
	for _, banned := range []string{"limit", "skip", "sort"} {
		if got.Has(banned) {
			t.Errorf("AgentsPageQuery sent %q, which the agents listing silently ignores", banned)
		}
	}
}

func TestGroupsPageQuery(t *testing.T) {
	if got := GroupsPageQuery(0, 0); len(got) != 0 {
		t.Errorf("GroupsPageQuery(0, 0) = %v, want empty", got)
	}
	got := GroupsPageQuery(10, 5)
	if got.Get("limit") != "10" || got.Get("skip") != "5" {
		t.Errorf("GroupsPageQuery(10, 5) = %v", got)
	}
	if got.Has("pageSize") {
		t.Error("GroupsPageQuery sent pageSize, which the agent groups listing ignores")
	}
}

// --- Filtering ------------------------------------------------------------

func TestValidateAgentFilter(t *testing.T) {
	// Verified live against the server's own validation.
	for _, ok := range []string{"name:eq:AWS JKC Agent", "email:ne:x@example.com", "state:eq:suspended"} {
		if err := ValidateAgentFilter(ok); err != nil {
			t.Errorf("ValidateAgentFilter(%q) = %v, want nil", ok, err)
		}
	}
	// The server answers each of these 400 "unsupported filter field".
	for _, bad := range []string{"id:eq:x", "ownerId:eq:x", "description:eq:x", "organizationId:eq:x", "allowedAudiences:eq:x"} {
		if err := ValidateAgentFilter(bad); err == nil {
			t.Errorf("ValidateAgentFilter(%q) = nil, but the server rejects that field", bad)
		}
	}
	// There is no substring match in this area.
	for _, bad := range []string{"name:contains:AWS", "name:startsWith:AWS", "name:gt:AWS"} {
		err := ValidateAgentFilter(bad)
		if err == nil {
			t.Errorf("ValidateAgentFilter(%q) = nil, but the server rejects that operator", bad)
		} else if !strings.Contains(err.Error(), "substring") {
			t.Errorf("ValidateAgentFilter(%q) should say no substring match is available, got: %v", bad, err)
		}
	}
}

func TestValidateGroupFilter(t *testing.T) {
	for _, ok := range []string{"name:eq:SF Agent", "type:eq:agent_group", "name:ne:x"} {
		if err := ValidateGroupFilter(ok); err != nil {
			t.Errorf("ValidateGroupFilter(%q) = %v, want nil", ok, err)
		}
	}
	// The groups listing answers these with 500 "failed to list groups:
	// failed to list collection objects", which tells an operator nothing —
	// which is why they are caught here instead of forwarded.
	for _, bad := range []string{"id:eq:x", "email:eq:x", "description:eq:x", "organizationObjectId:eq:x"} {
		if err := ValidateGroupFilter(bad); err == nil {
			t.Errorf("ValidateGroupFilter(%q) = nil, but the server fails that field with an internal error", bad)
		}
	}
	// The two listings do NOT accept the same fields. email works on agents
	// and fails on groups; type is the other way round.
	if ValidateAgentFilter("type:eq:agent") == nil {
		t.Error("the agents listing does not filter on type")
	}
	if ValidateGroupFilter("email:eq:x@example.com") == nil {
		t.Error("the agent groups listing does not filter on email")
	}
}

func TestValidateFilter_Malformed(t *testing.T) {
	for _, bad := range []string{"", "name", "name:eq", "name:eq:"} {
		if err := ValidateAgentFilter(bad); err == nil {
			t.Errorf("ValidateAgentFilter(%q) = nil, want an error", bad)
		}
	}
}

func TestValidateFilter_ValueMayContainColons(t *testing.T) {
	// Splitting on every colon would reject a legitimate value.
	if err := ValidateAgentFilter("name:eq:agent:with:colons"); err != nil {
		t.Errorf("a filter value containing colons was rejected: %v", err)
	}
}

// --- Association targets --------------------------------------------------

func TestAssociationsQuery(t *testing.T) {
	// targets is required and the spec does not say so.
	q, err := AssociationsQuery("")
	if err != nil {
		t.Fatalf("AssociationsQuery(\"\") = %v", err)
	}
	if q.Get("targets") != AssociationTargetApplication {
		t.Errorf("AssociationsQuery defaulted targets to %q, want %q — without it the server "+
			"answers 400 \"Parameter (targets) is Required\"", q.Get("targets"), AssociationTargetApplication)
	}
	if _, err := AssociationsQuery("user_group"); err == nil {
		t.Error("AssociationsQuery accepted a target outside the enum; agent groups reach applications only")
	}
}

// --- Envelopes ------------------------------------------------------------

const agentsPage = `{"agents":[
  {"id":"6aa81dbfde6925dca6a551f1","name":"AWS BedRock AgentCore","state":"active"},
  {"id":"6aa85171a36f7d16b20d9b60","name":"AWS JKC Agent","state":"active"}
],"totalSize":3,"nextPageToken":"eyJsIjoyLCJzIjoyLCJ2IjoxfQ"}`

func TestParseAgents(t *testing.T) {
	rows, total, next, err := ParseAgents(json.RawMessage(agentsPage))
	if err != nil {
		t.Fatalf("ParseAgents: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2", len(rows))
	}
	// The count is the server's match count, not the page length. Reporting
	// len(rows) as the total is the defect this distinction prevents.
	if total != 3 {
		t.Errorf("totalSize = %d, want 3 — it counts matches, not rows on the page", total)
	}
	if next == "" {
		t.Error("nextPageToken was dropped; the paging loop would stop after one page")
	}
}

func TestParseAgents_LastPageHasNoToken(t *testing.T) {
	_, _, next, err := ParseAgents(json.RawMessage(`{"agents":[{"id":"a"}],"totalSize":3}`))
	if err != nil {
		t.Fatalf("ParseAgents: %v", err)
	}
	if next != "" {
		t.Errorf("nextPageToken = %q on a response that has none", next)
	}
}

// Absent data is not evidence of a negative: a response jc cannot read must
// not look like an empty one.
func TestParsersReportDecodeFailure(t *testing.T) {
	bad := json.RawMessage(`{"agents": "not an array"}`)
	if _, _, _, err := ParseAgents(bad); err == nil {
		t.Error("ParseAgents accepted a malformed envelope, reporting no agents for a response it could not read")
	}
	if _, err := ParseGroups(json.RawMessage(`{"not":"an array"}`)); err == nil {
		t.Error("ParseGroups accepted a malformed envelope")
	}
	if _, err := ParseCredentials(json.RawMessage(`{"credentials":5}`)); err == nil {
		t.Error("ParseCredentials accepted a malformed envelope, which would report an agent " +
			"as having no way in when it has one")
	}
	if _, err := ParseGraph(json.RawMessage(`{"not":"an array"}`), "memberof"); err == nil {
		t.Error("ParseGraph accepted a malformed envelope")
	}
	if _, err := ParseMembers(json.RawMessage(`"nope"`), "members"); err == nil {
		t.Error("ParseMembers accepted a malformed envelope")
	}
}

func TestParseGroups_BareArray(t *testing.T) {
	rows, err := ParseGroups(json.RawMessage(`[{"id":"g1","name":"AWS JKC Agent"},{"id":"g2","name":"SF Agent"}]`))
	if err != nil {
		t.Fatalf("ParseGroups: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2", len(rows))
	}
}

func TestParseCredentials_CarryNoSecret(t *testing.T) {
	// The live shape, verified against every agent on the probe tenant. The
	// absence of a secret here is what makes listing credentials an ordinary
	// read.
	const live = `{"credentials":[{"authType":"client_credentials",
      "clientId":"agent-6aa81dbfde6925dca6a551f1-3e0cd1627daff878",
      "createdAt":"2026-09-14T16:16:04.709598Z","expiresAt":"2027-09-14T16:16:04.709598Z",
      "status":"active","tokenEndpointAuthMethod":"client_secret_basic",
      "updatedAt":"2026-09-14T16:16:04.709598Z"}]}`
	rows, err := ParseCredentials(json.RawMessage(live))
	if err != nil {
		t.Fatalf("ParseCredentials: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	// Checked by KEY, not by substring: tokenEndpointAuthMethod's value is
	// "client_secret_basic", which names an auth method and contains no
	// secret. A substring test on the raw JSON fails on it, and a test that
	// cries wolf is a test someone deletes.
	var rec map[string]json.RawMessage
	if err := json.Unmarshal(rows[0], &rec); err != nil {
		t.Fatalf("decoding the credential record: %v", err)
	}
	for key := range rec {
		switch key {
		case "clientSecret", "secret", "password", "privateKey":
			t.Errorf("a credential record carried the key %q — the listing is documented as "+
				"never returning a secret, and surfaces rely on that", key)
		}
	}
	if _, ok := rec["clientId"]; !ok {
		t.Error("the credential record has no clientId, so this test is no longer reading the live shape")
	}
}

// --- Graph and membership shapes ------------------------------------------

// The live /applications response for an agent that reaches an app through a
// group: two hops, the group then the application.
const appsViaGroup = `[{"compiledAttributes":null,"id":"6331214f6655eeee1d2e2ab5","type":"application",
  "paths":[[{"to":{"id":"6aa85141bda774000189bca6","type":"agent_group"}},
            {"to":{"id":"6331214f6655eeee1d2e2ab5","type":"application"}}]]}]`

// The live /memberof response: a single hop straight to the group.
const memberOfDirect = `[{"compiledAttributes":null,"id":"6aa85141bda774000189bca6","type":"agent_group",
  "paths":[[{"to":{"id":"6aa85141bda774000189bca6","type":"agent_group"}}]]}]`

func TestParseGraph_ViaRecordsTheIntermediateGroup(t *testing.T) {
	refs, err := ParseGraph(json.RawMessage(appsViaGroup), "applications")
	if err != nil {
		t.Fatalf("ParseGraph: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %d, want 1", len(refs))
	}
	if refs[0].ID != "6331214f6655eeee1d2e2ab5" || refs[0].Type != "application" {
		t.Errorf("ref = %+v", refs[0])
	}
	if len(refs[0].Via) != 1 || refs[0].Via[0] != "6aa85141bda774000189bca6" {
		t.Errorf("Via = %v, want the group the access came through", refs[0].Via)
	}
	if refs[0].Direct() {
		t.Error("an application reached through a group reported Direct() = true")
	}
}

func TestParseGraph_DirectMembershipHasNoVia(t *testing.T) {
	refs, err := ParseGraph(json.RawMessage(memberOfDirect), "memberof")
	if err != nil {
		t.Fatalf("ParseGraph: %v", err)
	}
	if len(refs[0].Via) != 0 {
		t.Errorf("Via = %v on a single-hop path; the target is not its own intermediary", refs[0].Via)
	}
	if !refs[0].Direct() {
		t.Error("a single-hop membership reported Direct() = false")
	}
}

func TestParseGraph_RecordWithNoIDIsReported(t *testing.T) {
	// Dropping it silently would understate an agent's access, which is the
	// wrong direction for a number used in a review.
	_, err := ParseGraph(json.RawMessage(`[{"type":"application","paths":[]}]`), "applications")
	if err == nil {
		t.Error("ParseGraph silently dropped a record with no id")
	}
}

func TestParseMembers_ReadsTheNestedID(t *testing.T) {
	const live = `[{"attributes":null,"to":{"attributes":null,"id":"6aa85171a36f7d16b20d9b60","type":"agent"}}]`
	refs, err := ParseMembers(json.RawMessage(live), "members")
	if err != nil {
		t.Fatalf("ParseMembers: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "6aa85171a36f7d16b20d9b60" || refs[0].Type != "agent" {
		t.Errorf("refs = %+v — the id sits under \"to\" in this shape, not at the top level", refs)
	}
}

func TestParseMembers_RecordWithNoIDIsReported(t *testing.T) {
	if _, err := ParseMembers(json.RawMessage(`[{"attributes":null}]`), "members"); err == nil {
		t.Error("ParseMembers silently dropped a record with no id under \"to\"")
	}
}

// --- Endpoints ------------------------------------------------------------

func TestEndpoints(t *testing.T) {
	const a = "aaa"
	const g = "ggg"
	cases := map[string]string{
		AgentsEndpoint:               "/agents",
		AgentEndpoint(a):             "/agents/aaa",
		AgentMemberOfEndpoint(a):     "/agents/aaa/memberof",
		AgentApplicationsEndpoint(a): "/agents/aaa/applications",
		AgentCredentialsEndpoint(a):  "/agents/aaa/inbound-credentials",
		GroupsEndpoint:               "/agentgroups",
		GroupEndpoint(g):             "/agentgroups/ggg",
		GroupMembersEndpoint(g):      "/agentgroups/ggg/members",
		GroupAssociationsEndpoint(g): "/agentgroups/ggg/associations",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("endpoint = %q, want %q", got, want)
		}
	}
	// The server spells it one word. A hyphen here is a 404.
	if strings.Contains(GroupsEndpoint, "-") {
		t.Errorf("GroupsEndpoint = %q; the endpoint is /agentgroups, not /agent-groups", GroupsEndpoint)
	}
}

func TestWithQuery(t *testing.T) {
	if got := WithQuery("/agents", nil); got != "/agents" {
		t.Errorf("WithQuery with no params = %q", got)
	}
	if got := WithQuery("/agents", AgentsPageQuery(2, "")); got != "/agents?pageSize=2" {
		t.Errorf("WithQuery = %q", got)
	}
}
