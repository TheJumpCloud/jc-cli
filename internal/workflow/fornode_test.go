package workflow

import "testing"

func intp(n int) *int { return &n }

// A `for` node inverts the envelope predicate that governs every other node
// type, which a live pass established by running both outcomes:
//
//	loop completed -> node_output NULL,      iteration_count 7, success true
//	loop failed    -> node_output POPULATED, iteration_count 1, success false
//
// So the generic rule — node_output != nil means it ran — reads a loop that
// iterated seven times as a step that never ran. On a fleet-wide sweep that is
// the difference between "we processed everyone" and "we processed nobody".
func TestRunNode_ForLoopInvertsTheEnvelopeRule(t *testing.T) {
	completed := RunNode{
		Name: "loopUsers", Type: NodeTypeFor, IsExecuted: true, Success: true,
		Message: "For loop ran for 7 iterations and completed.", IterationCount: intp(7),
		// node_output deliberately absent — this is the real shape.
	}
	if !completed.Ran() {
		state, why := completed.State()
		t.Errorf("a loop that ran 7 iterations reported %q (%s); the envelope rule "+
			"does not apply to a for node", state, why)
	}

	failed := RunNode{
		Name: "loopUsers", Type: NodeTypeFor, IsExecuted: true, Success: false,
		Message:        "failed at iteration 2",
		IterationCount: intp(1),
		NodeOutput:     &NodeOutput{Status: TraceStatus{Code: 404}},
	}
	if state, _ := failed.State(); state != RunStateFailed {
		t.Errorf("a failed loop reported %q, want failed", state)
	}
}

// A loop that matched nothing is neither a failure nor a run. Reporting it as
// having run would hide an empty extract, which is a common and quiet bug.
func TestRunNode_EmptyLoopIsSkippedNotRan(t *testing.T) {
	empty := RunNode{
		Name: "loopUsers", Type: NodeTypeFor, IsExecuted: true, Success: true,
		Message: "For loop ran for 0 iterations and completed.", IterationCount: intp(0),
	}
	state, why := empty.State()
	if state != RunStateSkipped {
		t.Errorf("an empty loop reported %q, want skipped", state)
	}
	if why == "" {
		t.Error("the reason should say the loop had nothing to iterate over")
	}
}

// A `for` node with no iteration_count at all must still not be read through
// the envelope rule — absence of the count is not evidence the loop did not
// run, and this area has paid for that inference before.
func TestRunNode_ForWithoutIterationCountStillCountsAsRan(t *testing.T) {
	n := RunNode{Name: "loopUsers", Type: NodeTypeFor, IsExecuted: true, Success: true,
		Message: "For loop completed."}
	if !n.Ran() {
		t.Error("a successful for node with no iteration_count was read as not having run")
	}
}
