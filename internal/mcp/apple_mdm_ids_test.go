package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// validIDFor supplies a well-formed value for every id argument in the area,
// so a traversal can be placed in ONE slot at a time with every other slot
// valid. Without this, a tool taking two ids would only ever be tested with a
// traversal in whichever slot the table happened to name — and the other slot
// could interpolate anything.
var validIDFor = map[string]string{
	"token_id":     "6a7e1bb88cf0be0001a4e3cd",
	"apple_mdm_id": "690b6c939c0bf5000100039a",
	"device_id":    "69c45aabb695320001aff063",
}

// extraArgs carries the non-id arguments a tool needs to reach its id check.
var extraArgs = map[string]map[string]any{
	"apple_mdm_ade_settings_get": {"os_family": "DARWIN"},
	// These two carry required properties of their own, and the MCP schema
	// layer rejects a call missing them before the handler's id check can
	// run — so without these the test would report the tool as accepting a
	// traversal when in fact it never got that far.
	"apple_mdm_device_schedule_os_update": {
		"install_action": "DOWNLOAD_ONLY",
		"product_key":    "MSU_UPDATE_24A348_patch_15.1",
	},
	// false is the disable side of the oneof: a valid request that needs no
	// message or phone number, so the id check is what fails.
	"apple_mdm_device_lost_mode": {"enable": false},
	// One real change, so that a well-formed id gets past "nothing to change"
	// and the id check is the only thing left to fail.
	"apple_mdm_ade_settings_update": {"os_family": "IOS", "zero_touch": true},
}

// idTakingAppleMDMTools is every Apple MDM tool that puts a caller-supplied
// value into a request path. It is checked for completeness against the live
// registry by TestEveryAppleMDMToolTakingAnIDValidatesIt below, which fails
// when a tool is added without being listed here.
//
// optionalIDs names arguments where OMITTING the value is a legitimate
// request rather than a mistake: apple_mdm_abm_devices_list without an
// apple_mdm_id reads the whole organization through an endpoint with nothing
// interpolated into it, so an empty value there must NOT be refused — while a
// malformed one still must.
var idTakingAppleMDMTools = map[string][]string{
	"apple_mdm_ade_token_get":         nil,
	"apple_mdm_ade_settings_list":     nil,
	"apple_mdm_ade_settings_get":      nil,
	"apple_mdm_abm_devices_list":      {"apple_mdm_id"},
	"apple_mdm_background_tasks_list": nil,
	"apple_mdm_adue_validate":         nil,
	// The device reads.
	"apple_mdm_device_get":           nil,
	"apple_mdm_device_lock_state":    nil,
	"apple_mdm_recovery_lock_config": nil,
	"apple_mdm_csr_get":              nil,
	// The device actions (slice 3). Every one of these interpolates both ids
	// into a request path that then performs an action, so the check matters
	// more here than on the reads: a traversal that reached the wire would be
	// a POST, not a GET. They were all caught by the structural test below
	// when they were added, which is what the exclusion-list rewrite was for.
	"apple_mdm_device_erase":                       nil,
	"apple_mdm_device_lock":                        nil,
	"apple_mdm_device_restart":                     nil,
	"apple_mdm_device_shutdown":                    nil,
	"apple_mdm_device_unenroll":                    nil,
	"apple_mdm_device_clear_passcode":              nil,
	"apple_mdm_device_clear_restrictions_password": nil,
	"apple_mdm_device_clear_activation_lock":       nil,
	"apple_mdm_device_refresh_activation_lock":     nil,
	"apple_mdm_device_os_update_status":            nil,
	"apple_mdm_device_schedule_os_update":          nil,
	"apple_mdm_device_lost_mode":                   nil,
	"apple_mdm_refresh_dep_devices":                nil,
	// Slice 4: the ADE/ADUE writes. All four were caught by the structural
	// test before they were listed here.
	"apple_mdm_ade_settings_update":           nil,
	"apple_mdm_ade_token_refresh_abm_devices": nil,
	"apple_mdm_adue_enable":                   nil,
	"apple_mdm_adue_disable":                  nil,
}

// payloadTypeArg is the one id-shaped argument in this area that is NOT an
// object id: the payload tools take a Configuration Profile payload type such
// as com.apple.wifi.managed, which is a dotted name and would be refused by
// an object-id check. They build no request path from it.
const payloadTypeArg = "payloadtype_or_id"

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

// idArgsOf returns a tool's id-shaped arguments, read from the live schema
// rather than from a list, so a new argument on an existing tool is covered
// without anyone remembering to add it.
func idArgsOf(t *testing.T, cs toolLister, tool string) []string {
	t.Helper()
	for _, got := range listAppleMDMTools(t, cs) {
		if got.name == tool {
			return got.idArgs
		}
	}
	t.Fatalf("tool %q is listed in idTakingAppleMDMTools but is not registered", tool)
	return nil
}

