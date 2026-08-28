package workflow

import (
	"strings"
	"testing"
)

func node(name, message string, executed, success bool, status int) RunNode {
	n := RunNode{Name: name, Type: NodeTypeOperation, Message: message, IsExecuted: executed, Success: success}
	if status != 0 {
		n.NodeOutput = &struct {
			Method string `json:"method"`
			Status int    `json:"status"`
			URL    string `json:"url"`
			Body   any    `json:"body"`
		}{Method: "GET", Status: status, URL: "/x"}
	}
	return n
}

func runWith(nodes ...RunNode) Run {
	all := append([]RunNode{{Name: "__trigger", Type: "trigger", IsExecuted: true, Success: true}}, nodes...)
	return Run{ID: "run-1", ExecutionDetails: ExecutionDetails{Nodes: all}}
}

// node_output is the only field that tells the truth about whether a call node
// did its work. is_executed, success and message are all identical between a
// task that ran and a task a branch routed around.
//
// This is not a style preference. Reading those fields at face value is how
// this repo previously concluded that untargeted tasks run in array order —
// they do not. A switch jumped past an untargeted task that would have created
// a user group; afterwards the group did not exist, while the jump target's
// group did.
func TestRunNode_StateUsesNodeOutputNotTheLyingFields(t *testing.T) {
	// The trap: every field except node_output says this succeeded.
	branchSkipped := node("orphan", "Task completed.", true, true, 0)
	if st, why := branchSkipped.State(); st != RunStateSkipped {
		t.Errorf("State = %q (%s); a call node with no node_output made no call", st, why)
	}
	if branchSkipped.Ran() {
		t.Error("an untargeted task did not run, whatever is_executed and success say")
	}
	if _, why := branchSkipped.State(); !strings.Contains(why, "branch not taken") {
		t.Errorf("the reason should distinguish a branch skip from a guard skip: %q", why)
	}

	guardSkipped := node("guarded", "Skipping — if condition did not match.", true, true, 0)
	if !guardSkipped.Skipped() {
		t.Error("a guard-skipped node must report skipped")
	}
	if _, why := guardSkipped.State(); !strings.Contains(why, "guard") {
		t.Errorf("a guard skip should say so: %q", why)
	}

	ran := node("fetch", "Task completed.", true, true, 200)
	if ran.Skipped() || !ran.Ran() {
		t.Error("a node carrying a response must report as having run")
	}

	// A run that halted leaves later nodes with is_executed=false.
	notReached := node("after", "Not executed — workflow failed at a prior task.", false, true, 0)
	if st, why := notReached.State(); st != RunStateSkipped || !strings.Contains(why, "earlier task") {
		t.Errorf("State = %q (%s), want skipped with the halt named", st, why)
	}

	failed := node("boom", "Request failed.", true, false, 404)
	if st, _ := failed.State(); st != RunStateFailed {
		t.Errorf("State = %q, want failed", st)
	}

	// Describe must agree, or the trace renders a task that did nothing as ok.
	if got := branchSkipped.Describe(); !strings.Contains(got, "[skipped]") {
		t.Errorf("Describe = %q, want it marked skipped", got)
	}
}

// A node type this package has not observed must not be guessed at. An email
// task legitimately carries no node_output, so applying the call-node rule to
// it would report every sent email as skipped.
func TestRunNode_UnobservedTypeIsUnknownNotSkipped(t *testing.T) {
	email := RunNode{Name: "notify", Type: "sendEmailsToAddresses",
		Message: "Task completed.", IsExecuted: true, Success: true}

	st, why := email.State()
	if st != RunStateUnknown {
		t.Errorf("State = %q, want unknown — claiming it skipped would be a fabrication", st)
	}
	if !strings.Contains(why, "no established trace shape") {
		t.Errorf("the reason should admit the gap: %q", why)
	}
	if email.Skipped() {
		t.Error("unknown must not collapse into skipped")
	}

	// A guard message is still conclusive for any type.
	skipped := RunNode{Name: "notify", Type: "sendEmailsToAddresses",
		Message: "Skipping — if condition did not match.", IsExecuted: true, Success: true}
	if !skipped.Skipped() {
		t.Error("an explicit skip message is conclusive whatever the node type")
	}
}

func TestCompareRun_Agreement(t *testing.T) {
	sim := SimResult{Steps: []SimStep{
		{Task: "fetch", Status: SimWouldCall},
		{Task: "guarded", Status: SimSkipped, Why: "condition false"},
	}}
	got := CompareRun(sim, runWith(
		node("fetch", "Task completed.", true, true, 200),
		node("guarded", "Skipping — if condition did not match.", true, true, 0),
	))

	if got.Diverge != 0 {
		t.Errorf("plan and run agree; got %d divergences: %+v", got.Diverge, got.Tasks)
	}
	if got.Agree != 2 {
		t.Errorf("Agree = %d, want 2", got.Agree)
	}
	if got.RunID != "run-1" {
		t.Errorf("RunID = %q", got.RunID)
	}
}

