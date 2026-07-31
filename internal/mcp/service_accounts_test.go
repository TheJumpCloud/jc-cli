package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startServiceAccountsV2Server serves the /api/v2/service-accounts family for
// MCP tool tests, mirroring the live contract (probed 2026-07-24): list wrapped
// in {results}, single GET + POST wrapped in {serviceAccount}, auth-config POST
// wrapped in {authConfig}, roles list wrapped in {results}.
func startServiceAccountsV2Server(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	account := map[string]any{
		"objectId": "aaa111aaa111aaa111aaa111", "name": "ci-bot", "roleName": "Administrator",
		"status": "ACTIVE", "authConfigList": []any{},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
		switch {
		case p == "/service-accounts" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{account}, "totalCount": 1})
		case p == "/roles" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": "role-1", "name": "Administrator"}}})
		case p == "/service-accounts" && r.Method == http.MethodPost:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"serviceAccount": map[string]any{
				"objectId": "new111new111new111new111", "name": "ci-bot",
				"authConfigList": []any{map[string]any{"apiKeyConfig": map[string]any{"apiKey": "jcapi_SECRET123"}}},
			}})
		case strings.HasPrefix(p, "/service-accounts/") && strings.HasSuffix(p, "/auth-config") && r.Method == http.MethodPost:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"authConfig": map[string]any{
				"clientSecretConfig": map[string]any{"clientSecret": "cs_SECRET456"},
			}})
		case strings.HasPrefix(p, "/service-accounts/") && strings.Contains(p, "/auth-config/") && r.Method == http.MethodDelete:
			if capture != nil {
				(*capture)["deletedPath"] = p
			}
			w.Write([]byte(`{}`))
		case strings.HasPrefix(p, "/service-accounts/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"serviceAccount": account})
		case strings.HasPrefix(p, "/service-accounts/") && r.Method == http.MethodDelete:
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

func TestMCPServiceAccountsList(t *testing.T) {
	srv := startServiceAccountsV2Server(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "service_accounts_list", map[string]any{}))
	var wrapper struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &wrapper); err != nil || len(wrapper.Data) != 1 {
		t.Fatalf("ResponseKey unwrap failed: %v\n%s", err, out)
	}
}

// TestMCPServiceAccountsGet_Unwraps guards the {serviceAccount} envelope unwrap.
func TestMCPServiceAccountsGet_Unwraps(t *testing.T) {
	srv := startServiceAccountsV2Server(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "service_accounts_get", map[string]any{"identifier": "ci-bot"}))
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if obj["name"] != "ci-bot" {
		t.Errorf("get did not unwrap serviceAccount: %v", obj)
	}
	if _, wrapped := obj["serviceAccount"]; wrapped {
		t.Error("response still wrapped in serviceAccount envelope")
	}
}

func TestMCPServiceAccountsCreate_ExecuteBody(t *testing.T) {
	var body map[string]any
	srv := startServiceAccountsV2Server(t, &body)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "service_accounts_create", map[string]any{
		"name": "ci-bot", "role": "Administrator", "auth_type": "api_key", "lifetime": "90 Days", "execute": true,
	}))
	if body["name"] != "ci-bot" || body["roleId"] != "role-1" {
		t.Errorf("create body name/roleId wrong: %v", body)
	}
	ac, _ := body["authConfig"].(map[string]any)
	if ac["authType"] != "API_KEY" {
		t.Errorf("authType = %v, want API_KEY", ac["authType"])
	}
	// Minted secret is surfaced (unwrapped).
	if !strings.Contains(out, "jcapi_SECRET123") {
		t.Errorf("minted apiKey not surfaced: %s", out)
	}
}

func TestMCPServiceAccountsCreate_PlanNoPost(t *testing.T) {
	var body map[string]any
	srv := startServiceAccountsV2Server(t, &body)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "service_accounts_create", map[string]any{
		"name": "x", "role": "Administrator", "auth_type": "api_key",
	}))
	if body != nil {
		t.Errorf("plan must not POST, captured: %v", body)
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan result, got: %s", out)
	}
}

func TestMCPServiceAccountsCreate_ValidatesAuthType(t *testing.T) {
	srv := startServiceAccountsV2Server(t, nil)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "service_accounts_create", map[string]any{
		"name": "x", "role": "Administrator", "auth_type": "totp", "execute": true,
	}))
	if !strings.Contains(out, "invalid --auth-type") {
		t.Errorf("expected auth-type validation error, got: %s", out)
	}
}

func TestMCPServiceAccountsRotate_UnwrapsAuthConfig(t *testing.T) {
	var body map[string]any
	srv := startServiceAccountsV2Server(t, &body)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "service_accounts_rotate", map[string]any{
		"identifier": "ci-bot", "auth_type": "client_secret", "lifetime": "30 Days", "execute": true,
	}))
	if body["authType"] != "CLIENT_SECRET" {
		t.Errorf("rotate authType = %v", body["authType"])
	}
	if !strings.Contains(out, "cs_SECRET456") {
		t.Errorf("rotated secret not surfaced (unwrap authConfig): %s", out)
	}
}

func TestMCPServiceAccountsRevoke_PathAndPlan(t *testing.T) {
	var cap = map[string]any{}
	srv := startServiceAccountsV2Server(t, &cap)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	// Plan: no DELETE.
	out := getResultText(t, callTool(t, cs, "service_accounts_revoke", map[string]any{
		"identifier": "ci-bot", "auth_config_id": "authcfg-9",
	}))
	if _, deleted := cap["deletedPath"]; deleted {
		t.Error("plan must not DELETE")
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}

	// Execute: correct auth-config path.
	callTool(t, cs, "service_accounts_revoke", map[string]any{
		"identifier": "ci-bot", "auth_config_id": "authcfg-9", "execute": true,
	})
	if cap["deletedPath"] != "/service-accounts/aaa111aaa111aaa111aaa111/auth-config/authcfg-9" {
		t.Errorf("revoke hit wrong path: %v", cap["deletedPath"])
	}
}

func TestMCPServiceAccountsDelete_PathAndPlan(t *testing.T) {
	var cap = map[string]any{}
	srv := startServiceAccountsV2Server(t, &cap)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "service_accounts_delete", map[string]any{"identifier": "ci-bot"}))
	if _, deleted := cap["deletedPath"]; deleted {
		t.Error("plan must not DELETE")
	}
	if !strings.Contains(out, "\"plan\": true") {
		t.Errorf("expected plan, got: %s", out)
	}

	callTool(t, cs, "service_accounts_delete", map[string]any{"identifier": "ci-bot", "execute": true})
	if cap["deletedPath"] != "/service-accounts/aaa111aaa111aaa111aaa111" {
		t.Errorf("delete hit wrong path: %v", cap["deletedPath"])
	}
}
