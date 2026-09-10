package workflow

import (
	"strings"
	"testing"
	"time"
)

// A verification pass read a live row saying verdict "firing",
// events_in_window 1, window_days 30 — on a tenant with 22 matching events in
// its 30-day history. All correct: the window is clamped to the workflow's
// creation time, and the clamp beats the requested days, which is the
// precedence that prevents a false never-fired.
//
// But the note explaining that lived only in the events==0 branch, so the
// firing row carried the surprising number with nothing to explain it. The
// field is called events_in_window and window_days says 30; read literally
// they contradict each other, and the number is the part that gets quoted.
func TestAssessHealth_LifetimeClampIsExplainedOnEveryVerdict(t *testing.T) {
	created := time.Date(2026, 9, 4, 17, 4, 57, 0, time.UTC)
	windowStart := created // clamped: the workflow is younger than the window
	w := Workflow{
		ID: "w1", Name: "zz-fixture", Status: StatusActive, TriggerType: TriggerEvents,
		CreatedAt: created.Format(time.RFC3339),
		DSL:       []byte(`{"schedule":{"on":{"one":{"with":{"source":"jc_events","type":"admin_login_attempt"}}}},"do":[]}`),
	}
	known := EventRecency{Known: true}

	for name, tc := range map[string]struct{ events, runs int }{
		"firing":       {events: 1, runs: 1},
		"never-fired":  {events: 1, runs: 0},
		"unverifiable": {events: 0, runs: 0},
	} {
		t.Run(name, func(t *testing.T) {
			r := AssessHealth(w, tc.events, tc.runs, windowStart, known)
			if !strings.Contains(r.Detail, "covers its lifetime") {
				t.Errorf("verdict %q does not explain the clamped window: %s", r.Verdict, r.Detail)
			}
		})
	}
}

// The note must NOT appear when the full window applied, or every mature
// workflow gains a sentence about a clamp that did not happen.
func TestAssessHealth_NoClampNoteWhenTheFullWindowApplied(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	windowStart := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC) // long after creation
	w := Workflow{
		ID: "w1", Name: "old", Status: StatusActive, TriggerType: TriggerEvents,
		CreatedAt: created.Format(time.RFC3339),
		DSL:       []byte(`{"schedule":{"on":{"one":{"with":{"source":"jc_events","type":"admin_login_attempt"}}}},"do":[]}`),
	}
	r := AssessHealth(w, 5, 5, windowStart, EventRecency{Known: true})
	if strings.Contains(r.Detail, "covers its lifetime") {
		t.Errorf("a workflow older than the window should carry no clamp note: %s", r.Detail)
	}
}
