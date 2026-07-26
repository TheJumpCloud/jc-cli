package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serviceAccountsFixture mirrors the live /service-accounts shape (probed
// 2026-07-24): wrapped in {results,totalCount}, id under objectId, single
// GET wrapped in {serviceAccount}, roles wrapped in {results}.
func startServiceAccountsServer(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	accounts := []map[string]any{
		{"objectId": "aaa111aaa111aaa111aaa111", "name": "ci-bot", "roleId": "role-1", "roleName": "Administrator", "status": "ACTIVE", "authConfigList": []any{}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/service-accounts" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"results": accounts, "totalCount": len(accounts)})
		case p == "/roles" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": "role-1", "name": "Administrator"}}})
		case p == "/service-accounts" && r.Method == http.MethodPost:
			if capture != nil {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"serviceAccount": map[string]any{
				"objectId": "new111new111new111new111", "name": "ci-bot", "authConfigList": []any{
					map[string]any{"authType": "API_KEY", "apiKeyConfig": map[string]any{"apiKey": "jcapi_SECRET123"}},
				},
			}})
		case strings.HasPrefix(p, "/service-accounts/") && strings.HasSuffix(p, "/auth-config") && r.Method == http.MethodPost:
			if capture != nil {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"authConfig": map[string]any{
				"authType": "CLIENT_SECRET", "clientSecretConfig": map[string]any{"clientSecret": "cs_SECRET456"},
			}})
		case strings.HasPrefix(p, "/service-accounts/") && strings.Contains(p, "/auth-config/") && r.Method == http.MethodDelete:
			if capture != nil {
				(*capture)["deletedPath"] = p
			}
			w.Write([]byte(`{}`))
		case strings.HasPrefix(p, "/service-accounts/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"serviceAccount": accounts[0]})
		case strings.HasPrefix(p, "/service-accounts/") && r.Method == http.MethodDelete:
			if capture != nil {
				(*capture)["deletedPath"] = p
			}
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runSA(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"service-accounts"}, args...))
	err := root.Execute()
	return out.String(), errBuf.String(), err
}

