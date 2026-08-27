package pwpolicy

import (
	"encoding/json"
	"strings"
	"testing"
)

// liveListResponse is the verbatim body GET /passwordpolicies returned from
// org 5ec71e8e96bfda0611fc6c5b on 2026-08-27: a "results" envelope wrapping a
// flat, sparse projection. It is kept exact so a server-side reshape breaks
// this test rather than the CLI.
const liveListResponse = `{
  "results": [
    {
      "default": true,
      "description": "",
      "enableMaxLoginAttempts": true,
      "enableMinLength": true,
      "enablePasswordExpirationInDays": false,
      "groupCount": 0,
      "maxLoginAttempts": 6,
      "minLength": 8,
      "name": "",
      "objectId": "67dc22433f87810001e2bf1c",
      "passwordExpirationInDays": 90,
      "precedence": 1
    }
  ]
}`

// liveDetailResponse is the verbatim body GET /passwordpolicies/{id} returned
// from the same probe.
const liveDetailResponse = `{
  "cached": false,
  "groups": [],
  "objectId": "67dc22433f87810001e2bf1c",
  "policy": {
    "allowUnenrolledMFAPasswordReset": false,
    "allowUsernameSubstring": false,
    "daysAfterExpirationToSelfRecover": -1,
    "daysBeforeExpirationToForceReset": 10,
    "default": true,
    "description": "",
    "disallowCommonlyUsedPasswords": false,
    "disallowSequentialOrRepetitiveChars": false,
    "displayComplexityOnResetScreen": true,
    "effectiveDate": "2026-07-16T23:36:44.820Z",
    "enableDaysAfterExpirationToSelfRecover": false,
    "enableDaysBeforeExpirationToForceReset": false,
    "enableLockoutTimeInSeconds": true,
    "enableMaxHistory": false,
    "enableMaxLoginAttempts": true,
    "enableMinChangePeriodInDays": false,
    "enableMinLength": true,
    "enablePasswordExpirationInDays": false,
    "enableRecoveryEmail": true,
    "enableResetLockoutCounter": true,
    "lockoutTimeInSeconds": 3601,
    "maxHistory": 3,
    "maxLoginAttempts": 6,
    "minChangePeriodInDays": 0,
    "minLength": 8,
    "name": "",
    "needsLowercase": false,
    "needsNumeric": false,
    "needsSymbolic": false,
    "needsUppercase": false,
    "passwordExpirationInDays": 90,
    "precedence": 1,
    "resetLockoutCounterMinutes": 30
  }
}`

func TestEndpoints(t *testing.T) {
	if got := PolicyEndpoint("abc"); got != "/passwordpolicies/abc" {
		t.Errorf("PolicyEndpoint = %q", got)
	}
	if got := UserEndpoint("u1"); got != "/passwordpolicies/user/u1" {
		t.Errorf("UserEndpoint = %q", got)
	}
	if got := UserGroupEndpoint("g1"); got != "/passwordpolicies/usergroup/g1" {
		t.Errorf("UserGroupEndpoint = %q", got)
	}
}

func TestParseList_ResultsEnvelope(t *testing.T) {
	items, err := ParseListItems(json.RawMessage(liveListResponse))
	if err != nil {
		t.Fatalf("ParseListItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].ObjectID != "67dc22433f87810001e2bf1c" {
		t.Errorf("ObjectID = %q", items[0].ObjectID)
	}
	if !items[0].Default || items[0].Precedence != 1 || items[0].GroupCount != 0 {
		t.Errorf("unexpected item: %+v", items[0])
	}
}

func TestParseDetail_LiveShape(t *testing.T) {
	d, err := ParseDetail(json.RawMessage(liveDetailResponse))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	if d.ObjectID != "67dc22433f87810001e2bf1c" {
		t.Errorf("ObjectID = %q", d.ObjectID)
	}
	if d.Policy.MinLength != 8 || !d.Policy.EnableMinLength {
		t.Errorf("minLength not decoded: %+v", d.Policy)
	}
	// A negative sentinel round-trips: -1 means "no self-recovery window",
	// not "zero days", so it must survive a merge untouched.
	if d.Policy.DaysAfterExpirationToSelfRecover != -1 {
		t.Errorf("DaysAfterExpirationToSelfRecover = %d, want -1", d.Policy.DaysAfterExpirationToSelfRecover)
	}
	if d.GroupIDs() != nil {
		t.Errorf("GroupIDs on unbound policy = %v, want nil", d.GroupIDs())
	}
}

func TestGroupIDs(t *testing.T) {
	d := Detail{Groups: []Group{{GroupID: "g1", Name: "Eng"}, {GroupID: "g2", Name: "Sales"}}}
	got := d.GroupIDs()
	if len(got) != 2 || got[0] != "g1" || got[1] != "g2" {
		t.Errorf("GroupIDs = %v", got)
	}
}

func TestApplyChanges_SettingValueEnablesIt(t *testing.T) {
	cur := Policy{MinLength: 8, EnableMinLength: false}
	next, err := ApplyChanges(cur, map[string]any{"minLength": 16})
	if err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}
	if next.MinLength != 16 {
		t.Errorf("MinLength = %d, want 16", next.MinLength)
	}
	if !next.EnableMinLength {
		t.Error("setting minLength must switch on enableMinLength, else the value is inert")
	}
}

