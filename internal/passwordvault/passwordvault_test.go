package passwordvault

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseStatus(t *testing.T) {
	// The live shape from an activated org.
	got, err := ParseStatus(json.RawMessage(`{"isActive":true,"isPam":false,"jumpcloudSsoApplicationId":"6ab6b8afbb3721c754172949","vaultoneConsoleUrl":"https://jc-x.vault.jumpcloud.com/account/login"}`))
	if err != nil {
		t.Fatalf("ParseStatus error: %v", err)
	}
	if !got.IsActive || got.IsPAM || got.SSOAppID == "" || got.ConsoleURL == "" {
		t.Errorf("status = %+v", got)
	}

	// And from one that has not activated.
	got, err = ParseStatus(json.RawMessage(`{"isActive":false,"isPam":false,"jumpcloudSsoApplicationId":"","vaultoneConsoleUrl":""}`))
	if err != nil || got.IsActive {
		t.Errorf("inactive = %+v, %v", got, err)
	}

	// An unreadable body must NOT read as "not activated": that would send an
	// operator to enable something already on.
	if _, err := ParseStatus(json.RawMessage(`not json`)); err == nil {
		t.Error("an undecodable status must be an error, not an inactive status")
	}
}

func TestErrNotActivated(t *testing.T) {
	msg := ErrNotActivated().Error()
	for _, want := range []string{"not active", "/password-vault/status", "404", "console"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should mention %q: %s", want, msg)
		}
	}
}

func TestParseGroupID(t *testing.T) {
	for _, s := range []string{"1", "42", "56719"} {
		if n, err := ParseGroupID(s); err != nil || n <= 0 {
			t.Errorf("ParseGroupID(%q) = %d, %v", s, n, err)
		}
	}

	// The two shapes a user will try first, each named explicitly, because
	// the server's own answer to them is unhelpful.
	cases := map[string]string{
		"6ab6bdcd1d6931fd7863bd4f":             "object id",
		"00000000-0000-0000-0000-000000000000": "UUID",
		"":                                     "not a Password Vault group id",
		"abc":                                  "not a Password Vault group id",
		"0":                                    "not a Password Vault group id",
		"-3":                                   "not a Password Vault group id",
	}
	for in, want := range cases {
		_, err := ParseGroupID(in)
		if err == nil {
			t.Errorf("ParseGroupID(%q) should have failed", in)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ParseGroupID(%q) error should mention %q: %v", in, want, err)
		}
	}
	// The UUID message must point at Password Manager, since that is where a
	// user holding a UUID has come from.
	_, err := ParseGroupID("00000000-0000-0000-0000-000000000000")
	if !strings.Contains(err.Error(), "MANAGER") {
		t.Errorf("the UUID message should name Password Manager: %v", err)
	}
}

func TestTagsQuery(t *testing.T) {
	// The spec marks filter optional and the server rejects the call without
	// it; its error names a bare targetKind parameter that does not work.
	for _, k := range []string{"CREDENTIAL", "credential", " Resource "} {
		v, err := TagsQuery(k)
		if err != nil {
			t.Fatalf("TagsQuery(%q) error: %v", k, err)
		}
		f := v.Get("filter")
		if !strings.HasPrefix(f, "targetKind:eq:") {
			t.Errorf("TagsQuery(%q) = %q, want the filter form", k, f)
		}
		if strings.ToUpper(strings.TrimSpace(k)) != strings.TrimPrefix(f, "targetKind:eq:") {
			t.Errorf("TagsQuery(%q) did not normalise: %q", k, f)
		}
	}
	// This one the server DOES validate, so rejecting locally keeps the
	// message useful.
	if _, err := TagsQuery("FOLDER"); err == nil {
		t.Error("an invalid target kind should be rejected")
	}
	if _, err := TagsQuery(""); err == nil {
		t.Error("an empty target kind should be rejected")
	}
}

