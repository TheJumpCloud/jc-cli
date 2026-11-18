package privaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

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

const (
	srvA = "aaaaaaaaaaaaaaaaaaaaaaaa"
	srvB = "bbbbbbbbbbbbbbbbbbbbbbbb"
	dbA  = "dddddddddddddddddddddddd"
	cred = "cccccccccccccccccccccccc"
)

func page(skip int) string {
	return fmt.Sprintf("?maxResultCount=100&skipCount=%d", skip)
}

func TestRequireActive(t *testing.T) {
	for body, wantErr := range map[string]string{
		`{"isActive":true,"isPam":true,"isExpired":false}`:  "",
		`{"isActive":true,"isPam":false,"isExpired":false}`: "not active",
		`{"isActive":false,"isPam":true}`:                   "not active",
		`{"isActive":true,"isPam":true,"isExpired":true}`:   "expired",
		`not json`: "decoding",
	} {
		f := &fakeGetter{responses: map[string]string{StatusEndpoint: body}}
		err := RequireActive(context.Background(), f)
		if wantErr == "" && err != nil || wantErr != "" && (err == nil || !strings.Contains(err.Error(), wantErr)) {
			t.Errorf("%s: got %v, want %q", body, err, wantErr)
		}
	}
}

func TestListPaged_WalksPagesAndDedupes(t *testing.T) {
	f := &fakeGetter{responses: map[string]string{
		ServersEndpoint + page(0):   `{"items":[{"id":"` + srvA + `"}],"totalCount":2}`,
		ServersEndpoint + page(100): `{"items":[{"id":"` + srvA + `"},{"id":"` + srvB + `"}],"totalCount":2}`,
	}}
	rows, total, err := ListPaged(context.Background(), f, ServersEndpoint, "servers")
	if err != nil || total != 2 || len(rows) != 2 {
		t.Fatalf("rows=%d total=%d err=%v", len(rows), total, err)
	}
}

