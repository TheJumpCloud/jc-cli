package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// agentsWriteTools is the twelve write tools, with arguments good enough to
// reach each handler's body.
//
// Every one of them carries execute: true. A tool that is NOT refused here
// would be a write that Options.ReadOnly fails to disable.
var agentsWriteTools = []struct {
	tool string
	args map[string]any
}{
	{"agent_register", map[string]any{
		"name": "a", "email": "a@example.com",
		"owner_id": "5ec7221c4d224c0e309577e7", "execute": true,
	}},
	{"agent_update", map[string]any{
		"agent_id": "6aa85171a36f7d16b20d9b60", "description": "x", "execute": true,
	}},
	{"agent_suspend", map[string]any{"agent_id": "6aa85171a36f7d16b20d9b60", "execute": true}},
	{"agent_activate", map[string]any{"agent_id": "6aa85171a36f7d16b20d9b60", "execute": true}},
	{"agent_delete", map[string]any{"agent_id": "6aa85171a36f7d16b20d9b60", "execute": true}},
	{"agent_group_create", map[string]any{"name": "g", "execute": true}},
	{"agent_group_update", map[string]any{
		"group_id": "6aa85141bda774000189bca6", "name": "g2", "execute": true,
	}},
	{"agent_group_delete", map[string]any{"group_id": "6aa85141bda774000189bca6", "execute": true}},
	{"agent_group_add_member", map[string]any{
		"group_id": "6aa85141bda774000189bca6", "agent_id": "6aa85171a36f7d16b20d9b60", "execute": true,
	}},
	{"agent_group_remove_member", map[string]any{
		"group_id": "6aa85141bda774000189bca6", "agent_id": "6aa85171a36f7d16b20d9b60", "execute": true,
	}},
	{"agent_group_assign_app", map[string]any{
		"group_id": "6aa85141bda774000189bca6", "application_id": "6331214f6655eeee1d2e2ab5", "execute": true,
	}},
	{"agent_group_unassign_app", map[string]any{
		"group_id": "6aa85141bda774000189bca6", "application_id": "6331214f6655eeee1d2e2ab5", "execute": true,
	}},
}

// readOnlyTripwire fails the test if any request reaches it. A read-only
// server must refuse before it calls the API, not after.
func readOnlyTripwire(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("a read-only server sent a %s to %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"6aa85141bda774000189bca6","name":"g","description":"d"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The finding this test exists for: the first version of agents_writes.go
// omitted the Options.ReadOnly check on all twelve write tools, so
// `jc mcp serve --read-only` would still have created, suspended and deleted
// agents. A background security review caught it.
func TestAgentsWriteTools_RefusedInReadOnlyMode(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, readOnlyTripwire(t).URL)
	cs := connectToolTestServer(t, Options{ReadOnly: true})

	for _, c := range agentsWriteTools {
		out := getResultText(t, callTool(t, cs, c.tool, c.args))
		if !strings.Contains(out, "read-only") {
			t.Errorf("%s was NOT refused in read-only mode. Got: %s", c.tool, out)
		}
	}
}

// Planning writes nothing, so a read-only server should still answer it —
// otherwise an operator cannot ask what a change would do.
func TestAgentsWriteTools_PlanStillWorksInReadOnlyMode(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, readOnlyTripwire(t).URL)
	cs := connectToolTestServer(t, Options{ReadOnly: true})

	for _, c := range agentsWriteTools {
		args := map[string]any{}
		for k, v := range c.args {
			args[k] = v
		}
		args["execute"] = false

		out := getResultText(t, callTool(t, cs, c.tool, args))
		if strings.Contains(out, "read-only") {
			t.Errorf("%s refused to PLAN in read-only mode; planning makes no writes", c.tool)
		}
		if !strings.Contains(out, "plan") {
			t.Errorf("%s did not return a plan: %s", c.tool, out)
		}
	}
}

// The structural half: derive the set of write tools from the registry
// rather than from the list above, so a tool added later cannot quietly
// escape the gate just because nobody added it to a test table.
func TestEveryAgentsToolTakingExecuteIsGated(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, readOnlyTripwire(t).URL)

	listing := connectToolTestServer(t, Options{})
	res, err := listing.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	covered := map[string]bool{}
	for _, c := range agentsWriteTools {
		covered[c.tool] = true
	}

	var found int
	for _, tool := range res.Tools {
		if !strings.HasPrefix(tool.Name, "agent") {
			continue
		}
		// An "execute" argument is this server's marker for a tool that
		// mutates: it is what the plan-by-default convention is built on.
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshalling the schema for %s: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("decoding the schema for %s: %v", tool.Name, err)
		}
		if _, ok := schema.Properties["execute"]; !ok {
			continue
		}
		found++
		if !covered[tool.Name] {
			t.Errorf("%s takes an execute argument, so it mutates, but no read-only case "+
				"covers it — add it to agentsWriteTools. A write tool that Options.ReadOnly "+
				"does not disable is an authorization bypass.", tool.Name)
		}
	}

	if found != len(agentsWriteTools) {
		t.Errorf("found %d agents tools taking execute, but the table has %d — the table and "+
			"the registry have drifted", found, len(agentsWriteTools))
	}
	if found == 0 {
		t.Error("found no agents write tools at all, so this test is measuring nothing")
	}
}
