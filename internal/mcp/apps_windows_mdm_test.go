package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startWindowsPolicyTemplateServer stubs the JumpCloud V2 endpoints the
// two windows_mdm tools hit — the policy-template lookup (list +
// detail) for both Windows templates and the POST /policies.
//
// onCreate is invoked with the marshalled body so individual tests can
// assert what got POSTed. Pass nil for preview-only tests.
func startWindowsPolicyTemplateServer(t *testing.T, onCreate func(body []byte)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v2/policytemplates" && r.Method == "GET":
			filter := r.URL.Query().Get("filter")
			switch {
			case strings.Contains(filter, "custom_oma_uri_mdm_windows"):
				_, _ = w.Write([]byte(`[{"id":"tmpl-oma","name":"custom_oma_uri_mdm_windows"}]`))
			case strings.Contains(filter, "custom_registry_keys_policy_windows"):
				_, _ = w.Write([]byte(`[{"id":"tmpl-reg","name":"custom_registry_keys_policy_windows"}]`))
			default:
				_, _ = w.Write([]byte(`[]`))
			}
		case r.URL.Path == "/api/v2/policytemplates/tmpl-oma" && r.Method == "GET":
			_, _ = w.Write([]byte(`{
				"id":"tmpl-oma","name":"custom_oma_uri_mdm_windows",
				"configFields":[{"id":"urifid","name":"uriList"}]
			}`))
		case r.URL.Path == "/api/v2/policytemplates/tmpl-reg" && r.Method == "GET":
			_, _ = w.Write([]byte(`{
				"id":"tmpl-reg","name":"custom_registry_keys_policy_windows",
				"configFields":[{"id":"regfid","name":"customRegTable"}]
			}`))
		case r.URL.Path == "/api/v2/policies" && r.Method == "POST":
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			if onCreate != nil {
				onCreate(body)
			}
			_, _ = w.Write([]byte(`{"id":"pol-8888","name":"Stub Windows Policy"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"path":"` + r.URL.Path + `","method":"` + r.Method + `"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestWindowsMDMOMAURI_PreviewResolvesTemplate is the without-execute
// path: no POST, but the tool SHOULD reach the tenant to resolve the
// template — the agent uses the template ID as confirmation before
// flipping execute: true, and a tenant without Windows MDM fails here
// instead of after a step-up prompt.
func TestWindowsMDMOMAURI_PreviewResolvesTemplate(t *testing.T) {
	setupToolTest(t)

	posted := false
	srv := startWindowsPolicyTemplateServer(t, func([]byte) { posted = true })
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "windows_mdm_oma_uri_create_policy", map[string]any{
		"policy_name": "Preview Test",
		"settings": []map[string]any{
			{"uri": "./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera", "format": "integer", "value": "0"},
		},
		// No execute — preview only.
	})
	if res.IsError {
		t.Fatalf("preview errored: %s", getResultText(t, res))
	}
	if posted {
		t.Fatal("preview must not POST /policies")
	}
	var out windowsMDMCreatePolicyResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Executed {
		t.Error("Executed should be false on preview")
	}
	if out.JCTemplateID != "tmpl-oma" {
		t.Errorf("JCTemplateID = %q, want tmpl-oma", out.JCTemplateID)
	}
	// The preview must echo the NORMALIZED settings — `integer` is the
	// Admin Portal alias; the wire format is `int`.
	if len(out.Settings) != 1 || out.Settings[0].Format != "int" {
		t.Errorf("preview should normalize the format alias: %+v", out.Settings)
	}
	if out.PolicyID != "" {
		t.Errorf("PolicyID should be empty on preview, got %q", out.PolicyID)
	}
}

// TestWindowsMDMOMAURI_ExecuteCreatesPolicy is the with-execute path;
// asserts the POSTed body carries the resolved configField ID and the
// array-of-triples value shape (the key difference from Apple's scalar
// base64 payload).
func TestWindowsMDMOMAURI_ExecuteCreatesPolicy(t *testing.T) {
	setupToolTest(t)

	var capturedBody []byte
	srv := startWindowsPolicyTemplateServer(t, func(body []byte) { capturedBody = body })
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "windows_mdm_oma_uri_create_policy", map[string]any{
		"policy_name": "Require BitLocker",
		"settings": []map[string]any{
			{"uri": "./Device/Vendor/MSFT/Policy/Config/BitLocker/RequireDeviceEncryption", "format": "int", "value": "1"},
		},
		"execute": true,
	})
	if res.IsError {
		t.Fatalf("execute errored: %s", getResultText(t, res))
	}
	var out windowsMDMCreatePolicyResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Executed {
		t.Error("Executed should be true after a successful POST")
	}
	if out.PolicyID != "pol-8888" {
		t.Errorf("PolicyID = %q, want pol-8888", out.PolicyID)
	}
	if len(capturedBody) == 0 {
		t.Fatal("server-side POST capture was empty")
	}
	body := string(capturedBody)
	// The resolved (not hardcoded) field ID, the field name, and the
	// wire sub-field names of the triple must all appear.
	for _, want := range []string{`"urifid"`, `"uriList"`, `"uri":"./Device/Vendor/MSFT/Policy/Config/BitLocker/RequireDeviceEncryption"`, `"format":"int"`} {
		if !strings.Contains(body, want) {
			t.Errorf("POST body missing %s:\n%s", want, body)
		}
	}
}

// TestWindowsMDMRegistry_ExecuteCreatesPolicy covers the registry
// sibling end-to-end, asserting the wire column names
// (customLocation/customValueName/customRegType/customData).
func TestWindowsMDMRegistry_ExecuteCreatesPolicy(t *testing.T) {
	setupToolTest(t)

	var capturedBody []byte
	srv := startWindowsPolicyTemplateServer(t, func(body []byte) { capturedBody = body })
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "windows_mdm_registry_create_policy", map[string]any{
		"policy_name": "Disable Autorun",
		"keys": []map[string]any{
			{"location": `SOFTWARE\Policies\Microsoft\Windows\Explorer`, "name": "NoAutorun", "type": "REG_DWORD", "data": "1"},
		},
		"execute": true,
	})
	if res.IsError {
		t.Fatalf("execute errored: %s", getResultText(t, res))
	}
	var out windowsMDMCreatePolicyResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Executed || out.PolicyID != "pol-8888" {
		t.Errorf("execute result wrong: %+v", out)
	}
	// REG_DWORD alias must have normalized to the wire value DWORD.
	if len(out.Keys) != 1 || out.Keys[0].RegType != "DWORD" {
		t.Errorf("reg type alias not normalized: %+v", out.Keys)
	}
	body := string(capturedBody)
	for _, want := range []string{`"regfid"`, `"customRegTable"`, `"customValueName":"NoAutorun"`, `"customRegType":"DWORD"`, `"customData":"1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("POST body missing %s:\n%s", want, body)
		}
	}
}

