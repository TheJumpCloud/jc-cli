package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// fakeGetter serves canned responses by exact endpoint and records requests.
type fakeGetter struct {
	responses map[string]string
	requests  []string
}

func (f *fakeGetter) Get(ctx context.Context, endpoint string) (json.RawMessage, error) {
	f.requests = append(f.requests, endpoint)
	r, ok := f.responses[endpoint]
	if !ok {
		return nil, fmt.Errorf("unexpected request %s", endpoint)
	}
	return json.RawMessage(r), nil
}

const serverA = `{"object_id":"aaaaaaaaaaaaaaaaaaaaaaaa","name":"Salesforce","prefix":"sf","target_url":"https://sf.example/mcp","application_object_id":"111111111111111111111111","auth_config":{"oauth":{"client_id":"cid","scope":"api","client_secret":"S3CRET","client_secret_hint":"…cret"}}}`
const serverB = `{"object_id":"bbbbbbbbbbbbbbbbbbbbbbbb","name":"Wiki","prefix":"wk","target_url":"https://wiki.example/mcp","application_object_id":"222222222222222222222222","auth_config":{"api_token":{"token_header_name":"Authorization","token_value":"tok-123","new_field":"x"}}}`

func TestListServers_PagesAndRedacts(t *testing.T) {
	f := &fakeGetter{responses: map[string]string{
		ServersEndpoint + "?page_size=100":               `{"servers":[` + serverA + `],"next_page_token":"t1"}`,
		ServersEndpoint + "?page_size=100&page_token=t1": `{"servers":[` + serverB + `],"next_page_token":""}`,
	}}
	got, err := ListServers(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d servers, want 2 across two pages", len(got))
	}
	all := string(got[0]) + string(got[1])
	for _, secret := range []string{"S3CRET", "tok-123"} {
		if strings.Contains(all, secret) {
			t.Errorf("secret %q leaked: %s", secret, all)
		}
	}
	// An unknown key is hidden too: the redaction is an allow-list.
	if strings.Contains(all, `"new_field":"x"`) {
		t.Errorf("unknown auth_config key shown: %s", all)
	}
	for _, kept := range []string{`"client_id":"cid"`, `"token_header_name":"Authorization"`, `"client_secret_hint":"…cret"`, `"target_url":"https://wiki.example/mcp"`} {
		if !strings.Contains(all, kept) {
			t.Errorf("safe value %s missing: %s", kept, all)
		}
	}
	if AuthType(got[0]) != "oauth" || AuthType(got[1]) != "api_token" {
		t.Errorf("AuthType = %q, %q", AuthType(got[0]), AuthType(got[1]))
	}
}

func TestListServers_RejectsUnknownShapes(t *testing.T) {
	for name, body := range map[string]string{
		"not an object":   `[]`,
		"no servers key":  `{"items":[]}`,
		"servers is null": `{"servers":null}`,
		"bad token":       `{"servers":[],"next_page_token":5}`,
		"server no id":    `{"servers":[{"name":"x"}],"next_page_token":""}`,
	} {
		f := &fakeGetter{responses: map[string]string{ServersEndpoint + "?page_size=100": body}}
		if _, err := ListServers(context.Background(), f); err == nil || !strings.Contains(err.Error(), "preview") {
			t.Errorf("%s: want an unexpected-response error, got %v", name, err)
		}
	}
}

func TestListServers_RepeatedTokenStops(t *testing.T) {
	f := &fakeGetter{responses: map[string]string{
		ServersEndpoint + "?page_size=100":               `{"servers":[],"next_page_token":"t1"}`,
		ServersEndpoint + "?page_size=100&page_token=t1": `{"servers":[],"next_page_token":"t1"}`,
	}}
	if _, err := ListServers(context.Background(), f); err == nil {
		t.Error("a repeated page token must be an error, not a loop or a partial list")
	}
}

