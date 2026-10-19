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

// writeServer records every request body so a test can assert on what jc
// actually sent, and lets a test script the membership that reads back.
type writeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
	// members is the membership the server reports; a test mutates it to
	// simulate a write that did or did not take effect.
	members  []string
	groupRec map[string]any
	agentRec map[string]any
}

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
}

func (w *writeServer) seen() []recordedRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]recordedRequest(nil), w.requests...)
}

func (w *writeServer) bodyFor(method, path string) map[string]any {
	for _, r := range w.seen() {
		if r.Method == method && r.Path == path {
			return r.Body
		}
	}
	return nil
}

func startWriteServer(t *testing.T) *writeServer {
	t.Helper()
	srv := &writeServer{
		members:  nil,
		groupRec: map[string]any{"id": testGroupID, "name": "my-group", "description": "important note", "type": "agent_group"},
		agentRec: map[string]any{"id": testAgentID, "name": "my-agent", "email": "a@example.com", "state": "active"},
	}

	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		srv.mu.Lock()
		srv.requests = append(srv.requests, recordedRequest{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: body,
		})
		members := append([]string(nil), srv.members...)
		agentRec, groupRec := srv.agentRec, srv.groupRec
		srv.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		switch {
		case r.URL.Path == "/agents" && r.Method == http.MethodPost:
			out := map[string]any{"id": testAgentID, "state": "active"}
			for k, v := range body {
				out[k] = v
			}
			w.WriteHeader(http.StatusCreated)
			_ = enc.Encode(out)

		case r.URL.Path == "/agents/"+testAgentID && r.Method == http.MethodPatch:
			out := map[string]any{}
			for k, v := range agentRec {
				out[k] = v
			}
			for k, v := range body {
				out[k] = v
			}
			_ = enc.Encode(out)

		case r.URL.Path == "/agents/"+testAgentID && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/agents/"+testAgentID+"/suspend":
			_ = enc.Encode(map[string]any{"id": testAgentID, "state": "suspended"})

		case r.URL.Path == "/agents/"+testAgentID+"/activate":
			_ = enc.Encode(map[string]any{"id": testAgentID, "state": "active"})

		case r.URL.Path == "/agentgroups" && r.Method == http.MethodPost:
			out := map[string]any{"id": testGroupID, "type": "agent_group"}
			for k, v := range body {
				out[k] = v
			}
			w.WriteHeader(http.StatusCreated)
			_ = enc.Encode(out)

		case r.URL.Path == "/agentgroups/"+testGroupID && r.Method == http.MethodGet:
			_ = enc.Encode(groupRec)

		case r.URL.Path == "/agentgroups/"+testGroupID && r.Method == http.MethodPatch:
			// Faithful to the real endpoint: it REPLACES, so the response
			// reflects only what the request carried.
			out := map[string]any{"id": testGroupID, "type": "agent_group", "description": ""}
			for k, v := range body {
				out[k] = v
			}
			_ = enc.Encode(out)

		case r.URL.Path == "/agentgroups/"+testGroupID && r.Method == http.MethodDelete:
			_ = enc.Encode(groupRec)

		case strings.HasSuffix(r.URL.Path, "/members"), strings.HasSuffix(r.URL.Path, "/associations"):
			if r.Method == http.MethodPost {
				// The real endpoint answers with an EMPTY body.
				op, _ := body["op"].(string)
				id, _ := body["id"].(string)
				srv.mu.Lock()
				if op == "add" {
					srv.members = append(srv.members, id)
				} else {
					kept := srv.members[:0]
					for _, m := range srv.members {
						if m != id {
							kept = append(kept, m)
						}
					}
					srv.members = append([]string(nil), kept...)
				}
				srv.mu.Unlock()
				w.WriteHeader(http.StatusOK)
				return
			}
			rows := make([]map[string]any, 0, len(members))
			for _, m := range members {
				rows = append(rows, map[string]any{"to": map[string]any{"id": m, "type": "agent"}})
			}
			_ = enc.Encode(rows)

		case r.URL.Path == "/agents":
			_ = enc.Encode(map[string]any{"agents": []any{agentRec}, "totalSize": 1})

		case r.URL.Path == "/applications":
			_ = enc.Encode([]map[string]any{{"_id": "6331214f6655eeee1d2e2ab5", "displayLabel": "AWS"}})

		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runWrite(t *testing.T, srv *writeServer, args ...string) (string, string, error) {
	t.Helper()
	setupUsersTest(t)
	viper.Set("org_id", "5ec71e8e96bfda0611fc6c5b")
	viper.Set("force", true) // skip the confirmation prompt; it has its own tests
	overrideV2Client(t, srv.URL)

	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

const testOwner = "5ec7221c4d224c0e309577e7"

// --- register -------------------------------------------------------------

func TestAgentsRegister(t *testing.T) {
	srv := startWriteServer(t)
	_, _, err := runWrite(t, srv, "agents", "register",
		"--name", "build-bot", "--email", "build-bot@example.com", "--owner", testOwner)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := srv.bodyFor("POST", "/agents")
	if body == nil {
		t.Fatal("no create request reached the server")
	}
	for k, want := range map[string]any{"name": "build-bot", "email": "build-bot@example.com", "ownerId": testOwner} {
		if body[k] != want {
			t.Errorf("body[%q] = %v, want %v", k, body[k], want)
		}
	}
}

func TestAgentsRegister_RequiresTheThreeUndocumentedFields(t *testing.T) {
	srv := startWriteServer(t)
	cases := [][]string{
		{"agents", "register", "--email", "a@example.com", "--owner", testOwner},
		{"agents", "register", "--name", "a", "--owner", testOwner},
		{"agents", "register", "--name", "a", "--email", "a@example.com"},
	}
	for _, args := range cases {
		if _, _, err := runWrite(t, srv, args...); err == nil {
			t.Errorf("%v was accepted; all three fields are required", args[2:])
		}
	}
	if n := len(srv.seen()); n != 0 {
		t.Errorf("made %d requests that could not succeed", n)
	}
}

// --- update: PARTIAL ------------------------------------------------------

func TestAgentsUpdate_SendsOnlyTheFlagsThatWereSet(t *testing.T) {
	srv := startWriteServer(t)
	_, _, err := runWrite(t, srv, "agents", "update", testAgentID, "--description", "nightly builds")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := srv.bodyFor("PATCH", "/agents/"+testAgentID)
	if body == nil {
		t.Fatal("no update request reached the server")
	}
	if len(body) != 1 {
		t.Errorf("body = %v, want only the description — sending an unset flag would overwrite "+
			"a field the operator never mentioned", body)
	}
	if body["description"] != "nightly builds" {
		t.Errorf("description = %v", body["description"])
	}
}

func TestAgentsUpdate_EmptyDescriptionIsSentAndClears(t *testing.T) {
	// An unset flag and a flag set to "" are different requests, which is
	// why the update path takes pointers.
	srv := startWriteServer(t)
	if _, _, err := runWrite(t, srv, "agents", "update", testAgentID, "--description", ""); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := srv.bodyFor("PATCH", "/agents/"+testAgentID)
	v, ok := body["description"]
	if !ok {
		t.Fatalf("body = %v, want an explicit empty description", body)
	}
	if v != "" {
		t.Errorf("description = %v, want empty", v)
	}
}

func TestAgentsUpdate_NoFlagsIsRefusedWithoutCalling(t *testing.T) {
	srv := startWriteServer(t)
	_, _, err := runWrite(t, srv, "agents", "update", testAgentID)
	if err == nil {
		t.Fatal("an update with no flags was accepted")
	}
	if len(srv.seen()) != 0 {
		t.Errorf("made %d requests for an update with nothing in it", len(srv.seen()))
	}
}

func TestAgentsUpdate_HasNoStateFlag(t *testing.T) {
	// The API refuses a state change through PATCH and names the right
	// commands; offering the flag would promise something that cannot work.
	srv := startWriteServer(t)
	_, _, err := runWrite(t, srv, "agents", "update", testAgentID, "--state", "suspended")
	if err == nil {
		t.Fatal("--state was accepted on agents update")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected an unknown-flag error, got: %v", err)
	}
}

// --- suspend / activate ---------------------------------------------------

func TestAgentsSuspend_ReadsTheStateBack(t *testing.T) {
	srv := startWriteServer(t)
	out, _, err := runWrite(t, srv, "agents", "suspend", testAgentID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("parsing output: %v\n%s", err, out)
	}
	if rec["state"] != "suspended" {
		t.Errorf("state = %v, want suspended", rec["state"])
	}
}

// A transition that reports a state other than the one asked for is a failure,
// not a success: reporting the requested state rather than the observed one is
// how a surface claims a change that never happened.
func TestAgentsSuspend_WrongStateBackIsAnError(t *testing.T) {
	srv := &writeServer{}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + testAgentID + `","state":"active"}`))
	}))
	t.Cleanup(srv.Close)

	_, _, err := runWrite(t, srv, "agents", "suspend", testAgentID)
	if err == nil {
		t.Fatal("a suspend that came back active was reported as a success")
	}
	if !strings.Contains(err.Error(), "may not have taken effect") {
		t.Errorf("the error does not say the change may not have applied: %v", err)
	}
}