func TestListPaged_RejectsUnknownShapes(t *testing.T) {
	for name, body := range map[string]string{
		"results not items": `{"results":[],"totalCount":0}`,
		"items null":        `{"items":null,"totalCount":0}`,
		"no count":          `{"items":[]}`,
		"not an object":     `[]`,
	} {
		f := &fakeGetter{responses: map[string]string{ServersEndpoint + page(0): body}}
		if _, _, err := ListPaged(context.Background(), f, ServersEndpoint, "servers"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestListPaged_StopsOnShortServer(t *testing.T) {
	// A count the server never reaches must end on the empty page, not loop.
	f := &fakeGetter{responses: map[string]string{
		ServersEndpoint + page(0):   `{"items":[{"id":"` + srvA + `"}],"totalCount":5}`,
		ServersEndpoint + page(100): `{"items":[],"totalCount":5}`,
	}}
	rows, total, err := ListPaged(context.Background(), f, ServersEndpoint, "servers")
	if err != nil || len(rows) != 1 || total != 5 {
		t.Fatalf("rows=%d total=%d err=%v", len(rows), total, err)
	}
}

func TestGet_ResolvesByNameAndRedacts(t *testing.T) {
	f := &fakeGetter{responses: map[string]string{
		DatabasesEndpoint + page(0):             `{"items":[{"id":"` + dbA + `","name":"Prod DB"}],"totalCount":1}`,
		DatabasesEndpoint + "/" + dbA + "/edit": `{"database":{"id":"` + dbA + `","connectionString":"Server=x;Password=hunter2","isDefaultConnectionString":false,"port":5432},"credentials":[{"id":"x","password":"p"}]}`,
	}}
	raw, err := Get(context.Background(), f, Databases, "Prod DB")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, "hunter2") || strings.Contains(s, `"p"`) {
		t.Errorf("secret leaked: %s", s)
	}
	if !strings.Contains(s, `"isDefaultConnectionString":false`) || !strings.Contains(s, `"port":5432`) {
		t.Errorf("safe values dropped: %s", s)
	}
}

func TestGet_UnknownServerNeverReachesEditView(t *testing.T) {
	// An unknown server's edit view is a 500 the client retries; the listing
	// must answer first.
	f := &fakeGetter{responses: map[string]string{
		ServersEndpoint + page(0): `{"items":[],"totalCount":0}`,
	}}
	if _, err := Get(context.Background(), f, Servers, srvA); err == nil || !strings.Contains(err.Error(), "no Privileged Access server") {
		t.Fatalf("got %v", err)
	}
	for _, r := range f.requests {
		if strings.HasSuffix(r, "/edit") {
			t.Errorf("edit view requested for an unknown server: %v", f.requests)
		}
	}
}

func TestFind_AmbiguousName(t *testing.T) {
	rows := []json.RawMessage{
		json.RawMessage(`{"id":"` + srvA + `","name":"jump"}`),
		json.RawMessage(`{"id":"` + srvB + `","name":"jump"}`),
	}
	if _, err := Find(rows, "server", "jump"); err == nil || !strings.Contains(err.Error(), "use an id") {
		t.Errorf("got %v", err)
	}
	if r, err := Find(rows, "server", srvB); err != nil || idOf(r) != srvB {
		t.Errorf("by id: %s %v", r, err)
	}
}

func TestGetCredential_AllowListAndValidation(t *testing.T) {
	f := &fakeGetter{responses: map[string]string{
		CredentialsEndpoint + "/" + cred + "/edit-shell": `{"id":"` + cred + `","name":"root","credentialType":"PASSWORD","canViewSecret":true,"password":"hunter2","newField":"x"}`,
	}}
	raw, err := GetCredential(context.Background(), f, cred)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, "hunter2") || strings.Contains(s, "newField") {
		t.Errorf("unlisted key shown: %s", s)
	}
	if !strings.Contains(s, `"canViewSecret":true`) {
		t.Errorf("flag dropped: %s", s)
	}
	if _, err := GetCredential(context.Background(), f, "123"); err == nil || !strings.Contains(err.Error(), "number") {
		t.Errorf("numeric id: %v", err)
	}
}

func TestCredentialLinkedServers_ChecksExistenceFirst(t *testing.T) {
	f := &fakeGetter{responses: map[string]string{
		CredentialsEndpoint + "/" + cred + "/edit-shell":     `{"id":"` + cred + `"}`,
		CredentialsEndpoint + "/" + cred + "/linked-servers": `{"results":[{"id":"` + srvA + `"}],"continuationToken":"t2"}`,
	}}
	p, err := CredentialLinkedServers(context.Background(), f, cred, "")
	if err != nil || len(p.Items) != 1 || p.Next != "t2" {
		t.Fatalf("%+v %v", p, err)
	}
	if !strings.HasSuffix(f.requests[0], "/edit-shell") {
		t.Errorf("existence not checked first: %v", f.requests)
	}
}

func TestStripPersonalFields(t *testing.T) {
	rows, err := StripPersonalFields([]json.RawMessage{json.RawMessage(`{"id":"x","name":"Visa","last4Digits":"4242","cardholderName":"J Doe","idType":"PASSPORT","firstName":"J"}`)})
	if err != nil {
		t.Fatal(err)
	}
	s := string(rows[0])
	for _, v := range []string{"4242", "J Doe", "PASSPORT", `"J"`} {
		if strings.Contains(s, v) {
			t.Errorf("%s kept: %s", v, s)
		}
	}
	if !strings.Contains(s, "Visa") {
		t.Errorf("name dropped: %s", s)
	}
}

func TestGetBlockingRule_ShortCriteriaType(t *testing.T) {
	f := &fakeGetter{responses: map[string]string{
		BlockingRulesEndpoint + page(0):                                    `{"items":[{"id":"` + srvA + `","name":"no drops","criteriaType":"BLOCKING_RULE_CRITERIA_TYPE_DATABASE"}],"totalCount":1}`,
		BlockingRulesEndpoint + "/" + srvA + "/edit?criteriaType=DATABASE": `{"blockingRule":{"id":"` + srvA + `"}}`,
	}}
	if _, err := GetBlockingRule(context.Background(), f, "no drops"); err != nil {
		t.Fatal(err)
	}
}

func TestResourceConnectionStatus_UnknownIsAnError(t *testing.T) {
	f := &fakeGetter{responses: map[string]string{
		ServersEndpoint + page(0):                          `{"items":[],"totalCount":0}`,
		DatabasesEndpoint + page(0):                        `{"items":[{"id":"` + dbA + `","name":"Prod DB"}],"totalCount":1}`,
		WebsitesEndpoint:                                   `{"results":[],"totalCount":0}`,
		ConnectionsEndpoint + "/status/by-resource/" + dbA: `{"statusDescription":"Available","numberConnectedUsers":0}`,
	}}
	if _, kind, err := ResourceConnectionStatus(context.Background(), f, "Prod DB"); err != nil || kind != "database" {
		t.Errorf("kind=%q err=%v", kind, err)
	}
	if _, _, err := ResourceConnectionStatus(context.Background(), f, srvB); err == nil {
		t.Error("an id matching no resource must be an error, not \"Available\"")
	}
}

func TestGroupsEndpoint(t *testing.T) {
	if e, err := GroupsEndpoint("pam"); err != nil || !strings.HasSuffix(e, "GROUP_PRODUCT_PAM") {
		t.Errorf("%s %v", e, err)
	}
	if _, err := GroupsEndpoint("PAM_X"); err == nil {
		t.Error("bad product accepted")
	}
}
