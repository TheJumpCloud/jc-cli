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
	r := AssessHealth(eventsWorkflow("user_create", "", StatusActive), 47, 0, time.Time{})
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
	r := AssessHealth(eventsWorkflow("user_create", "", StatusActive), 0, 0, time.Time{})
	if r.Verdict != HealthUnverifiable {
		t.Fatalf("verdict = %q, want unverifiable", r.Verdict)
	}
	if !strings.Contains(r.Detail, "indistinguishable") {
		t.Errorf("detail should say why it cannot be judged: %q", r.Detail)
	}
}

// An unknown event type turns an ambiguous silence into a strong hint.
func TestAssessHealth_UnknownEventTypeSharpensTheVerdict(t *testing.T) {
	quiet := AssessHealth(eventsWorkflow("user_creat", "", StatusActive), 0, 0, time.Time{})
	if !quiet.UnknownEventType {
		t.Error("a type absent from the catalog should be flagged")
	}
	if !strings.Contains(quiet.Detail, "typo likely") {
		t.Errorf("silence plus an unknown type should point at the spelling: %q", quiet.Detail)
	}

	fired := AssessHealth(eventsWorkflow("user_creat", "", StatusActive), 5, 0, time.Time{})
	if !strings.Contains(fired.Detail, "check the spelling first") {
		t.Errorf("never-fired plus an unknown type should lead with spelling: %q", fired.Detail)
	}
}

// A trigger condition legitimately filters, so fewer runs than events is not
// evidence of a fault. Reporting it as one would make the check untrustworthy.
func TestAssessHealth_ConditionExplainsFewerRuns(t *testing.T) {
	r := AssessHealth(eventsWorkflow("user_create", "input.x == 1", StatusActive), 100, 3, time.Time{})
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
	none := AssessHealth(eventsWorkflow("user_create", "input.x == 1", StatusActive), 100, 0, time.Time{})
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
	inactive := AssessHealth(eventsWorkflow("user_create", "", StatusInactive), 47, 0, time.Time{})
	if inactive.Verdict != HealthNotApplicable {
		t.Errorf("an inactive workflow is not broken: %q", inactive.Verdict)
	}

	external := Workflow{ID: "w", Name: "n", Status: StatusActive, TriggerType: TriggerExternal,
		DSL: json.RawMessage(`{"schedule":{"on":{"one":{"with":{"source":"external"}}}},"do":[]}`)}
	if got := AssessHealth(external, 0, 0, time.Time{}); got.Verdict != HealthNotApplicable {
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

	r := AssessHealth(w, 0, 0, start)
	if !strings.Contains(r.Detail, "covers its lifetime") {
		t.Errorf("detail should disclose the narrowed window: %q", r.Detail)
	}
	if r.WindowStart != "2026-08-27T00:00:00Z" {
		t.Errorf("WindowStart = %q", r.WindowStart)
	}
}

func TestAssessHealth_CountsReadAsEnglish(t *testing.T) {
	one := AssessHealth(eventsWorkflow("user_create", "", StatusActive), 1, 0, time.Time{})
	if !strings.Contains(one.Detail, "1 user_create event occurred") {
		t.Errorf("a single event should not read as \"1 events\": %q", one.Detail)
	}
	firing := AssessHealth(eventsWorkflow("user_create", "", StatusActive), 1, 1, time.Time{})
	if !strings.Contains(firing.Detail, "1 run against 1 user_create event") {
		t.Errorf("detail = %q", firing.Detail)
	}
}
