package workflow

import (
	"encoding/json"
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
