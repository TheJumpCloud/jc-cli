package apple_mdm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEndpoints(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{ADETokenEndpoint("tok"), "/applemdms/ade_tokens/tok"},
		{ADESettingsEndpoint("tok"), "/applemdms/ade_tokens/tok/ade_settings"},
		{ADESettingEndpoint("tok", "DARWIN"), "/applemdms/ade_tokens/tok/ade_settings/DARWIN"},
		{ManagerABMDevicesEndpoint("mdm"), "/applemdms/mdm/abm_devices"},
		{BackgroundTasksEndpoint("dev"), "/applemdms/dev/background_tasks"},
		{ADUEEndpoint("org"), "/applemdms/org/account-driven-service-discovery"},
		{ADUEConfigEndpoint("org"), "/applemdms/org/account-driven-service-discovery/config"},
		{ValidateServiceDiscoveryURLEndpoint("mdm"), "/applemdms/mdm/validate-service-discovery-url"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q want %q", tc.got, tc.want)
		}
	}
}

// UNKNOWN is in the spec's enum and rejected by the API. Offering it would
// hand an operator a guaranteed 400 for one of three documented values.
func TestNormalizeOSFamily_UnknownIsRefusedLocally(t *testing.T) {
	_, err := NormalizeOSFamily("UNKNOWN")
	if err == nil {
		t.Fatal("UNKNOWN must be refused before it reaches the API")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("the error should say why UNKNOWN is refused, got %q", err)
	}
	for _, f := range OSFamilies {
		if f == "UNKNOWN" {
			t.Fatal("UNKNOWN must not appear in OSFamilies")
		}
	}
}