// TestWindowsMDMTools_PreFlightBeforeStepUp is the KLA-452 order guard
// applied to both Windows tools: invalid input must be rejected BEFORE
// the step-up gate fires (no wasted Touch ID approval), and a valid
// execute must fire the gate exactly once.
func TestWindowsMDMTools_PreFlightBeforeStepUp(t *testing.T) {
	setupToolTest(t)

	stepUp := &recordingStepUp{}
	srv := startWindowsPolicyTemplateServer(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{stepUp: stepUp})

	// Bad format enum — preflight must reject without a gate prompt.
	res := callTool(t, cs, "windows_mdm_oma_uri_create_policy", map[string]any{
		"policy_name": "Bad Format",
		"settings": []map[string]any{
			{"uri": "./Device/Vendor/MSFT/X", "format": "dword", "value": "1"},
		},
		"execute": true,
	})
	if !res.IsError {
		t.Fatal("expected format-enum error")
	}
	if !strings.Contains(getResultText(t, res), "dword") {
		t.Errorf("error should name the bad format: %s", getResultText(t, res))
	}
	if gateCalls := stepUp.calls.Load(); gateCalls != 0 {
		t.Errorf("step-up gate fired %d time(s) for invalid input", gateCalls)
	}

	// Registry sibling: hive prefix — same guarantee.
	res = callTool(t, cs, "windows_mdm_registry_create_policy", map[string]any{
		"policy_name": "Hive Prefix",
		"keys": []map[string]any{
			{"location": `HKEY_LOCAL_MACHINE\SOFTWARE\X`, "name": "V", "type": "DWORD", "data": "1"},
		},
		"execute": true,
	})
	if !res.IsError {
		t.Fatal("expected hive-prefix error")
	}
	if gateCalls := stepUp.calls.Load(); gateCalls != 0 {
		t.Errorf("step-up gate fired %d time(s) for invalid registry input", gateCalls)
	}

	// Empty policy_name — the JSON-RPC layer doesn't catch a present-
	// but-blank field; preflight must.
	res = callTool(t, cs, "windows_mdm_oma_uri_create_policy", map[string]any{
		"policy_name": "",
		"settings": []map[string]any{
			{"uri": "./Device/Vendor/MSFT/X", "format": "int", "value": "1"},
		},
		"execute": true,
	})
	if !res.IsError || !strings.Contains(getResultText(t, res), "policy_name") {
		t.Errorf("expected policy_name preflight error, got %s", getResultText(t, res))
	}
	if gateCalls := stepUp.calls.Load(); gateCalls != 0 {
		t.Errorf("step-up gate fired %d time(s) for blank policy_name", gateCalls)
	}

	// Valid execute — gate fires exactly once.
	res = callTool(t, cs, "windows_mdm_oma_uri_create_policy", map[string]any{
		"policy_name": "Real Policy",
		"settings": []map[string]any{
			{"uri": "./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera", "format": "int", "value": "0"},
		},
		"execute": true,
	})
	if res.IsError {
		t.Fatalf("valid execute path errored: %s", getResultText(t, res))
	}
	if gateCalls := stepUp.calls.Load(); gateCalls != 1 {
		t.Errorf("expected exactly 1 step-up call on the valid path; got %d", gateCalls)
	}
}

