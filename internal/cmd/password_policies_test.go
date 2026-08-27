package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pwPolicyServer mocks the password policy endpoints. It is deliberately
// faithful to the live API in the two ways that matter:
//
//   - the list endpoint answers a {results:[…]} envelope carrying the FLAT,
//     sparse projection, not the {objectId, policy:{…}} single-read shape;
//   - the PUT is treated as a FULL REPLACE, so a partial body visibly drops
//     every field the caller failed to send. That is the semantic the CLI has
//     to survive, and live probing could not confirm it either way.
type pwPolicyServer struct {
	*httptest.Server
	policies map[string]map[string]any // objectId -> {policy, groups}
	lastPUT  map[string]any
	deleted  []string
}

func startPWPolicyServer(t *testing.T) *pwPolicyServer {
	t.Helper()

	fullPolicy := func(name string, isDefault bool, precedence, minLength int) map[string]any {
		return map[string]any{
			"allowUnenrolledMFAPasswordReset":        false,
			"allowUsernameSubstring":                 false,
			"daysAfterExpirationToSelfRecover":       -1,
			"daysBeforeExpirationToForceReset":       10,
			"default":                                isDefault,
			"description":                            "",
			"disallowCommonlyUsedPasswords":          false,
			"disallowSequentialOrRepetitiveChars":    false,
			"displayComplexityOnResetScreen":         true,
			"effectiveDate":                          "2026-07-16T23:36:44.820Z",
			"enableDaysAfterExpirationToSelfRecover": false,
			"enableDaysBeforeExpirationToForceReset": false,
			"enableLockoutTimeInSeconds":             true,
			"enableMaxHistory":                       false,
			"enableMaxLoginAttempts":                 true,
			"enableMinChangePeriodInDays":            false,
			"enableMinLength":                        true,
			"enablePasswordExpirationInDays":         false,
			"enableRecoveryEmail":                    true,
			"enableResetLockoutCounter":              true,
			"lockoutTimeInSeconds":                   3601,
			"maxHistory":                             3,
			"maxLoginAttempts":                       6,
			"minChangePeriodInDays":                  0,
			"minLength":                              minLength,
			"name":                                   name,
			"needsLowercase":                         false,
			"needsNumeric":                           false,
			"needsSymbolic":                          false,
			"needsUppercase":                         false,
			"passwordExpirationInDays":               90,
			"precedence":                             precedence,
			"resetLockoutCounterMinutes":             30,
		}
	}

	ps := &pwPolicyServer{
		policies: map[string]map[string]any{
			"67dc22433f87810001e2bf1c": {
				"cached":   false,
				"objectId": "67dc22433f87810001e2bf1c",
				"groups":   []any{},
				"policy":   fullPolicy("", true, 1, 8),
			},
			"67dc22433f87810001e2bf2d": {
				"cached":   false,
				"objectId": "67dc22433f87810001e2bf2d",
				"groups":   []any{map[string]any{"groupId": "664ab86614f2240001cb68ed", "name": "Engineering"}},
				"policy":   fullPolicy("Engineering", false, 2, 12),
			},
		},
	}

	ps.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/passwordpolicies")

		switch {
		case path == "" && r.Method == http.MethodGet:
			results := []map[string]any{}
			for _, p := range ps.policies {
				pol := p["policy"].(map[string]any)
				groups := p["groups"].([]any)
				// The live projection is flat and sparse: only enabled
				// requirements appear.
				row := map[string]any{
					"objectId":   p["objectId"],
					"name":       pol["name"],
					"precedence": pol["precedence"],
					"default":    pol["default"],
					"groupCount": len(groups),
				}
				if pol["enableMinLength"] == true {
					row["minLength"] = pol["minLength"]
					row["enableMinLength"] = true
				}
				results = append(results, row)
			}
			json.NewEncoder(w).Encode(map[string]any{"results": results})

		case path == "" && r.Method == http.MethodPost:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			ps.lastPUT = in
			json.NewEncoder(w).Encode(map[string]any{
				"objectId": "67dc22433f87810001e2bf99",
				"groups":   []any{},
				"policy":   in["policy"],
			})

		case path == "" && r.Method == http.MethodDelete:
			var in struct {
				ObjectIDs []string `json:"objectIds"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			if len(in.ObjectIDs) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"message":"objectIds required"}`))
				return
			}
			ps.deleted = append(ps.deleted, in.ObjectIDs...)
			for _, id := range in.ObjectIDs {
				delete(ps.policies, id)
			}
			w.Write([]byte(`{}`))

		case path == "/precedence/set" && r.Method == http.MethodPut:
			var in []map[string]any
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				// The precedence body is a BARE ARRAY; an object would mean
				// the CLI built the wrong shape.
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"message":"expected an array"}`))
				return
			}
			ps.lastPUT = map[string]any{"entries": in}
			w.Write([]byte(`{}`))

		case strings.HasPrefix(path, "/user/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(ps.policies["67dc22433f87810001e2bf1c"])

		case strings.HasPrefix(path, "/usergroup/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(ps.policies["67dc22433f87810001e2bf2d"])

		case r.Method == http.MethodGet:
			id := strings.TrimPrefix(path, "/")
			p, ok := ps.policies[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			json.NewEncoder(w).Encode(p)

		case r.Method == http.MethodPut:
			id := strings.TrimPrefix(path, "/")
			p, ok := ps.policies[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			ps.lastPUT = in
			// FULL REPLACE: whatever the caller sent becomes the policy.
			p["policy"] = in["policy"]
			if g, ok := in["groupIds"]; ok {
				groups := []any{}
				for _, id := range g.([]any) {
					groups = append(groups, map[string]any{"groupId": id, "name": "Engineering"})
				}
				p["groups"] = groups
			}
			json.NewEncoder(w).Encode(p)

		case r.Method == http.MethodDelete:
			id := strings.TrimPrefix(path, "/")
			ps.deleted = append(ps.deleted, id)
			delete(ps.policies, id)
			w.Write([]byte(`{}`))

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ps.Close)
	return ps
}

func runPWCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

func TestPasswordPolicies_List_UnwrapsResultsEnvelope(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startPWPolicyServer(t).URL)

	out, _, err := runPWCmd(t, "password-policies", "list")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("list must unwrap {results:[…]} into a bare array, got: %s", out)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 policies, got %d: %s", len(got), out)
	}
}

func TestPasswordPolicies_Get_ByID(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startPWPolicyServer(t).URL)

	out, _, err := runPWCmd(t, "password-policies", "get", "67dc22433f87810001e2bf2d")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	policy, _ := got["policy"].(map[string]any)
	if policy == nil || policy["name"] != "Engineering" {
		t.Errorf("unexpected policy: %s", out)
	}
}

func TestPasswordPolicies_Get_ByName(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startPWPolicyServer(t).URL)

	out, _, err := runPWCmd(t, "password-policies", "get", "engineering")
	if err != nil {
		t.Fatalf("name lookup should be case-insensitive: %v", err)
	}
	if !strings.Contains(out, "67dc22433f87810001e2bf2d") {
		t.Errorf("resolved to the wrong policy: %s", out)
	}
}

func TestPasswordPolicies_Get_UnknownName(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startPWPolicyServer(t).URL)

	_, _, err := runPWCmd(t, "password-policies", "get", "nope")
	if err == nil {
		t.Fatal("want an error for an unknown policy name")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v", err)
	}
}

func TestPasswordPolicies_ForUser(t *testing.T) {
	setupUsersTest(t)
	srv := startPWPolicyServer(t)
	overrideV2Client(t, srv.URL)
	overrideV1Client(t, startUsersServer(t, sampleUsers()).URL)

	out, _, err := runPWCmd(t, "password-policies", "for-user", "alice")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out, "67dc22433f87810001e2bf1c") {
		t.Errorf("expected the effective policy, got: %s", out)
	}
}

func TestPasswordPolicies_Update_SendsCompletePolicy(t *testing.T) {
	setupUsersTest(t)
	srv := startPWPolicyServer(t)
	overrideV2Client(t, srv.URL)

	_, _, err := runPWCmd(t, "password-policies", "update", "Engineering", "--min-length", "16", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	policy, _ := srv.lastPUT["policy"].(map[string]any)
	if policy == nil {
		t.Fatalf("no policy in PUT body: %#v", srv.lastPUT)
	}
	if policy["minLength"] != float64(16) {
		t.Errorf("minLength = %v, want 16", policy["minLength"])
	}
	// The whole object must go back, or the full-replace PUT silently wipes
	// everything the caller did not mention.
	if policy["lockoutTimeInSeconds"] != float64(3601) {
		t.Errorf("untouched lockoutTimeInSeconds lost: %v", policy["lockoutTimeInSeconds"])
	}
	if policy["resetLockoutCounterMinutes"] != float64(30) {
		t.Errorf("untouched resetLockoutCounterMinutes lost: %v", policy["resetLockoutCounterMinutes"])
	}
	if policy["daysAfterExpirationToSelfRecover"] != float64(-1) {
		t.Errorf("the -1 sentinel must survive, got %v", policy["daysAfterExpirationToSelfRecover"])
	}
}

func TestPasswordPolicies_Update_PreservesGroupBindings(t *testing.T) {
	setupUsersTest(t)
	srv := startPWPolicyServer(t)
	overrideV2Client(t, srv.URL)

	_, _, err := runPWCmd(t, "password-policies", "update", "Engineering", "--min-length", "16", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Reads report bindings under "groups"; writes take them under "groupIds".
	// An update that does not mention groups must still send them back.
	ids, ok := srv.lastPUT["groupIds"].([]any)
	if !ok || len(ids) != 1 || ids[0] != "664ab86614f2240001cb68ed" {
		t.Errorf("group bindings not preserved through update: %#v", srv.lastPUT["groupIds"])
	}
}

func TestPasswordPolicies_Update_SettingValueEnablesIt(t *testing.T) {
	setupUsersTest(t)
	srv := startPWPolicyServer(t)
	overrideV2Client(t, srv.URL)

	// Expiration starts disabled on the fixture.
	_, _, err := runPWCmd(t, "password-policies", "update", "Engineering", "--expiration-days", "45", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	policy, _ := srv.lastPUT["policy"].(map[string]any)
	if policy["passwordExpirationInDays"] != float64(45) {
		t.Errorf("passwordExpirationInDays = %v", policy["passwordExpirationInDays"])
	}
	if policy["enablePasswordExpirationInDays"] != true {
		t.Error("setting a value must switch its requirement on, else it is silently inert")
	}
}

func TestPasswordPolicies_Update_Disable(t *testing.T) {
	setupUsersTest(t)
	srv := startPWPolicyServer(t)
	overrideV2Client(t, srv.URL)

	_, _, err := runPWCmd(t, "password-policies", "update", "Engineering", "--disable", "max-login-attempts", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	policy, _ := srv.lastPUT["policy"].(map[string]any)
	if policy["enableMaxLoginAttempts"] != false {
		t.Errorf("enableMaxLoginAttempts = %v, want false", policy["enableMaxLoginAttempts"])
	}
	// The value itself is left alone, so re-enabling restores it.
	if policy["maxLoginAttempts"] != float64(6) {
		t.Errorf("--disable must not clear the value, got %v", policy["maxLoginAttempts"])
	}
}

func TestPasswordPolicies_Update_UnknownDisableTarget(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startPWPolicyServer(t).URL)

	_, _, err := runPWCmd(t, "password-policies", "update", "Engineering", "--disable", "nonsense", "--force")
	if err == nil {
		t.Fatal("want an error for an unknown --disable target")
	}
	if !strings.Contains(err.Error(), "nonsense") {
		t.Errorf("error should name the bad target, got %v", err)
	}
}

func TestPasswordPolicies_Update_NoFlags(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startPWPolicyServer(t).URL)

	_, _, err := runPWCmd(t, "password-policies", "update", "Engineering", "--force")
	if err == nil {
		t.Fatal("want an error when nothing was asked for")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error = %v", err)
	}
}

func TestPasswordPolicies_Update_Plan(t *testing.T) {
	setupUsersTest(t)
	srv := startPWPolicyServer(t)
	overrideV2Client(t, srv.URL)

	// Plan mode signals "changes pending" with a non-zero exit code by
	// design, so the error is expected here; what matters is that nothing
	// was written.
	out, errOut, _ := runPWCmd(t, "password-policies", "update", "Engineering", "--min-length", "16", "--plan")
	if srv.lastPUT != nil {
		t.Fatalf("--plan must not write, but a PUT was sent: %#v", srv.lastPUT)
	}
	combined := out + errOut
	if !strings.Contains(combined, "minLength") || !strings.Contains(combined, "16") {
		t.Errorf("plan should show the change: %s", combined)
	}
}

func TestPasswordPolicies_Create(t *testing.T) {
	setupUsersTest(t)
	srv := startPWPolicyServer(t)
	overrideV2Client(t, srv.URL)

	_, _, err := runPWCmd(t, "password-policies", "create", "--name", "Contractors",
		"--min-length", "14", "--needs-numeric", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	policy, _ := srv.lastPUT["policy"].(map[string]any)
	if policy["name"] != "Contractors" || policy["minLength"] != float64(14) {
		t.Errorf("unexpected create body: %#v", policy)
	}
	if policy["enableMinLength"] != true || policy["needsNumeric"] != true {
		t.Errorf("create must enable what it sets: %#v", policy)
	}
	// Nothing was asked for here, so it must stay off.
	if policy["needsSymbolic"] != false {
		t.Errorf("needsSymbolic = %v, want false", policy["needsSymbolic"])
	}
}

func TestPasswordPolicies_Delete_Single(t *testing.T) {
	setupUsersTest(t)
	srv := startPWPolicyServer(t)
	overrideV2Client(t, srv.URL)

	_, _, err := runPWCmd(t, "password-policies", "delete", "Engineering", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(srv.deleted) != 1 || srv.deleted[0] != "67dc22433f87810001e2bf2d" {
		t.Errorf("deleted = %v", srv.deleted)
	}
}

func TestPasswordPolicies_Delete_RefusesDefault(t *testing.T) {
	setupUsersTest(t)
	srv := startPWPolicyServer(t)
	overrideV2Client(t, srv.URL)

	_, _, err := runPWCmd(t, "password-policies", "delete", "67dc22433f87810001e2bf1c", "--force")
	if err == nil {
		t.Fatal("the org default policy must not be deletable")
	}
	if !strings.Contains(err.Error(), "default") {
		t.Errorf("error = %v", err)
	}
	if len(srv.deleted) != 0 {
		t.Errorf("nothing should have been deleted, got %v", srv.deleted)
	}
}

func TestPasswordPolicies_Delete_BatchUsesCollectionEndpoint(t *testing.T) {
	setupUsersTest(t)
	srv := startPWPolicyServer(t)
	// Two non-default policies so the batch path is exercised.
	srv.policies["67dc22433f87810001e2bf3e"] = map[string]any{
		"objectId": "67dc22433f87810001e2bf3e",
		"groups":   []any{},
		"policy":   map[string]any{"name": "Contractors", "default": false},
	}
	overrideV2Client(t, srv.URL)

	_, _, err := runPWCmd(t, "password-policies", "delete",
		"67dc22433f87810001e2bf2d", "67dc22433f87810001e2bf3e", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(srv.deleted) != 2 {
		t.Errorf("batch delete should remove both, got %v", srv.deleted)
	}
}

func TestPasswordPolicies_SetPrecedence_SendsBareArray(t *testing.T) {
	setupUsersTest(t)
	srv := startPWPolicyServer(t)
	overrideV2Client(t, srv.URL)

	_, _, err := runPWCmd(t, "password-policies", "set-precedence", "Engineering=3", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	entries, _ := srv.lastPUT["entries"].([]map[string]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %#v", srv.lastPUT)
	}
	if entries[0]["objectId"] != "67dc22433f87810001e2bf2d" || entries[0]["precedence"] != float64(3) {
		t.Errorf("unexpected entry: %#v", entries[0])
	}
}

func TestPasswordPolicies_SetPrecedence_BadArg(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startPWPolicyServer(t).URL)

	_, _, err := runPWCmd(t, "password-policies", "set-precedence", "Engineering", "--force")
	if err == nil {
		t.Fatal("want an error for a missing =<precedence>")
	}
	if !strings.Contains(err.Error(), "expected") {
		t.Errorf("error = %v", err)
	}
}