func TestAppleMDMTools_RefuseAMalformedID(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, pathTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})

	for tool := range idTakingAppleMDMTools {
		// One case per id slot: traversal here, valid everywhere else.
		for _, target := range idArgsOf(t, cs, tool) {
			args := map[string]any{}
			for k, v := range extraArgs[tool] {
				args[k] = v
			}
			for _, name := range idArgsOf(t, cs, tool) {
				args[name] = validIDFor[name]
			}
			args[target] = "../../systemusers"

			out := getResultText(t, callTool(t, cs, tool, args))
			if !strings.Contains(out, "not a JumpCloud object id") {
				t.Errorf("%s accepted a traversal in %s. Got: %s", tool, target, out)
			}
		}
	}
}

// An empty id must be refused too, and for the same reason rather than by a
// separate code path — the earlier version had a bespoke "x is required"
// check that said nothing about shape.
func TestAppleMDMTools_RefuseAnEmptyID(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, pathTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})

	for tool, optional := range idTakingAppleMDMTools {
		for _, target := range idArgsOf(t, cs, tool) {
			if contains(optional, target) {
				continue
			}
			args := map[string]any{}
			for k, v := range extraArgs[tool] {
				args[k] = v
			}
			for _, name := range idArgsOf(t, cs, tool) {
				args[name] = validIDFor[name]
			}
			args[target] = ""

			out := getResultText(t, callTool(t, cs, tool, args))
			if !strings.Contains(out, "not a JumpCloud object id") {
				t.Errorf("%s accepted an empty %s. Got: %s", tool, target, out)
			}
		}
	}
}

// A well-formed id must still work, or the guard has broken the tools it was
// added to protect.
func TestAppleMDMTools_AcceptAWellFormedID(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, pathTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "apple_mdm_ade_token_get",
		map[string]any{"token_id": "6a7e1bb88cf0be0001a4e3cd"}))
	if strings.Contains(out, "not a JumpCloud object id") {
		t.Errorf("a valid object id was refused: %s", out)
	}
}

// The structural half, and the reason this file was rewritten.
//
// The first version filtered the registry by the prefixes apple_mdm_ade,
// apple_mdm_abm, apple_mdm_adue and apple_mdm_background. That list FAILED
// OPEN: the four device-read tools added afterwards matched none of those
// prefixes, so they were silently outside the check that exists to stop
// exactly that. The filter is now an exclusion — every Apple MDM tool taking
// an id-shaped argument must be covered, and a tool added later is included by
// default and fails this test until it is listed. Forgetting now fails loudly
// instead of quietly.
func TestEveryAppleMDMToolTakingAnIDValidatesIt(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, pathTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})

	var found int
	for _, tool := range listAppleMDMTools(t, cs) {
		if len(tool.idArgs) == 0 {
			continue
		}
		found++
		if _, listed := idTakingAppleMDMTools[tool.name]; !listed {
			t.Errorf("%s takes %v, which becomes part of a request path, but it is not "+
				"covered by TestAppleMDMTools_RefuseAMalformedID — add it to "+
				"idTakingAppleMDMTools", tool.name, tool.idArgs)
		}
		for _, name := range tool.idArgs {
			if _, ok := validIDFor[name]; !ok {
				t.Errorf("%s takes id argument %q with no entry in validIDFor, so the other "+
					"slots cannot be filled with valid values while this one is attacked",
					tool.name, name)
			}
		}
	}

	if found == 0 {
		t.Fatal("no id-taking Apple MDM tool was found — the filter or the schema shape " +
			"changed, and this test would now pass by examining nothing")
	}
}

type appleMDMTool struct {
	name   string
	idArgs []string
}

type toolLister interface {
	ListTools(context.Context, *mcp.ListToolsParams) (*mcp.ListToolsResult, error)
}

// listAppleMDMTools reads the registry and returns each Apple MDM tool with
// its id-shaped arguments. payloadTypeArg is excluded with a reason; nothing
// else is.
func listAppleMDMTools(t *testing.T, cs toolLister) []appleMDMTool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var out []appleMDMTool
	for _, tool := range res.Tools {
		if !strings.HasPrefix(tool.Name, "apple_mdm") {
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
		var ids []string
		for name := range schema.Properties {
			if strings.HasSuffix(name, "_id") && name != payloadTypeArg {
				ids = append(ids, name)
			}
		}
		sortStrings(ids)
		out = append(out, appleMDMTool{name: tool.Name, idArgs: ids})
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
