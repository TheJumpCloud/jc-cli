package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const pamTestID = "6aa85171a36f7d16b20d9b60"

// pamWriteTools is every Privileged Access write tool with arguments good
// enough to pass validation and reach the execute gate.
var pamWriteTools = []struct {
	tool string
	args map[string]any
}{
	{"privileged_access_resource_create", map[string]any{"kind": "server", "name": "s", "uri": "10.0.0.1"}},
	{"privileged_access_resource_update", map[string]any{"kind": "database", "identifier": pamTestID, "notes": "x"}},
	{"privileged_access_resource_delete", map[string]any{"kind": "website", "identifier": pamTestID}},
	{"privileged_access_resource_archive", map[string]any{"kind": "server", "identifiers": []string{pamTestID}}},
	{"privileged_access_resource_unarchive", map[string]any{"kind": "server", "identifiers": []string{pamTestID}}},
	{"privileged_access_resource_clone", map[string]any{"kind": "database", "identifier": pamTestID}},
	{"privileged_access_credential_delete", map[string]any{"identifier": pamTestID}},
	{"privileged_access_credential_archive", map[string]any{"ids": []string{pamTestID}}},
	{"privileged_access_credential_unarchive", map[string]any{"ids": []string{pamTestID}}},
	{"privileged_access_credential_clone", map[string]any{"identifier": pamTestID}},
	{"privileged_access_blocking_rule_update", map[string]any{"identifier": pamTestID, "priority": 2}},
	{"privileged_access_blocking_rule_delete", map[string]any{"identifier": pamTestID}},
}

func withExecute(args map[string]any, execute bool) map[string]any {
	out := map[string]any{"execute": execute}
	for k, v := range args {
		out[k] = v
	}
	return out
}

func TestPAMWriteTools_RefusedInReadOnlyMode(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, readOnlyTripwire(t).URL)
	cs := connectToolTestServer(t, Options{ReadOnly: true})
	for _, c := range pamWriteTools {
		out := getResultText(t, callTool(t, cs, c.tool, withExecute(c.args, true)))
		if !strings.Contains(out, "read-only") {
			t.Errorf("%s was NOT refused in read-only mode: %s", c.tool, out)
		}
	}
}

// Without execute every tool plans and sends nothing: the tripwire fails the
// test on any non-GET request, and a plan needs no request at all.
func TestPAMWriteTools_PlanByDefault(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, readOnlyTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})
	for _, c := range pamWriteTools {
		out := getResultText(t, callTool(t, cs, c.tool, withExecute(c.args, false)))
		if !strings.Contains(out, `"plan"`) {
			t.Errorf("%s did not return a plan: %s", c.tool, out)
		}
	}
}

func TestPAMWriteTools_ValidateBeforePlanning(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, readOnlyTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})
	for tool, args := range map[string]map[string]any{
		"privileged_access_resource_create":  {"kind": "website", "name": "w", "uri": "https://x", "port": 1},
		"privileged_access_resource_update":  {"kind": "database", "identifier": pamTestID, "os": "linux"},
		"privileged_access_credential_clone": {"identifier": "not-an-id"},
		"privileged_access_resource_delete":  {"kind": "folder", "identifier": pamTestID},
	} {
		out := getResultText(t, callTool(t, cs, tool, args))
		if strings.Contains(out, `"plan"`) {
			t.Errorf("%s planned an invalid request: %s", tool, out)
		}
	}
}

// Every privileged_access_* tool that takes execute must be in the table
// above, so a tool added later cannot escape the read-only gate unnoticed.
func TestEveryPAMToolTakingExecuteIsCovered(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, readOnlyTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, c := range pamWriteTools {
		covered[c.tool] = true
	}
	for _, tool := range res.Tools {
		if !strings.HasPrefix(tool.Name, "privileged_access_") {
			continue
		}
		raw, _ := json.Marshal(tool.InputSchema)
		if strings.Contains(string(raw), `"execute"`) && !covered[tool.Name] {
			t.Errorf("%s takes execute but is not in pamWriteTools", tool.Name)
		}
	}
}
