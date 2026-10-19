package agents

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The live create response, with the secret shape it actually returns.
const liveCreateResponse = `{"clientId":"agent-6ad62d452afdcd6b70fc69c3-ac06a069410adf14",
  "clientSecret":"s3cr3t-value-not-real-abc","expiresAt":"2026-11-18T14:46:30.289936Z",
  "tokenEndpointAuthMethod":"client_secret_basic","authType":"client_credentials"}`

const testSecret = "s3cr3t-value-not-real-abc"

// --- The secret must be structurally unreachable -------------------------

// The guard that matters most in this file.
//
// encoding/json cannot reach an unexported field, so marshalling a
// NewCredential — or handing it to any generic printer, logger or output
// formatter — cannot emit the secret. This is not theoretical: while probing
// this endpoint, a generic "print the response" helper dumped a live
// clientSecret into a terminal transcript. The response shape invites it.
func TestNewCredential_MarshalsWithoutTheSecret(t *testing.T) {
	c, err := ParseNewCredential(json.RawMessage(liveCreateResponse))
	if err != nil {
		t.Fatalf("ParseNewCredential: %v", err)
	}
	if c.Secret() != testSecret {
		t.Fatalf("the secret did not survive parsing; this test would pass for the wrong reason")
	}

	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(raw), testSecret) {
		t.Errorf("json.Marshal emitted the secret: %s", raw)
	}
	// Check by key too, so a rename cannot slip past the value check.
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	for k := range fields {
		switch strings.ToLower(k) {
		case "secret", "clientsecret", "password":
			t.Errorf("marshalled output carries the key %q", k)
		}
	}
	// And the harmless fields must still be there, or the type is useless.
	if fields["clientId"] == nil {
		t.Error("clientId was not marshalled, so the type cannot report what it created")
	}
}

// A %v or %s on the value must not leak it either — the usual way a secret
// reaches a log line.
func TestNewCredential_StringAndVerbsDoNotLeak(t *testing.T) {
	c, err := ParseNewCredential(json.RawMessage(liveCreateResponse))
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{
		c.String(),
		fmt.Sprintf("%v", c),
		fmt.Sprintf("%s", c),
		fmt.Sprintf("%+v", c),
		fmt.Sprint(c),
	} {
		if strings.Contains(rendered, testSecret) {
			t.Errorf("a format verb leaked the secret: %q", rendered)
		}
	}
	if !strings.Contains(c.String(), c.ClientID) {
		t.Error("String() does not name the credential, so it is not useful for reporting")
	}
}

// %#v reaches unexported fields by design, so it is NOT safe and no surface
// uses it. Asserted so the claim in the comments stays honest rather than
// being quietly wrong.
func TestNewCredential_GoSyntaxVerbIsKnownUnsafe(t *testing.T) {
	c, _ := ParseNewCredential(json.RawMessage(liveCreateResponse))
	if !strings.Contains(fmt.Sprintf("%#v", c), testSecret) {
		t.Skip("the Go-syntax verb no longer exposes unexported fields; the warning in the comments can go")
	}
}

// --- Parsing --------------------------------------------------------------

func TestParseNewCredential(t *testing.T) {
	c, err := ParseNewCredential(json.RawMessage(liveCreateResponse))
	if err != nil {
		t.Fatalf("ParseNewCredential: %v", err)
	}
	if c.ClientID != "agent-6ad62d452afdcd6b70fc69c3-ac06a069410adf14" {
		t.Errorf("ClientID = %q", c.ClientID)
	}
	if !c.HasSecret() {
		t.Error("HasSecret() = false on a response that carried one")
	}
	if c.NeverExpires() {
		t.Error("NeverExpires() = true on a credential with an expiry")
	}
}

// A lifetime of 0 produces a credential with NO expiresAt at all — verified
// live. Reading that as "expired" or as an error would be wrong.
func TestParseNewCredential_NoExpiryMeansNeverExpires(t *testing.T) {
	c, err := ParseNewCredential(json.RawMessage(
		`{"clientId":"agent-x-1","clientSecret":"abc","tokenEndpointAuthMethod":"client_secret_basic"}`))
	if err != nil {
		t.Fatalf("ParseNewCredential: %v", err)
	}
	if !c.NeverExpires() {
		t.Error("a credential with no expiresAt did not report NeverExpires()")
	}
	if !strings.Contains(c.String(), "never") {
		t.Errorf("String() does not say it never expires: %q", c.String())
	}
}

// Absent data is not evidence of a negative, and here the stakes are higher
// than usual: the create is the only call that returns a secret, so a
// response without one means the operator holds nothing usable.
func TestParseNewCredential_MissingSecretIsAnError(t *testing.T) {
	_, err := ParseNewCredential(json.RawMessage(`{"clientId":"agent-x-1","expiresAt":"2027-01-01T00:00:00Z"}`))
	if err == nil {
		t.Fatal("a response with no secret was accepted as a usable credential")
	}
	if !strings.Contains(err.Error(), "delete it and create another") {
		t.Errorf("the error does not say how to recover: %v", err)
	}
}