func TestAgentsSuspend_MissingStateBackIsAnError(t *testing.T) {
	srv := &writeServer{}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + testAgentID + `"}`))
	}))
	t.Cleanup(srv.Close)

	if _, _, err := runWrite(t, srv, "agents", "suspend", testAgentID); err == nil {
		t.Error("a response carrying no state was accepted as a successful transition")
	}
}

// --- group update: FULL REPLACE ------------------------------------------

func TestAgentGroupsUpdate_ReadsFirstAndPreservesTheDescription(t *testing.T) {
	srv := startWriteServer(t)
	_, _, err := runWrite(t, srv, "agent-groups", "update", testGroupID, "--name", "renamed")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The read has to happen, because the endpoint clears what it is not
	// given and there is no correct request that omits the current values.
	var sawGet bool
	for _, r := range srv.seen() {
		if r.Method == "GET" && r.Path == "/agentgroups/"+testGroupID {
			sawGet = true
		}
	}
	if !sawGet {
		t.Error("the group was not read before updating it; this endpoint replaces the record, " +
			"so a rename without a read would wipe the description")
	}

	body := srv.bodyFor("PATCH", "/agentgroups/"+testGroupID)
	if body["name"] != "renamed" {
		t.Errorf("name = %v, want renamed", body["name"])
	}
	if body["description"] != "important note" {
		t.Errorf("description = %v, want the existing value resent", body["description"])
	}
}

