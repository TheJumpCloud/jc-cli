package googleemm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The live enterprise, verbatim from the probe.
const liveEnterprises = `{"count":1,"enterprises":[{
 "allowDeviceEnrollment":true,"contactEmail":"","createdAt":"2023-04-19T23:02:41.120Z",
 "deviceGroupId":"6440738accb5e7000144d1b2","displayName":"klaassen.consulting",
 "enterpriseType":"MANAGED_GOOGLE_PLAY_ACCOUNTS_ENTERPRISE",
 "name":"enterprises/LC01m7doq1","objectId":"644073116f8b61c2b15ab423",
 "organizationObjectId":"5ec71e8e96bfda0611fc6c5b"}]}`

func TestParseEnterprises_LiveShape(t *testing.T) {
	got, err := ParseEnterprises(json.RawMessage(liveEnterprises))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d enterprises, want 1", len(got))
	}
	e := got[0]
	if e.ObjectID != "644073116f8b61c2b15ab423" {
		t.Errorf("objectId = %q", e.ObjectID)
	}
	// Google's id is display-only, and separating it from the object id is
	// the distinction the whole package exists to enforce.
	if e.GoogleID() != "LC01m7doq1" {
		t.Errorf("GoogleID() = %q, want LC01m7doq1", e.GoogleID())
	}
}

// The spec says two id systems. There is one. A record that will not decode
// must not quietly shrink the list.
func TestParseEnterprises_UndecodableRecordIsReported(t *testing.T) {
	_, err := ParseEnterprises(json.RawMessage(`{"count":1,"enterprises":["a bare string"]}`))
	if err == nil {
		t.Fatal("an undecodable enterprise was skipped; its devices would then be unreachable")
	}
	if !strings.Contains(err.Error(), "did not decode") {
		t.Errorf("error should say what happened, got: %v", err)
	}
}

// Three list endpoints, three envelopes. Callers should not have to know.
func TestParse_ThreeEnvelopeShapes(t *testing.T) {
	rows, total, err := ParseDevices(json.RawMessage(`{"count":0,"devices":[]}`))
	if err != nil || len(rows) != 0 || total != 0 {
		t.Errorf("devices: rows=%d total=%d err=%v", len(rows), total, err)
	}
	// enrollment-tokens uses BOTH a different array key and a different count
	// key, which is the mistake this hides.
	rows, total, err = ParseTokens(json.RawMessage(`{"results":[{"id":"t1"},{"id":"t2"}],"totalCount":7}`))
	if err != nil || len(rows) != 2 || total != 7 {
		t.Errorf("tokens: rows=%d total=%d err=%v", len(rows), total, err)
	}
}

// A count absent from the envelope must fall back to the row count rather
// than reporting zero.
func TestParse_MissingCountFallsBackToRowCount(t *testing.T) {
	_, total, err := ParseTokens(json.RawMessage(`{"results":[{"id":"t1"}]}`))
	if err != nil || total != 1 {
		t.Errorf("total = %d, want 1 (err=%v)", total, err)
	}
}

func TestIsObjectID(t *testing.T) {
	for s, want := range map[string]bool{
		"644073116f8b61c2b15ab423": true,
		"LC01m7doq1":               false, // Google's id — rejected by every endpoint
		"enterprises/LC01m7doq1":   false,
		"":                         false,
		"644073116f8b61c2b15ab42":  false, // 23 chars
	} {
		if got := IsObjectID(s); got != want {
			t.Errorf("IsObjectID(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestFindEnterprise_ByEveryIdentifierAnOperatorMightType(t *testing.T) {
	list, err := ParseEnterprises(json.RawMessage(liveEnterprises))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"644073116f8b61c2b15ab423", // object id
		"klaassen.consulting",      // display name
		"enterprises/LC01m7doq1",   // Google resource name
		"LC01m7doq1",               // bare Google id
		"KLAASSEN.CONSULTING",      // case-insensitive
	} {
		got, err := FindEnterprise(list, id)
		if err != nil {
			t.Errorf("FindEnterprise(%q): %v", id, err)
			continue
		}
		if got.ObjectID != "644073116f8b61c2b15ab423" {
			t.Errorf("FindEnterprise(%q) resolved to %q", id, got.ObjectID)
		}
	}
}

func TestFindEnterprise_DuplicateDisplayNameIsAmbiguous(t *testing.T) {
	list := []Enterprise{
		{ObjectID: "644073116f8b61c2b15ab423", DisplayName: "acme"},
		{ObjectID: "644073116f8b61c2b15ab424", DisplayName: "acme"},
	}
	_, err := FindEnterprise(list, "acme")
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("want *AmbiguousError, got %T: %v", err, err)
	}
	for _, want := range []string{"ambiguous", "644073116f8b61c2b15ab423", "644073116f8b61c2b15ab424"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message missing %q: %v", want, err)
		}
	}
}

