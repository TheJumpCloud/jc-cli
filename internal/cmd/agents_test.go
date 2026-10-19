package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/viper"
)

// --- Fixtures -------------------------------------------------------------

// The live shapes, trimmed. Five envelopes in one feature.
const (
	fixtureAgents = `{"agents":[
      {"id":"6aa81dbfde6925dca6a551f1","name":"AWS BedRock AgentCore","email":"a@example.com","state":"active","ownerId":"5ec7221c4d224c0e309577e7"},
      {"id":"6aa85171a36f7d16b20d9b60","name":"AWS JKC Agent","email":"b@example.com","state":"active","ownerId":"5ec7221c4d224c0e309577e7"}
    ],"totalSize":2}`

	fixtureGroups = `[
      {"id":"6aa85141bda774000189bca6","name":"AWS JKC Agent","description":"","type":"agent_group"},
      {"id":"6a9891184ce65c0001dfc698","name":"SF Agent","description":"","type":"agent_group"}]`

	fixtureMemberOf = `[{"compiledAttributes":null,"id":"6aa85141bda774000189bca6","type":"agent_group",
      "paths":[[{"to":{"id":"6aa85141bda774000189bca6","type":"agent_group"}}]]}]`

	fixtureApplications = `[{"compiledAttributes":null,"id":"6331214f6655eeee1d2e2ab5","type":"application",
      "paths":[[{"to":{"id":"6aa85141bda774000189bca6","type":"agent_group"}},
                {"to":{"id":"6331214f6655eeee1d2e2ab5","type":"application"}}]]}]`

	fixtureMembers = `[{"attributes":null,"to":{"attributes":null,"id":"6aa85171a36f7d16b20d9b60","type":"agent"}}]`

	fixtureAssociations = `[{"attributes":null,"to":{"attributes":null,"id":"6331214f6655eeee1d2e2ab5","type":"application"}}]`

	fixtureCredentials = `{"credentials":[{"authType":"client_credentials",
      "clientId":"agent-6aa85171a36f7d16b20d9b60-e242fb7cd915dd8a","status":"active",
      "createdAt":"2026-09-14T19:58:35Z","expiresAt":"2027-09-14T19:58:35Z",
      "tokenEndpointAuthMethod":"client_secret_basic","updatedAt":"2026-09-14T19:58:35Z"}]}`

	fixtureApps = `[{"_id":"6331214f6655eeee1d2e2ab5","name":"aws-sso","displayName":"AWS IAM Identity Center","displayLabel":"AWS"}]`
)

const (
	testAgentID = "6aa85171a36f7d16b20d9b60"
	testGroupID = "6aa85141bda774000189bca6"
)

// agentsServer answers this area's endpoints and records every request, so a
// test can assert on the headers and query jc actually sent.
type agentsServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []*http.Request
}

func (a *agentsServer) seen() []*http.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*http.Request(nil), a.requests...)
}

// orgHeaders returns the x-org-id value sent with each request to a path.
func (a *agentsServer) orgHeaders(path string) []string {
	var out []string
	for _, r := range a.seen() {
		if r.URL.Path == path {
			out = append(out, r.Header.Get("x-org-id"))
		}
	}
	return out
}