func TestAgentGroupsUpdate_DescriptionOnlyStillSendsTheName(t *testing.T) {
	srv := startWriteServer(t)
	if _, _, err := runWrite(t, srv, "agent-groups", "update", testGroupID, "--description", "new"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := srv.bodyFor("PATCH", "/agentgroups/"+testGroupID)
	if body["name"] != "my-group" {
		t.Errorf("name = %v, want the current name — this endpoint requires it on every update", body["name"])
	}
	if body["description"] != "new" {
		t.Errorf("description = %v", body["description"])
	}
}

func TestAgentGroupsUpdate_NoFlagsIsRefusedWithoutReading(t *testing.T) {
	srv := startWriteServer(t)
	_, _, err := runWrite(t, srv, "agent-groups", "update", testGroupID)
	if err == nil {
		t.Fatal("an update with no flags was accepted")
	}
	if len(srv.seen()) != 0 {
		t.Errorf("made %d requests for an update with nothing in it", len(srv.seen()))
	}
}

// --- membership: read-back ------------------------------------------------

func TestAgentGroupsAddMember_VerifiesByReadingBack(t *testing.T) {
	srv := startWriteServer(t)
	out, errOut, err := runWrite(t, srv, "agent-groups", "add-member", testGroupID, "--agent", testAgentID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	body := srv.bodyFor("POST", "/agentgroups/"+testGroupID+"/members")
	if body["op"] != "add" {
		t.Errorf("op = %v, want add — the endpoint requires it", body["op"])
	}
	if body["type"] != "agent" || body["id"] != testAgentID {
		t.Errorf("body = %v", body)
	}

	// The write answers with an empty body, so a GET must follow.
	var sawGet bool
	for _, r := range srv.seen() {
		if r.Method == "GET" && strings.HasSuffix(r.Path, "/members") {
			sawGet = true
		}
	}
	if !sawGet {
		t.Error("the membership was not read back; the write returns an empty body, so without " +
			"a read jc would be reporting the request rather than the change")
	}
	if !strings.Contains(errOut, "confirmed by read-back") {
		t.Errorf("stderr does not report the verification: %q", errOut)
	}
	if !strings.Contains(out+errOut, testAgentID) {
		t.Errorf("neither stream mentions the agent that was added")
	}
}

// The failure this read-back exists to catch: the API accepts the write and
// the membership does not change.
func TestAgentGroupsAddMember_SilentNoOpIsReported(t *testing.T) {
	srv := &writeServer{}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK) // accepted, empty body, no effect
			return
		}
		_, _ = w.Write([]byte(`[]`)) // and the membership is still empty
	}))
	t.Cleanup(srv.Close)

	_, _, err := runWrite(t, srv, "agent-groups", "add-member", testGroupID, "--agent", testAgentID)
	if err == nil {
		t.Fatal("a write that took no effect was reported as a success")
	}
	if !strings.Contains(err.Error(), "without taking effect") {
		t.Errorf("the error does not say the write did not apply: %v", err)
	}
}

