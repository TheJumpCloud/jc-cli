package workflow

import (
	"fmt"
	"sort"
	"strings"
)

// Comparing a dry run against a real one.
//
// Simulate says what a workflow WOULD call; the run trace says what it did.
// Holding them side by side is the only way to find out whether the planner is
// actually right, rather than asserting that it is. Every divergence is either
// a bug in this package's reading of the DSL or a genuine engine behaviour
// worth recording — and both are worth knowing.

// TraceSkipPrefix is how the engine reports a guard that excluded a task.
//
// The trace does NOT distinguish these with is_executed: a skipped node still
// carries is_executed=true and success=true, and only the message says what
// happened. Observed directly in a run where a task guarded on `1 == 2`
// reported exactly this. Anything deciding "did this task run?" must key on
// the message, not on is_executed.
const TraceSkipPrefix = "Skipping"

// Skipped reports whether a guard or branch excluded this step.
func (n RunNode) Skipped() bool {
	return strings.HasPrefix(strings.TrimSpace(n.Message), TraceSkipPrefix)
}

// Ran reports whether this step actually did its work.
func (n RunNode) Ran() bool {
	return n.IsExecuted && !n.Skipped()
}

// Verdict classifies one task's agreement between plan and run.
type Verdict string

const (
	// VerdictAgree means the plan and the run did the same thing.
	VerdictAgree Verdict = "agree"
	// VerdictRanButPlannedSkip means the workflow did something the plan said
	// it would not. This is the direction that matters: the plan understated
	// what the workflow touches.
	VerdictRanButPlannedSkip Verdict = "ran-but-planned-skip"
	// VerdictSkippedButPlannedRun means the plan overstated.
	VerdictSkippedButPlannedRun Verdict = "skipped-but-planned-run"
	// VerdictNotInRun means the plan named a task the run never reached,
	// which a halt earlier in the run explains.
	VerdictNotInRun Verdict = "not-in-run"
	// VerdictNotInPlan means the run has a task the plan never mentioned.
	VerdictNotInPlan Verdict = "not-in-plan"
	// VerdictUnresolved means the plan could not resolve the step's
	// parameters, so there is nothing to compare.
	VerdictUnresolved Verdict = "unresolved-in-plan"
)

// TaskComparison is one task, as planned and as run.
type TaskComparison struct {
	Task    string  `json:"task"`
	Verdict Verdict `json:"verdict"`
	// Planned is the simulate status, empty when the plan never saw it.
	Planned SimStatus `json:"planned,omitempty"`
	// Ran is what the trace shows.
	Ran bool `json:"ran"`
	// Status is the HTTP status the step really got, when it made a call.
	Status int `json:"status,omitempty"`
	// Detail explains the verdict in a sentence.
	Detail string `json:"detail,omitempty"`
}

// Comparison is a whole plan measured against a whole run.
type Comparison struct {
	RunID   string           `json:"run_id"`
	Tasks   []TaskComparison `json:"tasks"`
	Agree   int              `json:"agree"`
	Diverge int              `json:"diverge"`
	// Unresolved is counted apart from Diverge. The plan declared it could
	// not evaluate these, so holding them against it inflates the
	// disagreement and trains the reader to ignore the number.
	Unresolved int `json:"unresolved"`
	// RunHalted records that the run stopped early, which explains any
	// not-in-run verdicts after the failing step rather than leaving them
	// looking like planner bugs.
	RunHalted   bool   `json:"run_halted"`
	HaltedAt    string `json:"halted_at,omitempty"`
	Caveat      string `json:"caveat"`
	Interpreted string `json:"how_to_read,omitempty"`
}

// CompareRun measures a dry run against a real one.
//
// Task names are the join key. A name in one and not the other is reported
// rather than quietly dropped, because a systematic naming mismatch — nested
// tasks, for instance — would otherwise look like perfect agreement on an
// empty set.
func CompareRun(sim SimResult, run Run) Comparison {
	c := Comparison{
		RunID: run.ID,
		Caveat: "Divergence is not automatically a planner bug: a guard reading a prior " +
			"step's response body cannot be evaluated without one, and the plan says so " +
			"rather than guessing. Read it as a list of things to explain.",
		Interpreted: "ran-but-planned-skip is the direction worth acting on — the workflow " +
			"touched something the plan did not predict.",
	}

	planned := make(map[string]SimStep, len(sim.Steps))
	order := make([]string, 0, len(sim.Steps))
	for _, s := range sim.Steps {
		if _, seen := planned[s.Task]; !seen {
			order = append(order, s.Task)
		}
		planned[s.Task] = s
	}

	nodes := map[string]RunNode{}
	for _, n := range run.ExecutionDetails.Nodes {
		// The synthetic trigger node is not a task and has no counterpart.
		if n.Name == "__trigger" || n.Type == "trigger" {
			continue
		}
		nodes[n.Name] = n
		if _, seen := planned[n.Name]; !seen {
			order = append(order, n.Name)
		}
		if n.IsExecuted && !n.Success && !c.RunHalted {
			c.RunHalted, c.HaltedAt = true, n.Name
		}
	}

	for _, name := range order {
		step, inPlan := planned[name]
		node, inRun := nodes[name]

		tc := TaskComparison{Task: name, Planned: step.Status, Ran: node.Ran()}
		if node.NodeOutput != nil {
			tc.Status = node.NodeOutput.Status
		}

		plannedToRun := step.Status == SimWouldCall || step.Status == SimStubbed

		switch {
		case inPlan && step.Status == SimUnresolved:
			tc.Verdict = VerdictUnresolved
			tc.Detail = "the plan could not resolve this step's parameters, so there is nothing to compare: " + step.Why

		case !inRun:
			tc.Verdict = VerdictNotInRun
			tc.Detail = "the plan named this task but the run has no node for it"
			if c.RunHalted {
				tc.Detail += fmt.Sprintf("; the run halted at %q, which explains a task after it never being reached", c.HaltedAt)
			}

		case !inPlan:
			tc.Verdict = VerdictNotInPlan
			tc.Detail = "the run has this task but the plan never mentioned it"

		case plannedToRun && !node.Ran():
			tc.Verdict = VerdictSkippedButPlannedRun
			tc.Detail = "the plan expected this to run; the engine skipped it — " + node.Message

		case step.Status == SimSkipped && node.Ran():
			tc.Verdict = VerdictRanButPlannedSkip
			tc.Detail = "the engine ran this; the plan predicted it would be skipped because: " + step.Why

		default:
			tc.Verdict = VerdictAgree
		}

		switch tc.Verdict {
		case VerdictAgree:
			c.Agree++
		case VerdictUnresolved:
			c.Unresolved++
		default:
			c.Diverge++
		}
		c.Tasks = append(c.Tasks, tc)
	}

	SortComparison(c.Tasks)
	return c
}

// SortComparison puts divergences first, agreement last.
func SortComparison(tasks []TaskComparison) {
	rank := map[Verdict]int{
		VerdictRanButPlannedSkip:    0,
		VerdictSkippedButPlannedRun: 1,
		VerdictNotInPlan:            2,
		VerdictNotInRun:             3,
		VerdictUnresolved:           4,
		VerdictAgree:                5,
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		return rank[tasks[i].Verdict] < rank[tasks[j].Verdict]
	})
}
