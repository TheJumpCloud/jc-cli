package workflow

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func validateDSL(t *testing.T, doc string) Result {
	t.Helper()
	d, err := ParseDSL(json.RawMessage(doc))
	if err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return Validate(d)
}

func findingsMatching(r Result, substr string) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if strings.Contains(f.Message, substr) {
			out = append(out, f)
		}
	}
	return out
}

// The live case. pathParams {"userid": ...} against
// /api/v2/users/{user_id}/memberof passed validate AND simulate silently —
// simulate echoed the wrong name into its plan — and JumpCloud rejected the
// create with HTTP 400 "required parameter 'user_id' is missing". Validating
// locally exists to fail before that round trip.
func TestValidate_PathParamTypoIsCaughtWithTheCorrectName(t *testing.T) {
	res := validateDSL(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	 "do":[{"g":{"call":"jc_operation","with":{
	   "operationId":"getApiV2UsersByUserIdMemberof","version":2,
	   "pathParams":{"userid":"${ input.userId }"}}}}]}`)

	got := findingsMatching(res, "user_id")
	if len(got) == 0 {
		t.Fatal(`"userid" was accepted for an operation whose path is /api/v2/users/{user_id}/memberof`)
	}
	f := got[0]
	if f.Severity != Error {
		t.Errorf("severity = %v, want error — the API rejects this", f.Severity)
	}
	// Naming the wrong key and the right one together is the whole value: the
	// author sees the rename rather than two separate facts.
	if !strings.Contains(f.Message, "userid") {
		t.Errorf("message should name what was written, got: %s", f.Message)
	}
	if !strings.Contains(f.Hint, "/api/v2/users/{user_id}/memberof") {
		t.Errorf("hint should show the path, got: %s", f.Hint)
	}
}

func TestValidate_CorrectPathParamIsClean(t *testing.T) {
	res := validateDSL(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	 "do":[{"g":{"call":"jc_operation","with":{
	   "operationId":"getApiV2UsersByUserIdMemberof","version":2,
	   "pathParams":{"user_id":"${ input.userId }"}}}}]}`)
	for _, f := range res.Findings {
		if f.Severity == Error {
			t.Errorf("a correct DSL produced an error: %s — %s", f.Path, f.Message)
		}
	}
}

// pathParams absent entirely is already reported by a coarser check that
// names the whole path. Reporting each placeholder again would say the same
// thing once per parameter.
func TestValidate_MissingPathParamsObjectIsReportedOnce(t *testing.T) {
	res := validateDSL(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	 "do":[{"g":{"call":"jc_operation","with":{
	   "operationId":"getApiSystemusersById","version":1}}}]}`)
	if n := len(findingsMatching(res, "pathParams")); n != 1 {
		t.Errorf("got %d findings about pathParams, want exactly 1", n)
	}
}

// A parameter the operation does not take is a warning, not an error: the
// API may ignore it, and blocking on it would be stronger than the evidence.
func TestValidate_UnexpectedPathParamWarnsRatherThanBlocks(t *testing.T) {
	res := validateDSL(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	 "do":[{"g":{"call":"jc_operation","with":{
	   "operationId":"getApiSystemusers","version":1,
	   "pathParams":{"id":"x"}}}}]}`)
	got := findingsMatching(res, "takes no path parameters")
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if got[0].Severity != Warning {
		t.Errorf("severity = %v, want warning", got[0].Severity)
	}
}

// The rule must not fire on anything JumpCloud ships. A check that flags
// correct documents gets switched off.
//
// Scoped to this rule's own messages: templates legitimately carry
// REPLACE_WITH_ placeholders inside pathParams, and the separate placeholder
// check reports those — lint excludes them for exactly that reason.
func TestValidate_ShippedTemplatesHaveNoPathParamFindings(t *testing.T) {
	mine := []string{
		"expects path parameter", "requires path parameter",
		"takes no path parameters", "has no path parameter",
	}
	for _, tmpl := range CorrectedTemplates() {
		d, err := ParseDSL(tmpl.DSL)
		if err != nil {
			continue
		}
		for _, f := range Validate(d).Findings {
			for _, m := range mine {
				if strings.Contains(f.Message, m) {
					t.Errorf("%s: %s — %s", tmpl.Name, f.Path, f.Message)
				}
			}
		}
	}
}

