package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startNotificationChannelsV2Server serves the /api/v2/notifications/channels
// family for MCP tool tests, mirroring the live contract (probed 2026-07-31):
// list wrapped in {channels}, single GET/POST/PATCH wrapped in {channel},
// DELETE → {}.
func startNotificationChannelsV2Server(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	channel := map[string]any{
		"objectId": "aaa111aaa111aaa111aaa111", "organizationObjectId": "org1",
		"name": "Ops Webhook", "description": "ops alerts", "type": "CHANNEL_TYPE_WEBHOOK",
		"enabled":   true,
		"config":    map[string]any{"webhook": map[string]any{"objectId": "wh1", "url": "https://example.com/hook"}},
		"createdAt": "2026-01-01T00:00:00Z", "createdBy": "admin1", "updatedAt": "2026-01-02T00:00:00Z", "updatedBy": "admin1",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
		switch {
		case p == "/notifications/channels" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"channels": []map[string]any{channel}, "count": 1})
		case p == "/notifications/channels" && r.Method == http.MethodPost:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"channel": channel})
		case strings.HasPrefix(p, "/notifications/channels/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"channel": channel})
		case strings.HasPrefix(p, "/notifications/channels/") && r.Method == http.MethodPatch:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"channel": channel})
		case strings.HasPrefix(p, "/notifications/channels/") && r.Method == http.MethodDelete:
			if capture != nil {
				(*capture)["deletedPath"] = p
			}
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected: %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMCPChannelsList(t *testing.T) {
	srv := startNotificationChannelsV2Server(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "notification_channels_list", map[string]any{}))
	var wrapper struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &wrapper); err != nil || len(wrapper.Data) != 1 {
		t.Fatalf("ResponseKey unwrap failed: %v\n%s", err, out)
	}
}

func TestMCPChannelsGet_Unwraps(t *testing.T) {
	srv := startNotificationChannelsV2Server(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "notification_channels_get", map[string]any{"identifier": "Ops Webhook"}))
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if obj["name"] != "Ops Webhook" {
		t.Errorf("get did not unwrap channel: %v", obj)
	}
	if _, wrapped := obj["channel"]; wrapped {
		t.Error("response still wrapped")
	}
}

func TestMCPChannelsCreate_WebhookExecute(t *testing.T) {
	var body map[string]any
	srv := startNotificationChannelsV2Server(t, &body)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	callTool(t, cs, "notification_channels_create", map[string]any{
		"name": "Ops Webhook", "type": "webhook", "url": "https://example.com/hook", "enabled": true, "execute": true,
	})
	ch, ok := body["channel"].(map[string]any)
	if !ok {
		t.Fatalf("POST body not wrapped in channel: %v", body)
	}
	if ch["type"] != "CHANNEL_TYPE_WEBHOOK" {
		t.Errorf("type not normalized: %v", ch["type"])
	}
	cfg, _ := ch["config"].(map[string]any)
	wh, _ := cfg["webhook"].(map[string]any)
	if wh["url"] != "https://example.com/hook" {
		t.Errorf("url not set: %v", ch["config"])
	}
}

func TestMCPChannelsCreate_PlanNoPost(t *testing.T) {
	var body map[string]any
	srv := startNotificationChannelsV2Server(t, &body)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "notification_channels_create", map[string]any{
		"name": "X", "type": "webhook", "url": "https://x.example",
	}))
	if body != nil {
		t.Errorf("plan must not POST, captured: %v", body)
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}
}

func TestMCPChannelsCreate_EmailNeedsConfigJSON(t *testing.T) {
	srv := startNotificationChannelsV2Server(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "notification_channels_create", map[string]any{
		"name": "X", "type": "email", "execute": true,
	}))
	if !strings.Contains(out, "config_json") {
		t.Errorf("expected config_json requirement for email, got: %s", out)
	}
}

// TestMCPChannelsUpdate_RMW: partial update (enabled only) preserves
// name/type/config, PATCH wraps {channel}, strips server-managed keys.
func TestMCPChannelsUpdate_RMW(t *testing.T) {
	var patch map[string]any
	srv := startNotificationChannelsV2Server(t, &patch)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	callTool(t, cs, "notification_channels_update", map[string]any{
		"identifier": "aaa111aaa111aaa111aaa111", "enabled": false, "execute": true,
	})
	ch, ok := patch["channel"].(map[string]any)
	if !ok {
		t.Fatalf("PATCH body not wrapped: %v", patch)
	}
	if ch["enabled"] != false || ch["name"] != "Ops Webhook" {
		t.Errorf("RMW wrong: %v", ch)
	}
	if _, present := ch["createdAt"]; present {
		t.Error("server-managed createdAt must be stripped")
	}
}

func TestMCPChannelsDelete_PathAndPlan(t *testing.T) {
	var cap = map[string]any{}
	srv := startNotificationChannelsV2Server(t, &cap)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "notification_channels_delete", map[string]any{"identifier": "Ops Webhook"}))
	if _, deleted := cap["deletedPath"]; deleted {
		t.Error("plan must not DELETE")
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}

	callTool(t, cs, "notification_channels_delete", map[string]any{"identifier": "Ops Webhook", "execute": true})
	if cap["deletedPath"] != "/notifications/channels/aaa111aaa111aaa111aaa111" {
		t.Errorf("delete hit wrong path: %v", cap["deletedPath"])
	}
}
