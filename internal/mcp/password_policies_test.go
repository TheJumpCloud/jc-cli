package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pwPolicyMock mirrors the CLI test's mock: a {results:[…]} list envelope, a
// full-replace PUT, and a batch DELETE that takes objectIds in the body.
type pwPolicyMock struct {
	*httptest.Server
	lastBody map[string]any
	lastPath string
	deleted  []string
}

func startPWPolicyV2Server(t *testing.T) *pwPolicyMock {
	t.Helper()

	engineering := map[string]any{
		"cached":   false,
		"objectId": "67dc22433f87810001e2bf2d",
		"groups":   []any{map[string]any{"groupId": "664ab86614f2240001cb68ed", "name": "Engineering"}},
		"policy": map[string]any{
			"name": "Engineering", "default": false, "precedence": 2,
			"minLength": 12, "enableMinLength": true,
			"maxLoginAttempts": 6, "enableMaxLoginAttempts": true,
			"lockoutTimeInSeconds": 3601, "enableLockoutTimeInSeconds": true,
			"passwordExpirationInDays": 90, "enablePasswordExpirationInDays": false,
			"daysAfterExpirationToSelfRecover": -1,
			"resetLockoutCounterMinutes":       30,
		},
	}
	defaultPolicy := map[string]any{
		"cached":   false,
		"objectId": "67dc22433f87810001e2bf1c",
		"groups":   []any{},
		"policy": map[string]any{
			"name": "", "default": true, "precedence": 1,
			"minLength": 8, "enableMinLength": true,
		},
	}

	m := &pwPolicyMock{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
		m.lastPath = p
		rest := strings.TrimPrefix(p, "/passwordpolicies")

		switch {
		case rest == "" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"objectId": "67dc22433f87810001e2bf1c", "name": "", "precedence": 1, "default": true, "groupCount": 0, "minLength": 8},
				{"objectId": "67dc22433f87810001e2bf2d", "name": "Engineering", "precedence": 2, "default": false, "groupCount": 1, "minLength": 12},
			}})

		case rest == "" && r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&m.lastBody)
			json.NewEncoder(w).Encode(map[string]any{
				"objectId": "67dc22433f87810001e2bf99",
				"groups":   []any{},
				"policy":   m.lastBody["policy"],
			})

		case rest == "" && r.Method == http.MethodDelete:
			var in struct {
				ObjectIDs []string `json:"objectIds"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			if len(in.ObjectIDs) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"message":"objectIds required"}`))
				return
			}
			m.deleted = append(m.deleted, in.ObjectIDs...)
			w.Write([]byte(`{}`))

		case rest == "/precedence/set" && r.Method == http.MethodPut:
			var in []map[string]any
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"message":"expected an array"}`))
				return
			}
			m.lastBody = map[string]any{"entries": in}
			w.Write([]byte(`{}`))

		case strings.HasPrefix(rest, "/user/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(defaultPolicy)

		case strings.HasPrefix(rest, "/usergroup/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(engineering)

		case rest == "/67dc22433f87810001e2bf2d" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(engineering)

		case rest == "/67dc22433f87810001e2bf1c" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(defaultPolicy)

		case r.Method == http.MethodPut:
			json.NewDecoder(r.Body).Decode(&m.lastBody)
			json.NewEncoder(w).Encode(engineering)

		case r.Method == http.MethodDelete:
			m.deleted = append(m.deleted, strings.TrimPrefix(rest, "/"))
			w.Write([]byte(`{}`))

		default:
			t.Errorf("unexpected: %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(m.Close)
	return m
}

func TestMCPPasswordPolicies_List(t *testing.T) {
	overrideV2ClientForTest(t, startPWPolicyV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "password_policies_list", map[string]any{}))
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("list must unwrap {results:[…]} into a bare array: %v\n%s", err, out)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 policies, got %d: %s", len(rows), out)
	}
}

func TestMCPPasswordPolicies_Get_ResolvesName(t *testing.T) {
	overrideV2ClientForTest(t, startPWPolicyV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "password_policies_get", map[string]any{
		"identifier": "Engineering",
	}))
	if !strings.Contains(out, "67dc22433f87810001e2bf2d") {
		t.Errorf("name did not resolve to the right policy: %s", out)
	}
}

func TestMCPPasswordPolicies_ForGroup(t *testing.T) {
	overrideV2ClientForTest(t, startPWPolicyV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "password_policies_for_group", map[string]any{
		"group": "664ab86614f2240001cb68ed",
	}))
	if !strings.Contains(out, "Engineering") {
		t.Errorf("unexpected result: %s", out)
	}
}

func TestMCPPasswordPolicies_UpdatePlanDoesNotWrite(t *testing.T) {
	srv := startPWPolicyV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "password_policies_update", map[string]any{
		"identifier": "Engineering", "min_length": 16,
	}))
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if m["plan"] != true {
		t.Fatalf("expected a plan, got %s", out)
	}
	if srv.lastBody != nil {
		t.Errorf("plan mode must not write, but a body was sent: %#v", srv.lastBody)
	}

	effects, _ := m["effects"].(map[string]any)
	changes, _ := effects["changes"].([]any)
	joined := ""
	for _, c := range changes {
		joined += c.(string) + "\n"
	}
	if !strings.Contains(joined, "minLength: 12 -> 16") {
		t.Errorf("plan should show the before/after, got: %s", joined)
	}
}

func TestMCPPasswordPolicies_UpdateSendsCompletePolicy(t *testing.T) {
	srv := startPWPolicyV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	callTool(t, cs, "password_policies_update", map[string]any{
		"identifier": "Engineering", "min_length": 16, "execute": true,
	})

	policy, _ := srv.lastBody["policy"].(map[string]any)
	if policy == nil {
		t.Fatalf("no policy sent: %#v", srv.lastBody)
	}
	if policy["minLength"] != float64(16) {
		t.Errorf("minLength = %v", policy["minLength"])
	}
	// The full-replace PUT drops anything omitted, so untouched settings must
	// still be present.
	if policy["lockoutTimeInSeconds"] != float64(3601) || policy["resetLockoutCounterMinutes"] != float64(30) {
		t.Errorf("untouched settings lost: %#v", policy)
	}
	if policy["daysAfterExpirationToSelfRecover"] != float64(-1) {
		t.Errorf("the -1 sentinel must survive: %v", policy["daysAfterExpirationToSelfRecover"])
	}
	// Bindings come back as groups on reads and go out as groupIds on writes.
	ids, _ := srv.lastBody["groupIds"].([]any)
	if len(ids) != 1 || ids[0] != "664ab86614f2240001cb68ed" {
		t.Errorf("group bindings not preserved: %#v", srv.lastBody["groupIds"])
	}
}

func TestMCPPasswordPolicies_UpdateAutoEnables(t *testing.T) {
	srv := startPWPolicyV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	callTool(t, cs, "password_policies_update", map[string]any{
		"identifier": "Engineering", "expiration_days": 45, "execute": true,
	})

	policy, _ := srv.lastBody["policy"].(map[string]any)
	if policy["passwordExpirationInDays"] != float64(45) {
		t.Errorf("expiration not set: %v", policy["passwordExpirationInDays"])
	}
	if policy["enablePasswordExpirationInDays"] != true {
		t.Error("setting a value must switch its requirement on, else it is silently inert")
	}
}

func TestMCPPasswordPolicies_UpdateDisable(t *testing.T) {
	srv := startPWPolicyV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	callTool(t, cs, "password_policies_update", map[string]any{
		"identifier": "Engineering", "disable": []string{"max-login-attempts"}, "execute": true,
	})

	policy, _ := srv.lastBody["policy"].(map[string]any)
	if policy["enableMaxLoginAttempts"] != false {
		t.Errorf("enableMaxLoginAttempts = %v, want false", policy["enableMaxLoginAttempts"])
	}
	if policy["maxLoginAttempts"] != float64(6) {
		t.Errorf("disable must not clear the value: %v", policy["maxLoginAttempts"])
	}
}

func TestMCPPasswordPolicies_UpdateNoChanges(t *testing.T) {
	overrideV2ClientForTest(t, startPWPolicyV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "password_policies_update", map[string]any{
		"identifier": "Engineering", "execute": true,
	}))
	if !strings.Contains(out, "no changes requested") {
		t.Errorf("expected a no-changes error, got: %s", out)
	}
}

func TestMCPPasswordPolicies_Create(t *testing.T) {
	srv := startPWPolicyV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	callTool(t, cs, "password_policies_create", map[string]any{
		"name": "Contractors", "min_length": 14, "needs_numeric": true, "execute": true,
	})

	policy, _ := srv.lastBody["policy"].(map[string]any)
	if policy["name"] != "Contractors" || policy["minLength"] != float64(14) {
		t.Errorf("unexpected create body: %#v", policy)
	}
	if policy["enableMinLength"] != true || policy["needsNumeric"] != true {
		t.Errorf("create must enable what it sets: %#v", policy)
	}
	if policy["needsSymbolic"] != false {
		t.Errorf("unrequested requirement should stay off: %v", policy["needsSymbolic"])
	}
}

func TestMCPPasswordPolicies_DeleteRefusesDefault(t *testing.T) {
	srv := startPWPolicyV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "password_policies_delete", map[string]any{
		"identifiers": []string{"67dc22433f87810001e2bf1c"}, "execute": true,
	}))
	if !strings.Contains(out, "default") {
		t.Errorf("the org default must not be deletable, got: %s", out)
	}
	if len(srv.deleted) != 0 {
		t.Errorf("nothing should have been deleted: %v", srv.deleted)
	}
}

func TestMCPPasswordPolicies_DeleteSingleUsesItemEndpoint(t *testing.T) {
	srv := startPWPolicyV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	callTool(t, cs, "password_policies_delete", map[string]any{
		"identifiers": []string{"Engineering"}, "execute": true,
	})
	if len(srv.deleted) != 1 || srv.deleted[0] != "67dc22433f87810001e2bf2d" {
		t.Errorf("deleted = %v", srv.deleted)
	}
	if srv.lastPath != "/passwordpolicies/67dc22433f87810001e2bf2d" {
		t.Errorf("a single delete should address the item, got %s", srv.lastPath)
	}
}

func TestMCPPasswordPolicies_SetPrecedenceSendsBareArray(t *testing.T) {
	srv := startPWPolicyV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	callTool(t, cs, "password_policies_set_precedence", map[string]any{
		"entries": []map[string]any{{"identifier": "Engineering", "precedence": 3}},
		"execute": true,
	})

	entries, _ := srv.lastBody["entries"].([]map[string]any)
	if len(entries) != 1 {
		t.Fatalf("the precedence body must be a bare array: %#v", srv.lastBody)
	}
	if entries[0]["objectId"] != "67dc22433f87810001e2bf2d" || entries[0]["precedence"] != float64(3) {
		t.Errorf("unexpected entry: %#v", entries[0])
	}
}

func TestMCPPasswordPolicies_ReadOnlyModeBlocksWrites(t *testing.T) {
	overrideV2ClientForTest(t, startPWPolicyV2Server(t).URL)
	cs := connectToolTestServer(t, Options{ReadOnly: true})

	for name, args := range map[string]map[string]any{
		"password_policies_create":         {"name": "x", "execute": true},
		"password_policies_update":         {"identifier": "Engineering", "min_length": 16, "execute": true},
		"password_policies_delete":         {"identifiers": []string{"Engineering"}, "execute": true},
		"password_policies_set_precedence": {"entries": []map[string]any{{"identifier": "Engineering", "precedence": 3}}, "execute": true},
	} {
		out := getResultText(t, callTool(t, cs, name, args))
		if !strings.Contains(out, "read-only") {
			t.Errorf("%s should be refused in read-only mode, got: %s", name, out)
		}
	}
}
