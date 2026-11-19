package privaccess

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeWriter answers reads from fixed responses and records writes.
type fakeWriter struct {
	fakeGetter
	// after, when set, replaces responses once the first write lands, so a
	// read-back sees the server's new state.
	after  map[string]string
	writes []fakeWrite
}

type fakeWrite struct {
	method, endpoint string
	body             map[string]any
}

func (f *fakeWriter) record(method, endpoint string, body any) {
	var m map[string]any
	b, _ := json.Marshal(body)
	_ = json.Unmarshal(b, &m)
	f.writes = append(f.writes, fakeWrite{method, endpoint, m})
	if f.after != nil {
		f.responses = f.after
	}
}

func (f *fakeWriter) Create(ctx context.Context, endpoint string, body any) (json.RawMessage, error) {
	f.record("POST", endpoint, body)
	return json.RawMessage(`{}`), nil
}

func (f *fakeWriter) Update(ctx context.Context, endpoint string, body any) (json.RawMessage, error) {
	f.record("PUT", endpoint, body)
	return json.RawMessage(`{}`), nil
}

func (f *fakeWriter) Delete(ctx context.Context, endpoint string) (json.RawMessage, error) {
	f.record("DELETE", endpoint, nil)
	return json.RawMessage(`{}`), nil
}

const ownerPolicy = `{"isPrivate":false,"users":[{"id":7,"permissions":["Manage","View Detail","Connect"]}],"groups":[]}`