// DARWIN is the kernel, not the product. "macos" is what an operator will
// type, and the API does not accept it.
func TestNormalizeOSFamily_AcceptsWhatOperatorsType(t *testing.T) {
	for in, want := range map[string]string{
		"macos": "DARWIN", "macOS": "DARWIN", "mac": "DARWIN", "osx": "DARWIN",
		"darwin": "DARWIN", "DARWIN": "DARWIN",
		"ios": "IOS", "iOS": "IOS", "iPadOS": "IOS", "IOS": "IOS",
	} {
		got, err := NormalizeOSFamily(in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	if _, err := NormalizeOSFamily("tvos"); err == nil {
		t.Error("tvos is not a documented family and must not be guessed into one")
	}
	if _, err := NormalizeOSFamily(""); err == nil {
		t.Error("an empty family must ask which one")
	}
}

// A list read and a single read answer under different keys. A decoder that
// accepted either would silently return one row where the caller asked for a
// collection, or vice versa.
func TestEnvelopes_SingularAndPluralDoNotInterchange(t *testing.T) {
	list := json.RawMessage(`{"totalCount":2,"adeTokens":[{"objectId":"a"},{"objectId":"b"}]}`)
	single := json.RawMessage(`{"adeToken":{"objectId":"a"}}`)

	rows, total, err := ParseADETokens(list)
	if err != nil || len(rows) != 2 || total != 2 {
		t.Fatalf("list: rows=%d total=%d err=%v", len(rows), total, err)
	}
	if _, _, err := ParseADETokens(single); err == nil {
		t.Error("the single-read envelope must not decode as a list")
	}
	if _, err := ParseADEToken(list); err == nil {
		t.Error("the list envelope must not decode as a single read")
	}
	if _, err := ParseADEToken(single); err != nil {
		t.Errorf("single: %v", err)
	}
}

// totalCount is the server's count, not the page length. Trusting len(rows)
// would under-report on a paged response.
func TestParseADETokens_PrefersTheServerCount(t *testing.T) {
	rows, total, err := ParseADETokens(json.RawMessage(`{"totalCount":7,"adeTokens":[{"objectId":"a"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || total != 7 {
		t.Errorf("rows=%d total=%d, want 1 and 7", len(rows), total)
	}
}

// The core convention: a response that cannot be decoded is an error, never
// an empty collection. "This org has no ABM devices" and "jc could not read
// the ABM device list" must not look the same.
func TestParsers_UndecodableIsAnErrorNotEmptiness(t *testing.T) {
	for name, fn := range map[string]func(json.RawMessage) ([]json.RawMessage, int, error){
		"ADETokens":       ParseADETokens,
		"ADESettings":     ParseADESettings,
		"ABMDevices":      ParseABMDevices,
		"BackgroundTasks": ParseBackgroundTasks,
	} {
		for _, bad := range []string{`{}`, `{"wrongKey":[]}`, `not json`, `{"abmDevices":{}}`, `[]`} {
			rows, total, err := fn(json.RawMessage(bad))
			if err == nil && name == "ABMDevices" && bad == `{"abmDevices":{}}` {
				t.Errorf("%s: an object where an array belongs must not decode", name)
			}
			if err == nil && (bad == `{}` || bad == `{"wrongKey":[]}` || bad == `not json` || bad == `[]`) {
				t.Errorf("%s: %s decoded to %d rows (total %d) instead of erroring",
					name, bad, len(rows), total)
			}
		}
	}
	// An empty list is a real answer and must NOT be an error.
	rows, total, err := ParseABMDevices(json.RawMessage(`{"abmDevices":[],"totalCount":0}`))
	if err != nil || len(rows) != 0 || total != 0 {
		t.Errorf("a genuinely empty list must decode cleanly: rows=%d total=%d err=%v",
			len(rows), total, err)
	}
}

func TestParseOrgNames(t *testing.T) {
	names, total, err := ParseOrgNames(json.RawMessage(`{"totalCount":1,"orgNames":["KLAASSEN CONSULTING"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(names) != 1 || names[0] != "KLAASSEN CONSULTING" {
		t.Errorf("got %v (total %d)", names, total)
	}
	if _, _, err := ParseOrgNames(json.RawMessage(`{"orgNames":[{"name":"x"}]}`)); err == nil {
		t.Error("objects where strings belong must error, not stringify")
	}
}

// The live tenant's own numbers: one token, expiring, and NOT valid. A
// summary that reports "1 ADE token" without the breakdown says nothing about
// whether enrollment works.
func TestADETokenStats_CountedIsNotHealthy(t *testing.T) {
	s, err := ParseADETokenStats(json.RawMessage(
		`{"expiredCount":0,"expiringCount":1,"totalCount":1,"validCount":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalCount != 1 || s.ExpiringCount != 1 || s.ValidCount != 0 {
		t.Fatalf("decoded wrong: %+v", s)
	}
	if !s.NeedsAttention() {
		t.Error("a token that is expiring and not valid needs attention")
	}
	if ok := (ADETokenStats{TotalCount: 2, ValidCount: 2}).NeedsAttention(); ok {
		t.Error("two valid tokens need no attention")
	}
	// Tokens exist but none is valid: attention, even with nothing expiring.
	if !(ADETokenStats{TotalCount: 1, ValidCount: 0}).NeedsAttention() {
		t.Error("tokens present with none valid needs attention")
	}
	// No tokens at all is not a fault.
	if (ADETokenStats{}).NeedsAttention() {
		t.Error("an org with no ADE tokens is not in a faulty state")
	}
}

// Absence arrives as HTTP 200. Code that trusts the status reports success for
// a tenant with no service-discovery URL at all.
func TestServiceDiscovery_NotFoundIsNotSuccess(t *testing.T) {
	r, err := ParseServiceDiscovery(json.RawMessage(`{"responseCode":"NOT_FOUND"}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.OK() {
		t.Error("NOT_FOUND must not read as OK")
	}
	if !strings.Contains(r.Describe(), "no service discovery URL") {
		t.Errorf("describe should say what is missing, got %q", r.Describe())
	}

	ok, err := ParseServiceDiscovery(json.RawMessage(`{"responseCode":"OK"}`))
	if err != nil || !ok.OK() {
		t.Errorf("OK must read as OK: %+v %v", ok, err)
	}

	// An unrecognised code is NOT success. Matching "not NOT_FOUND" would
	// call a future failure code a pass.
	other, err := ParseServiceDiscovery(json.RawMessage(`{"responseCode":"SOME_NEW_FAILURE"}`))
	if err != nil {
		t.Fatal(err)
	}
	if other.OK() {
		t.Error("an unrecognised responseCode must not read as success")
	}
	if !strings.Contains(other.Describe(), "SOME_NEW_FAILURE") {
		t.Errorf("describe should surface the unrecognised code, got %q", other.Describe())
	}

	if _, err := ParseServiceDiscovery(json.RawMessage(`{}`)); err == nil {
		t.Error("a response with no responseCode must error rather than read as failure")
	}
}

// The config is a JSON document inside a JSON string. Emitting the outer
// object hands the caller an escaped blob.
func TestParseADUEConfig_UnwrapsTheInnerDocument(t *testing.T) {
	raw := json.RawMessage(`{"jsonConfig":"{\"Servers\":[{\"BaseURL\":\"https://example.test/e\",\"Version\":\"mdm-byod\"}]}"}`)
	inner, err := ParseADUEConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Servers []struct {
			BaseURL string
			Version string
		}
	}
	if err := json.Unmarshal(inner, &cfg); err != nil {
		t.Fatalf("the unwrapped config should be a document, not a string: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Version != "mdm-byod" {
		t.Errorf("decoded %+v", cfg)
	}
	// A string that is not JSON must not be passed off as a document.
	if _, err := ParseADUEConfig(json.RawMessage(`{"jsonConfig":"not json"}`)); err == nil {
		t.Error("an unparseable jsonConfig must error")
	}
	if _, err := ParseADUEConfig(json.RawMessage(`{"jsonConfig":""}`)); err == nil {
		t.Error("an empty jsonConfig must error rather than yield an empty document")
	}
}

func TestParseADUERedirect(t *testing.T) {
	u, err := ParseADUERedirect(json.RawMessage(`{"redirectUrl":"https://example.test/x"}`))
	if err != nil || u != "https://example.test/x" {
		t.Fatalf("got %q err=%v", u, err)
	}
	if _, err := ParseADUERedirect(json.RawMessage(`{}`)); err == nil {
		t.Error("a missing redirectUrl must error, not return an empty string")
	}
}

// The ADUE pair are siblings in the path and share nothing in the body. A
// reviewer reading only the paths would reasonably expect one shape.
func TestADUE_SiblingEndpointsHaveUnrelatedShapes(t *testing.T) {
	redirect := json.RawMessage(`{"redirectUrl":"https://example.test/x"}`)
	config := json.RawMessage(`{"jsonConfig":"{\"Servers\":[]}"}`)

	if _, err := ParseADUEConfig(redirect); err == nil {
		t.Error("the redirect response must not decode as a config")
	}
	if _, err := ParseADUERedirect(config); err == nil {
		t.Error("the config response must not decode as a redirect")
	}
}

// Every identifier in this area is interpolated into a URL path, and the MCP
// server grants these reads as individually allowable tools — so an
// unvalidated id turns one read into a request-path builder. The traversal
// does not escape against JumpCloud today (four variants were refused with
// 400 or 404, because it does not normalise `..`), but that is the upstream
// server's behaviour, not jc's.
func TestRequireObjectID_RefusesAnythingThatIsNotAnID(t *testing.T) {
	for _, bad := range []string{
		"", "../../systemusers", "x/../../systemusers", "/systemusers",
		"%2e%2e%2fsystemusers", "6a7e1bb88cf0be0001a4e3cd/../..",
		"6a7e1bb88cf0be0001a4e3cd?limit=1", "6a7e1bb88cf0be0001a4e3c", // 23 chars
		"6a7e1bb88cf0be0001a4e3cdd", // 25 chars
		"zzzzzzzzzzzzzzzzzzzzzzzz",  // right length, not hex
		"6a7e1bb8 8cf0be0001a4e3c",  // space
	} {
		if IsObjectID(bad) {
			t.Errorf("%q must not pass as an object id", bad)
		}
		if err := RequireObjectID("an ADE token", bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	for _, good := range []string{
		"6a7e1bb88cf0be0001a4e3cd", "690b6c939c0bf5000100039a",
		"6A7E1BB88CF0BE0001A4E3CD", // the API's ids are lower case, but hex is hex
	} {
		if !IsObjectID(good) {
			t.Errorf("%q is a real object id and was refused", good)
		}
		if err := RequireObjectID("an ADE token", good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
}

// The message has to name what was wrong, or an operator pasting a device
// name instead of an id gets no idea what the command wanted.
func TestErrNotObjectID_SaysWhatIsExpected(t *testing.T) {
	err := RequireObjectID("an ADE token", "my-token")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"my-token", "24-character hex", "ADE token"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %q, got %q", want, err)
		}
	}
}
