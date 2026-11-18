package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func startAIGatewayTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/ai-admin/servers":
			w.Write([]byte(`{"servers":[{"object_id":"aaaaaaaaaaaaaaaaaaaaaaaa","name":"AWS","application_object_id":"111111111111111111111111","auth_config":{"oauth":{"client_id":"cid","client_secret":"S3CRET"}}},{"object_id":"bbbbbbbbbbbbbbbbbbbbbbbb","name":"Wiki","application_object_id":"222222222222222222222222","auth_config":{"api_token":{"token_value":"tok-123"}}}],"next_page_token":""}`))
		case "/api/v2/ai-admin/settings/redirect-uris":
			w.Write([]byte(`{"redirect_uris":[{"object_id":"cccccccccccccccccccccccc","url":"https://claude.ai/api/mcp/auth_callback"}],"next_page_token":""}`))
		case "/api/v2/ai-admin/catalog/resolve":
			w.Write([]byte(`{"associated":true,"connector_id":"salesforce"}`))
		case "/api/v2/ai-admin/catalog/connectors/salesforce":
			w.Write([]byte(`{"connector_id":"salesforce","display_name":"Salesforce MCP"}`))
		case "/api/applications":
			w.Write([]byte(`{"results":[{"_id":"222222222222222222222222","name":"bookmark","displayLabel":"Wiki"}],"totalCount":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestAIGatewayTools(t *testing.T) {
	setupToolTest(t)
	ts := startAIGatewayTestServer(t)
	defer ts.Close()
	overrideV1ClientForTest(t, ts.URL)
	overrideV2ClientForTest(t, ts.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "ai_gateway_servers_list", map[string]any{})
	text := getResultText(t, res)
	if res.IsError || !strings.Contains(text, "aaaaaaaaaaaaaaaaaaaaaaaa") || !strings.Contains(text, "bbbbbbbbbbbbbbbbbbbbbbbb") {
		t.Fatalf("list: %s", text)
	}
	if strings.Contains(text, "S3CRET") || strings.Contains(text, "tok-123") {
		t.Errorf("credential leaked to the agent: %s", text)
	}

	res = callTool(t, cs, "ai_gateway_servers_list", map[string]any{"app": "Wiki"})
	text = getResultText(t, res)
	if res.IsError || strings.Contains(text, "aaaaaaaaaaaaaaaaaaaaaaaa") || !strings.Contains(text, "bbbbbbbbbbbbbbbbbbbbbbbb") {
		t.Errorf("app filter: %s", text)
	}

	res = callTool(t, cs, "ai_gateway_server_get", map[string]any{"server": "AWS"})
	if text = getResultText(t, res); res.IsError || strings.Contains(text, "S3CRET") || !strings.Contains(text, "cid") {
		t.Errorf("get: %s", text)
	}

	res = callTool(t, cs, "ai_gateway_redirect_uris_list", map[string]any{})
	if text = getResultText(t, res); res.IsError || !strings.Contains(text, "auth_callback") {
		t.Errorf("redirect uris: %s", text)
	}

	res = callTool(t, cs, "ai_gateway_catalog_resolve", map[string]any{"app": "222222222222222222222222"})
	if text = getResultText(t, res); res.IsError || !strings.Contains(text, "salesforce") {
		t.Errorf("resolve: %s", text)
	}

	res = callTool(t, cs, "ai_gateway_catalog_connector_get", map[string]any{"connector_id": "salesforce"})
	if text = getResultText(t, res); res.IsError || !strings.Contains(text, "Salesforce MCP") {
		t.Errorf("connector: %s", text)
	}
}