func TestFindEnterprise_NoMatchIsDistinguishable(t *testing.T) {
	list, _ := ParseEnterprises(json.RawMessage(liveEnterprises))
	if _, err := FindEnterprise(list, "no-such-enterprise"); !errors.Is(err, ErrNoMatch) {
		t.Errorf("want ErrNoMatch, got %v", err)
	}
}

// THE TRAP. /enterprises/{any well-formed 24-hex}/devices returns
// {"count":0,"devices":[]} for an enterprise that does not exist, while its
// siblings under the same prefix return 404. So a device count must only ever
// be reported for an enterprise seen in the list.
func TestResolveEnterprise_RefusesAnIDThatIsMerelyWellFormed(t *testing.T) {
	get := func(_ context.Context, ep string) (json.RawMessage, error) {
		if ep != EnterprisesEndpoint {
			t.Fatalf("resolution must consult the list, not %s", ep)
		}
		return json.RawMessage(liveEnterprises), nil
	}
	// Well-formed, and the devices endpoint would happily answer "0" for it.
	_, err := ResolveEnterprise(context.Background(), get, "000000000000000000000000")
	if err == nil {
		t.Fatal("a 24-hex id that no enterprise has was accepted; jc would then " +
			"report \"0 devices\" for an enterprise that does not exist")
	}
	if !strings.Contains(err.Error(), "no Google EMM enterprise") {
		t.Errorf("error should say the enterprise is unknown, got: %v", err)
	}
}

func TestResolveEnterprise_AcceptsTheRealOne(t *testing.T) {
	get := func(_ context.Context, _ string) (json.RawMessage, error) {
		return json.RawMessage(liveEnterprises), nil
	}
	e, err := ResolveEnterprise(context.Background(), get, "klaassen.consulting")
	if err != nil {
		t.Fatal(err)
	}
	if e.ObjectID != "644073116f8b61c2b15ab423" {
		t.Errorf("resolved to %q", e.ObjectID)
	}
}

// The endpoints the area advertises and does not serve are recorded rather
// than rediscovered one 404 at a time.
func TestNotImplemented_RecordsTheProbedGaps(t *testing.T) {
	if len(NotImplemented) < 2 {
		t.Errorf("want the probed 404s recorded, got %d", len(NotImplemented))
	}
	for k, v := range NotImplemented {
		if !strings.Contains(v, "404") {
			t.Errorf("%s: the note should say what the API actually does: %s", k, v)
		}
	}
}

func TestEndpoints_UseTheHyphenatedTree(t *testing.T) {
	// /googleemm as one word is a 404.
	for _, ep := range []string{
		EnterprisesEndpoint,
		EnterpriseDevicesEndpoint("x"),
		EnterpriseTokensEndpoint("x"),
		ConnectionStatusEndpoint("x"),
		DeviceEndpoint("x"),
		DevicePolicyResultsEndpoint("x"),
	} {
		if !strings.HasPrefix(ep, "/google-emm/") {
			t.Errorf("%q does not use the hyphenated tree", ep)
		}
	}
}
