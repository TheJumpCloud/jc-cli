package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func node(name, message string, executed, success bool, status int) RunNode {
	n := RunNode{Name: name, Type: NodeTypeOperation, Message: message, IsExecuted: executed, Success: success}
	if status != 0 {
		n.NodeOutput = &NodeOutput{Method: "GET", Status: TraceStatus{Code: status}, URL: "/x"}
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

// Email nodes, now observed. A send populates node_output — the same envelope
// rule as a jc_operation — so the guess this test used to hedge against is
// settled by evidence rather than reasoning.
//
// From run 71db73cc-0221-4fef-8eaa-24f4dc1e788f: the engine reports the call
// type as "email" (NOT "sendEmailsToAddresses"), a send carries
// {"notification_type":"workflows_common","status":"success"}, and a guarded-off
// send carries a null node_output with if_condition.result=false.
func TestRunNode_EmailFollowsTheEnvelopeRule(t *testing.T) {
	sent := RunNode{Name: "notify", Type: NodeTypeEmail,
		Message: "Email notification completed.", IsExecuted: true, Success: true,
		NodeOutput: &NodeOutput{NotificationType: "workflows_common",
			Status: TraceStatus{Text: "success"}}}
	if !sent.Ran() {
		t.Error("a send that populated node_output ran")
	}

	skipped := RunNode{Name: "notify", Type: NodeTypeEmail,
		Message: "Skipping — if condition did not match.", IsExecuted: true, Success: true,
		IfCondition: &IfCondition{Expression: "${ 1 == 2 }", Result: false}}
	if !skipped.Skipped() {
		t.Error("a guarded-off send did not run")
	}
	// The structured guard result is better evidence than English prose, so
	// it should be what the reason quotes.
	if _, why := skipped.State(); !strings.Contains(why, "1 == 2") {
		t.Errorf("the reason should quote the recorded guard: %q", why)
	}
}

// A node type still never seen in any trace must not be guessed at.
// connector_operation is the remaining one; reporting it as skipped on a null
// node_output could be exactly backwards, which is the class of error this
// whole predicate exists to avoid.
func TestRunNode_UnobservedTypeIsUnknownNotSkipped(t *testing.T) {
	mystery := RunNode{Name: "webhook", Type: "some_future_call_type",
		Message: "Task completed.", IsExecuted: true, Success: true}

	st, why := mystery.State()
	if st != RunStateUnknown {
		t.Errorf("State = %q, want unknown — claiming it skipped would be a fabrication", st)
	}
	if !strings.Contains(why, "never been observed") {
		t.Errorf("the reason should admit the gap: %q", why)
	}
	if mystery.Skipped() {
		t.Error("unknown must not collapse into skipped")
	}

	// A recorded guard result is conclusive whatever the node type.
	guarded := RunNode{Name: "webhook", Type: "some_future_call_type",
		Message: "Task completed.", IsExecuted: true, Success: true,
		IfCondition: &IfCondition{Expression: "${ false }", Result: false}}
	if !guarded.Skipped() {
		t.Error("a recorded false guard settles it for any node type")
	}
}

// The engine reports a status as a NUMBER for an API call and a STRING for an
// email send. Typing it as an int made `runs get --trace` fail outright on any
// run containing an email step.
func TestTraceStatus_AcceptsBothForms(t *testing.T) {
	var http TraceStatus
	if err := json.Unmarshal([]byte(`200`), &http); err != nil {
		t.Fatalf("numeric status must parse: %v", err)
	}
	if http.Code != 200 || http.String() != "200" || !http.OK() {
		t.Errorf("numeric status wrong: %+v", http)
	}

	var word TraceStatus
	if err := json.Unmarshal([]byte(`"success"`), &word); err != nil {
		t.Fatalf("string status must parse: %v", err)
	}
	if word.Text != "success" || word.String() != "success" || !word.OK() {
		t.Errorf("string status wrong: %+v", word)
	}

	if failed := (TraceStatus{Code: 404}); failed.OK() {
		t.Error("404 is not OK")
	}

	// A whole trace containing both must parse, which is the actual bug.
	raw := `{"executionDetails":{"nodes":[
	  {"name":"call","type":"jc_operation","is_executed":true,"success":true,
	   "node_output":{"method":"GET","status":200,"url":"/x"}},
	  {"name":"mail","type":"email","is_executed":true,"success":true,
	   "node_output":{"notification_type":"workflows_common","status":"success"}}]}}`
	run, err := ParseRun([]byte(raw))
	if err != nil {
		t.Fatalf("a run mixing both status forms must parse: %v", err)
	}
	if len(run.ExecutionDetails.Nodes) != 2 {
		t.Fatalf("got %d nodes", len(run.ExecutionDetails.Nodes))
	}
	for _, n := range run.ExecutionDetails.Nodes {
		if !n.Ran() {
			t.Errorf("%s should read as having run", n.Name)
		}
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
	if got.Tasks[0].Status != "201" {
		t.Errorf("the real status should be carried through, got %q", got.Tasks[0].Status)
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
