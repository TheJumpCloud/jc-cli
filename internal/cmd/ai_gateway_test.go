package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startAIGatewayServer serves the AI Gateway preview endpoints (v2) and the
// v1 application list used to resolve --app. Server "AWS" carries an OAuth
// client secret so tests can check it never reaches the output.
func startAIGatewayServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/ai-admin/servers" && r.URL.Query().Get("page_token") == "":
			w.Write([]byte(`{"servers":[{"object_id":"aaaaaaaaaaaaaaaaaaaaaaaa","name":"AWS","prefix":"aws","target_url":"https://aws.example/mcp","application_object_id":"111111111111111111111111","auth_config":{"oauth":{"client_id":"cid","client_secret":"S3CRET"}}}],"next_page_token":"p2"}`))
		case r.URL.Path == "/ai-admin/servers":
			w.Write([]byte(`{"servers":[{"object_id":"bbbbbbbbbbbbbbbbbbbbbbbb","name":"Wiki","prefix":"wk","target_url":"https://wiki.example/mcp","application_object_id":"222222222222222222222222","auth_config":{"api_token":{"token_value":"tok"}}}],"next_page_token":""}`))
		case r.URL.Path == "/ai-admin/settings/redirect-uris":
			w.Write([]byte(`{"redirect_uris":[{"object_id":"cccccccccccccccccccccccc","url":"https://claude.ai/api/mcp/auth_callback"}],"next_page_token":""}`))
		case r.URL.Path == "/ai-admin/catalog/resolve":
			w.Write([]byte(`{"associated":false,"reason":"no_catalog_connector"}`))
		case r.URL.Path == "/applications":
			w.Write([]byte(`{"results":[{"_id":"111111111111111111111111","name":"aws-sso","displayLabel":"AWS"},{"_id":"222222222222222222222222","name":"bookmark","displayLabel":"Wiki"}],"totalCount":2}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
}

func runJC(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestAIGatewayServersList_PagesRedactsAndFilters(t *testing.T) {
	setupUsersTest(t)
	ts := startAIGatewayServer(t)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	out, err := runJC(t, "ai-gateway", "servers", "list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var servers []map[string]any
	if err := json.Unmarshal([]byte(out), &servers); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if len(servers) != 2 {
		t.Fatalf("got %d servers, want 2 across two pages", len(servers))
	}
	if strings.Contains(out, "S3CRET") || strings.Contains(out, `"tok"`) {
		t.Errorf("credential leaked:\n%s", out)
	}
	if servers[0]["auth_type"] != "oauth" || servers[1]["auth_type"] != "api_token" {
		t.Errorf("auth_type = %v, %v", servers[0]["auth_type"], servers[1]["auth_type"])
	}

	out, err = runJC(t, "ai-gateway", "servers", "list", "--app", "Wiki", "--no-cache")
	if err != nil {
		t.Fatalf("--app: %v", err)
	}
	if !strings.Contains(out, "bbbbbbbbbbbbbbbbbbbbbbbb") || strings.Contains(out, "aaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Errorf("--app Wiki should keep only the Wiki server:\n%s", out)
	}
}

func TestAIGatewayServersGet_ByName(t *testing.T) {
	setupUsersTest(t)
	ts := startAIGatewayServer(t)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	out, err := runJC(t, "ai-gateway", "servers", "get", "AWS")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "aaaaaaaaaaaaaaaaaaaaaaaa") || strings.Contains(out, "S3CRET") {
		t.Errorf("unexpected output:\n%s", out)
	}
	if _, err := runJC(t, "ai-gateway", "servers", "get", "nope"); err == nil {
		t.Error("an unknown server must be an error")
	}
}

func TestAIGatewayRedirectURIsAndResolve(t *testing.T) {
	setupUsersTest(t)
	ts := startAIGatewayServer(t)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	out, err := runJC(t, "ai-gateway", "redirect-uris", "list")
	if err != nil || !strings.Contains(out, "auth_callback") {
		t.Errorf("redirect-uris: %v\n%s", err, out)
	}
	out, err = runJC(t, "ai-gateway", "catalog", "resolve", "AWS", "--no-cache")
	if err != nil || !strings.Contains(out, "no_catalog_connector") {
		t.Errorf("resolve: %v\n%s", err, out)
	}
	if _, err := runJC(t, "ai-gateway", "catalog", "connector", "../servers"); err == nil {
		t.Error("an invalid connector id must be rejected")
	}
}

// A shape change in this undocumented API must fail loudly, not print [].
func TestAIGatewayServersList_UnknownShapeIsAnError(t *testing.T) {
	setupUsersTest(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[]}`))
	}))
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	_, err := runJC(t, "ai-gateway", "servers", "list")
	if err == nil || !strings.Contains(err.Error(), "preview") {
		t.Errorf("want an unexpected-response error naming the preview API, got %v", err)
	}
}
