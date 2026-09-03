package pwm

import (
	"encoding/json"
	"strings"
	"testing"
)

// Password Manager keys its records by UUID, not by the 24-hex ObjectID the
// rest of JumpCloud uses. The shared resolver's IsID would reject every valid
// PWM id, which is why this area needs its own.
func TestIsID(t *testing.T) {
	const real = "82bfcbd8-2612-4a5a-be65-f048c3927843" // observed live
	if !IsID(real) {
		t.Errorf("%q is a real Password Manager id", real)
	}
	for _, bad := range []string{
		"608b8cf181c10b3303716501", // a JumpCloud user id
		"juergen",                  // a username
		"",
		"82bfcbd8-2612-4a5a-be65", // truncated
	} {
		if IsID(bad) {
			t.Errorf("IsID(%q) = true", bad)
		}
	}
}

// The API answers HTTP 500 for a JumpCloud id, for a well-formed UUID that does
// not exist, and presumably for a genuine fault — all with "Unexpected error".
// Both non-fault cases were confirmed live. So validating locally is required,
// not defensive: an unvalidated id produces an error message that blames
// JumpCloud for the caller's typo.
func TestErrNotPWMID_ExplainsTheMismatch(t *testing.T) {
	jc := ErrNotPWMID("608b8cf181c10b3303716501")
	if !strings.Contains(jc.Error(), "externalId") {
		t.Errorf("a JumpCloud id should be explained via the bridge: %v", jc)
	}
	if !strings.Contains(jc.Error(), "name or username") {
		t.Errorf("the message should say what to pass instead: %v", jc)
	}

	other := ErrNotPWMID("juergen")
	if strings.Contains(other.Error(), "JumpCloud object id") {
		t.Errorf("a username is not a JumpCloud id: %v", other)
	}
}

const usersJSON = `{"totalCount":1,"results":[{
  "id":"82bfcbd8-2612-4a5a-be65-f048c3927843",
  "externalId":"608b8cf181c10b3303716501",
  "employeeUuid":"82bfcbd8-2612-4a5a-be65-f048c3927843",
  "name":"Juergen Klaassen","username":"juergen",
  "email":"juergen@klaassen.consulting","status":"Active",
  "itemsCount":3,"passwordsCount":3,"passwordsScore":72,"weakPasswords":1}]}`