func TestAgentGroupsRemoveMember_VerifiesTheAbsence(t *testing.T) {
	srv := startWriteServer(t)
	srv.members = []string{testAgentID}

	_, errOut, err := runWrite(t, srv, "agent-groups", "remove-member", testGroupID, "--agent", testAgentID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := srv.bodyFor("POST", "/agentgroups/"+testGroupID+"/members")
	if body["op"] != "remove" {
		t.Errorf("op = %v, want remove — there is no DELETE on this endpoint", body["op"])
	}
	if !strings.Contains(errOut, "removed from") {
		t.Errorf("stderr does not report the removal: %q", errOut)
	}
}

func TestAgentGroupsAssignApp_UsesTheAssociationsEndpointWithTargets(t *testing.T) {
	srv := startWriteServer(t)
	const appID = "6331214f6655eeee1d2e2ab5"

	if _, _, err := runWrite(t, srv, "agent-groups", "assign-app", testGroupID, "--app", appID); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := srv.bodyFor("POST", "/agentgroups/"+testGroupID+"/associations")
	if body == nil {
		t.Fatal("the request did not go to the associations endpoint")
	}
	if body["type"] != "application" {
		t.Errorf("type = %v, want application — it is the only valid target", body["type"])
	}

	// The read-back needs targets, which the server requires and the spec
	// does not mention.
	var sawTargets bool
	for _, r := range srv.seen() {
		if r.Method == "GET" && strings.HasSuffix(r.Path, "/associations") {
			if strings.Contains(r.Query, "targets=application") {
				sawTargets = true
			}
		}
	}
	if !sawTargets {
		t.Error("the associations read-back did not send targets=application, which the API requires")
	}
}

func TestAgentGroupsMembership_RejectsAMalformedTargetID(t *testing.T) {
	srv := startWriteServer(t)
	_, _, err := runWrite(t, srv, "agent-groups", "add-member", testGroupID, "--agent", "not-an-id")
	if err == nil {
		t.Fatal("a malformed agent id was accepted")
	}
	if len(srv.seen()) != 0 {
		t.Errorf("made %d requests for a malformed id", len(srv.seen()))
	}
}

// --- deletes --------------------------------------------------------------

func TestAgentsDelete(t *testing.T) {
	srv := startWriteServer(t)
	_, errOut, err := runWrite(t, srv, "agents", "delete", testAgentID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var sawDelete bool
	for _, r := range srv.seen() {
		if r.Method == "DELETE" && r.Path == "/agents/"+testAgentID {
			sawDelete = true
		}
	}
	if !sawDelete {
		t.Error("no DELETE reached the server")
	}
	if !strings.Contains(errOut, "Deleted agent") {
		t.Errorf("stderr does not confirm the deletion: %q", errOut)
	}
}

func TestAgentGroupsDelete_CountsTheAgentsThatLoseAccess(t *testing.T) {
	srv := startWriteServer(t)
	srv.members = []string{testAgentID}

	out, _, err := runWrite(t, srv, "agent-groups", "delete", testGroupID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The members are read so the operator can be told the blast radius.
	var sawMembers bool
	for _, r := range srv.seen() {
		if r.Method == "GET" && strings.HasSuffix(r.Path, "/members") {
			sawMembers = true
		}
	}
	if !sawMembers {
		t.Error("the group's members were not read before deleting it, so the confirmation " +
			"could not say how many agents lose access")
	}
	// A group delete answers with the record it removed, which is the
	// read-back.
	if !strings.Contains(out, testGroupID) {
		t.Errorf("output does not carry the deleted group: %s", out)
	}
}

// --- --plan ---------------------------------------------------------------

func runPlan(t *testing.T, srv *writeServer, args ...string) (string, string) {
	t.Helper()
	setupUsersTest(t)
	viper.Set("org_id", "5ec71e8e96bfda0611fc6c5b")
	viper.Set("plan", true)
	overrideV2Client(t, srv.URL)

	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	_ = cmd.Execute()
	return out.String(), errBuf.String()
}

func TestAgentWrites_PlanMakesNoWriteRequest(t *testing.T) {
	cases := [][]string{
		{"agents", "register", "--name", "a", "--email", "a@example.com", "--owner", testOwner},
		{"agents", "update", testAgentID, "--description", "x"},
		{"agents", "suspend", testAgentID},
		{"agents", "activate", testAgentID},
		{"agents", "delete", testAgentID},
		{"agent-groups", "create", "--name", "g"},
		{"agent-groups", "delete", testGroupID},
		{"agent-groups", "add-member", testGroupID, "--agent", testAgentID},
		{"agent-groups", "remove-member", testGroupID, "--agent", testAgentID},
		{"agent-groups", "assign-app", testGroupID, "--app", "6331214f6655eeee1d2e2ab5"},
		{"agent-groups", "unassign-app", testGroupID, "--app", "6331214f6655eeee1d2e2ab5"},
	}
	for _, args := range cases {
		srv := startWriteServer(t)
		out, errOut := runPlan(t, srv, args...)

		for _, r := range srv.seen() {
			if r.Method != http.MethodGet {
				t.Errorf("%v with --plan made a %s request to %s", args, r.Method, r.Path)
			}
		}
		if !strings.Contains(out+errOut, "Plan") && !strings.Contains(out+errOut, "plan") {
			t.Errorf("%v with --plan printed no plan:\nstdout=%s\nstderr=%s", args, out, errOut)
		}
	}
}

// `agent-groups update` is the one write whose plan legitimately reads first,
// because it cannot build a correct body without the current record.
func TestAgentGroupsUpdate_PlanStillReadsButDoesNotWrite(t *testing.T) {
	srv := startWriteServer(t)
	runPlan(t, srv, "agent-groups", "update", testGroupID, "--name", "renamed")

	for _, r := range srv.seen() {
		if r.Method != http.MethodGet {
			t.Errorf("--plan made a %s request to %s", r.Method, r.Path)
		}
	}
}
