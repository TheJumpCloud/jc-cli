package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The finding this file exists for: the first version of apple_mdm_ade.go
// checked only that an id argument was non-empty, then interpolated it
// straight into a request path. A background security review flagged it as
// path injection.
//
// It did not escape in practice — /applemdms/ade_tokens/../../systemusers and
// three encoded variants were all refused by the API with 400 or 404, because
// JumpCloud does not normalise `..` in a path. But that is the upstream
// server's behaviour, not a property jc controls, and the MCP server grants
// these reads as individually allowable tools: an operator who permits
// apple_mdm_ade_token_get is permitting one read, not a general request-path
// builder. Five sibling contract packages already validate ids; this one
// didn't.
//
// idTakingADETools is every ADE/ABM/ADUE tool that puts a caller-supplied
// value into a path, with a traversal attempt in that position.
var idTakingADETools = []struct {
	tool string
	args map[string]any
	// optionalID is true where omitting the id is a legitimate request
	// rather than a mistake. apple_mdm_abm_devices_list without an
	// apple_mdm_id reads the whole organization, and takes an endpoint with
	// nothing interpolated into it — so an empty value there must NOT be
	// refused, while a malformed one still must.
	optionalID bool
}{
	{tool: "apple_mdm_ade_token_get", args: map[string]any{"token_id": "../../systemusers"}},
	{tool: "apple_mdm_ade_settings_list", args: map[string]any{"token_id": "../../systemusers"}},
	{tool: "apple_mdm_ade_settings_get", args: map[string]any{
		"token_id": "../../systemusers", "os_family": "DARWIN",
	}},
	{tool: "apple_mdm_abm_devices_list", args: map[string]any{"apple_mdm_id": "../../systemusers"},
		optionalID: true},
	{tool: "apple_mdm_background_tasks_list", args: map[string]any{"device_id": "../../systemusers"}},
	{tool: "apple_mdm_adue_validate", args: map[string]any{"apple_mdm_id": "../../systemusers"}},
}

// pathTripwire fails the test if a request escapes the endpoint the tool is
// supposed to read. Refusing after the call would still have sent it.
func pathTripwire(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// RequestURI, not URL.Path: Go's URL parsing may clean a path, and
		// what matters is the bytes that went down the wire.
		if strings.Contains(r.RequestURI, "..") || strings.Contains(r.RequestURI, "%2e") {
			t.Errorf("a traversal reached the wire: %s", r.RequestURI)
		}
		if !strings.Contains(r.RequestURI, "/applemdms") {
			t.Errorf("a request escaped the applemdms tree: %s", r.RequestURI)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"adeToken":{"objectId":"6a7e1bb88cf0be0001a4e3cd"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestADETools_RefuseAMalformedID(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, pathTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})

	for _, c := range idTakingADETools {
		out := getResultText(t, callTool(t, cs, c.tool, c.args))
		if !strings.Contains(out, "not a JumpCloud object id") {
			t.Errorf("%s accepted a traversal in place of an id. Got: %s", c.tool, out)
		}
	}
}

// An empty id must be refused too, and for the same reason rather than by a
// separate code path — the earlier version had a bespoke "x is required"
// check that said nothing about shape.
func TestADETools_RefuseAnEmptyID(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, pathTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})

	for _, c := range idTakingADETools {
		if c.optionalID {
			continue
		}
		args := map[string]any{}
		for k, v := range c.args {
			args[k] = v
		}
		for k := range args {
			if strings.HasSuffix(k, "_id") {
				args[k] = ""
			}
		}
		out := getResultText(t, callTool(t, cs, c.tool, args))
		if !strings.Contains(out, "not a JumpCloud object id") {
			t.Errorf("%s accepted an empty id. Got: %s", c.tool, out)
		}
	}
}

// A well-formed id must still work, or the guard has broken the tools it was
// added to protect.
func TestADETools_AcceptAWellFormedID(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, pathTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "apple_mdm_ade_token_get",
		map[string]any{"token_id": "6a7e1bb88cf0be0001a4e3cd"}))
	if strings.Contains(out, "not a JumpCloud object id") {
		t.Errorf("a valid object id was refused: %s", out)
	}
}

// The structural half: derive the id-taking tools from the registry rather
// than from the table above, so a tool added later cannot escape the check
// because nobody remembered to list it. Same shape as
// TestEveryAgentsToolTakingExecuteIsGated, and for the same reason — the
// agents read-only gate was missed on all twelve tools at once.
func TestEveryADEToolTakingAnIDValidatesIt(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, pathTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	listed := map[string]bool{}
	for _, c := range idTakingADETools {
		listed[c.tool] = true
	}

	var found int
	for _, tool := range res.Tools {
		if !strings.HasPrefix(tool.Name, "apple_mdm_ade") &&
			!strings.HasPrefix(tool.Name, "apple_mdm_abm") &&
			!strings.HasPrefix(tool.Name, "apple_mdm_adue") &&
			!strings.HasPrefix(tool.Name, "apple_mdm_background") {
			continue
		}
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
		// A property ending in _id is a caller-supplied value that becomes
		// part of a request path, so the tool must be covered above.
		takesID := false
		for name := range schema.Properties {
			if strings.HasSuffix(name, "_id") {
				takesID = true
			}
		}
		if !takesID {
			continue
		}
		found++
		if !listed[tool.Name] {
			t.Errorf("%s takes an id but is not covered by TestADETools_RefuseAMalformedID; "+
				"add it to idTakingADETools", tool.Name)
		}
	}

	if found == 0 {
		t.Fatal("no id-taking ADE tool was found — the prefix filter or the schema shape " +
			"changed, and this test would now pass by examining nothing")
	}
}