func TestServiceAccountsList(t *testing.T) {
	setupUsersTest(t)
	srv := startServiceAccountsServer(t, nil)
	overrideV2Client(t, srv.URL)
	out, errBuf, err := runSA(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []map[string]any
	if e := json.Unmarshal([]byte(out), &rows); e != nil {
		t.Fatalf("output not JSON array (ResponseKey unwrap failed?): %v\n%s", e, out)
	}
	if len(rows) != 1 || rows[0]["name"] != "ci-bot" {
		t.Errorf("rows = %v", rows)
	}
	if !strings.Contains(errBuf, "1 items") {
		t.Errorf("footer missing: %s", errBuf)
	}
}

func TestServiceAccountsGetByName_Unwraps(t *testing.T) {
	setupUsersTest(t)
	srv := startServiceAccountsServer(t, nil)
	overrideV2Client(t, srv.URL)
	// "ci-bot" is a name → resolver lists /service-accounts, matches objectId.
	out, _, err := runSA(t, "get", "ci-bot")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var obj map[string]any
	if e := json.Unmarshal([]byte(out), &obj); e != nil {
		t.Fatalf("not JSON: %v", e)
	}
	// Must be the unwrapped serviceAccount, not the {serviceAccount:…} envelope.
	if obj["name"] != "ci-bot" {
		t.Errorf("get did not unwrap serviceAccount: %v", obj)
	}
	if _, wrapped := obj["serviceAccount"]; wrapped {
		t.Error("response still wrapped in serviceAccount envelope")
	}
}

func TestServiceAccountsCreate_BodyAndSecret(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	srv := startServiceAccountsServer(t, &body)
	overrideV2Client(t, srv.URL)
	out, _, err := runSA(t, "create", "--name", "ci-bot", "--role", "Administrator",
		"--auth-type", "api_key", "--lifetime", "90 Days")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Body: name, roleId (resolved from role name), authConfig{authType, apiKeyConfig{lifetime}}.
	if body["name"] != "ci-bot" || body["roleId"] != "role-1" {
		t.Errorf("create body name/roleId wrong: %v", body)
	}
	ac, _ := body["authConfig"].(map[string]any)
	if ac["authType"] != "API_KEY" {
		t.Errorf("authType = %v, want API_KEY", ac["authType"])
	}
	akc, _ := ac["apiKeyConfig"].(map[string]any)
	if akc["lifetime"] != "90 Days" {
		t.Errorf("lifetime = %v, want 90 Days", akc["lifetime"])
	}
	if _, hasClient := ac["clientSecretConfig"]; hasClient {
		t.Error("api_key request must not carry clientSecretConfig")
	}
	// Output is the unwrapped serviceAccount carrying the minted secret.
	if !strings.Contains(out, "jcapi_SECRET123") {
		t.Errorf("minted apiKey not surfaced in output: %s", out)
	}
}

func TestServiceAccountsCreate_Validation(t *testing.T) {
	setupUsersTest(t)
	srv := startServiceAccountsServer(t, nil)
	overrideV2Client(t, srv.URL)
	if _, _, err := runSA(t, "create", "--name", "x", "--role", "Administrator", "--auth-type", "totp"); err == nil || !strings.Contains(err.Error(), "invalid --auth-type") {
		t.Fatalf("expected auth-type error, got %v", err)
	}
	if _, _, err := runSA(t, "create", "--name", "x", "--role", "Administrator", "--auth-type", "api_key", "--lifetime", "45 Days"); err == nil || !strings.Contains(err.Error(), "invalid --lifetime") {
		t.Fatalf("expected lifetime error, got %v", err)
	}
}

func TestServiceAccountsRotate_ClientSecret(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	srv := startServiceAccountsServer(t, &body)
	overrideV2Client(t, srv.URL)
	out, _, err := runSA(t, "rotate", "ci-bot", "--auth-type", "client_secret", "--lifetime", "30 Days")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	ac := body
	if ac["authType"] != "CLIENT_SECRET" {
		t.Errorf("rotate authType = %v", ac["authType"])
	}
	csc, _ := ac["clientSecretConfig"].(map[string]any)
	if csc["lifetime"] != "30 Days" {
		t.Errorf("rotate lifetime = %v", csc["lifetime"])
	}
	if !strings.Contains(out, "cs_SECRET456") {
		t.Errorf("rotated secret not surfaced: %s", out)
	}
}

// TestServiceAccountsList_IDs guards the --ids fix: service accounts key on
// objectId, so `list --ids` must emit those (else the list|delete pipeline
// silently produces nothing).
func TestServiceAccountsList_IDs(t *testing.T) {
	setupUsersTest(t)
	srv := startServiceAccountsServer(t, nil)
	overrideV2Client(t, srv.URL)
	out, _, err := runSA(t, "list", "--ids")
	if err != nil {
		t.Fatalf("list --ids: %v", err)
	}
	if strings.TrimSpace(out) != "aaa111aaa111aaa111aaa111" {
		t.Errorf("--ids must emit the objectId, got %q", out)
	}
}

// TestServiceAccounts_PlanNoMutation guards that --plan previews instead of
// executing: with --plan, create must render a plan and issue no POST (the
// fake captures POST bodies; a mutation would populate `body`).
func TestServiceAccounts_PlanNoMutation(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	srv := startServiceAccountsServer(t, &body)
	overrideV2Client(t, srv.URL)
	_, errBuf, err := runSA(t, "create", "--name", "x", "--role", "Administrator", "--auth-type", "api_key", "--plan")
	// Plan mode returns an ExitError with the plan exit code (10) and makes
	// no changes. The human-readable plan renders to stderr.
	var exitErr *ExitError
	if !errorAs(err, &exitErr) || exitErr.Code != 10 {
		t.Fatalf("expected plan ExitError(10), got: %v", err)
	}
	if body != nil {
		t.Errorf("--plan must not POST, but a create body was captured: %v", body)
	}
	if !strings.Contains(errBuf, "Plan:") {
		t.Errorf("expected a plan preview on stderr, got: %s", errBuf)
	}
}

func TestServiceAccountsDeleteAndRevoke(t *testing.T) {
	setupUsersTest(t)
	var cap map[string]any = map[string]any{}
	srv := startServiceAccountsServer(t, &cap)
	overrideV2Client(t, srv.URL)

	out, _, err := runSA(t, "delete", "ci-bot", "--force")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, _ := cap["deletedPath"].(string); got != "/service-accounts/aaa111aaa111aaa111aaa111" {
		t.Errorf("delete hit wrong path: %v", cap["deletedPath"])
	}
	// The success message must name the account (GET is wrapped; unwrap must
	// happen) — not an empty string.
	if !strings.Contains(out, "ci-bot") {
		t.Errorf("delete message must show the real name (unwrap), got: %q", out)
	}

	cap = map[string]any{}
	srv2 := startServiceAccountsServer(t, &cap)
	overrideV2Client(t, srv2.URL)
	if _, _, err := runSA(t, "revoke", "ci-bot", "authcfg-9", "--force"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got, _ := cap["deletedPath"].(string); got != "/service-accounts/aaa111aaa111aaa111aaa111/auth-config/authcfg-9" {
		t.Errorf("revoke hit wrong path: %v", cap["deletedPath"])
	}
}
