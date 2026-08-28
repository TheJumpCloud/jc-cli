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

// RunState is what a trace node actually did.
//
// The trace's own fields cannot answer this. A step a branch routed around
// reports is_executed=true, success=true and "Task completed." — identical in
// every field to one that ran, except that node_output is null.
//
// EVIDENCE. This predicate is derived from a small number of real runs against
// org 5ec71e8e96bfda0611fc6c5b on 2026-08-28. It is recorded here in full so
// the next person can falsify it in one read rather than re-deriving it — this
// repo got the rule wrong twice, and both times the finding travelled as a
// conclusion with its evidence left behind.
//
//	run 0c5c7e6e-3b9f-4807-80a0-0c77ff63b7f4 — the decisive one. A switch
//	  jumped past an untargeted jc_operation that would have CREATED A USER
//	  GROUP; afterwards the group did not exist, while the jump target's did.
//	  Ground truth from outside the trace, because the trace was in question.
//	    __trigger    type=trigger      node_output=null  "Workflow invoked."
//	    router       type=switch       node_output=null  "Switch evaluated."
//	    orphanStep   type=jc_operation node_output=null  "Task completed."   <- did NOT run
//	    jumpTarget   type=jc_operation node_output=201   "Task completed."   <- ran
//	run 49770a2e-1cdc-44e5-80f2-5afcd10ed6d6 — guard skips report
//	  "Skipping — if condition did not match."; references to a skipped task
//	  evaluate false without erroring.
//	run c5d40e74-00b3-4269-a26c-ec2c6a4e4cef — a create returned 201, and a
//	  task guarded on `status == 200` was skipped while `>= 200 && < 300` ran.
//	run 71db73cc-0221-4fef-8eaa-24f4dc1e788f — email. The engine reports the
//	  call type as "email", not "sendEmailsToAddresses". A send carries
//	  node_output {"notification_type":"workflows_common","status":"success"}
//	  — so the envelope rule holds for email too — and a guarded-off send
//	  carries a null node_output plus if_condition {expression, result:false}.
//	  Note the STRING status: typing it as an int made the trace view fail
//	  outright on any run containing an email step.
//
// SCOPE, stated honestly. Observed: jc_operation, switch, trigger, and email
// (sendEmailsToAddresses). NOT observed in any state: connector_operation, and
// sendEmailsToChannel — which presumably shares the "email" node type, though
// that is an assumption, not an observation. Unobserved types return
// RunStateUnknown; see the note at the fallthrough, which is not merely
// "nobody has looked yet".
//
// State table as observed:
//
//	state                       is_executed  success  node_output  message
//	ran                         true         true     populated    "Task completed."
//	skipped, branch not taken   true         true     null         "Task completed."
//	skipped, guard false        true         true     null         "Skipping — …"
//	skipped, prior task failed  false        true     null         "Not executed — …"
//	failed                      true         false    populated    descriptive
type RunState string

const (
	// RunStateRan means the step did its work.
	RunStateRan RunState = "ran"
	// RunStateSkipped means the step was visited but did nothing.
	RunStateSkipped RunState = "skipped"
	// RunStateFailed means the step ran and failed, which halts the run.
	RunStateFailed RunState = "failed"
	// RunStateUnknown is for node types whose trace shape has not been
	// observed. Guessing here would repeat the mistake this type exists to
	// prevent.
	RunStateUnknown RunState = "unknown"
)

// Node types seen in real traces.
const (
	NodeTypeOperation = "jc_operation"
	NodeTypeConnector = "connector_operation"
	NodeTypeSwitch    = "switch"
	NodeTypeTrigger   = "trigger"
	// NodeTypeEmail covers sendEmailsToAddresses — the engine reports the
	// call type as plain "email". sendEmailsToChannel presumably shares it,
	// but that has not been observed.
	NodeTypeEmail = "email"
)