func TestApplyChanges_ExplicitEnableWins(t *testing.T) {
	cur := Policy{MinLength: 8}
	next, err := ApplyChanges(cur, map[string]any{"minLength": 16, "enableMinLength": false})
	if err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}
	if next.MinLength != 16 {
		t.Errorf("MinLength = %d, want 16", next.MinLength)
	}
	if next.EnableMinLength {
		t.Error("an explicit enableMinLength=false must not be overridden by the auto-enable rule")
	}
}

func TestApplyChanges_PreservesUntouchedFields(t *testing.T) {
	d, err := ParseDetail(json.RawMessage(liveDetailResponse))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	next, err := ApplyChanges(d.Policy, map[string]any{"name": "Engineering"})
	if err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}
	if next.Name != "Engineering" {
		t.Errorf("Name = %q", next.Name)
	}
	// Everything else must survive: this is what makes the merge correct
	// whether the API's PUT replaces or merges.
	if next.LockoutTimeInSeconds != 3601 || next.MaxHistory != 3 ||
		next.ResetLockoutCounterMinutes != 30 || !next.EnableRecoveryEmail ||
		next.DaysAfterExpirationToSelfRecover != -1 {
		t.Errorf("untouched fields lost: %+v", next)
	}
}

func TestApplyChanges_RejectsUnknownField(t *testing.T) {
	_, err := ApplyChanges(Policy{}, map[string]any{"minLenght": 16})
	if err == nil {
		t.Fatal("want error for misspelled field")
	}
	if !strings.Contains(err.Error(), "minLenght") {
		t.Errorf("error should name the offending field, got %v", err)
	}
}

func TestApplyChanges_RejectsEmpty(t *testing.T) {
	if _, err := ApplyChanges(Policy{}, nil); err == nil {
		t.Fatal("want error for no changes")
	}
}

func TestApplyChanges_DefaultAndPrecedenceNotSettable(t *testing.T) {
	// Precedence is owned by the precedence/set endpoint and `default` is
	// server-controlled; neither may be smuggled through an update.
	for _, f := range []string{"default", "precedence", "effectiveDate"} {
		if Settable[f] {
			t.Errorf("%q must not be settable via update", f)
		}
	}
}

func TestBody(t *testing.T) {
	p := Policy{Name: "Eng", MinLength: 12}

	b := Body(p, nil)
	if _, ok := b["groupIds"]; ok {
		t.Error("nil groupIDs must omit groupIds so bindings are left alone")
	}

	b = Body(p, []string{})
	got, ok := b["groupIds"].([]string)
	if !ok || len(got) != 0 {
		t.Errorf("empty (non-nil) groupIDs must send an empty array to clear bindings, got %#v", b["groupIds"])
	}

	b = Body(p, []string{"g1"})
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out struct {
		GroupIDs []string `json:"groupIds"`
		Policy   Policy   `json:"policy"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.GroupIDs) != 1 || out.GroupIDs[0] != "g1" || out.Policy.Name != "Eng" {
		t.Errorf("round-trip mismatch: %+v", out)
	}
}

func TestDiff(t *testing.T) {
	before := Policy{MinLength: 8, Name: "old"}
	after := Policy{MinLength: 16, Name: "new", EnableMinLength: true}
	got := Diff(before, after)
	joined := strings.Join(got, "\n")
	for _, want := range []string{"minLength: 8 -> 16", "name: old -> new", "enableMinLength: false -> true"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Diff missing %q, got:\n%s", want, joined)
		}
	}
	if len(Diff(before, before)) != 0 {
		t.Error("identical policies must diff empty")
	}
}

func TestDescribe(t *testing.T) {
	d, _ := ParseDetail(json.RawMessage(liveDetailResponse))
	got := d.Policy.Describe()
	for _, want := range []string{"min length 8", "lockout after 6 attempts"} {
		if !strings.Contains(got, want) {
			t.Errorf("Describe = %q, missing %q", got, want)
		}
	}
	// Expiration is disabled on this policy, so it must not be advertised.
	if strings.Contains(got, "expires") {
		t.Errorf("Describe reported a disabled setting: %q", got)
	}
	if got := (Policy{}).Describe(); got != "no requirements enabled" {
		t.Errorf("empty policy Describe = %q", got)
	}
}

func TestFindByName(t *testing.T) {
	items := []ListItem{{ObjectID: "1", Name: "Engineering"}, {ObjectID: "2", Name: "Sales"}}
	if it, ok := FindByName(items, "engineering"); !ok || it.ObjectID != "1" {
		t.Errorf("case-insensitive match failed: %+v %v", it, ok)
	}
	if _, ok := FindByName(items, "nope"); ok {
		t.Error("unexpected match")
	}
}

func TestBatchDeleteBody(t *testing.T) {
	b := BatchDeleteBody([]string{"a", "b"})
	raw, _ := json.Marshal(b)
	if string(raw) != `{"objectIds":["a","b"]}` {
		t.Errorf("BatchDeleteBody = %s", raw)
	}
}

func TestPrecedenceEntry_MarshalsAsBareArray(t *testing.T) {
	raw, err := json.Marshal([]PrecedenceEntry{{ObjectID: "a", Precedence: 1}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `[{"objectId":"a","precedence":1}]` {
		t.Errorf("precedence body = %s", raw)
	}
}