func TestParseUsers(t *testing.T) {
	users, err := ParseUsers(json.RawMessage(usersJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("got %d users", len(users))
	}
	u := users[0]
	if u.ID != "82bfcbd8-2612-4a5a-be65-f048c3927843" {
		t.Errorf("id = %q", u.ID)
	}
	if u.ExternalID != "608b8cf181c10b3303716501" {
		t.Errorf("externalId = %q — this is the only link back to the directory", u.ExternalID)
	}
}

// The bridge: a name becomes a JumpCloud id becomes a PWM record. Nothing else
// connects the two.
func TestFindUserByExternalID(t *testing.T) {
	users, _ := ParseUsers(json.RawMessage(usersJSON))
	got, ok := FindUserByExternalID(users, "608b8cf181c10b3303716501")
	if !ok {
		t.Fatal("the JumpCloud id should match through externalId")
	}
	if got.ID != "82bfcbd8-2612-4a5a-be65-f048c3927843" {
		t.Errorf("resolved to %q", got.ID)
	}
	if _, ok := FindUserByExternalID(users, "000000000000000000000000"); ok {
		t.Error("an unknown JumpCloud id must not match")
	}
}

func TestFindUser_ByWhatSomeoneTypes(t *testing.T) {
	users, _ := ParseUsers(json.RawMessage(usersJSON))
	for _, id := range []string{
		"82bfcbd8-2612-4a5a-be65-f048c3927843",
		"juergen",
		"JUERGEN",
		"juergen@klaassen.consulting",
		"Juergen Klaassen",
	} {
		if _, ok := FindUser(users, id); !ok {
			t.Errorf("FindUser(%q) found nothing", id)
		}
	}
	// A JumpCloud id is NOT a PWM id and must not match by accident — it
	// resolves only through the bridge, with the directory consulted.
	if _, ok := FindUser(users, "608b8cf181c10b3303716501"); ok {
		t.Error("a JumpCloud id must not resolve directly; it goes through FindUserByExternalID")
	}
}

// Three envelope shapes exist in this one area, which is why parsing is
// centralised rather than repeated per call site.
func TestParseList_AndItems(t *testing.T) {
	rows, total, err := ParseList(json.RawMessage(`{"totalCount":2,"results":[{"a":1},{"a":2}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || total != 2 {
		t.Errorf("rows=%d total=%d", len(rows), total)
	}

	// /items returns results AND items. Both were empty on the tenant probed,
	// so which wins when populated is unknown — the parser hands back both
	// rather than silently choosing.
	res, items, total, err := ParseItems(json.RawMessage(`{"totalCount":1,"results":[{"a":1}],"items":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || items == nil && len(items) != 0 {
		t.Errorf("results=%d items=%v", len(res), items)
	}
	if total != 1 {
		t.Errorf("total = %d", total)
	}
}

// The same folder identity arrives under three different keys depending on
// which endpoint produced the record: `folderId` from a create, `uuid` from the
// list. Both were observed live. Identity hides that from callers.
func TestFolder_IdentityAcrossSpellings(t *testing.T) {
	fromList := Folder{ID: "c35b3898-296c-4c83-97f7-5e0d9f8322ab", Name: "probe"}
	fromCreate := Folder{FolderID: "f83faec9-45e6-4986-994e-d6748d2bc06f"}

	if fromList.Identity() != "c35b3898-296c-4c83-97f7-5e0d9f8322ab" {
		t.Errorf("list identity = %q", fromList.Identity())
	}
	if fromCreate.Identity() != "f83faec9-45e6-4986-994e-d6748d2bc06f" {
		t.Errorf("create identity = %q", fromCreate.Identity())
	}

	folders, err := ParseFolders(json.RawMessage(
		`{"totalCount":1,"results":[{"uuid":"c35b3898-296c-4c83-97f7-5e0d9f8322ab",` +
			`"name":"zz-pwm-probe-folder","itemsInFolder":0,"usersWithAccess":0}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].Identity() == "" {
		t.Fatalf("parsed %+v", folders)
	}
	if _, ok := FindFolder(folders, "zz-pwm-probe-folder"); !ok {
		t.Error("a folder should resolve by name")
	}
}

// The not-implemented map is documentation with teeth: it stops the surface
// being re-expanded from a spec that advertises endpoints nobody built.
func TestNotImplemented_RecordsWhatWasProbed(t *testing.T) {
	if len(NotImplemented) < 8 {
		t.Errorf("only %d entries — the probe found more than that", len(NotImplemented))
	}
	// The two that would otherwise be rediscovered as bugs.
	for _, key := range []string{
		GroupsEndpoint + "/{id}",
		"DELETE " + SharedFoldersEndpoint + "/{id}",
	} {
		if NotImplemented[key] == "" {
			t.Errorf("%q must be recorded — it is the surprising one", key)
		}
	}
	if !strings.Contains(NotImplemented[GroupsEndpoint+"/{id}"], "LIST-ONLY") {
		t.Error("the groups entry should say why: there is no detail endpoint at all")
	}
}

// A record that will not decode must be REPORTED, not skipped.
//
// Groups arrive as objects. Typing them as []string made every user record
// fail to unmarshal, and a silent `continue` turned that into "0 users are
// enrolled in Password Manager" — a confident wrong answer, with no error
// anywhere, on a tenant that plainly had an enrolled user.
//
// This is the same shape as every health-verdict bug in this repo: absent data
// treated as evidence of a negative.
func TestParseUsers_ReportsUndecodableRecords(t *testing.T) {
	bad := `{"totalCount":1,"results":[{"id":"82bfcbd8-2612-4a5a-be65-f048c3927843",` +
		`"groups":"not-an-array-of-objects"}]}`
	_, err := ParseUsers(json.RawMessage(bad))
	if err == nil {
		t.Fatal("a record that does not decode must produce an error, not a shorter list")
	}
	if !strings.Contains(err.Error(), "fewer users than exist") {
		t.Errorf("the error should name what the silent version got wrong: %v", err)
	}

	// The shape the API actually returns.
	good := `{"totalCount":1,"results":[{"id":"82bfcbd8-2612-4a5a-be65-f048c3927843",` +
		`"username":"juergen","groups":[{"id":"g1","name":"PWM Users"}]}]}`
	users, err := ParseUsers(json.RawMessage(good))
	if err != nil {
		t.Fatalf("the live shape must decode: %v", err)
	}
	if len(users[0].Groups) != 1 || users[0].Groups[0].Name != "PWM Users" {
		t.Errorf("groups did not decode: %+v", users[0].Groups)
	}
}