func TestAssignableQuery(t *testing.T) {
	v, err := AssignableQuery("credential")
	if err != nil || v.Get("filter") != "category:eq:CREDENTIAL" {
		t.Errorf("AssignableQuery = %q, %v", v.Get("filter"), err)
	}
	if _, err := AssignableQuery("  "); err == nil {
		t.Error("an empty category should be rejected")
	}
	// An unrecognised category is deliberately NOT rejected: the server
	// accepts anything and returns an empty list, so refusing here would
	// diverge from what the API actually does. The trap is documented
	// instead, and surfaces repeat it.
	if _, err := AssignableQuery("NOT_A_REAL_CATEGORY"); err != nil {
		t.Errorf("an unknown category should pass through, since the server accepts it: %v", err)
	}
	for _, want := range []string{"does not validate", "empty list"} {
		if !strings.Contains(CategoryUnvalidated, want) {
			t.Errorf("CategoryUnvalidated should mention %q", want)
		}
	}
}

func TestParseList(t *testing.T) {
	rows, total, err := ParseList(json.RawMessage(`{"results":[{"id":1}],"totalCount":4}`), "users")
	if err != nil || len(rows) != 1 || total != 4 {
		t.Errorf("= %d rows, total %d, %v", len(rows), total, err)
	}
	// The self-injection case: one row, zero matches. A caller reading
	// len(results) would report a match that does not exist.
	rows, total, err = ParseList(json.RawMessage(`{"results":[{"id":56719}],"totalCount":0}`), "users")
	if err != nil || len(rows) != 1 || total != 0 {
		t.Errorf("injection case = %d rows, total %d, %v", len(rows), total, err)
	}
	if _, _, err := ParseList(json.RawMessage(`[]`), "users"); err == nil {
		t.Error("a bare array is not this envelope")
	}
	if _, _, err := ParseList(json.RawMessage(`{`), "users"); err == nil {
		t.Error("a truncated body must be an error, not an empty list")
	}
}

func TestParseResultsOnly(t *testing.T) {
	rows, err := ParseResultsOnly(json.RawMessage(`{"results":[{"id":1},{"id":2}]}`), "tags")
	if err != nil || len(rows) != 2 {
		t.Errorf("= %d, %v", len(rows), err)
	}
	if _, err := ParseResultsOnly(json.RawMessage(`{"results":"nope"}`), "tags"); err == nil {
		t.Error("a wrong-typed results field must be an error")
	}
}

func TestParseWrapped(t *testing.T) {
	inner, err := ParseWrapped(json.RawMessage(`{"values":{"allowCredentialExport":true}}`), "values", "tenant settings")
	if err != nil {
		t.Fatalf("ParseWrapped error: %v", err)
	}
	if !strings.Contains(string(inner), "allowCredentialExport") {
		t.Errorf("inner = %s", inner)
	}
	// A missing key is a changed envelope, which is worth saying rather than
	// silently returning nothing.
	if _, err := ParseWrapped(json.RawMessage(`{"other":{}}`), "values", "tenant settings"); err == nil {
		t.Error("a missing wrapper key must be an error")
	}
	if _, err := ParseWrapped(json.RawMessage(`nope`), "values", "tenant settings"); err == nil {
		t.Error("an undecodable body must be an error")
	}
}

func TestTenantSettingsBody(t *testing.T) {
	// The GET nests under "values" and the PUT takes the same shape. Getting
	// that wrong writes nothing and reports success.
	b, err := TenantSettingsBody(json.RawMessage(`{"allowCredentialExport":false}`))
	if err != nil {
		t.Fatalf("TenantSettingsBody error: %v", err)
	}
	var env map[string]map[string]any
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if _, ok := env["values"]; !ok {
		t.Errorf("body must nest under \"values\": %s", b)
	}
}

func TestEndpoints(t *testing.T) {
	cases := map[string]string{
		StatusEndpoint:             "/password-vault/status",
		DefaultPermsEndpoint:       "/password-vault/tenant-settings/default-permissions",
		ActivePWMTenantsEndpoint:   "/password-vault/users/active-pwm-tenants",
		GroupsAllEndpoint:          "/password-vault/groups/all",
		GroupEndpoint(7):           "/password-vault/groups/7",
		GroupMembersEndpoint(7):    "/password-vault/groups/7/members",
		GroupResourcesEndpoint(7):  "/password-vault/groups/7/resources",
		GroupAssignableEndpoint(7): "/password-vault/groups/7/assignable-resources",
		GroupDeactivateEndpoint(7): "/password-vault/groups/7/deactivate",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("endpoint = %q, want %q", got, want)
		}
	}
}

