package workflow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func eventsWorkflow(eventType, condition, status string) Workflow {
	with := `"source":"jc_events","type":"` + eventType + `"`
	if condition != "" {
		with += `,"condition":"` + condition + `"`
	}
	return Workflow{
		ID: "wf-1", Name: "probe", Status: status, TriggerType: TriggerEvents,
		DSL: json.RawMessage(`{"schedule":{"on":{"one":{"with":{` + with + `}}}},
		  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`),
	}
}

// The verdict the product cannot produce: the event happened, repeatedly, and
// the workflow never ran.
func TestAssessHealth_NeverFiredIsTheActionableCase(t *testing.T) {
	r := AssessHealth(eventsWorkflow("user_create", "", StatusActive), 47, 0, time.Time{}, EventRecency{Known: true})
	if r.Verdict != HealthNeverFired {
		t.Fatalf("verdict = %q, want never-fired", r.Verdict)
	}
	if !strings.Contains(r.Detail, "47 user_create events occurred") {
		t.Errorf("detail should quantify it: %q", r.Detail)
	}
}

// No events means nothing can be concluded — a typo and a quiet period look
// identical from here. Claiming health either way would be a lie.
func TestAssessHealth_NoEventsIsUnverifiable(t *testing.T) {
	r := AssessHealth(eventsWorkflow("user_create", "", StatusActive), 0, 0, time.Time{}, EventRecency{Known: true})
	if r.Verdict != HealthUnverifiable {
		t.Fatalf("verdict = %q, want unverifiable", r.Verdict)
	}
	if !strings.Contains(r.Detail, "indistinguishable") {
		t.Errorf("detail should say why it cannot be judged: %q", r.Detail)
	}
}

// An unknown event type turns an ambiguous silence into a strong hint.
func TestAssessHealth_UnknownEventTypeSharpensTheVerdict(t *testing.T) {
	quiet := AssessHealth(eventsWorkflow("user_creat", "", StatusActive), 0, 0, time.Time{}, EventRecency{Known: true})
	if !quiet.UnknownEventType {
		t.Error("a type absent from the catalog should be flagged")
	}
	if !strings.Contains(quiet.Detail, "typo likely") {
		t.Errorf("silence plus an unknown type should point at the spelling: %q", quiet.Detail)
	}

	fired := AssessHealth(eventsWorkflow("user_creat", "", StatusActive), 5, 0, time.Time{}, EventRecency{Known: true})
	if !strings.Contains(fired.Detail, "check the spelling first") {
		t.Errorf("never-fired plus an unknown type should lead with spelling: %q", fired.Detail)
	}
}

// A trigger condition legitimately filters, so fewer runs than events is not
// evidence of a fault. Reporting it as one would make the check untrustworthy.
func TestAssessHealth_ConditionExplainsFewerRuns(t *testing.T) {
	r := AssessHealth(eventsWorkflow("user_create", "input.x == 1", StatusActive), 100, 3, time.Time{}, EventRecency{Known: true})
	if r.Verdict != HealthFiring {
		t.Fatalf("3 runs against 100 events is firing, not broken: %q", r.Verdict)
	}
	if !r.Conditioned {
		t.Error("the trigger condition should be recorded")
	}
	if !strings.Contains(r.Detail, "fewer runs than events is expected") {
		t.Errorf("detail should explain the gap rather than imply a fault: %q", r.Detail)
	}

	// But zero runs with a condition still warrants attention — pointed at
	// the condition rather than the trigger.
	none := AssessHealth(eventsWorkflow("user_create", "input.x == 1", StatusActive), 100, 0, time.Time{}, EventRecency{Known: true})
	if none.Verdict != HealthNeverFired {
		t.Errorf("verdict = %q, want never-fired", none.Verdict)
	}
	if !strings.Contains(none.Detail, "check whether it can ever match") {
		t.Errorf("detail should point at the condition: %q", none.Detail)
	}
}

// Inactive and non-event workflows are not expected to run, so judging them
// would be noise.
func TestAssessHealth_NotApplicableCases(t *testing.T) {
	inactive := AssessHealth(eventsWorkflow("user_create", "", StatusInactive), 47, 0, time.Time{}, EventRecency{Known: true})
	if inactive.Verdict != HealthNotApplicable {
		t.Errorf("an inactive workflow is not broken: %q", inactive.Verdict)
	}

	external := Workflow{ID: "w", Name: "n", Status: StatusActive, TriggerType: TriggerExternal,
		DSL: json.RawMessage(`{"schedule":{"on":{"one":{"with":{"source":"external"}}}},"do":[]}`)}
	if got := AssessHealth(external, 0, 0, time.Time{}, EventRecency{Known: true}); got.Verdict != HealthNotApplicable {
		t.Errorf("an external workflow has no event stream to compare: %q", got.Verdict)
	}
}

