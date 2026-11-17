package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOrgFeatureSettingsList(t *testing.T) {
	setupToolTest(t)
	var calls int32
	var lastFilters []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/feature-settings" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		lastFilters = r.URL.Query()["filter"]
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"features":[{"name":"mcp","category":"ai","settings":[{"name":"enabled","value":true}]}]}`))
	}))
	defer ts.Close()
	overrideV2ClientForTest(t, ts.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "org_feature_settings_list", map[string]any{"name": "mcp", "category": "ai"})
	if res.IsError {
		t.Fatalf("error: %s", getResultText(t, res))
	}
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(getResultText(t, res)), &env); err != nil || len(env.Data) != 1 || env.Data[0]["name"] != "mcp" {
		t.Errorf("result %s (%v)", getResultText(t, res), err)
	}
	if strings.Join(lastFilters, ",") != "name:eq:mcp,category:eq:ai" {
		t.Errorf("filters %v", lastFilters)
	}

	before := atomic.LoadInt32(&calls)
	res = callTool(t, cs, "org_feature_settings_list", map[string]any{"name": "mcp&filter=x"})
	if !res.IsError || !strings.Contains(getResultText(t, res), "lower-case") {
		t.Errorf("invalid name: %s", getResultText(t, res))
	}
	if atomic.LoadInt32(&calls) != before {
		t.Error("invalid name reached the server")
	}
}

func TestAppsUpdate_AIReadyIgnoredIsAnError(t *testing.T) {
	setupToolTest(t)
	const id = "aabbccddee112233aabb2001"
	var body map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		const app = `{"_id":"` + id + `","name":"bookmark","ssoUrl":"https://example.invalid","aiReady":false}`
		if r.Method == http.MethodGet && r.URL.Path == "/api/applications/"+id {
			w.Write([]byte(app))
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/api/applications/"+id {
			_ = json.NewDecoder(r.Body).Decode(&body)
			// Live behaviour: 200, aiReady stays false without an MCP connection.
			w.Write([]byte(app))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	overrideV1ClientForTest(t, ts.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apps_update", map[string]any{"identifier": id, "aiReady": true, "execute": true})
	if !res.IsError || !strings.Contains(getResultText(t, res), "MCP connection") {
		t.Errorf("want an error naming the MCP connection, got %s", getResultText(t, res))
	}
	if body["aiReady"] != true || body["name"] != "bookmark" || body["ssoUrl"] != "https://example.invalid" {
		t.Errorf("PUT body %v: want the full app with aiReady overlaid", body)
	}

	res = callTool(t, cs, "apps_update", map[string]any{"identifier": id, "aiReady": false, "execute": true})
	if res.IsError {
		t.Errorf("false applied should succeed: %s", getResultText(t, res))
	}
}

// apps_create: name is the catalog connector key, the title goes in
// displayLabel, and sso_type is not sent (JumpCloud ignores it).
func TestAppsCreate_ConnectorKeyAndLabel(t *testing.T) {
	setupToolTest(t)
	var body map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/api/applications" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "bookmark" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status":400,"error":"Could not find the service provider."}`))
			return
		}
		w.Write([]byte(`{"_id":"aabbccddee112233aabb9001","name":"bookmark","sso":{"type":"bookmark"}}`))
	}))
	defer ts.Close()
	overrideV1ClientForTest(t, ts.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apps_create", map[string]any{"name": "bookmark", "display_label": "Team Wiki",
		"sso_url": "https://wiki.example.com", "sso_type": "saml", "execute": true})
	if res.IsError {
		t.Fatalf("create failed: %s", getResultText(t, res))
	}
	if body["displayLabel"] != "Team Wiki" || body["ssoUrl"] != "https://wiki.example.com" {
		t.Errorf("POST body %v", body)
	}
	if _, sent := body["ssoType"]; sent {
		t.Errorf("ssoType must not be sent: %v", body)
	}

	res = callTool(t, cs, "apps_create", map[string]any{"name": "Team Wiki", "execute": true})
	if !res.IsError || !strings.Contains(getResultText(t, res), "catalog connector key") {
		t.Errorf("want the unknown-connector hint, got %s", getResultText(t, res))
	}
}

func TestAppsUpdate_NameRefusedAndConfigMerged(t *testing.T) {
	setupToolTest(t)
	const id = "aabbccddee112233aabb2001"
	var body map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		const app = `{"_id":"` + id + `","name":"aws-sso","displayLabel":"AWS","ssoUrl":"https://u","config":{"acsUrl":{"value":"https://a"},"idpEntityId":{"value":"jc"}}}`
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/applications/"+id:
			w.Write([]byte(app))
		case r.Method == http.MethodPut && r.URL.Path == "/api/applications/"+id:
			_ = json.NewDecoder(r.Body).Decode(&body)
			out, _ := json.Marshal(body)
			w.Write(out)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()
	overrideV1ClientForTest(t, ts.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apps_update", map[string]any{"identifier": id, "name": "slack", "execute": true})
	if !res.IsError || !strings.Contains(getResultText(t, res), "cannot be changed") {
		t.Errorf("want a refusal for name, got %s", getResultText(t, res))
	}
	if body != nil {
		t.Errorf("no PUT expected for a refused rename, got %v", body)
	}

	res = callTool(t, cs, "apps_update", map[string]any{"identifier": id, "display_label": "Amazon",
		"config": `{"acsUrl":{"value":"https://b"}}`, "execute": true})
	if res.IsError {
		t.Fatalf("update failed: %s", getResultText(t, res))
	}
	cfg := body["config"].(map[string]any)
	if cfg["acsUrl"].(map[string]any)["value"] != "https://b" || cfg["idpEntityId"] == nil {
		t.Errorf("config must be merged key by key, got %v", cfg)
	}
	if body["displayLabel"] != "Amazon" || body["name"] != "aws-sso" {
		t.Errorf("PUT body %v", body)
	}
}
