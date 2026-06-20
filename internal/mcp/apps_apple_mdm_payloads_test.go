package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startApplePolicyTemplateServer stubs the JumpCloud V2 endpoints the
// create_policy tool hits — the policy-template lookup (both list +
// detail) and the POST /policies that creates the policy.
//
// onCreate is invoked with the marshalled body so individual tests can
// assert what got POSTed. Pass nil if the test only cares about the
// preview path.
func startApplePolicyTemplateServer(t *testing.T, onCreate func(body []byte)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v2/policytemplates" && r.Method == "GET":
			// Match either macOS or iOS depending on the filter.
			filter := r.URL.Query().Get("filter")
			switch {
			case strings.Contains(filter, "custom_mdm_profile_darwin"):
				_, _ = w.Write([]byte(`[{"id":"tmpl-darwin","name":"custom_mdm_profile_darwin"}]`))
			case strings.Contains(filter, "custom_mdm_profile_ios"):
				_, _ = w.Write([]byte(`[{"id":"tmpl-ios","name":"custom_mdm_profile_ios"}]`))
			default:
				_, _ = w.Write([]byte(`[]`))
			}
		case r.URL.Path == "/api/v2/policytemplates/tmpl-darwin" && r.Method == "GET":
			_, _ = w.Write([]byte(`{
				"id":"tmpl-darwin","name":"custom_mdm_profile_darwin",
				"configFields":[
					{"id":"pfid","name":"payload"},
					{"id":"rdid","name":"redispatchPolicy"}
				]
			}`))
		case r.URL.Path == "/api/v2/policytemplates/tmpl-ios" && r.Method == "GET":
			// Mirrors the live iOS template — no redispatch field.
			_, _ = w.Write([]byte(`{
				"id":"tmpl-ios","name":"custom_mdm_profile_ios",
				"configFields":[
					{"id":"iospfid","name":"payload"}
				]
			}`))
		case r.URL.Path == "/api/v2/policies" && r.Method == "POST":
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			if onCreate != nil {
				onCreate(body)
			}
			_, _ = w.Write([]byte(`{"id":"pol-7777","name":"Stub Created Policy"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"path":"` + r.URL.Path + `","method":"` + r.Method + `"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAppleMDMPayloadsSearch_FiltersByOSAndSearch confirms the search
// tool actually applies its filters — without this it would silently
// devolve into a 125-entry dump (which still "works" but is useless to
// an agent reasoning under a context budget).
func TestAppleMDMPayloadsSearch_FiltersByOSAndSearch(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	// No filter — sanity-check the upper bound. The catalog is large
	// enough that a missing filter is obvious.
	res := callTool(t, cs, "apple_mdm_payloads_search", map[string]any{})
	if res.IsError {
		t.Fatalf("unfiltered search errored: %s", getResultText(t, res))
	}
	var all appleMDMPayloadsSearchResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &all); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if all.Total < 50 {
		t.Errorf("catalog suspiciously small: total=%d", all.Total)
	}
	if all.Matched != all.Total {
		t.Errorf("unfiltered matched=%d != total=%d", all.Matched, all.Total)
	}

	// OS filter — iOS-supported only. macOS-only payloads must drop.
	res = callTool(t, cs, "apple_mdm_payloads_search", map[string]any{"os": "iOS"})
	var iosOnly appleMDMPayloadsSearchResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &iosOnly); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if iosOnly.Matched >= all.Total {
		t.Errorf("iOS filter didn't drop anything: matched=%d total=%d", iosOnly.Matched, all.Total)
	}
	for _, p := range iosOnly.Payloads {
		found := false
		for _, plat := range p.SupportedOS {
			if plat == "iOS" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("payload %s slipped past iOS filter (supported=%v)", p.Type, p.SupportedOS)
		}
	}

	// Search filter — pick a string we know is in the catalog.
	res = callTool(t, cs, "apple_mdm_payloads_search", map[string]any{"search": "firewall"})
	var firewall appleMDMPayloadsSearchResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &firewall); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if firewall.Matched == 0 {
		t.Fatal("expected at least one firewall match")
	}
	for _, p := range firewall.Payloads {
		// Substring must appear in either type, title, or description.
		hay := strings.ToLower(p.Type + " " + p.Title + " " + p.Description)
		if !strings.Contains(hay, "firewall") {
			t.Errorf("payload %q slipped past search filter", p.Type)
		}
	}
}

// TestAppleMDMPayloadsShow_ResolvesByTypeAndID covers both the
// happy-path lookup and the MCX ambiguity error — the same disambig
// behavior the CLI surfaces. Agents need the error to mention "ambiguous"
// + list the IDs so they can re-call with the right one.
func TestAppleMDMPayloadsShow_ResolvesByTypeAndID(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	// By type — unambiguous single-variant payload.
	res := callTool(t, cs, "apple_mdm_payloads_show", map[string]any{
		"payloadtype_or_id": "com.apple.security.firewall",
	})
	if res.IsError {
		t.Fatalf("show by type errored: %s", getResultText(t, res))
	}
	var show appleMDMPayloadsShowResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &show); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if show.Payload.Type != "com.apple.security.firewall" {
		t.Errorf("Type = %q", show.Payload.Type)
	}
	if len(show.Payload.Keys) == 0 {
		t.Error("Keys empty — show should surface the full schema")
	}

	// By ID — MCX variant disambiguation.
	res = callTool(t, cs, "apple_mdm_payloads_show", map[string]any{
		"payloadtype_or_id": "com.apple.MCX(EnergySaver)",
	})
	if res.IsError {
		t.Fatalf("show by ID errored: %s", getResultText(t, res))
	}
	var mcx appleMDMPayloadsShowResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &mcx); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if mcx.Payload.ID != "com.apple.MCX(EnergySaver)" {
		t.Errorf("ID = %q, want com.apple.MCX(EnergySaver)", mcx.Payload.ID)
	}

	// Ambiguous type — must error with both "ambiguous" and the
	// available IDs so the agent can fix its call.
	res = callTool(t, cs, "apple_mdm_payloads_show", map[string]any{
		"payloadtype_or_id": "com.apple.MCX",
	})
	if !res.IsError {
		t.Fatal("expected error for ambiguous com.apple.MCX")
	}
	msg := getResultText(t, res)
	if !strings.Contains(msg, "ambiguous") {
		t.Errorf("error should mention ambiguity: %s", msg)
	}
	if !strings.Contains(msg, "com.apple.MCX(") {
		t.Errorf("error should list variant IDs: %s", msg)
	}
}

// TestAppleMDMPayloadsTemplate_EmitsValidMobileconfig is end-to-end
// against the real catalog — agent-supplied values flow through
// validation, then the emitter, and the response surfaces both the
// validated values (so the agent can echo "this is what I built") and
// the raw plist bytes (so a downstream tool or human can verify the
// shape without rebuilding).
func TestAppleMDMPayloadsTemplate_EmitsValidMobileconfig(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_payloads_template", map[string]any{
		"payloadtype_or_id": "com.apple.security.firewall",
		"values": map[string]any{
			"EnableFirewall":    true,
			"EnableStealthMode": true,
		},
		"display_name":       "Corp Firewall",
		"organization":       "ACME",
		"removal_disallowed": true,
	})
	if res.IsError {
		t.Fatalf("template errored: %s", getResultText(t, res))
	}
	var out appleMDMPayloadsTemplateResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.PayloadType != "com.apple.security.firewall" {
		t.Errorf("PayloadType = %q", out.PayloadType)
	}
	if out.ValidatedValues["EnableFirewall"] != true {
		t.Errorf("ValidatedValues missing EnableFirewall=true: %v", out.ValidatedValues)
	}
	if out.MobileconfigBytes == 0 {
		t.Error("MobileconfigBytes = 0")
	}
	// Quick shape check — the result is a property-list XML envelope
	// with the Apple PayloadType embedded.
	if !strings.Contains(out.Mobileconfig, "<plist") {
		t.Error("Mobileconfig missing <plist root")
	}
	if !strings.Contains(out.Mobileconfig, "com.apple.security.firewall") {
		t.Error("Mobileconfig missing inner PayloadType")
	}
}

// TestAppleMDMPayloadsTemplate_RejectsInvalidValues guards the
// validation surface — the agent should get a precise error rather
// than a silently-shipped malformed plist.
func TestAppleMDMPayloadsTemplate_RejectsInvalidValues(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_payloads_template", map[string]any{
		"payloadtype_or_id": "com.apple.security.firewall",
		"values": map[string]any{
			"NotARealKey": "x",
		},
	})
	if !res.IsError {
		t.Fatal("expected validation error for unknown key")
	}
	if !strings.Contains(getResultText(t, res), "NotARealKey") {
		t.Errorf("error should name the unknown key: %s", getResultText(t, res))
	}
}

// TestAppleMDMPayloadsCreatePolicy_PreviewResolvesTemplate is the
// without-execute path: the tool should NOT POST, but it SHOULD reach
// the JC tenant to resolve the template — the agent uses that template
// ID as confirmation it's pointing at the right family before flipping
// execute: true.
func TestAppleMDMPayloadsCreatePolicy_PreviewResolvesTemplate(t *testing.T) {
	setupToolTest(t)

	posted := false
	srv := startApplePolicyTemplateServer(t, func([]byte) { posted = true })
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_payloads_create_policy", map[string]any{
		"payloadtype_or_id": "com.apple.security.firewall",
		"values": map[string]any{
			"EnableFirewall": true,
		},
		"policy_name": "Preview Test",
		"os":          "macOS",
		// No execute — preview only.
	})
	if res.IsError {
		t.Fatalf("preview errored: %s", getResultText(t, res))
	}
	if posted {
		t.Fatal("preview must not POST /policies")
	}
	var out appleMDMPayloadsCreatePolicyResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Executed {
		t.Error("Executed should be false on preview")
	}
	if out.JCTemplateID != "tmpl-darwin" {
		t.Errorf("JCTemplateID = %q, want tmpl-darwin", out.JCTemplateID)
	}
	if out.MobileconfigBytes == 0 {
		t.Error("Mobileconfig should be returned on preview too")
	}
	if out.PolicyID != "" {
		t.Errorf("PolicyID should be empty on preview, got %q", out.PolicyID)
	}
}

// TestAppleMDMPayloadsCreatePolicy_ExecuteCreatesPolicy is the
// with-execute path — the addTypedTool wrapper carries the step-up
// gate but server-default tests get the noop authenticator, so the
// handler runs normally.
func TestAppleMDMPayloadsCreatePolicy_ExecuteCreatesPolicy(t *testing.T) {
	setupToolTest(t)

	var capturedBody []byte
	srv := startApplePolicyTemplateServer(t, func(body []byte) { capturedBody = body })
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_payloads_create_policy", map[string]any{
		"payloadtype_or_id": "com.apple.security.firewall",
		"values": map[string]any{
			"EnableFirewall": true,
		},
		"policy_name": "Real Policy",
		"os":          "macOS",
		"redispatch":  true,
		"execute":     true,
	})
	if res.IsError {
		t.Fatalf("execute errored: %s", getResultText(t, res))
	}
	var out appleMDMPayloadsCreatePolicyResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Executed {
		t.Error("Executed should be true after a successful POST")
	}
	if out.PolicyID != "pol-7777" {
		t.Errorf("PolicyID = %q, want pol-7777", out.PolicyID)
	}
	if len(capturedBody) == 0 {
		t.Fatal("server-side POST capture was empty")
	}
	// Verify the body shape — both the payload field and the
	// redispatch field should appear (macOS template carries both).
	if !strings.Contains(string(capturedBody), "pfid") {
		t.Errorf("POST body missing payload configFieldID: %s", string(capturedBody))
	}
	if !strings.Contains(string(capturedBody), "rdid") {
		t.Errorf("POST body missing redispatch configFieldID despite redispatch=true: %s", string(capturedBody))
	}
}

// TestAppleMDMPayloadsCreatePolicy_RejectsUnsupportedOS is the Bugbot
// PR #59 carryover — single-payload create-policy already gated on
// SupportedOS in the CLI; the MCP path must enforce the same. An
// iOS-only payload shipped as a macOS policy is a silent data-loss bug
// devices will ignore.
func TestAppleMDMPayloadsCreatePolicy_RejectsUnsupportedOS(t *testing.T) {
	setupToolTest(t)
	srv := startApplePolicyTemplateServer(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	// com.apple.cellular is iOS-only in Apple's schemas — shipping as
	// a macOS policy should be refused before the JC API is touched.
	res := callTool(t, cs, "apple_mdm_payloads_create_policy", map[string]any{
		"payloadtype_or_id": "com.apple.cellular",
		"policy_name":       "Bad OS",
		"os":                "macOS",
	})
	if !res.IsError {
		t.Fatal("expected error for iOS-only payload shipped as macOS")
	}
	msg := getResultText(t, res)
	if !strings.Contains(msg, "macOS") || !strings.Contains(msg, "com.apple.cellular") {
		t.Errorf("error should name the payload + platform: %s", msg)
	}
}

// TestAppleMDMPayloadsCreatePolicy_RefusesReadOnly mirrors the
// pattern recipe_run / users_delete enforce: a read-only server must
// reject execute: true regardless of whether the payload is otherwise
// valid. The destructive intent itself is the trigger.
func TestAppleMDMPayloadsCreatePolicy_RefusesReadOnly(t *testing.T) {
	setupToolTest(t)
	srv := startApplePolicyTemplateServer(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{ReadOnly: true})

	res := callTool(t, cs, "apple_mdm_payloads_create_policy", map[string]any{
		"payloadtype_or_id": "com.apple.security.firewall",
		"values":            map[string]any{"EnableFirewall": true},
		"policy_name":       "Should Refuse",
		"os":                "macOS",
		"execute":           true,
	})
	if !res.IsError {
		t.Fatal("expected read-only refusal")
	}
	if !strings.Contains(getResultText(t, res), "read-only") {
		t.Errorf("error should mention read-only mode: %s", getResultText(t, res))
	}
}

// TestAppleMDMPayloadsSearch_NormalizesOSAlias guards Bugbot PR #60
// finding 1: the search tool used to pass the raw `os` arg into
// Catalog.Filter, which keys SupportedOS as `macOS`/`iOS`. Agents
// passing the JC family alias (`ios`, `darwin`) — which we explicitly
// accept everywhere else — would get an empty list.
func TestAppleMDMPayloadsSearch_NormalizesOSAlias(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	// Lowercase `ios` MUST behave identically to canonical `iOS`.
	canon := callTool(t, cs, "apple_mdm_payloads_search", map[string]any{"os": "iOS"})
	alias := callTool(t, cs, "apple_mdm_payloads_search", map[string]any{"os": "ios"})

	var a, b appleMDMPayloadsSearchResult
	if err := json.Unmarshal([]byte(getResultText(t, canon)), &a); err != nil {
		t.Fatalf("canon unmarshal: %v", err)
	}
	if err := json.Unmarshal([]byte(getResultText(t, alias)), &b); err != nil {
		t.Fatalf("alias unmarshal: %v", err)
	}
	if a.Matched != b.Matched {
		t.Errorf("alias mismatch: iOS matched=%d, ios matched=%d", a.Matched, b.Matched)
	}
	if b.Matched == 0 {
		t.Error("ios alias matched 0 — normalization regression")
	}

	// Same for darwin → macOS.
	canon = callTool(t, cs, "apple_mdm_payloads_search", map[string]any{"os": "macOS"})
	alias = callTool(t, cs, "apple_mdm_payloads_search", map[string]any{"os": "darwin"})
	if err := json.Unmarshal([]byte(getResultText(t, canon)), &a); err != nil {
		t.Fatalf("canon unmarshal: %v", err)
	}
	if err := json.Unmarshal([]byte(getResultText(t, alias)), &b); err != nil {
		t.Fatalf("alias unmarshal: %v", err)
	}
	if a.Matched != b.Matched {
		t.Errorf("alias mismatch: macOS matched=%d, darwin matched=%d", a.Matched, b.Matched)
	}
}

// TestAppleMDMPayloadsCreatePolicy_RequiresPolicyName guards Bugbot
// PR #60 finding 3: `policy_name` is what JumpCloud stores as the
// policy name on /policies — an empty value would create a nameless
// policy in the tenant. The preflight check must surface this before
// the API hit (and before any step-up prompt).
func TestAppleMDMPayloadsCreatePolicy_RequiresPolicyName(t *testing.T) {
	setupToolTest(t)
	srv := startApplePolicyTemplateServer(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	// Empty string is the failure mode the JSON-RPC layer doesn't
	// catch — the field is *present*, just blank. The preflight has
	// to surface the policy_name requirement before the JC POST fires.
	res := callTool(t, cs, "apple_mdm_payloads_create_policy", map[string]any{
		"payloadtype_or_id": "com.apple.security.firewall",
		"values":            map[string]any{"EnableFirewall": true},
		"policy_name":       "",
		"os":                "macOS",
	})
	if !res.IsError {
		t.Fatal("expected error when policy_name is empty")
	}
	if !strings.Contains(getResultText(t, res), "policy_name") {
		t.Errorf("error should name the missing field: %s", getResultText(t, res))
	}
}

// TestAddTypedToolWithPreFlight_RunsBeforeStepUp guards Bugbot PR #60
// finding 2: the SupportedOS check used to live inside the handler,
// AFTER the addTypedTool wrapper had already run step-up. With the
// preFlight hook the check runs first; this test confirms the order
// by wiring a counting step-up authenticator and asserting it never
// fires when preFlight rejects the input.
func TestAddTypedToolWithPreFlight_RunsBeforeStepUp(t *testing.T) {
	setupToolTest(t)

	// Drop in a step-up authenticator that records every authorize
	// call. The default Options{} server uses noopStepUp, but we want
	// the destructive path to actually challenge so order matters.
	stepUp := &recordingStepUp{}
	srv := startApplePolicyTemplateServer(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{stepUp: stepUp})

	// iOS-only payload as macOS — preFlight should reject before
	// the gate fires.
	res := callTool(t, cs, "apple_mdm_payloads_create_policy", map[string]any{
		"payloadtype_or_id": "com.apple.cellular",
		"policy_name":       "Should Refuse",
		"os":                "macOS",
		"execute":           true,
	})
	if !res.IsError {
		t.Fatal("expected unsupported-OS error")
	}
	if gateCalls := stepUp.calls.Load(); gateCalls != 0 {
		t.Errorf("step-up gate fired %d time(s); preFlight should have short-circuited", gateCalls)
	}

	// Now a valid call — gate must fire exactly once.
	res = callTool(t, cs, "apple_mdm_payloads_create_policy", map[string]any{
		"payloadtype_or_id": "com.apple.security.firewall",
		"values":            map[string]any{"EnableFirewall": true},
		"policy_name":       "Real Policy",
		"os":                "macOS",
		"execute":           true,
	})
	if res.IsError {
		t.Fatalf("valid execute path errored: %s", getResultText(t, res))
	}
	if gateCalls := stepUp.calls.Load(); gateCalls != 1 {
		t.Errorf("expected exactly 1 step-up call on the valid path; got %d", gateCalls)
	}
}

// TestJCOSFamilyForMCP and TestCanonicalApplePlatformForMCP cover the
// MCP-package twins of the CLI helpers. The CLI tests live in
// apple_mdm_payloads_test.go; duplicating here keeps the MCP package
// self-contained and ensures parity if either gets edited.
func TestJCOSFamilyForMCP(t *testing.T) {
	tests := []struct {
		in        string
		want      string
		wantErr   bool
		errSubstr string
	}{
		{"macOS", "darwin", false, ""},
		{"darwin", "darwin", false, ""},
		{"", "darwin", false, ""}, // default to macOS
		{"iOS", "ios", false, ""},
		{"ios", "ios", false, ""},
		{"tvOS", "", true, "does not manage"},
		{"visionOS", "", true, "does not manage"},
		{"watchOS", "", true, "does not manage"},
		{"madeup", "", true, "unknown Apple platform"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := jcOSFamilyForMCP(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %q", tc.in)
				} else if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("error %q missing %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCanonicalApplePlatformForMCP(t *testing.T) {
	tests := []struct{ in, want string }{
		{"macOS", "macOS"},
		{"darwin", "macOS"},
		{"iOS", "iOS"},
		{"ios", "iOS"},
		{"tvOS", "tvOS"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := canonicalApplePlatformForMCP(tc.in); got != tc.want {
			t.Errorf("canonicalApplePlatformForMCP(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