func TestSortHealth_ActionableFirst(t *testing.T) {
	reports := []HealthReport{
		{Name: "d", Verdict: HealthNotApplicable},
		{Name: "c", Verdict: HealthFiring},
		{Name: "b", Verdict: HealthUnverifiable},
		{Name: "a", Verdict: HealthNeverFired},
	}
	SortHealth(reports)
	if reports[0].Verdict != HealthNeverFired || reports[3].Verdict != HealthNotApplicable {
		t.Errorf("order = %v", []HealthVerdict{reports[0].Verdict, reports[1].Verdict,
			reports[2].Verdict, reports[3].Verdict})
	}
}

func TestRunsWithin(t *testing.T) {
	since := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	runs := []Run{
		{WorkflowID: "wf-1", StartedAt: "2026-08-25T10:00:00Z"},
		{WorkflowID: "wf-1", StartedAt: "2026-08-01T10:00:00Z"}, // outside
		{WorkflowID: "wf-2", StartedAt: "2026-08-25T10:00:00Z"}, // other workflow
	}
	if got := RunsWithin(runs, "wf-1", since); got != 1 {
		t.Errorf("RunsWithin = %d, want 1", got)
	}

	// An unparseable timestamp is counted, not dropped: undercounting
	// produces a false never-fired verdict, which is the expensive direction.
	odd := []Run{{WorkflowID: "wf-1", StartedAt: "not-a-time"}}
	if got := RunsWithin(odd, "wf-1", since); got != 1 {
		t.Errorf("an unparseable run should count, got %d", got)
	}
}

// A workflow younger than the window cannot have answered events that predate
// it. Without the clamp every freshly created workflow reads as broken, which
// is the fastest way to make the whole report ignorable.
func TestEffectiveSince_ClampsToCreation(t *testing.T) {
	since := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)

	young := eventsWorkflow("user_create", "", StatusActive)
	young.CreatedAt = created.Format(time.RFC3339)
	if got := EffectiveSince(young, since); !got.Equal(created) {
		t.Errorf("EffectiveSince = %v, want the creation time %v", got, created)
	}

	old := eventsWorkflow("user_create", "", StatusActive)
	old.CreatedAt = "2026-01-01T00:00:00Z"
	if got := EffectiveSince(old, since); !got.Equal(since) {
		t.Errorf("an older workflow keeps the requested window, got %v", got)
	}

	// A missing timestamp must not narrow the window to nothing.
	if got := EffectiveSince(eventsWorkflow("user_create", "", StatusActive), since); !got.Equal(since) {
		t.Errorf("absent created_at should fall back to the window, got %v", got)
	}
}

// When the window collapsed to the workflow's lifetime, say so — otherwise
// "no events" reads as a quiet tenant rather than a short observation.
func TestAssessHealth_NamesTheNarrowedWindow(t *testing.T) {
	start := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	w := eventsWorkflow("user_create", "", StatusActive)
	w.CreatedAt = start.Format(time.RFC3339)

	r := AssessHealth(w, 0, 0, start, EventRecency{Known: true})
	if !strings.Contains(r.Detail, "covers its lifetime") {
		t.Errorf("detail should disclose the narrowed window: %q", r.Detail)
	}
	if r.WindowStart != "2026-08-27T00:00:00Z" {
		t.Errorf("WindowStart = %q", r.WindowStart)
	}
}

func TestAssessHealth_CountsReadAsEnglish(t *testing.T) {
	one := AssessHealth(eventsWorkflow("user_create", "", StatusActive), 1, 0, time.Time{}, EventRecency{Known: true})
	if !strings.Contains(one.Detail, "1 user_create event occurred") {
		t.Errorf("a single event should not read as \"1 events\": %q", one.Detail)
	}
	firing := AssessHealth(eventsWorkflow("user_create", "", StatusActive), 1, 1, time.Time{}, EventRecency{Known: true})
	if !strings.Contains(firing.Detail, "1 run against 1 user_create event") {
		t.Errorf("detail = %q", firing.Detail)
	}
}