func startAgentsServer(t *testing.T) *agentsServer {
	t.Helper()
	srv := &agentsServer{}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.mu.Lock()
		srv.requests = append(srv.requests, r.Clone(r.Context()))
		srv.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/agents":
			_, _ = w.Write([]byte(fixtureAgents))
		case "/agents/" + testAgentID:
			_, _ = w.Write([]byte(`{"id":"` + testAgentID + `","name":"AWS JKC Agent","state":"active","allowedAudiences":[]}`))
		case "/agents/" + testAgentID + "/memberof":
			_, _ = w.Write([]byte(fixtureMemberOf))
		case "/agents/" + testAgentID + "/applications":
			_, _ = w.Write([]byte(fixtureApplications))
		case "/agents/" + testAgentID + "/inbound-credentials":
			_, _ = w.Write([]byte(fixtureCredentials))
		case "/agentgroups":
			_, _ = w.Write([]byte(fixtureGroups))
		case "/agentgroups/" + testGroupID:
			_, _ = w.Write([]byte(`{"id":"` + testGroupID + `","name":"AWS JKC Agent","type":"agent_group"}`))
		case "/agentgroups/" + testGroupID + "/members":
			_, _ = w.Write([]byte(fixtureMembers))
		case "/agentgroups/" + testGroupID + "/associations":
			if r.URL.Query().Get("targets") == "" {
				// The real server's answer, which is the whole reason jc
				// supplies the parameter itself.
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"Error: Request validation failed: Parameter (targets) is Required"}`))
				return
			}
			_, _ = w.Write([]byte(fixtureAssociations))
		case "/applications":
			_, _ = w.Write([]byte(fixtureApps))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runAgents executes a command against the test server with an org id
// configured, and returns stdout and stderr.
func runAgents(t *testing.T, srv *agentsServer, args ...string) (string, string, error) {
	t.Helper()
	setupUsersTest(t)
	viper.Set("org_id", "5ec71e8e96bfda0611fc6c5b")
	overrideV2Client(t, srv.URL)

	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

// --- The nine reads -------------------------------------------------------

func TestAgentsList(t *testing.T) {
	srv := startAgentsServer(t)
	out, errOut, err := runAgents(t, srv, "agents", "list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("parsing output: %v\n%s", err, out)
	}
	if len(rows) != 2 {
		t.Errorf("got %d agents, want 2", len(rows))
	}
	// The footer names agents, not "items": a paged listing that loses the
	// noun exactly when the page is complete is the defect that added
	// writeNounListFooter.
	if !strings.Contains(errOut, "2 agents") {
		t.Errorf("footer = %q, want it to name agents", errOut)
	}
}

func TestAgentsList_SendsTheOrgHeader(t *testing.T) {
	srv := startAgentsServer(t)
	if _, _, err := runAgents(t, srv, "agents", "list"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := srv.orgHeaders("/agents")
	if len(got) == 0 {
		t.Fatal("no request reached /agents")
	}
	for _, v := range got {
		if v != "5ec71e8e96bfda0611fc6c5b" {
			t.Errorf("x-org-id = %q, want the profile's org id — the endpoint answers "+
				"400 \"x-org-id is required\" without it, and the spec marks it optional", v)
		}
	}
}

func TestAgentsList_DoesNotSendIgnoredParameters(t *testing.T) {
	// /agents ignores limit, skip and sort in silence.
	srv := startAgentsServer(t)
	if _, _, err := runAgents(t, srv, "agents", "list"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, r := range srv.seen() {
		if r.URL.Path != "/agents" {
			continue
		}
		for _, banned := range []string{"limit", "skip", "sort"} {
			if r.URL.Query().Has(banned) {
				t.Errorf("sent %q to /agents, which ignores it: %s", banned, r.URL.RawQuery)
			}
		}
	}
}

func TestAgentsList_RejectsAnUnsupportedFilterBeforeCalling(t *testing.T) {
	srv := startAgentsServer(t)
	_, _, err := runAgents(t, srv, "agents", "list", "--filter", "ownerId:eq:x")
	if err == nil {
		t.Fatal("an unsupported filter field was accepted")
	}
	if !strings.Contains(err.Error(), "email, name, state") {
		t.Errorf("the error does not say which fields work: %v", err)
	}
	if len(srv.seen()) != 0 {
		t.Errorf("made %d requests for a filter that cannot work", len(srv.seen()))
	}
}

func TestAgentsList_RejectsSubstringFilters(t *testing.T) {
	srv := startAgentsServer(t)
	_, _, err := runAgents(t, srv, "agents", "list", "--filter", "name:contains:AWS")
	if err == nil {
		t.Fatal("a contains filter was accepted; this area has no substring match")
	}
	if !strings.Contains(err.Error(), "substring") {
		t.Errorf("the error does not explain that no substring match exists: %v", err)
	}
}

func TestAgentsGet(t *testing.T) {
	srv := startAgentsServer(t)
	out, _, err := runAgents(t, srv, "agents", "get", testAgentID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("parsing output: %v\n%s", err, out)
	}
	if rec["name"] != "AWS JKC Agent" {
		t.Errorf("name = %v", rec["name"])
	}
}

func TestAgentsGet_RejectsAMalformedID(t *testing.T) {
	srv := startAgentsServer(t)
	_, _, err := runAgents(t, srv, "agents", "get", "not-an-id")
	if err == nil {
		t.Fatal("a malformed id was sent to the server")
	}
	if len(srv.seen()) != 0 {
		t.Errorf("made %d requests for a malformed id", len(srv.seen()))
	}
}

func TestAgentsMemberOf_JoinsGroupNames(t *testing.T) {
	srv := startAgentsServer(t)
	out, _, err := runAgents(t, srv, "agents", "memberof", testAgentID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("parsing output: %v\n%s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	// The API returns an id and a type and no name. A row of hex answers
	// nothing, which is the defect this join exists to avoid.
	if rows[0]["name"] != "AWS JKC Agent" {
		t.Errorf("name = %v, want the group's name joined from the catalog", rows[0]["name"])
	}
}

func TestAgentsApplications_ReportsTheGroupTheAccessCameThrough(t *testing.T) {
	srv := startAgentsServer(t)
	out, _, err := runAgents(t, srv, "agents", "applications", testAgentID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("parsing output: %v\n%s", err, out)
	}
	if rows[0]["name"] != "AWS" {
		t.Errorf("name = %v, want the application's displayLabel", rows[0]["name"])
	}
	via, ok := rows[0]["via"].([]any)
	if !ok || len(via) != 1 || via[0] != "AWS JKC Agent" {
		t.Errorf("via = %v, want the group that granted the access — knowing an agent has "+
			"access is not knowing where to remove it", rows[0]["via"])
	}
}

func TestAgentsCredentialsList_ReturnsNoSecret(t *testing.T) {
	srv := startAgentsServer(t)
	out, errOut, err := runAgents(t, srv, "agents", "credentials", "list", testAgentID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("parsing output: %v\n%s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d credentials, want 1", len(rows))
	}
	// Checked by key: tokenEndpointAuthMethod's VALUE is
	// "client_secret_basic", which names a method and discloses nothing.
	for key := range rows[0] {
		switch key {
		case "clientSecret", "secret", "password":
			t.Errorf("the credential listing emitted %q", key)
		}
	}
	if rows[0]["clientId"] == nil {
		t.Error("no clientId in the output, so this test is not reading the live shape")
	}
	if !strings.Contains(errOut, "1 credential") {
		t.Errorf("footer = %q", errOut)
	}
}

func TestAgentGroupsList(t *testing.T) {
	srv := startAgentsServer(t)
	out, errOut, err := runAgents(t, srv, "agent-groups", "list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("parsing output: %v\n%s", err, out)
	}
	if len(rows) != 2 {
		t.Errorf("got %d groups, want 2", len(rows))
	}
	// A bare-array listing has no total, so the footer is a plain count
	// rather than the "N of M" form — there is no M to report honestly.
	if !strings.Contains(errOut, "2 agent groups") {
		t.Errorf("footer = %q", errOut)
	}
	if strings.Contains(errOut, " of ") {
		t.Errorf("footer claims a total this endpoint does not return: %q", errOut)
	}
}

func TestAgentGroupsList_RejectsAnUnsupportedFilter(t *testing.T) {
	// The server answers this with an internal error naming nothing the
	// operator typed, which is why it is caught locally.
	srv := startAgentsServer(t)
	_, _, err := runAgents(t, srv, "agent-groups", "list", "--filter", "email:eq:x")
	if err == nil {
		t.Fatal("an unsupported group filter field was accepted")
	}
	if !strings.Contains(err.Error(), "name, type") {
		t.Errorf("the error does not say which fields work: %v", err)
	}
}

func TestAgentGroupsList_FilterFieldsDifferFromAgents(t *testing.T) {
	// email filters agents and not groups. Accepting both on both would be
	// the drift this contract package exists to prevent.
	srv := startAgentsServer(t)
	if _, _, err := runAgents(t, srv, "agents", "list", "--filter", "email:eq:b@example.com"); err != nil {
		t.Errorf("the agents listing rejected email, which it supports: %v", err)
	}
	if _, _, err := runAgents(t, srv, "agent-groups", "list", "--filter", "email:eq:x"); err == nil {
		t.Error("the agent groups listing accepted email, which it does not support")
	}
}

func TestAgentGroupsGet(t *testing.T) {
	srv := startAgentsServer(t)
	out, _, err := runAgents(t, srv, "agent-groups", "get", testGroupID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "AWS JKC Agent") {
		t.Errorf("output = %s", out)
	}
}

func TestAgentGroupsMembers_JoinsAgentNames(t *testing.T) {
	srv := startAgentsServer(t)
	out, errOut, err := runAgents(t, srv, "agent-groups", "members", testGroupID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("parsing output: %v\n%s", err, out)
	}
	if rows[0]["name"] != "AWS JKC Agent" {
		t.Errorf("name = %v, want the agent's name joined from the catalog", rows[0]["name"])
	}
	if !strings.Contains(errOut, "1 agent") {
		t.Errorf("footer = %q", errOut)
	}
}

func TestAgentGroupsAssociations_SuppliesTheRequiredTarget(t *testing.T) {
	// Without targets the server answers 400 "Parameter (targets) is
	// Required", and the spec does not say the parameter is required at all.
	// The fake server reproduces that, so this test fails if jc stops
	// sending it.
	srv := startAgentsServer(t)
	out, _, err := runAgents(t, srv, "agent-groups", "associations", testGroupID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("parsing output: %v\n%s", err, out)
	}
	if rows[0]["name"] != "AWS" {
		t.Errorf("name = %v, want the application's name", rows[0]["name"])
	}

	var sent bool
	for _, r := range srv.seen() {
		if strings.HasSuffix(r.URL.Path, "/associations") {
			sent = true
			if r.URL.Query().Get("targets") != "application" {
				t.Errorf("targets = %q, want application", r.URL.Query().Get("targets"))
			}
		}
	}
	if !sent {
		t.Error("no request reached the associations endpoint")
	}
}

func TestAgentGroupsAssociations_RejectsATargetOutsideTheEnum(t *testing.T) {
	srv := startAgentsServer(t)
	_, _, err := runAgents(t, srv, "agent-groups", "associations", testGroupID, "--target", "user_group")
	if err == nil {
		t.Fatal("a target outside the enum was accepted; agent groups reach applications only")
	}
}

// --- The org id -----------------------------------------------------------

// runAgentsNoOrg is runAgents with no organization id configured anywhere.
func runAgentsNoOrg(t *testing.T, srv *agentsServer, args ...string) (string, string, error) {
	t.Helper()
	setupUsersTest(t)
	viper.Set("org_id", "")
	overrideV2Client(t, srv.URL)

	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

func TestAgents_NoOrgIDFailsNamingTheConfig(t *testing.T) {
	srv := startAgentsServer(t)
	_, _, err := runAgentsNoOrg(t, srv, "agents", "list")
	if err == nil {
		t.Fatal("an agents listing with no org id was sent anyway")
	}
	// The server's own message names a header, and no flag is called
	// x-org-id. The error has to name something an operator can set.
	for _, want := range []string{"jc auth login", "JC_ORG_ID"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "x-org-id is required") {
		t.Errorf("the error forwards the server's header name: %v", err)
	}
	if len(srv.seen()) != 0 {
		t.Errorf("made %d requests that could not succeed", len(srv.seen()))
	}
}

func TestAgentGroups_WorkWithNoOrgID(t *testing.T) {
	// The group endpoints do not take the header, so requiring one would
	// make jc refuse nine calls the server would have accepted.
	srv := startAgentsServer(t)
	out, _, err := runAgentsNoOrg(t, srv, "agent-groups", "list")
	if err != nil {
		t.Fatalf("the agent groups listing needed an org id, and the endpoint does not: %v", err)
	}
	if !strings.Contains(out, "AWS JKC Agent") {
		t.Errorf("output = %s", out)
	}
}

func TestAgents_OrgMismatchExplainsTheRealCause(t *testing.T) {
	// A wrong org id answers 404 "selected organization not found", which
	// jc's generic handling renders as "verify the resource exists" —
	// sending an operator to look for a missing agent.
	srv := &agentsServer{}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Not Found: selected organization not found","message":"Not Found: selected organization not found"}`))
	}))
	t.Cleanup(srv.Close)

	_, _, err := runAgents(t, srv, "agents", "list")
	if err == nil {
		t.Fatal("a wrong org id produced no error")
	}
	cliErr := ToCLIError(err)
	if !strings.Contains(cliErr.Suggestion, "JC_ORG_ID") {
		t.Errorf("suggestion = %q, want it to name the org id as the cause", cliErr.Suggestion)
	}
	if strings.Contains(cliErr.Suggestion, "Verify the resource exists") {
		t.Errorf("the suggestion still points at a missing agent: %q", cliErr.Suggestion)
	}
	if cliErr.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want the 404 kept", cliErr.HTTPStatus)
	}
}
