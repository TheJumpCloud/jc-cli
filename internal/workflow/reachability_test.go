package workflow

import "testing"

// The validator and the planner must agree about what the jump graph reaches.
// While they disagreed — the validator warning a task never runs, the planner
// predicting it would — comparing a plan against a real run reported a
// divergence that was only the planner's blind spot.
func TestUnreachableTasks_ValidatorAndPlannerAgree(t *testing.T) {
	dsl := []byte(`{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[
	    {"router":{"switch":[{"always":{"when":"${ 1 == 1 }","then":"stepC"}}]}},
	    {"orphanStep":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}},
	    {"stepC":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}
	  ]}`)

	d, err := ParseDSL(dsl)
	if err != nil {
		t.Fatal(err)
	}

	unreachable := UnreachableTasks(d.Tasks())
	if !unreachable["orphanStep"] {
		t.Error("orphanStep is not targeted and follows a switch, so it never runs")
	}
	if unreachable["stepC"] {
		t.Error("stepC is a branch target and is reachable")
	}
	if unreachable["router"] {
		t.Error("the first task is always reachable")
	}

	// The planner must reach the same conclusion, not merely the validator.
	sim := Simulate(d, map[string]any{})
	for _, s := range sim.Steps {
		if s.Task == "orphanStep" && s.Status != SimSkipped {
			t.Errorf("the planner must predict orphanStep skipped, got %q", s.Status)
		}
		if s.Task == "router" && s.Status != SimSwitched {
			t.Errorf("a routing switch plans as switched, not %q", s.Status)
		}
	}
}

// Fall-through is still reachable: without a jump, the next task runs.
func TestUnreachableTasks_NoJumpMeansReachable(t *testing.T) {
	d, err := ParseDSL([]byte(`{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[
	    {"first":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}},
	    {"second":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}
	  ]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := UnreachableTasks(d.Tasks()); len(got) != 0 {
		t.Errorf("nothing jumps away, so nothing is unreachable: %v", got)
	}
}