// State classifies what this node did, and why.
//
// For a call node the rule is node_output: a step that reached the API always
// has one, and a step that did not never does. Everything else is either not a
// call (trigger, switch) or a shape this package has not observed, and is
// reported as unknown rather than assumed.
func (n RunNode) State() (RunState, string) {
	if !n.IsExecuted {
		return RunStateSkipped, "not reached — the run failed at an earlier task"
	}
	if !n.Success {
		return RunStateFailed, n.Message
	}

	// The ENVELOPE is the evidence, never its body. A 204, or a 200 with an
	// empty body, is still a call that happened — testing node_output.body
	// here would invert this predicate exactly the way reading is_executed
	// did, and would report every empty-bodied success as a skip.
	if n.NodeOutput != nil {
		return RunStateRan, n.Message
	}

	// Neither a trigger nor a switch is a call, so neither ever carries a
	// node_output, and falling through would mark both unknown forever. Both
	// did evaluate: the trigger carries node_input, and a switch carries
	// switch_evaluation with every case, its when expression, whether it
	// matched, and chosen_next_node_id. Those switch traces were the most
	// reliable thing in the structure — correct while every other field lied —
	// so they must not be degraded to unknown.
	if n.Type == NodeTypeTrigger || n.Type == NodeTypeSwitch {
		return RunStateRan, n.Message
	}

	// A recorded guard result is the strongest signal available, and unlike
	// the message it is structured rather than English prose.
	if n.IfCondition != nil && !n.IfCondition.Result {
		return RunStateSkipped, "the task guard evaluated false: " + n.IfCondition.Expression
	}

	// An explicit skip message is conclusive for any node type.
	if strings.HasPrefix(strings.TrimSpace(n.Message), TraceSkipPrefix) {
		return RunStateSkipped, "a guard on this task evaluated false"
	}

	// A call node with no response made no call, and said nothing about it:
	// this is the branch-skip case that reports "Task completed."
	if n.Type == NodeTypeOperation || n.Type == NodeTypeConnector || n.Type == NodeTypeEmail {
		return RunStateSkipped, "branch not taken — no branch routed to this task"
	}

	// Anything else — connector_operation above all — has never been seen in
	// a trace here.
	//
	// This is NOT merely "unobserved, so assume the jc_operation rule". The
	// semantics could be TYPE-DEPENDENT: a call type that populates nothing
	// on success would make a null node_output mean "ran fine", the exact
	// opposite of what it means for a jc_operation, and collapsing the two
	// would silently report every success as never happening.
	//
	// Email was exactly this hedge until a run settled it, and the hedge was
	// worth keeping: the type string turned out to be "email" rather than the
	// DSL call name, so reasoning from the DSL would have got it wrong.
	// Resolve the rest the same way — run one and read the trace.
	return RunStateUnknown, fmt.Sprintf("node type %q has never been observed in a trace, and "+
		"a null node_output may mean something different for it than for a jc_operation, "+
		"so whether it ran cannot be told from the trace", n.Type)
}

// TraceSkipPrefix is how the engine reports a guard that excluded a task.
// A branch skip carries no such marker, which is why State does not rely on it
// alone.
const TraceSkipPrefix = "Skipping"

// Skipped reports whether this step was visited without doing its work.
func (n RunNode) Skipped() bool {
	st, _ := n.State()
	return st == RunStateSkipped
}

// Ran reports whether this step actually did its work.
func (n RunNode) Ran() bool {
	st, _ := n.State()
	return st == RunStateRan
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
	// VerdictCannotCompare means the TRACE cannot say whether the step ran,
	// because its node type has no established shape. This is ranked with the
	// divergences, not with agreement: treating it as "no disagreement" would
	// make the tool quietest exactly where its evidence is weakest, which is
	// the failure mode that produced the bug this comparison exists to catch.
	VerdictCannotCompare Verdict = "cannot-compare"
)

// TaskComparison is one task, as planned and as run.
type TaskComparison struct {
	Task    string  `json:"task"`
	Verdict Verdict `json:"verdict"`
	// Planned is the simulate status, empty when the plan never saw it.
	Planned SimStatus `json:"planned,omitempty"`
	// Ran is what the trace shows.
	Ran bool `json:"ran"`
	// Status is what the step really reported: an HTTP status for an API
	// call, or a word like "success" for an email send.
	Status string `json:"status,omitempty"`
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
	// CannotCompare counts steps whose trace shape cannot establish whether
	// they ran. Unlike Unresolved this is NOT a clean bill of health: it
	// means the comparison has a hole, and it is reported as one.
	CannotCompare int `json:"cannot_compare"`
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
			tc.Status = node.NodeOutput.Status.String()
		}

		plannedToRun := step.Status == SimWouldCall || step.Status == SimStubbed
		runState, runWhy := node.State()

		switch {
		case inRun && runState == RunStateUnknown:
			// Checked before every other case: without this the step would
			// fall through to a comparison built on node.Ran(), which is
			// false for an unknown, and be reported as agreement or as a
			// skip. Both would be assertions the trace does not support.
			tc.Verdict = VerdictCannotCompare
			tc.Detail = "the trace cannot establish whether this ran, so the plan cannot be " +
				"checked against it: " + runWhy

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
		case VerdictCannotCompare:
			c.CannotCompare++
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
		// A hole in the evidence sorts among the divergences, above the
		// merely-unresolved, because it is the tool admitting it cannot see.
		VerdictCannotCompare: 2,
		VerdictNotInPlan:     3,
		VerdictNotInRun:      4,
		VerdictUnresolved:    5,
		VerdictAgree:         6,
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		return rank[tasks[i].Verdict] < rank[tasks[j].Verdict]
	})
}
