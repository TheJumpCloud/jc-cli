package mcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/bundle"
)

// isolateBundlesDirMCP keeps the developer's real ~/.config/jc/bundles
// out of MCP tests, mirroring the cmd-side helper.
func isolateBundlesDirMCP(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := bundle.BundlesDir
	bundle.BundlesDir = func() string { return dir }
	t.Cleanup(func() { bundle.BundlesDir = orig })
}

// startBundleTenantServer stubs every V2 endpoint bundle_apply and
// bundle_status touch. mutations records POSTs in order.
func startBundleTenantServer(t *testing.T, mutations *[]string) *httptest.Server {
	return startBundleTenantServerOpts(t, mutations, false)
}

// startBundleTenantServerOpts is startBundleTenantServer with a knob to
// fail the policy-group POST — used to exercise the partial-failure path
// (policies created, group create fails).
func startBundleTenantServerOpts(t *testing.T, mutations *[]string, failGroupPost bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		if failGroupPost && path == "/api/v2/policygroups" && r.Method == "POST" {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"group boom"}`))
			return
		}
		switch {
		case path == "/api/v2/policytemplates" && r.Method == "GET":
			filter := r.URL.Query().Get("filter")
			switch {
			case strings.Contains(filter, "custom_mdm_profile_darwin"):
				_, _ = w.Write([]byte(`[{"id":"tmpl-darwin","name":"custom_mdm_profile_darwin"}]`))
			case strings.Contains(filter, "custom_oma_uri_mdm_windows"):
				_, _ = w.Write([]byte(`[{"id":"tmpl-oma","name":"custom_oma_uri_mdm_windows"}]`))
			case strings.Contains(filter, "custom_registry_keys_policy_windows"):
				_, _ = w.Write([]byte(`[{"id":"tmpl-reg","name":"custom_registry_keys_policy_windows"}]`))
			default:
				_, _ = w.Write([]byte(`[]`))
			}
		case path == "/api/v2/policytemplates/tmpl-darwin":
			_, _ = w.Write([]byte(`{"id":"tmpl-darwin","name":"custom_mdm_profile_darwin",
				"configFields":[{"id":"payfid","name":"payload"},{"id":"redfid","name":"redispatchPolicy"}]}`))
		case path == "/api/v2/policytemplates/tmpl-oma":
			_, _ = w.Write([]byte(`{"id":"tmpl-oma","name":"custom_oma_uri_mdm_windows",
				"configFields":[{"id":"urifid","name":"uriList"}]}`))
		case path == "/api/v2/policytemplates/tmpl-reg":
			_, _ = w.Write([]byte(`{"id":"tmpl-reg","name":"custom_registry_keys_policy_windows",
				"configFields":[{"id":"regfid","name":"customRegTable"}]}`))
		case path == "/api/v2/policies" && r.Method == "GET":
			_, _ = w.Write([]byte(`[]`)) // no conflicts
		case path == "/api/v2/policygroups" && r.Method == "GET":
			_, _ = w.Write([]byte(`[]`))
		case path == "/api/v2/policies" && r.Method == "POST":
			body, _ := io.ReadAll(r.Body)
			var b struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(body, &b)
			*mutations = append(*mutations, "policy:"+b.Name)
			_, _ = w.Write([]byte(`{"id":"pol-` + b.Name[len(b.Name)-1:] + `","name":"` + b.Name + `"}`))
		case path == "/api/v2/policygroups" && r.Method == "POST":
			body, _ := io.ReadAll(r.Body)
			var b struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			_ = json.Unmarshal(body, &b)
			*mutations = append(*mutations, "group:"+b.Name+"|"+b.Description)
			_, _ = w.Write([]byte(`{"id":"pg-1","name":"` + b.Name + `"}`))
		case strings.HasSuffix(path, "/members") && r.Method == "POST":
			*mutations = append(*mutations, "member")
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"path":"` + path + `","method":"` + r.Method + `"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBundleList_IncludesBuiltin(t *testing.T) {
	setupToolTest(t)
	isolateBundlesDirMCP(t)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "bundle_list", map[string]any{})
	if res.IsError {
		t.Fatalf("bundle_list errored: %s", getResultText(t, res))
	}
	var out bundleListResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range out.Bundles {
		if b.Name == "example-baseline" {
			found = true
			if b.Origin != "builtin" || b.Policies != 3 || !strings.Contains(b.Platforms, "macOS") {
				t.Errorf("summary wrong: %+v", b)
			}
		}
	}
	if !found {
		t.Errorf("example-baseline missing: %+v", out.Bundles)
	}
}

func TestBundleShow_FullAndNotFound(t *testing.T) {
	setupToolTest(t)
	isolateBundlesDirMCP(t)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "bundle_show", map[string]any{"name": "example-baseline"})
	if res.IsError {
		t.Fatalf("bundle_show errored: %s", getResultText(t, res))
	}
	var b bundle.Bundle
	if err := json.Unmarshal([]byte(getResultText(t, res)), &b); err != nil {
		t.Fatal(err)
	}
	if b.Name != "example-baseline" || len(b.Policies) != 3 || b.Source.Attribution == "" {
		t.Errorf("show lost detail: %+v", b)
	}

	res = callTool(t, cs, "bundle_show", map[string]any{"name": "nope"})
	if !res.IsError || !strings.Contains(getResultText(t, res), "bundle_list") {
		t.Errorf("not-found should point at bundle_list: %s", getResultText(t, res))
	}
}

// TestBundleApply_PreviewThenExecute is the two-phase agent flow: the
// preview returns the full step plan without a single POST; the
// execute run POSTs everything in order.
func TestBundleApply_PreviewThenExecute(t *testing.T) {
	setupToolTest(t)
	isolateBundlesDirMCP(t)
	var mutations []string
	srv := startBundleTenantServer(t, &mutations)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "bundle_apply", map[string]any{"name": "example-baseline"})
	if res.IsError {
		t.Fatalf("preview errored: %s", getResultText(t, res))
	}
	var out bundleApplyResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatal(err)
	}
	if out.Executed || out.Result != nil {
		t.Error("preview must not execute")
	}
	// 3 policies + group + 3 members = 7 steps (no binding without
	// device_group).
	if len(out.Steps) != 7 {
		t.Errorf("steps = %d, want 7: %+v", len(out.Steps), out.Steps)
	}
	if len(mutations) != 0 {
		t.Fatalf("preview must not POST: %v", mutations)
	}

	res = callTool(t, cs, "bundle_apply", map[string]any{"name": "example-baseline", "execute": true})
	if res.IsError {
		t.Fatalf("execute errored: %s", getResultText(t, res))
	}
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Executed || out.Result == nil || len(out.Result.Created) != 4 {
		t.Errorf("execute result wrong: %+v", out)
	}
	// Order: 3 policies, then the group (with provenance marker), then
	// 3 member adds.
	if len(mutations) != 7 {
		t.Fatalf("mutations = %d, want 7: %v", len(mutations), mutations)
	}
	if !strings.HasPrefix(mutations[3], "group:example-baseline (v1.0.0)|bundle:example-baseline@1.0.0") {
		t.Errorf("group create wrong: %s", mutations[3])
	}
}

// TestBundleApply_PartialFailureCarriesResult guards the fix that stops
// bundle_apply from discarding the structured result on Execute error:
// when the group POST fails after the policies are created, the error
// result must still carry out.Result (with every created policy id) so
// the agent can clean up precisely — not just the bare error text.
func TestBundleApply_PartialFailureCarriesResult(t *testing.T) {
	setupToolTest(t)
	isolateBundlesDirMCP(t)
	var mutations []string
	// Fail the policy-group POST so the failure lands after all three
	// policies were created.
	srv := startBundleTenantServerOpts(t, &mutations, true)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "bundle_apply", map[string]any{"name": "example-baseline", "execute": true})
	if !res.IsError {
		t.Fatal("expected execute failure")
	}
	var out bundleApplyResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatalf("error result not structured JSON: %v\n%s", err, getResultText(t, res))
	}
	if out.Executed {
		t.Error("Executed must be false on failure")
	}
	if out.Error == "" || !strings.Contains(out.Error, "bundle_apply") {
		t.Errorf("Error text missing: %q", out.Error)
	}
	if out.Result == nil || len(out.Result.Created) != 3 {
		t.Fatalf("result must carry the 3 created policies: %+v", out.Result)
	}
}

// TestBundleApply_PreFlightBeforeStepUp: an unknown bundle name (or a
// broken bundle) must fail before the step-up gate fires.
func TestBundleApply_PreFlightBeforeStepUp(t *testing.T) {
	setupToolTest(t)
	isolateBundlesDirMCP(t)
	stepUp := &recordingStepUp{}
	cs := connectToolTestServer(t, Options{stepUp: stepUp})

	res := callTool(t, cs, "bundle_apply", map[string]any{"name": "no-such-bundle", "execute": true})
	if !res.IsError {
		t.Fatal("expected unknown-bundle error")
	}
	if gateCalls := stepUp.calls.Load(); gateCalls != 0 {
		t.Errorf("step-up gate fired %d time(s) for invalid input", gateCalls)
	}
}

func TestBundleApply_ReadOnlyRefusesExecute(t *testing.T) {
	setupToolTest(t)
	isolateBundlesDirMCP(t)
	cs := connectToolTestServer(t, Options{ReadOnly: true})

	res := callTool(t, cs, "bundle_apply", map[string]any{"name": "example-baseline", "execute": true})
	if !res.IsError || !strings.Contains(getResultText(t, res), "read-only") {
		t.Errorf("read-only server must refuse execute: %s", getResultText(t, res))
	}
}

// TestBundleStatus_InSyncViaMarker exercises the full read path: the
// policy group is found by its provenance marker, members decode, and
// the report says in-sync. The Apple member is a REAL emitted
// mobileconfig so the decode path sees genuine plist bytes.
func TestBundleStatus_InSyncViaMarker(t *testing.T) {
	setupToolTest(t)
	isolateBundlesDirMCP(t)

	cat, err := apple_mdm.Default()
	if err != nil {
		t.Fatal(err)
	}
	builtins, err := bundle.LoadBuiltIn()
	if err != nil {
		t.Fatal(err)
	}
	b := bundle.FindByName(builtins, "example-baseline")
	if b == nil {
		t.Fatal("example-baseline missing")
	}

	// Emit the Apple unit exactly as apply would have.
	instances, env, err := b.Policies[0].Profile.BuildPayloadInstances(cat)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := apple_mdm.EmitMobileconfig(&buf, env, instances); err != nil {
		t.Fatal(err)
	}
	applePolicy, _ := json.Marshal(map[string]any{
		"id": "pol-1", "name": bundle.PolicyName(b, b.Policies[0].Name),
		"template": map[string]string{"name": "custom_mdm_profile_darwin"},
		"values": []map[string]any{
			{"configFieldName": "payload", "value": base64.StdEncoding.EncodeToString(buf.Bytes())},
			{"configFieldName": "redispatchPolicy", "value": true},
		},
	})
	omaPolicy, _ := json.Marshal(map[string]any{
		"id": "pol-2", "name": bundle.PolicyName(b, b.Policies[1].Name),
		"template": map[string]string{"name": "custom_oma_uri_mdm_windows"},
		"values": []map[string]any{{
			"configFieldName": "uriList",
			"value":           []map[string]string{{"uri": b.Policies[1].Settings[0].URI, "format": "int", "value": "0"}},
		}},
	})
	regPolicy, _ := json.Marshal(map[string]any{
		"id": "pol-3", "name": bundle.PolicyName(b, b.Policies[2].Name),
		"template": map[string]string{"name": "custom_registry_keys_policy_windows"},
		"values": []map[string]any{{
			"configFieldName": "customRegTable",
			"value": []map[string]string{{
				"customLocation": b.Policies[2].Keys[0].Location, "customValueName": b.Policies[2].Keys[0].Name,
				"customRegType": "DWORD", "customData": "1",
			}},
		}},
	})

	policies := map[string]string{"pol-1": string(applePolicy), "pol-2": string(omaPolicy), "pol-3": string(regPolicy)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == "/api/v2/policygroups":
			_, _ = w.Write([]byte(`[{"id":"pg-1","name":"whatever the admin renamed it to","description":"bundle:example-baseline@1.0.0"}]`))
		case path == "/api/v2/policygroups/pg-1/members":
			_, _ = w.Write([]byte(`[{"to":{"id":"pol-1","type":"policy"}},{"to":{"id":"pol-2","type":"policy"}},{"to":{"id":"pol-3","type":"policy"}}]`))
		case strings.HasPrefix(path, "/api/v2/policies/"):
			_, _ = w.Write([]byte(policies[strings.TrimPrefix(path, "/api/v2/policies/")]))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "bundle_status", map[string]any{"name": "example-baseline"})
	if res.IsError {
		t.Fatalf("bundle_status errored: %s", getResultText(t, res))
	}
	var report bundle.StatusReport
	if err := json.Unmarshal([]byte(getResultText(t, res)), &report); err != nil {
		t.Fatal(err)
	}
	if !report.InSync || !report.MatchedByMarker {
		t.Errorf("report wrong: %+v", report)
	}
	// Rename-proof: the group was found by marker despite the name.
	if report.PolicyGroupName != "whatever the admin renamed it to" {
		t.Errorf("group name = %q", report.PolicyGroupName)
	}
}