func TestOperation_PathParams(t *testing.T) {
	for _, tc := range []struct {
		path string
		want []string
	}{
		{"/api/systemusers", nil},
		{"/api/systemusers/{id}", []string{"id"}},
		{"/api/v2/users/{user_id}/memberof", []string{"user_id"}},
		{"/api/v2/usergroups/{group_id}/members/{id}", []string{"group_id", "id"}},
	} {
		got := Operation{Path: tc.path}.PathParams()
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("PathParams(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// Validate and simulate must agree about the same document. The live report
// named BOTH as accepting pathParams {"userid": ...} silently, and fixing one
// would have left the other saying "would-call" on a plan the API rejects —
// which is how the two drift apart in the first place.
func TestSimulateAndValidateAgreeOnPathParams(t *testing.T) {
	const typo = `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	 "do":[{"g":{"call":"jc_operation","with":{
	   "operationId":"getApiV2UsersByUserIdMemberof","version":2,
	   "pathParams":{"userid":"x"}}}}]}`

	d, err := ParseDSL(json.RawMessage(typo))
	if err != nil {
		t.Fatal(err)
	}

	var validateObjected bool
	for _, f := range Validate(d).Findings {
		if f.Severity == Error && strings.Contains(f.Message, "user_id") {
			validateObjected = true
		}
	}

	res := Simulate(d, map[string]any{})
	var simulateObjected bool
	for _, s := range res.Steps {
		if s.Status == SimUnresolved && strings.Contains(s.Why, "user_id") {
			simulateObjected = true
		}
		if s.Status == SimWouldCall {
			t.Errorf("simulate planned a call the API rejects: %s — %v", s.Task, s.Params)
		}
	}

	if validateObjected != simulateObjected {
		t.Errorf("validate objected=%v but simulate objected=%v — the two surfaces "+
			"disagree about the same document", validateObjected, simulateObjected)
	}
	if !validateObjected {
		t.Error("neither surface objected")
	}
}

// The correct document must still plan, or the check has just broken simulate
// for every workflow that uses a path parameter — which is 509 of 732
// operations.
func TestSimulate_CorrectPathParamsStillPlans(t *testing.T) {
	d, err := ParseDSL(json.RawMessage(`{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	 "do":[{"g":{"call":"jc_operation","with":{
	   "operationId":"getApiV2UsersByUserIdMemberof","version":2,
	   "pathParams":{"user_id":"${ input.userId }"}}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	res := Simulate(d, map[string]any{"userId": "6a5a55c2af0a0dfa12103c3c"})
	if len(res.Steps) != 1 || res.Steps[0].Status != SimWouldCall {
		t.Fatalf("want one would-call step, got %+v", res.Steps)
	}
	pp, _ := res.Steps[0].Params["pathParams"].(map[string]any)
	if pp["user_id"] != "6a5a55c2af0a0dfa12103c3c" {
		t.Errorf("input did not resolve into the path parameter: %v", pp)
	}
}

// The seven parameter shapes a verification pass exercised against the live
// tenant, kept as a regression guard. It chose them to cover every shape it
// knew of, and three (a, b, d) are forms observed succeeding in real runs
// earlier in this project — so these are known-good documents, not guesses.
//
// This is the direction that matters. 509 of 732 catalogued operations take a
// path parameter, so a rule that over-fires breaks simulate for most real
// workflows: rejecting a correct document is a worse defect than the one this
// rule was added to catch.
func TestValidate_NoFalsePositivesAcrossPathParamShapes(t *testing.T) {
	for _, tc := range []struct {
		name, operationID string
		version           int
		pathParams        string
	}{
		{"single id", "getApiSystemusersById", 1, `{"id":"5ec9ce0000c9510e358c9918"}`},
		{"single id, v2", "getApiV2UsergroupsById", 2, `{"id":"5ec9ce0000c9510e358c9918"}`},
		{"two parameters", "postApiV2ApplemdmsByAppleMdmIdDevicesByDeviceIdLock", 2,
			`{"apple_mdm_id":"5ec9ce0000c9510e358c9918","device_id":"5ec9ce0000c9510e358c9919"}`},
		{"id in a nested path", "postApiSystemusersByIdStateSuspend", 1, `{"id":"5ec9ce0000c9510e358c9918"}`},
		{"named snake_case", "getApiV2PoliciesByPolicyIdPolicyresults", 2, `{"policy_id":"5ec9ce0000c9510e358c9918"}`},
		{"no path params, body only", "postApiRuncommand", 1, ""},
		{"no path params at all", "getApiSystemusers", 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pp := ""
			if tc.pathParams != "" {
				pp = `,"pathParams":` + tc.pathParams
			}
			doc := `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
			 "do":[{"g":{"call":"jc_operation","with":{"operationId":"` + tc.operationID +
				`","version":` + strconv.Itoa(tc.version) + pp + `}}}]}`

			d, err := ParseDSL(json.RawMessage(doc))
			if err != nil {
				t.Fatalf("fixture did not parse: %v", err)
			}
			for _, f := range Validate(d).Findings {
				if strings.Contains(f.Path, "pathParams") {
					t.Errorf("%s (%s): %s — %s", tc.name, tc.operationID, f.Severity, f.Message)
				}
			}
			// Simulate must agree, for the same reason the two share
			// ComparePathParams at all.
			for _, s := range Simulate(d, map[string]any{}).Steps {
				if s.Status == SimUnresolved && strings.Contains(s.Why, "path parameter") {
					t.Errorf("%s (%s): simulate refused a correct document — %s",
						tc.name, tc.operationID, s.Why)
				}
			}
		})
	}
}