// The direction that matters: the workflow touched something the plan said it
// would not.
func TestCompareRun_RanButPlannedSkipIsRankedFirst(t *testing.T) {
	sim := SimResult{Steps: []SimStep{
		{Task: "safe", Status: SimWouldCall},
		{Task: "surprise", Status: SimSkipped, Why: "guard looked false"},
	}}
	got := CompareRun(sim, runWith(
		node("safe", "Task completed.", true, true, 200),
		node("surprise", "Task completed.", true, true, 201),
	))

	if got.Tasks[0].Verdict != VerdictRanButPlannedSkip {
		t.Fatalf("the understated case must sort first, got %+v", got.Tasks)
	}
	if got.Tasks[0].Status != 201 {
		t.Errorf("the real status should be carried through, got %d", got.Tasks[0].Status)
	}
	if !strings.Contains(got.Tasks[0].Detail, "guard looked false") {
		t.Errorf("the detail should quote the plan's reasoning: %q", got.Tasks[0].Detail)
	}
}

func TestCompareRun_SkippedButPlannedRun(t *testing.T) {
	sim := SimResult{Steps: []SimStep{{Task: "email", Status: SimStubbed}}}
	got := CompareRun(sim, runWith(node("email", "Skipping — if condition did not match.", true, true, 0)))

	if got.Tasks[0].Verdict != VerdictSkippedButPlannedRun {
		t.Errorf("verdict = %q", got.Tasks[0].Verdict)
	}
}

// A halt explains every later task's absence. Without saying so, those look
// like planner bugs rather than the run stopping.
func TestCompareRun_HaltExplainsMissingTasks(t *testing.T) {
	sim := SimResult{Steps: []SimStep{
		{Task: "boom", Status: SimWouldCall},
		{Task: "after", Status: SimWouldCall},
	}}
	got := CompareRun(sim, runWith(node("boom", "Request failed.", true, false, 404)))

	if !got.RunHalted || got.HaltedAt != "boom" {
		t.Errorf("the halt should be recorded: halted=%v at=%q", got.RunHalted, got.HaltedAt)
	}
	var after TaskComparison
	for _, tc := range got.Tasks {
		if tc.Task == "after" {
			after = tc
		}
	}
	if after.Verdict != VerdictNotInRun {
		t.Fatalf("verdict = %q, want not-in-run", after.Verdict)
	}
	if !strings.Contains(after.Detail, "halted") {
		t.Errorf("the detail must explain the absence rather than imply a planner bug: %q", after.Detail)
	}
}

// A task in the run and not the plan must be reported, not dropped — a
// systematic naming mismatch would otherwise look like perfect agreement.
func TestCompareRun_TaskOnlyInRun(t *testing.T) {
	got := CompareRun(SimResult{}, runWith(node("mystery", "Task completed.", true, true, 200)))
	if len(got.Tasks) != 1 || got.Tasks[0].Verdict != VerdictNotInPlan {
		t.Errorf("an unplanned task must surface: %+v", got.Tasks)
	}
	if got.Diverge != 1 {
		t.Errorf("Diverge = %d, want 1", got.Diverge)
	}
}

// An unresolvable step is not a disagreement — the plan said it could not
// know, and holding that against it would train people to ignore divergences.
func TestCompareRun_UnresolvedIsNotCountedAsWrong(t *testing.T) {
	sim := SimResult{Steps: []SimStep{{Task: "needsBody", Status: SimUnresolved, Why: "references a prior response"}}}
	got := CompareRun(sim, runWith(node("needsBody", "Task completed.", true, true, 200)))

	if got.Tasks[0].Verdict != VerdictUnresolved {
		t.Errorf("verdict = %q, want unresolved-in-plan", got.Tasks[0].Verdict)
	}
	if !strings.Contains(got.Tasks[0].Detail, "references a prior response") {
		t.Errorf("the detail should say why: %q", got.Tasks[0].Detail)
	}
	// It must not inflate the divergence count, or the headline number stops
	// meaning "things that need explaining".
	if got.Diverge != 0 {
		t.Errorf("Diverge = %d, want 0 — the plan declared it could not know", got.Diverge)
	}
	if got.Unresolved != 1 {
		t.Errorf("Unresolved = %d, want 1", got.Unresolved)
	}
}

// The trigger node is synthetic and has no planned counterpart; comparing it
// would report a permanent false divergence on every run.
func TestCompareRun_IgnoresTriggerNode(t *testing.T) {
	got := CompareRun(SimResult{}, runWith())
	if len(got.Tasks) != 0 {
		t.Errorf("the trigger node must not be compared: %+v", got.Tasks)
	}
}