// TestWindowsMDMTools_RefuseReadOnly mirrors the read-only refusal the
// Apple create_policy tool (and recipe_run / users_delete) enforce.
func TestWindowsMDMTools_RefuseReadOnly(t *testing.T) {
	setupToolTest(t)
	srv := startWindowsPolicyTemplateServer(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{ReadOnly: true})

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"windows_mdm_oma_uri_create_policy", map[string]any{
			"policy_name": "Refuse Me",
			"settings": []map[string]any{
				{"uri": "./Device/Vendor/MSFT/X", "format": "int", "value": "1"},
			},
			"execute": true,
		}},
		{"windows_mdm_registry_create_policy", map[string]any{
			"policy_name": "Refuse Me",
			"keys": []map[string]any{
				{"location": `SOFTWARE\Policies\X`, "name": "V", "type": "DWORD", "data": "1"},
			},
			"execute": true,
		}},
	} {
		res := callTool(t, cs, tc.tool, tc.args)
		if !res.IsError {
			t.Errorf("%s: expected read-only refusal", tc.tool)
			continue
		}
		if !strings.Contains(getResultText(t, res), "read-only") {
			t.Errorf("%s: error should mention read-only mode: %s", tc.tool, getResultText(t, res))
		}
	}
}

// TestWindowsMDMOMAURI_TemplateNotFound covers the tenant-without-
// Windows-MDM case: the error must be actionable, not a bare 404 or
// empty-list panic.
func TestWindowsMDMOMAURI_TemplateNotFound(t *testing.T) {
	setupToolTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "windows_mdm_oma_uri_create_policy", map[string]any{
		"policy_name": "No Template",
		"settings": []map[string]any{
			{"uri": "./Device/Vendor/MSFT/X", "format": "int", "value": "1"},
		},
	})
	if !res.IsError {
		t.Fatal("expected template-not-found error")
	}
	msg := getResultText(t, res)
	if !strings.Contains(msg, "Windows MDM") {
		t.Errorf("error should hint at Windows MDM enablement: %s", msg)
	}
}