func TestParseNewCredential_MissingClientIDIsAnError(t *testing.T) {
	if _, err := ParseNewCredential(json.RawMessage(`{"clientSecret":"abc"}`)); err == nil {
		t.Error("a response with no client id was accepted")
	}
}

// A create that cannot be decoded may still have CREATED the credential, and
// an agent can hold only one — so the error has to tell the operator to look
// before retrying, or their retry fails with "credential already exists".
func TestParseNewCredential_UndecodableSaysToCheckFirst(t *testing.T) {
	_, err := ParseNewCredential(json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("an unreadable response was accepted")
	}
	if !strings.Contains(err.Error(), "credentials list") {
		t.Errorf("the error does not tell the operator to check what exists: %v", err)
	}
	if !strings.Contains(err.Error(), "only one") {
		t.Errorf("the error does not explain why a blind retry would fail: %v", err)
	}
}

// --- Lifetimes ------------------------------------------------------------

func TestBuildCredentialCreate_ValidatesTheEnum(t *testing.T) {
	for _, ok := range CredentialLifetimes() {
		raw, err := BuildCredentialCreate(ok)
		if err != nil {
			t.Errorf("lifetime %d was rejected: %v", ok, err)
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if body["lifetimeDays"] == nil {
			t.Errorf("lifetime %d was not sent", ok)
		}
	}
	// Each of these is refused by the server with
	// "lifetimeDays must be one of: 0, 30, 60, 90, 365".
	for _, bad := range []int{1, 15, 45, 180, 364, 400, 1000} {
		if _, err := BuildCredentialCreate(bad); err == nil {
			t.Errorf("lifetime %d was accepted, but the API refuses it", bad)
		}
	}
}

func TestBuildCredentialCreate_UnsetTakesTheServerDefault(t *testing.T) {
	// -1 is the "not specified" marker, because 0 is a real value meaning
	// "never expires" and cannot double as unset.
	raw, err := BuildCredentialCreate(-1)
	if err != nil {
		t.Fatalf("BuildCredentialCreate(-1): %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 {
		t.Errorf("body = %v, want empty so the server applies its own default", body)
	}
}

func TestBuildCredentialCreate_ZeroIsAllowedAndMeansNever(t *testing.T) {
	raw, err := BuildCredentialCreate(0)
	if err != nil {
		t.Fatalf("a lifetime of 0 was rejected, but the API accepts it: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["lifetimeDays"] != float64(0) {
		t.Errorf("body = %v, want lifetimeDays 0 sent explicitly — omitting it would silently "+
			"get the 365-day default instead of a non-expiring credential", body)
	}
	if !strings.Contains(LifetimeZeroNeverExpires, "NEVER EXPIRES") {
		t.Error("the constant no longer states what 0 means")
	}
}

func TestBuildCredentialCreate_ErrorNamesTheAllowedValues(t *testing.T) {
	_, err := BuildCredentialCreate(45)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"0", "30", "60", "90", "365", "never expires"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

// --- Updating -------------------------------------------------------------

func TestBuildCredentialPatch(t *testing.T) {
	for _, m := range TokenEndpointAuthMethods() {
		raw, err := BuildCredentialPatch(m, "", -1)
		if err != nil {
			t.Errorf("auth method %q was rejected: %v", m, err)
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if body["tokenEndpointAuthMethod"] != m {
			t.Errorf("body = %v", body)
		}
		// The server requires this field on every update, even an empty one.
		if len(body) != 1 {
			t.Errorf("body = %v, want only the auth method — the other fields are ignored", body)
		}
	}
	if _, err := BuildCredentialPatch("private_key_jwt", "", -1); err == nil {
		t.Error("an auth method outside the enum was accepted")
	}
	if _, err := BuildCredentialPatch("", "", -1); err == nil {
		t.Error("an update with no auth method was accepted, but the API requires one")
	}
}

// The server accepts status and lifetimeDays in an update and SILENTLY
// IGNORES them, returning 200 with nothing changed. Forwarding them would be
// reporting success for a change that was discarded — the same defect class
// as Password Vault's writes. The honest answer is that the operation does
// not exist.
func TestBuildCredentialPatch_RefusesTheFieldsTheAPIIgnores(t *testing.T) {
	_, err := BuildCredentialPatch("client_secret_post", "inactive", -1)
	if err == nil {
		t.Fatal("a status change was accepted, but the API ignores it and reports success")
	}
	if !strings.Contains(err.Error(), "ignores it") {
		t.Errorf("the error does not explain that the API discards it: %v", err)
	}
	if !strings.Contains(err.Error(), "Delete the credential") {
		t.Errorf("the error does not say what to do instead: %v", err)
	}

	_, err = BuildCredentialPatch("client_secret_post", "", 90)
	if err == nil {
		t.Fatal("a lifetime change was accepted, but the API ignores it")
	}
	if !strings.Contains(err.Error(), "cannot be changed after it is created") {
		t.Errorf("the error does not explain the limitation: %v", err)
	}
}

// --- Endpoints and recognisers -------------------------------------------

func TestAgentCredentialEndpoint(t *testing.T) {
	got := AgentCredentialEndpoint("aaa", "agent-aaa-1234")
	if got != "/agents/aaa/inbound-credentials/agent-aaa-1234" {
		t.Errorf("endpoint = %q", got)
	}
}

func TestCredentialRecognisers(t *testing.T) {
	if !CredentialAlreadyExists(errString(`{"message":"credential already exists"}`)) {
		t.Error("CredentialAlreadyExists did not recognise the one-per-agent refusal")
	}
	if CredentialAlreadyExists(errString(`{"message":"not found"}`)) {
		t.Error("CredentialAlreadyExists matched an unrelated error")
	}
	// A delete is not idempotent; the second answers "not found".
	if !CredentialNotFound(errString(`{"message":"not found"}`)) {
		t.Error("CredentialNotFound did not recognise the repeated-delete refusal")
	}
	if !CredentialNotFound(errString(`{"message":"Not Found"}`)) {
		t.Error("CredentialNotFound is case-sensitive, and the API uses both spellings")
	}
}

func TestConstantsStateTheConstraints(t *testing.T) {
	// These strings are quoted in help text and errors; a surface that lost
	// them would stop explaining the area's two sharpest edges.
	if !strings.Contains(OneCredentialPerAgent, "only one") {
		t.Error("OneCredentialPerAgent no longer states the constraint")
	}
	if !strings.Contains(OneCredentialPerAgent, "cannot authenticate between") {
		t.Error("OneCredentialPerAgent no longer warns about the gap during rotation")
	}
	if !strings.Contains(CredentialPatchChangesOnlyTheAuthMethod, "ignored") {
		t.Error("CredentialPatchChangesOnlyTheAuthMethod no longer says the other fields are dropped")
	}
}

// --- The sanitiser --------------------------------------------------------

// No read returns a secret today. This enforces the invariant rather than
// assuming it, because `credentials list` and `credentials update` both
// print the record through jc's ordinary output path — so an API that
// started including a secret would leak it in every output format at once,
// and no test of the fixtures would notice.
func TestSanitizeCredential_RemovesEverySecretBearingKey(t *testing.T) {
	for _, key := range []string{
		"clientSecret", "client_secret", "secret", "password", "privateKey", "private_key",
	} {
		raw := json.RawMessage(`{"clientId":"agent-x-1","status":"active","` + key + `":"` + testSecret + `"}`)
		got := SanitizeCredential(raw)

		if strings.Contains(string(got), testSecret) {
			t.Errorf("%s survived sanitising: %s", key, got)
		}
		var rec map[string]any
		if err := json.Unmarshal(got, &rec); err != nil {
			t.Fatalf("sanitised output does not decode: %v", err)
		}
		if _, ok := rec[key]; ok {
			t.Errorf("the key %q is still present", key)
		}
		// The useful fields must survive, or the command has nothing to show.
		if rec["clientId"] != "agent-x-1" || rec["status"] != "active" {
			t.Errorf("sanitising dropped harmless fields: %v", rec)
		}
	}
}

// tokenEndpointAuthMethod's VALUE is "client_secret_basic". A sanitiser that
// matched on substrings would mangle it; this one matches keys.
func TestSanitizeCredential_LeavesTheLiveRecordAlone(t *testing.T) {
	const live = `{"authType":"client_credentials","clientId":"agent-x-1",` +
		`"createdAt":"2026-09-14T16:16:04Z","expiresAt":"2027-09-14T16:16:04Z",` +
		`"status":"active","tokenEndpointAuthMethod":"client_secret_basic",` +
		`"updatedAt":"2026-09-14T16:16:04Z"}`

	got := SanitizeCredential(json.RawMessage(live))
	if string(got) != live {
		t.Errorf("a record with no secret was rewritten.\n got: %s\nwant: %s", got, live)
	}
	if !strings.Contains(string(got), "client_secret_basic") {
		t.Error("the auth method was stripped; it names a method and discloses nothing")
	}
}

func TestSanitizeCredential_NonObjectIsReturnedUnchanged(t *testing.T) {
	// Not an object, so there is no field to strip and nothing to hide.
	for _, in := range []string{`"a string"`, `[1,2,3]`, `not json`} {
		if got := string(SanitizeCredential(json.RawMessage(in))); got != in {
			t.Errorf("SanitizeCredential(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestSanitizeCredentials_AppliesToEveryRow(t *testing.T) {
	rows := []json.RawMessage{
		json.RawMessage(`{"clientId":"a","clientSecret":"` + testSecret + `"}`),
		json.RawMessage(`{"clientId":"b"}`),
	}
	got := SanitizeCredentials(rows)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	for i, r := range got {
		if strings.Contains(string(r), testSecret) {
			t.Errorf("row %d still carries the secret: %s", i, r)
		}
	}
}
