package windows_mdm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
)

func TestBuildOMAURIPolicyBody_ShapeMatchesJumpCloud(t *testing.T) {
	// Wire shape confirmed against the tenant's Custom MDM (OMA-URI)
	// template during the KLA-459 empirical gate (2026-07-08). The
	// values[] entry's value is a JSON ARRAY of triples — not a scalar
	// like the Apple side's base64 blob. If this test starts failing
	// it's a regression against what JumpCloud accepts.
	tmpl := CustomTemplate{
		ID:        "6763cf2e911237000168a2f8",
		Name:      TemplateNameOMAURI,
		FieldID:   "6763cf2e911237000168a2f9",
		FieldName: "uriList",
	}
	settings := []OMAURISetting{
		{URI: "./Device/Vendor/MSFT/Policy/Config/BitLocker/RequireDeviceEncryption", Format: "int", Value: "1"},
		{URI: "./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera", Format: "int", Value: "0"},
	}
	body := BuildOMAURIPolicyBody("Require BitLocker", tmpl, settings)

	if got := body["name"]; got != "Require BitLocker" {
		t.Errorf("name = %v", got)
	}
	tmplBody, ok := body["template"].(map[string]any)
	if !ok || tmplBody["id"] != tmpl.ID {
		t.Errorf("template.id wrong: %v", body["template"])
	}
	values, ok := body["values"].([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("values shape wrong: got %d entries", len(values))
	}
	entry, _ := values[0].(map[string]any)
	if entry["configFieldID"] != tmpl.FieldID || entry["configFieldName"] != "uriList" {
		t.Errorf("uriList entry wrong: %v", entry)
	}
	list, ok := entry["value"].([]OMAURISetting)
	if !ok || len(list) != 2 {
		t.Fatalf("value should be the settings slice, got %T", entry["value"])
	}

	// Round-trip through JSON to assert the wire sub-field names.
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"uri":`, `"format":"int"`, `"value":"1"`, `"configFieldName":"uriList"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("marshaled body missing %s:\n%s", want, b)
		}
	}
}