func TestListRedirectURIs(t *testing.T) {
	f := &fakeGetter{responses: map[string]string{
		RedirectURIsEndpoint + "?page_size=100": `{"redirect_uris":[{"object_id":"x","url":"https://claude.ai/api/mcp/auth_callback"}],"next_page_token":""}`,
	}}
	got, err := ListRedirectURIs(context.Background(), f)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestGetServer_ValidatesAndRedacts(t *testing.T) {
	f := &fakeGetter{responses: map[string]string{ServersEndpoint + "/aaaaaaaaaaaaaaaaaaaaaaaa": serverA}}
	got, err := GetServer(context.Background(), f, "aaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "S3CRET") {
		t.Errorf("secret leaked: %s", got)
	}
	if _, err := GetServer(context.Background(), f, "../settings"); err == nil {
		t.Error("a non-id must be rejected before any request")
	}
	if len(f.requests) != 1 {
		t.Errorf("requests = %v", f.requests)
	}
}

func TestResolveAndConnector(t *testing.T) {
	f := &fakeGetter{responses: map[string]string{
		ResolveEndpoint + "?application_object_id=111111111111111111111111": `{"associated":true,"connector_id":"salesforce"}`,
		ResolveEndpoint + "?application_object_id=222222222222222222222222": `{"unexpected":true}`,
		ConnectorsEndpoint + "/salesforce":                                  `{"connector_id":"salesforce","display_name":"Salesforce MCP"}`,
	}}
	if _, err := Resolve(context.Background(), f, "111111111111111111111111"); err != nil {
		t.Error(err)
	}
	if _, err := Resolve(context.Background(), f, "222222222222222222222222"); err == nil {
		t.Error("a resolve answer without associated must be an error")
	}
	if _, err := GetConnector(context.Background(), f, "salesforce"); err != nil {
		t.Error(err)
	}
	if _, err := GetConnector(context.Background(), f, "../servers"); err == nil {
		t.Error("an invalid connector id must be rejected")
	}
}

func TestFindServerAndFilterByApp(t *testing.T) {
	dup := strings.Replace(serverB, "bbbbbbbbbbbbbbbbbbbbbbbb", "cccccccccccccccccccccccc", 1)
	servers := []json.RawMessage{json.RawMessage(serverA), json.RawMessage(serverB), json.RawMessage(dup)}
	if s, err := FindServer(servers, "Salesforce"); err != nil || !strings.Contains(string(s), "aaaa") {
		t.Errorf("by name: %s, %v", s, err)
	}
	if s, err := FindServer(servers, "cccccccccccccccccccccccc"); err != nil || !strings.Contains(string(s), "cccc") {
		t.Errorf("by id: %s, %v", s, err)
	}
	if _, err := FindServer(servers, "Wiki"); err == nil || !strings.Contains(err.Error(), "use an id") {
		t.Errorf("ambiguous name: %v", err)
	}
	if _, err := FindServer(servers, "nope"); err == nil {
		t.Error("unknown server must be an error")
	}
	if got := FilterByApp(servers, "222222222222222222222222"); len(got) != 2 {
		t.Errorf("FilterByApp = %d, want 2", len(got))
	}
	if got := FilterByApp(servers, "999999999999999999999999"); got == nil || len(got) != 0 {
		t.Errorf("FilterByApp with no match = %v, want an empty non-nil slice", got)
	}
}

func TestFindServer_UnreadableRecord(t *testing.T) {
	servers := []json.RawMessage{json.RawMessage(`{"object_id":5,"name":"Wiki"}`), json.RawMessage(serverA)}
	if _, err := FindServer(servers, "Wiki"); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("want unreadable error, got %v", err)
	}
	if s, err := FindServer(servers, "Salesforce"); err != nil || !strings.Contains(string(s), "aaaa") {
		t.Errorf("readable match must still win: %s %v", s, err)
	}
	if _, err := FindServer([]json.RawMessage{json.RawMessage(serverA)}, "nope"); err == nil || strings.Contains(err.Error(), "could not be read") {
		t.Errorf("plain not-found expected, got %v", err)
	}
}
