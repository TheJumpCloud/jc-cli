package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// The DSL a live pass used: branch on a value only a real response carries.
const dataDrivenSwitch = `{
 "schedule":{"on":{"one":{"with":{"source":"external"}}}},
 "do":[
  {"getUser":{"call":"jc_operation","with":{"operationId":"getApiSystemusersById","version":1,
     "pathParams":{"id":"${ input.userId }"}}}},
  {"route":{"switch":[
     {"enrolled":{"when":"${ actions.getUser.body.mfaEnrollment.overallStatus == \"ENROLLED\" }","then":"markEnrolled"}},
     {"notEnrolled":{"when":"${ actions.getUser.body.mfaEnrollment.overallStatus != \"ENROLLED\" }","then":"markNotEnrolled"}}]}},
  {"markEnrolled":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}},
  {"markNotEnrolled":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`

// A switch whose `when` reads a prior step's response body cannot be evaluated
// in a dry run. It used to swallow the error and fall through to the default,
// so the plan named a branch it had not chosen — and compare_run then reported
// the ENGINE as wrong, in ran-but-planned-skip, the one verdict the tool tells
// you to act on.
//
// The if-guard path has always reported this honestly. This is the same rule
// for the other guard.
func TestSimulate_UnevaluableSwitchLeavesItsTargetsUnresolved(t *testing.T) {
	d, err := ParseDSL(json.RawMessage(dataDrivenSwitch))
	if err != nil {
		t.Fatal(err)
	}
	res := Simulate(d, map[string]any{"userId": "5ec9ce0000c9510e358c9918"})

	byTask := map[string]SimStep{}
	for _, s := range res.Steps {
		byTask[s.Task] = s
	}

	for _, target := range []string{"markEnrolled", "markNotEnrolled"} {
		s, ok := byTask[target]
		if !ok {
			t.Fatalf("%s missing from the plan", target)
		}
		if s.Status == SimSkipped {
			t.Errorf("%s was planned as skipped; the switch could not be evaluated, so "+
				"whether it runs is unknown — and calling it skipped makes any run that "+
				"takes this branch look like the engine disagreeing with the plan", target)
		}
		if s.Status != SimUnresolved {
			t.Errorf("%s = %q, want unresolved", target, s.Status)
		}
	}

	// The switch itself should say why it could not route.
	if sw := byTask["route"]; !strings.Contains(sw.Why, "cannot route") {
		t.Errorf("the switch should report that it could not route, got: %q", sw.Why)
	}
}

// A switch the planner CAN evaluate must still route, or this fix would blind
// the planner to every switch rather than only the undecidable ones.
func TestSimulate_EvaluableSwitchStillRoutes(t *testing.T) {
	d, err := ParseDSL(json.RawMessage(`{
	 "schedule":{"on":{"one":{"with":{"source":"external"}}}},
	 "do":[
	  {"route":{"switch":[
	     {"yes":{"when":"${ input.flag == \"go\" }","then":"taken"}},
	     {"no":{"when":"${ input.flag != \"go\" }","then":"notTaken"}}]}},
	  {"taken":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}},
	  {"notTaken":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	res := Simulate(d, map[string]any{"flag": "go"})
	byTask := map[string]SimStep{}
	for _, s := range res.Steps {
		byTask[s.Task] = s
	}
	if got := byTask["taken"].Status; got == SimSkipped || got == SimUnresolved {
		t.Errorf("the selected branch was planned as %q; an evaluable switch must still route", got)
	}
	if got := byTask["notTaken"].Status; got != SimSkipped {
		t.Errorf("the unselected branch = %q, want skipped", got)
	}
}

// The default branch must still be chosen when every `when` evaluated and none
// matched — that is a decision, not an inability to decide.
func TestSimulate_DefaultIsStillChosenWhenNothingMatched(t *testing.T) {
	d, err := ParseDSL(json.RawMessage(`{
	 "schedule":{"on":{"one":{"with":{"source":"external"}}}},
	 "do":[
	  {"route":{"switch":[
	     {"never":{"when":"${ input.flag == \"impossible\" }","then":"taken"}},
	     {"fallback":{"then":"fell"}}]}},
	  {"taken":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}},
	  {"fell":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	res := Simulate(d, map[string]any{"flag": "other"})
	for _, s := range res.Steps {
		if s.Task == "fell" && (s.Status == SimSkipped || s.Status == SimUnresolved) {
			t.Errorf("the default branch = %q, want selected", s.Status)
		}
	}
}