func TestBuildRegistryPolicyBody_ShapeMatchesJumpCloud(t *testing.T) {
	tmpl := CustomTemplate{
		ID:        "5f07273cb544065386e1ce6f",
		Name:      TemplateNameRegistry,
		FieldID:   "5f07273cb544065386e1ce70",
		FieldName: "customRegTable",
	}
	keys := []RegistryKey{{
		Location:  `SOFTWARE\Policies\Microsoft\Windows\Explorer`,
		ValueName: "NoAutorun",
		RegType:   "DWORD",
		Data:      "1",
	}}
	body := BuildRegistryPolicyBody("Disable Autorun", tmpl, keys)

	values := body["values"].([]any)
	entry := values[0].(map[string]any)
	if entry["configFieldName"] != "customRegTable" {
		t.Errorf("configFieldName = %v", entry["configFieldName"])
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The wire column names are JumpCloud's, not ours — a rename here
	// would ship a policy the Admin Portal can't render.
	for _, want := range []string{`"customLocation":`, `"customValueName":"NoAutorun"`, `"customRegType":"DWORD"`, `"customData":"1"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("marshaled body missing %s:\n%s", want, b)
		}
	}
}

func TestResolveTemplates_Roundtrip(t *testing.T) {
	// Stub /policytemplates list + detail for both Windows templates,
	// mirroring the response shapes from the KLA-459 empirical gate.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/policytemplates" && r.URL.Query().Get("filter") == "name:eq:"+TemplateNameOMAURI:
			w.Write([]byte(`[{"id":"omatmpl","name":"custom_oma_uri_mdm_windows"}]`))
		case r.URL.Path == "/policytemplates/omatmpl":
			w.Write([]byte(`{"id":"omatmpl","name":"custom_oma_uri_mdm_windows","configFields":[{"id":"urifid","name":"uriList"}]}`))
		case r.URL.Path == "/policytemplates" && r.URL.Query().Get("filter") == "name:eq:"+TemplateNameRegistry:
			w.Write([]byte(`[{"id":"regtmpl","name":"custom_registry_keys_policy_windows"}]`))
		case r.URL.Path == "/policytemplates/regtmpl":
			w.Write([]byte(`{"id":"regtmpl","name":"custom_registry_keys_policy_windows","configFields":[{"id":"regfid","name":"customRegTable"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewV2ClientWithKey("test-key")
	client.BaseURL = srv.URL

	oma, err := ResolveOMAURITemplate(context.Background(), client)
	if err != nil {
		t.Fatalf("resolve oma-uri: %v", err)
	}
	if oma.ID != "omatmpl" || oma.FieldID != "urifid" || oma.FieldName != "uriList" {
		t.Errorf("oma-uri template wrong: %+v", oma)
	}

	reg, err := ResolveRegistryTemplate(context.Background(), client)
	if err != nil {
		t.Fatalf("resolve registry: %v", err)
	}
	if reg.ID != "regtmpl" || reg.FieldID != "regfid" || reg.FieldName != "customRegTable" {
		t.Errorf("registry template wrong: %+v", reg)
	}
}

func TestResolveTemplate_NotFoundMentionsWindowsMDM(t *testing.T) {
	// A tenant without Windows MDM enabled won't have the template.
	// The error must be actionable, not a bare "no results".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := api.NewV2ClientWithKey("test-key")
	client.BaseURL = srv.URL

	_, err := ResolveOMAURITemplate(context.Background(), client)
	if err == nil {
		t.Fatal("expected error for missing template")
	}
	if !strings.Contains(err.Error(), TemplateNameOMAURI) || !strings.Contains(err.Error(), "Windows MDM") {
		t.Errorf("error should name the template and hint at Windows MDM enablement: %v", err)
	}
}

func TestResolveTemplate_MissingFieldErrors(t *testing.T) {
	// If JumpCloud renames the configField we must fail at resolve
	// time — a body with an empty configFieldID would be rejected
	// server-side with a far less actionable error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/policytemplates":
			w.Write([]byte(`[{"id":"omatmpl","name":"custom_oma_uri_mdm_windows"}]`))
		case "/policytemplates/omatmpl":
			w.Write([]byte(`{"id":"omatmpl","name":"custom_oma_uri_mdm_windows","configFields":[{"id":"x","name":"somethingElse"}]}`))
		}
	}))
	defer srv.Close()

	client := api.NewV2ClientWithKey("test-key")
	client.BaseURL = srv.URL

	_, err := ResolveOMAURITemplate(context.Background(), client)
	if err == nil {
		t.Fatal("expected error when uriList configField is missing")
	}
	if !strings.Contains(err.Error(), "uriList") {
		t.Errorf("error should mention the missing field: %v", err)
	}
}

// TestNoHardcodedConfigFieldIDs is the regression guard the KLA-459
// ticket calls for: the resolver must extract configField IDs from the
// template detail response, never from source-level constants. The
// stub below serves field IDs that differ from the real tenant's — if
// resolution ever consulted a hardcoded ID, the assertions would catch
// the mismatch.
func TestNoHardcodedConfigFieldIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/policytemplates":
			w.Write([]byte(`[{"id":"freshtmpl","name":"custom_oma_uri_mdm_windows"}]`))
		case "/policytemplates/freshtmpl":
			// Deliberately NOT the real tenant's 6763cf2e... IDs.
			w.Write([]byte(`{"id":"freshtmpl","name":"custom_oma_uri_mdm_windows","configFields":[{"id":"rotated-field-id","name":"uriList"}]}`))
		}
	}))
	defer srv.Close()

	client := api.NewV2ClientWithKey("test-key")
	client.BaseURL = srv.URL

	tmpl, err := ResolveOMAURITemplate(context.Background(), client)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if tmpl.FieldID != "rotated-field-id" {
		t.Errorf("FieldID should come from the API response, got %q", tmpl.FieldID)
	}
	body := BuildOMAURIPolicyBody("P", tmpl, []OMAURISetting{{URI: "./Vendor/x", Format: "int", Value: "1"}})
	b, _ := json.Marshal(body)
	if !strings.Contains(string(b), "rotated-field-id") {
		t.Errorf("body should carry the resolved field ID: %s", b)
	}
	if strings.Contains(string(b), "6763cf2e") {
		t.Errorf("body must not contain a hardcoded tenant field ID: %s", b)
	}
}
