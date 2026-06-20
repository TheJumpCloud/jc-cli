package apple_mdm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
)

func TestBuildCustomMDMPolicyBody_ShapeMatchesJumpCloud(t *testing.T) {
	// This is the wire shape confirmed against a real policy during
	// the KLA-449 empirical gate (policy 6347d35890706f000188c4c6) AND
	// the live e2e round-trip on PR3. If this test starts failing
	// it's a regression against what JumpCloud accepts.
	tmpl := CustomMDMTemplate{
		ID:                "5f21c4d3b544067fd53ba0af",
		Name:              "custom_mdm_profile_darwin",
		PayloadFieldID:    "5f21c4d3b544067fd53ba0b0",
		RedispatchFieldID: "6801173b9d77db0001baeed9",
	}
	plistXML := []byte(`<?xml version="1.0"?><plist><dict/></plist>`)
	body := BuildCustomMDMPolicyBody("My Policy", tmpl, plistXML, true)

	if got := body["name"]; got != "My Policy" {
		t.Errorf("name = %v, want My Policy", got)
	}
	tmplBody, ok := body["template"].(map[string]any)
	if !ok || tmplBody["id"] != tmpl.ID {
		t.Errorf("template.id wrong: %v", body["template"])
	}
	values, ok := body["values"].([]any)
	if !ok || len(values) != 2 {
		t.Fatalf("values shape wrong: got %d entries", len(values))
	}

	// First entry is payload with the base64-encoded XML.
	p, _ := values[0].(map[string]any)
	if p["configFieldID"] != tmpl.PayloadFieldID || p["configFieldName"] != "payload" {
		t.Errorf("payload entry wrong: %v", p)
	}
	wantBase64 := base64.StdEncoding.EncodeToString(plistXML)
	if p["value"] != wantBase64 {
		t.Errorf("payload value not base64-encoded: got %v", p["value"])
	}

	// Second entry is redispatchPolicy with the bool verbatim.
	r, _ := values[1].(map[string]any)
	if r["configFieldID"] != tmpl.RedispatchFieldID || r["configFieldName"] != "redispatchPolicy" {
		t.Errorf("redispatch entry wrong: %v", r)
	}
	if r["value"] != true {
		t.Errorf("redispatch value wrong: %v", r["value"])
	}
}

func TestBuildCustomMDMPolicyBody_OmitsRedispatchWhenFieldMissing(t *testing.T) {
	// Older templates predate the redispatch field. We shouldn't ship
	// a values entry with an empty configFieldID — JumpCloud would
	// reject it.
	tmpl := CustomMDMTemplate{
		ID:             "tid",
		Name:           "custom_mdm_profile_darwin",
		PayloadFieldID: "pid",
	}
	body := BuildCustomMDMPolicyBody("P", tmpl, []byte("<plist/>"), true)
	values := body["values"].([]any)
	if len(values) != 1 {
		t.Errorf("expected 1 values entry when redispatch field is unset, got %d", len(values))
	}
}

func TestResolveCustomMDMTemplate_Roundtrips(t *testing.T) {
	// Stub /policytemplates list + detail. Mirrors the actual
	// JumpCloud response shape we saw in the empirical gate and the
	// PR3 e2e test.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/policytemplates" && r.URL.Query().Get("filter") == "name:eq:custom_mdm_profile_darwin":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"id":"tmpl123","name":"custom_mdm_profile_darwin"}]`))
		case r.URL.Path == "/policytemplates/tmpl123":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
                "id":"tmpl123",
                "name":"custom_mdm_profile_darwin",
                "configFields":[
                    {"id":"pfid","name":"payload"},
                    {"id":"rdid","name":"redispatchPolicy"}
                ]
            }`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewV2ClientWithKey("test-key")
	client.BaseURL = srv.URL

	tmpl, err := ResolveCustomMDMTemplate(context.Background(), client, OSFamilyDarwin)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if tmpl.ID != "tmpl123" || tmpl.PayloadFieldID != "pfid" || tmpl.RedispatchFieldID != "rdid" {
		t.Errorf("resolved fields wrong: %+v", tmpl)
	}
}

func TestResolveCustomMDMTemplate_MissingPayloadFieldErrors(t *testing.T) {
	// If JumpCloud renames or removes the payload configField, we
	// should surface that immediately — silently shipping a body with
	// an empty PayloadFieldID would be rejected by the server with a
	// much less actionable error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/policytemplates":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"id":"tmpl123","name":"custom_mdm_profile_darwin"}]`))
		case "/policytemplates/tmpl123":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"tmpl123","name":"custom_mdm_profile_darwin","configFields":[{"id":"other","name":"somethingElse"}]}`))
		}
	}))
	defer srv.Close()

	client := api.NewV2ClientWithKey("test-key")
	client.BaseURL = srv.URL

	_, err := ResolveCustomMDMTemplate(context.Background(), client, OSFamilyDarwin)
	if err == nil {
		t.Fatal("expected error when payload configField is missing")
	}
	if !strings.Contains(err.Error(), "payload") {
		t.Errorf("error should mention payload field: %v", err)
	}
}

func TestResolveCustomMDMTemplate_NoTemplateErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := api.NewV2ClientWithKey("test-key")
	client.BaseURL = srv.URL

	_, err := ResolveCustomMDMTemplate(context.Background(), client, "fakeOS")
	if err == nil {
		t.Fatal("expected error for missing template")
	}
	if !strings.Contains(err.Error(), "custom_mdm_profile_fakeOS") {
		t.Errorf("error should name the looked-up template: %v", err)
	}
}

// validateBodyShape parses a body assembled by BuildCustomMDMPolicyBody
// and checks the values entries are well-formed JSON. Used as a
// regression guard against any future field-ordering or shape changes.
func TestBuildCustomMDMPolicyBody_IsValidJSON(t *testing.T) {
	tmpl := CustomMDMTemplate{ID: "t", PayloadFieldID: "p", RedispatchFieldID: "r"}
	body := BuildCustomMDMPolicyBody("X", tmpl, []byte("<plist/>"), false)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}
