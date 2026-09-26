package passwordvault

import (
	"encoding/json"
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