func TestCheckPolicy(t *testing.T) {
	var none any
	for name, tc := range map[string]struct {
		policy string
		ok     bool
	}{
		"user manage":   {ownerPolicy, true},
		"group manage":  {`{"users":[],"groups":[{"id":3,"permissions":["Manage"]}]}`, true},
		"view only":     {`{"users":[{"id":7,"permissions":["View Detail","Connect"]}]}`, false},
		"empty":         {`{"users":[],"groups":[]}`, false},
		"not an object": {`[]`, false},
	} {
		var p any
		_ = json.Unmarshal([]byte(tc.policy), &p)
		if err := CheckPolicy(p); (err == nil) != tc.ok {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if CheckPolicy(none) == nil {
		t.Error("a missing policy must be refused")
	}
}

func TestApplyChanges_OnlyNamedKeysPerKind(t *testing.T) {
	rec := func() map[string]any {
		var p any
		_ = json.Unmarshal([]byte(ownerPolicy), &p)
		return map[string]any{"accessPolicies": p}
	}
	if err := ApplyChanges(Servers, rec(), map[string]any{"accessPolicies": nil}); err == nil {
		t.Error("the access policy must not be changeable")
	}
	if err := ApplyChanges(Databases, rec(), map[string]any{"os": "linux"}); err == nil {
		t.Error("os is not a database field")
	}
	if err := ApplyChanges(Websites, rec(), map[string]any{"name": " "}); err == nil {
		t.Error("an empty name must be refused")
	}
	if err := ApplyChanges(Servers, rec(), map[string]any{}); err == nil {
		t.Error("no changes must be refused")
	}
	r := rec()
	if err := ApplyChanges(Databases, r, map[string]any{"port": 6543, "notes": ""}); err != nil {
		t.Fatal(err)
	}
	if r["port"] != 6543 || r["notes"] != "" {
		t.Errorf("changes not applied: %v", r)
	}
}

func TestCreateBody(t *testing.T) {
	if _, err := CreateBody(Websites, ResourceSpec{Name: "w", URI: "https://x", Port: 1}, 7); err == nil {
		t.Error("a website takes no port")
	}
	if _, err := CreateBody(Databases, ResourceSpec{Name: "d", URI: "h"}, 7); err == nil {
		t.Error("a database needs provider and port")
	}
	b, err := CreateBody(Servers, ResourceSpec{Name: "s", URI: "h", OS: "Linux", Protocol: "SSH", Port: 22}, 7)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(b)
	for _, want := range []string{`"server":{`, `"category":"ssh"`, `"os":"linux"`, `"id":7`, `"Manage"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("create body lacks %s: %s", want, out)
		}
	}
}

// editDB is a database edit view whose connection string embeds a password.
func editDB(notes string) string {
	return `{"database":{"id":"` + dbA + `","name":"Prod","notes":"` + notes +
		`","connectionString":"Host=db;Password=hunter2","accessPolicies":` + ownerPolicy + `},"rules":[]}`
}

func TestUpdateResource_SendsWholeRecordAndPolicyUnchanged(t *testing.T) {
	list := `{"items":[{"id":"` + dbA + `","name":"Prod"}],"totalCount":1}`
	edit := DatabasesEndpoint + "/" + dbA + "/edit"
	f := &fakeWriter{
		fakeGetter: fakeGetter{responses: map[string]string{
			DatabasesEndpoint + page(0): list, edit: editDB("old"),
		}},
		after: map[string]string{DatabasesEndpoint + page(0): list, edit: editDB("new")},
	}
	raw, err := UpdateResource(context.Background(), f, Databases, "Prod", map[string]any{"notes": "new"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 || f.writes[0].method != "PUT" || f.writes[0].endpoint != DatabasesEndpoint {
		t.Fatalf("writes: %+v", f.writes)
	}
	sent := f.writes[0].body["database"].(map[string]any)
	if sent["connectionString"] != "Host=db;Password=hunter2" {
		t.Errorf("the connection string must go back as read, not redacted: %v", sent["connectionString"])
	}
	if sent["notes"] != "new" || sent["accessPolicies"] == nil {
		t.Errorf("sent record wrong: %v", sent)
	}
	if strings.Contains(string(raw), "hunter2") {
		t.Errorf("the returned view must be redacted: %s", raw)
	}
}

func TestUpdateResource_RefusesPolicyWithoutManage(t *testing.T) {
	list := `{"results":[{"id":"` + srvA + `","name":"site"}],"totalCount":1}`
	f := &fakeWriter{fakeGetter: fakeGetter{responses: map[string]string{
		WebsitesEndpoint: list,
		WebsitesEndpoint + "/" + srvA + "/edit": `{"website":{"id":"` + srvA +
			`","name":"site","accessPolicies":{"users":[{"id":7,"permissions":["Connect"]}]}}}`,
	}}}
	_, err := UpdateResource(context.Background(), f, Websites, "site", map[string]any{"notes": "x"})
	if err == nil || !strings.Contains(err.Error(), "Manage") {
		t.Fatalf("got %v", err)
	}
	if len(f.writes) != 0 {
		t.Errorf("nothing may be sent: %+v", f.writes)
	}
}

func TestUpdateResource_DetectsUnconfirmedChange(t *testing.T) {
	list := `{"items":[{"id":"` + dbA + `","name":"Prod"}],"totalCount":1}`
	edit := DatabasesEndpoint + "/" + dbA + "/edit"
	f := &fakeWriter{fakeGetter: fakeGetter{responses: map[string]string{
		DatabasesEndpoint + page(0): list, edit: editDB("old"),
	}}}
	_, err := UpdateResource(context.Background(), f, Databases, dbA, map[string]any{"notes": "new"})
	if err == nil || !strings.Contains(err.Error(), "reads back") {
		t.Fatalf("an update that did not land must be an error, got %v", err)
	}
}

func TestDeleteResource_ArchivedServerRefused(t *testing.T) {
	f := &fakeWriter{fakeGetter: fakeGetter{responses: map[string]string{
		ServersEndpoint + page(0): `{"items":[],"totalCount":0}`,
		ServersEndpoint + "?archivedFilter=true&maxResultCount=100&skipCount=0": `{"items":[{"id":"` +
			srvA + `","name":"old"}],"totalCount":1}`,
	}}}
	_, err := DeleteResource(context.Background(), f, Servers, "old")
	if err == nil || !strings.Contains(err.Error(), "unarchive it first") {
		t.Fatalf("got %v", err)
	}
	if len(f.writes) != 0 {
		t.Errorf("nothing may be sent: %+v", f.writes)
	}
}

func TestSetArchived_BulkForServersSingleForWebsites(t *testing.T) {
	srvList := `{"items":[{"id":"` + srvA + `","name":"a"},{"id":"` + srvB + `","name":"b"}],"totalCount":2}`
	archivedEP := ServersEndpoint + "?archivedFilter=true&maxResultCount=100&skipCount=0"
	f := &fakeWriter{
		fakeGetter: fakeGetter{responses: map[string]string{
			ServersEndpoint + page(0): srvList, archivedEP: `{"items":[],"totalCount":0}`,
		}},
		after: map[string]string{archivedEP: srvList},
	}
	if _, err := SetArchived(context.Background(), f, Servers, []string{"a", srvB}, true); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 || f.writes[0].endpoint != ServersEndpoint+"/bulk-archive" {
		t.Fatalf("writes: %+v", f.writes)
	}
	if ids, _ := f.writes[0].body["ids"].([]any); len(ids) != 2 {
		t.Errorf("bulk body: %v", f.writes[0].body)
	}

	webList := `{"results":[{"id":"` + srvA + `","name":"a"},{"id":"` + srvB + `","name":"b"}],"totalCount":2}`
	webArchived := WebsitesEndpoint + "?filter=archived%3Aeq%3Atrue"
	w := &fakeWriter{
		fakeGetter: fakeGetter{responses: map[string]string{
			WebsitesEndpoint: webList, webArchived: `{"results":[],"totalCount":0}`,
		}},
		after: map[string]string{webArchived: webList},
	}
	if _, err := SetArchived(context.Background(), w, Websites, []string{"a", "b"}, true); err != nil {
		t.Fatal(err)
	}
	if len(w.writes) != 2 || w.writes[0].endpoint != WebsitesEndpoint+"/"+srvA+"/archive" ||
		w.writes[0].body == nil {
		t.Fatalf("website writes must be single calls with a {} body: %+v", w.writes)
	}
}

func TestSetArchived_UnconfirmedIsAnError(t *testing.T) {
	archivedEP := ServersEndpoint + "?archivedFilter=true&maxResultCount=100&skipCount=0"
	f := &fakeWriter{fakeGetter: fakeGetter{responses: map[string]string{
		ServersEndpoint + page(0): `{"items":[{"id":"` + srvA + `","name":"a"}],"totalCount":1}`,
		archivedEP:                `{"items":[],"totalCount":0}`,
	}}}
	if _, err := SetArchived(context.Background(), f, Servers, []string{"a"}, true); err == nil ||
		!strings.Contains(err.Error(), "not archived") {
		t.Fatalf("got %v", err)
	}
}

func TestIDsBody(t *testing.T) {
	if _, err := IDsBody(nil); err == nil {
		t.Error("an empty list must be refused")
	}
	if _, err := IDsBody([]string{"nope"}); err == nil {
		t.Error("a non-hex id must be refused")
	}
}

func TestUpdateBlockingRule_KeepsCriteria(t *testing.T) {
	rule := "eeeeeeeeeeeeeeeeeeeeeeee"
	list := `{"items":[{"id":"` + rule + `","name":"r","criteriaType":"BLOCKING_RULE_CRITERIA_TYPE_DATABASE"}],"totalCount":1}`
	edit := BlockingRulesEndpoint + "/" + rule + "/edit?criteriaType=DATABASE"
	view := func(p int) string {
		return `{"blockingRule":{"id":"` + rule + `","name":"r","priority":` + string(rune('0'+p)) +
			`,"criteria":{"type":0,"tableName":"t"},"accessPolicies":` + ownerPolicy + `}}`
	}
	f := &fakeWriter{
		fakeGetter: fakeGetter{responses: map[string]string{BlockingRulesEndpoint + page(0): list, edit: view(1)}},
		after:      map[string]string{BlockingRulesEndpoint + page(0): list, edit: view(3)},
	}
	if _, err := UpdateBlockingRule(context.Background(), f, "r", map[string]any{"priority": 3}); err != nil {
		t.Fatal(err)
	}
	sent := f.writes[0].body["blockingRule"].(map[string]any)
	if c, _ := sent["criteria"].(map[string]any); c["tableName"] != "t" {
		t.Errorf("criteria must go back unchanged: %v", sent)
	}
	if _, err := UpdateBlockingRule(context.Background(), f, "r", map[string]any{"criteria": nil}); err == nil {
		t.Error("criteria must not be changeable")
	}
}
