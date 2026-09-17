package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func loopFindings(t *testing.T, doc string) []Finding {
	t.Helper()
	d, err := ParseDSL(json.RawMessage(doc))
	if err != nil {
		t.Fatalf("fixture did not parse: %v", err)
	}
	var out []Finding
	for _, f := range Validate(d).Findings {
		if strings.Contains(f.Message, "inside a loop") {
			out = append(out, f)
		}
	}
	return out
}

const loopPrefix = `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
 "do":[{"list":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1,
   "extract":"${ page.response.body.results }"}}},
  {"sweep":{"for":{"each":"u","in":"${ actions.list }"},"do":[`
const loopSuffix = `]}}]}`

// A for.each does not isolate its iterations: a non-2xx halts the WHOLE run.
// Verified twice on a live tenant — a loop over three ids with a nonexistent
// one in the middle failed at iteration 2, never attempted the third, and
// skipped the task after the loop.
//
// This is the engine behaviour most likely to surprise, because it is the
// opposite of what a sweep needs, and no shipped template demonstrated it.
func TestValidate_UnguardedCallInALoopWarns(t *testing.T) {
	got := loopFindings(t, loopPrefix+
		`{"lock":{"call":"jc_operation","with":{"operationId":"getApiSystemusersById","version":1,
		  "pathParams":{"id":"${ u._id }"}}}}`+loopSuffix)
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if got[0].Severity != Warning {
		t.Errorf("severity = %v, want warning — this is a property to know, not a defect", got[0].Severity)
	}
	// The hint must say the blast radius is the RUN, not the iteration, and
	// must not recommend the thing that cannot work.
	for _, want := range []string{"WHOLE run", "after the call cannot help"} {
		if !strings.Contains(got[0].Hint, want) {
			t.Errorf("hint missing %q: %s", want, got[0].Hint)
		}
	}
}

// Doing the right thing has to silence it. A warning nobody can satisfy is
// one people learn to scroll past, and then it protects nothing.
func TestValidate_PreFilteredLoopIsQuiet(t *testing.T) {
	t.Run("if on the fallible task", func(t *testing.T) {
		got := loopFindings(t, loopPrefix+
			`{"lock":{"call":"jc_operation","if":"${ u.activated }","with":{
			  "operationId":"getApiSystemusersById","version":1,"pathParams":{"id":"${ u._id }"}}}}`+loopSuffix)
		if len(got) != 0 {
			t.Errorf("an `if` on the call is a real pre-filter — it decides before the call: %v", got[0].Message)
		}
	})

	t.Run("switch earlier in the body", func(t *testing.T) {
		got := loopFindings(t, loopPrefix+
			`{"route":{"switch":[{"active":{"when":"${ u.activated }","then":"lock"}},
			   {"skip":{"then":"continue"}}]}},
			 {"lock":{"call":"jc_operation","with":{"operationId":"getApiSystemusersById","version":1,
			  "pathParams":{"id":"${ u._id }"}}}}`+loopSuffix)
		if len(got) != 0 {
			t.Errorf("a switch routing past the record is a pre-filter: %v", got[0].Message)
		}
	})

	t.Run("no API call in the body", func(t *testing.T) {
		got := loopFindings(t, loopPrefix+
			`{"note":{"call":"sendEmailsToAddresses","with":{"message":{"subject":"x","body":"y"},
			  "recipients":{"to_addresses":["${ u.email }"]}}}}`+loopSuffix)
		if len(got) != 0 {
			t.Errorf("a loop with no jc_operation should not warn: %v", got[0].Message)
		}
	})
}

// One warning per loop, not one per call. Three unguarded calls in a body is
// one problem with one fix, and three copies of the same sentence is how a
// check earns a reputation for noise.
func TestValidate_OneWarningPerLoop(t *testing.T) {
	got := loopFindings(t, loopPrefix+
		`{"a":{"call":"jc_operation","with":{"operationId":"getApiSystemusersById","version":1,"pathParams":{"id":"${ u._id }"}}}},
		 {"b":{"call":"jc_operation","with":{"operationId":"getApiSystemusersById","version":1,"pathParams":{"id":"${ u._id }"}}}},
		 {"c":{"call":"jc_operation","with":{"operationId":"getApiSystemusersById","version":1,"pathParams":{"id":"${ u._id }"}}}}`+loopSuffix)
	if len(got) != 1 {
		t.Errorf("got %d warnings for one loop, want 1", len(got))
	}
}

// The shipped template this found. Recorded as a test so the finding is not
// lost if the catalogue changes: "Notify Users with Non-Compliant Endpoints"
// loops over policy results and calls getApiSystemsById on each with no
// guard, so one system deleted since the policy result was recorded kills the
// whole notification sweep.
//
// Unlike the `status == 200` family, there is NO corrected copy to offer: you
// cannot pre-filter a 404 on a system whose existence you can only establish
// by calling. The mitigation is architectural, not a deletion.
func TestValidate_ShippedTemplateWithAnUnguardedSweepIsFlagged(t *testing.T) {
	var flagged []string
	for _, tmpl := range CorrectedTemplates() {
		d, err := ParseDSL(tmpl.DSL)
		if err != nil {
			continue
		}
		for _, f := range Validate(d).Findings {
			if strings.Contains(f.Message, "inside a loop") {
				flagged = append(flagged, tmpl.Name)
			}
		}
	}
	// The corrected copies are the four status==200 repairs; none of them
	// sweeps, so none should trip this.
	if len(flagged) != 0 {
		t.Errorf("a corrected template tripped the loop warning: %v — corrected copies "+
			"are meant to be clean, so either the correction is incomplete or this "+
			"rule is over-firing", flagged)
	}
}