// A run proves the event fired, whatever Directory Insights has indexed yet.
//
// Nothing other than the trigger event starts a jc_events workflow, so a run in
// the window is direct evidence — and better evidence than the event count,
// which lags by a few seconds. Observed live: immediately after creating a
// group, health reported runs_in_window 1 alongside "no group_create events in
// the window, so ... indistinguishable", contradicting itself in one row. The
// same call 30 seconds later said firing.
//
// That window is exactly when someone checks a workflow they just wired up.
func TestAssessHealth_RunProvesTheEventFiredDespiteIndexingLag(t *testing.T) {
	r := AssessHealth(eventsWorkflow("group_create", "", StatusActive), 0, 1, time.Time{}, EventRecency{Known: true})

	if r.Verdict != HealthFiring {
		t.Fatalf("verdict = %q, want firing — a run cannot happen without the event", r.Verdict)
	}
	if !strings.Contains(r.Detail, "proof the event fired") {
		t.Errorf("the detail must rest the conclusion on run evidence: %q", r.Detail)
	}
	// It must not pretend the event count agreed.
	if !strings.Contains(r.Detail, "no indexed group_create events yet") {
		t.Errorf("the detail must disclose that DI has not caught up: %q", r.Detail)
	}
	if strings.Contains(r.Detail, "indistinguishable") {
		t.Errorf("the unverifiable wording must not survive here: %q", r.Detail)
	}
}

// The genuinely ambiguous case is unchanged: no events AND no runs.
func TestAssessHealth_NoEventsNoRunsStaysUnverifiable(t *testing.T) {
	r := AssessHealth(eventsWorkflow("group_create", "", StatusActive), 0, 0, time.Time{}, EventRecency{Known: true})
	if r.Verdict != HealthUnverifiable {
		t.Errorf("verdict = %q, want unverifiable", r.Verdict)
	}
}

// The whole truth table, so a future edit cannot quietly move one cell.
func TestAssessHealth_VerdictTruthTable(t *testing.T) {
	for _, c := range []struct {
		events, runs int
		want         HealthVerdict
	}{
		{0, 0, HealthUnverifiable},
		{0, 1, HealthFiring},     // the fixed row
		{5, 0, HealthNeverFired}, // the case the tool exists for
		{5, 2, HealthFiring},
	} {
		got := AssessHealth(eventsWorkflow("group_create", "", StatusActive), c.events, c.runs, time.Time{}, EventRecency{Known: true})
		if got.Verdict != c.want {
			t.Errorf("events=%d runs=%d → %q, want %q", c.events, c.runs, got.Verdict, c.want)
		}
	}
}

// never-fired is the verdict the tool exists for, and until now it had never
// been produced. All four verdicts were observed live in one sweep on
// 2026-08-31; this pins the shapes that generate them.
//
// Note the method: a MISSPELLED event type does NOT produce never-fired, which
// is what a work order proposed. Health counts events of the workflow's OWN
// trigger type, so a workflow watching "group_creat" sees zero events of that
// type and is correctly unverifiable — flagged with UnknownEventType, which is
// the useful signal there. Producing never-fired needs the opposite: an event
// type that IS emitted, with something else stopping the run. A trigger
// condition that can never match does it.
func TestAssessHealth_AllFourVerdictsFromRealShapes(t *testing.T) {
	// Observed: events=1 runs=0 — real type, condition that cannot match.
	cannotMatch := AssessHealth(
		eventsWorkflow("group_create", "resource.name == 'zz-no-such-group-ever'", StatusActive),
		1, 0, time.Time{}, EventRecency{Known: true})
	if cannotMatch.Verdict != HealthNeverFired {
		t.Errorf("a real event with no run is never-fired, got %q", cannotMatch.Verdict)
	}

	// Observed: events=0 runs=0 — misspelled type, nothing to count.
	typo := AssessHealth(eventsWorkflow("group_creat", "", StatusActive), 0, 0, time.Time{}, EventRecency{Known: true})
	if typo.Verdict != HealthUnverifiable {
		t.Errorf("a misspelled type yields no events of that type, so it is unverifiable, got %q",
			typo.Verdict)
	}
	if !typo.UnknownEventType {
		t.Error("the misspelling is the actionable signal here and must be flagged")
	}

	// Observed: events=2 runs=2.
	firing := AssessHealth(eventsWorkflow("group_create", "", StatusActive), 2, 2, time.Time{}, EventRecency{Known: true})
	if firing.Verdict != HealthFiring {
		t.Errorf("verdict = %q, want firing", firing.Verdict)
	}

	// Observed: events=0 runs=1, inside the DI indexing window.
	lagging := AssessHealth(eventsWorkflow("group_create", "", StatusActive), 0, 1, time.Time{}, EventRecency{Known: true})
	if lagging.Verdict != HealthFiring {
		t.Errorf("verdict = %q, want firing on run evidence", lagging.Verdict)
	}
}