func TestDocumentedTraps(t *testing.T) {
	// These constants are the contract surfaces repeat to operators. If one
	// stops saying the thing it exists to say, the trap quietly returns.
	if !strings.Contains(SelfInjected, "trust the count") {
		t.Errorf("SelfInjected should tell the reader what to trust: %q", SelfInjected)
	}
	for _, want := range []string{"ignores sort", "unpaginated"} {
		if !strings.Contains(PaginationUnsafe, want) {
			t.Errorf("PaginationUnsafe should mention %q: %q", want, PaginationUnsafe)
		}
	}
}

func TestErrNotVaultObjectID(t *testing.T) {
	// A number is the mistake somebody makes after using groups or users in
	// the same area, so it gets its own message.
	err := ErrNotVaultObjectID("credential", "42")
	if !strings.Contains(err.Error(), "only users and groups are numbered") {
		t.Errorf("a numeric id should explain the split: %v", err)
	}
	err = ErrNotVaultObjectID("folder", "nonsense")
	if !strings.Contains(err.Error(), "24-character hex") {
		t.Errorf("message should name the shape: %v", err)
	}
}

func TestDetailReadRefused(t *testing.T) {
	// The exact server text, reproduced against a record granting the
	// permission it asks for.
	refusal := errors.New(`JumpCloud API error (HTTP 400) /password-vault/credentials/x: {"message":"For this resource , you need permissions View Detail.","status":"INVALID_ARGUMENT"}`)
	if !DetailReadRefused(refusal) {
		t.Error("the View Detail refusal should be recognised")
	}
	for _, other := range []error{nil, errors.New("connection reset"),
		errors.New(`{"message":"Folder not found.","status":"INVALID_ARGUMENT"}`)} {
		if DetailReadRefused(other) {
			t.Errorf("unrelated error read as the refusal: %v", other)
		}
	}

	// The message must say it is not the operator's to fix, or they will go
	// and edit an access policy that is already correct.
	msg := ErrDetailReadRefused("credential", "Use the listing for metadata.").Error()
	for _, want := range []string{"holds", "defect", "Use the listing"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should contain %q: %s", want, msg)
		}
	}
}

func TestFolderNotFoundIs400(t *testing.T) {
	if !FolderNotFoundIs400(errors.New(`{"message":"Folder not found.","status":"INVALID_ARGUMENT"}`)) {
		t.Error("the folder absence signal should be recognised")
	}
	if FolderNotFoundIs400(errors.New(`{"message":"Not Found"}`)) {
		t.Error("a plain 404 is the credential/website signal, not the folder one")
	}
}

func TestParseHistory(t *testing.T) {
	items, token, err := ParseHistory(json.RawMessage(`{"continuationToken":"abc","items":[{"version":"1"},{"version":"2"}]}`))
	if err != nil || len(items) != 2 || token != "abc" {
		t.Errorf("= %d items, token %q, %v", len(items), token, err)
	}
	// An empty history is legitimate; an unreadable one is not.
	items, token, err = ParseHistory(json.RawMessage(`{"items":[]}`))
	if err != nil || len(items) != 0 || token != "" {
		t.Errorf("empty = %d, %q, %v", len(items), token, err)
	}
	if _, _, err := ParseHistory(json.RawMessage(`[]`)); err == nil {
		t.Error("a bare array must be an error")
	}
}

func TestParseWrappedFolderAndColumns(t *testing.T) {
	inner, err := ParseWrappedFolder(json.RawMessage(`{"folder":{"id":"abc","name":"x"}}`))
	if err != nil || !strings.Contains(string(inner), `"abc"`) {
		t.Errorf("folder unwrap = %s, %v", inner, err)
	}
	inner, err = ParseColumns(json.RawMessage(`{"columns":{"Name":"string"}}`))
	if err != nil || !strings.Contains(string(inner), "Name") {
		t.Errorf("columns unwrap = %s, %v", inner, err)
	}
	if _, err := ParseWrappedFolder(json.RawMessage(`{"notfolder":{}}`)); err == nil {
		t.Error("a changed envelope must be an error")
	}
}