// A workflow that has not run YET is not a workflow that failed to run.
//
// Observed: workflow created 16:39:36, group created 16:39:53, health called at
// 16:39:56 and 16:40:10 both said never-fired — and the run started at 16:40:12.
func TestAssessHealth_RecentEventIsTooSoonToJudge(t *testing.T) {
	now := time.Now().UTC()
	r := AssessHealth(eventsWorkflow("group_create", "", StatusActive), 1, 0, time.Time{},
		EventRecency{Known: true, WithinGrace: 1, Newest: now.Add(-3 * time.Second)})

	if r.Verdict != HealthUnverifiable {
		t.Fatalf("verdict = %q, want unverifiable — the run may still be pending", r.Verdict)
	}
	if !strings.Contains(r.Detail, "too recent to judge") {
		t.Errorf("the detail must say why it is withholding judgement: %q", r.Detail)
	}
	if r.GraceSeconds != int(TriggerGrace.Seconds()) {
		t.Errorf("grace_window_seconds = %d, want %d", r.GraceSeconds, int(TriggerGrace.Seconds()))
	}
	if r.NewestEvent == "" {
		t.Error("the newest event inside the window is the evidence and must be reported")
	}
}

// Past the window, silence is a fault again — the verdict the tool exists for.
func TestAssessHealth_OldEventWithNoRunIsStillNeverFired(t *testing.T) {
	r := AssessHealth(eventsWorkflow("group_create", "", StatusActive), 1, 0, time.Time{},
		EventRecency{Known: true, WithinGrace: 0})

	if r.Verdict != HealthNeverFired {
		t.Fatalf("verdict = %q, want never-fired", r.Verdict)
	}
	if !strings.Contains(r.Detail, "none arrived in the last") {
		t.Errorf("the detail should say the grace window was cleared: %q", r.Detail)
	}
}

// The third instance of one pattern: a verdict inferred from data that could
// not be obtained.
//
// Round 1 read "DI has not indexed it" as "the event did not happen". Round 2
// read "no run yet" as "it will never run". Round 3 read "recency lookup
// failed" as "nothing recent", falling through to never-fired with the guard
// silently skipped. Absent data is not evidence of a negative.
func TestAssessHealth_UnknownRecencyFailsSafe(t *testing.T) {
	r := AssessHealth(eventsWorkflow("group_create", "", StatusActive), 5, 0, time.Time{},
		EventRecency{Known: false})

	if r.Verdict != HealthUnverifiable {
		t.Fatalf("verdict = %q; a recency lookup that failed must not become an alarm", r.Verdict)
	}
	if !strings.Contains(r.Detail, "could not be determined") {
		t.Errorf("the detail must say the measurement failed, not imply a quiet tenant: %q", r.Detail)
	}
	// A window that was never applied must not be advertised as if it were.
	if r.GraceSeconds != 0 {
		t.Errorf("grace_window_seconds = %d on a row where the guard could not run", r.GraceSeconds)
	}
}

// A conditioned trigger is NOT exempt from the grace window.
//
// The tempting argument is that its non-run is expected rather than pending.
// That is wrong: a condition that DOES match still produces a pending run, so
// exempting conditioned workflows reintroduces the same false positive for the
// harder case, where "the condition filtered it" and "the run has not arrived"
// are indistinguishable from outside.
func TestAssessHealth_ConditionedTriggerIsNotExemptFromGrace(t *testing.T) {
	conditioned := eventsWorkflow("group_create", "resource.name == 'no-such-group'", StatusActive)
	now := time.Now().UTC()

	recent := AssessHealth(conditioned, 1, 0, time.Time{},
		EventRecency{Known: true, WithinGrace: 1, Newest: now.Add(-5 * time.Second)})
	if recent.Verdict != HealthUnverifiable {
		t.Errorf("a matching condition still produces a pending run, got %q", recent.Verdict)
	}

	settled := AssessHealth(conditioned, 1, 0, time.Time{}, EventRecency{Known: true})
	if settled.Verdict != HealthNeverFired {
		t.Fatalf("verdict = %q, want never-fired once the window has passed", settled.Verdict)
	}
	if !settled.Conditioned || !strings.Contains(settled.Detail, "can ever match") {
		t.Errorf("the condition caveat must survive: %+v", settled)
	}
}

// Every row the guard applies to reports the same fields, whichever branch ran.
// A shape that varies by code path makes a field-set assertion pass or fail by
// luck — the same defect as records of one response carrying different keys.
func TestAssessHealth_RowShapeIsUniformAcrossVerdicts(t *testing.T) {
	w := eventsWorkflow("group_create", "", StatusActive)
	now := time.Now().UTC()

	rows := []HealthReport{
		AssessHealth(w, 1, 0, time.Time{}, EventRecency{Known: true, WithinGrace: 1, Newest: now}),
		AssessHealth(w, 1, 0, time.Time{}, EventRecency{Known: true}),
		AssessHealth(w, 2, 2, time.Time{}, EventRecency{Known: true}),
		AssessHealth(w, 0, 0, time.Time{}, EventRecency{Known: true}),
	}
	for i, r := range rows {
		if r.GraceSeconds != int(TriggerGrace.Seconds()) {
			t.Errorf("row %d omits grace_window_seconds (verdict %q); the guard applied to it",
				i, r.Verdict)
		}
	}
}
