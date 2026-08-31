package workflow

import (
	"fmt"
	"sort"
	"time"
)

// Health reporting for jc_events workflows.
//
// A workflow with a mistyped or never-emitted event type saves, activates, and
// silently never fires. The product gives no signal for this: a workflow that
// can never match and one whose event has not happened yet look identical —
// there is no match counter and no last-evaluated timestamp.
//
// jc can tell them apart, because it holds both halves. Directory Insights
// knows how many times an event type actually occurred; the workflow runs list
// knows how many times the workflow ran. Cross-referencing them turns an
// invisible failure into something reportable.
//
// The reasoning is deliberately conservative. A trigger may carry a condition
// that legitimately filters most events, so a run count BELOW the event count
// proves nothing. Only two shapes are decisive: events occurred and the
// workflow never ran (suspicious), or no events occurred at all (unverifiable,
// because a typo and a quiet period are indistinguishable from here).

// TriggerGrace is how long after an event a workflow is given to run before
// its silence is called a fault.
//
// Directory Insights and the workflow engine lag INDEPENDENTLY, and either can
// be ahead. A previous fix covered DI being behind — a run with no indexed
// event. This covers the engine being behind: an indexed event with no run yet.
//
// Trigger latency is not characteristic. Two observations of the same event
// type, same DSL shape, same tenant:
//
//	2026-08-28   ~0.5s from event to run start
//	2026-08-31   ~19s
//
// A 40x spread with no apparent cause, so any inference from "no run yet" has
// to be sized for the slow case — and 19s is only the slowest OBSERVED, not a
// known bound. 60s covers it with margin.
//
// This costs nothing for the verdict's real use case: a workflow broken for
// days has events far older than any grace window.
const TriggerGrace = 60 * time.Second

// EventRecency answers the only question the grace window needs: was there a
// matching event too recently for silence to mean anything?
//
// It is deliberately NOT "the newest event's timestamp". Directory Insights
// ignores the sort parameter — verified by issuing the same query with
// "-timestamp", "timestamp" and no sort at all, and receiving byte-identical
// ascending results all three times. So there is no way to ask the API for the
// newest event, and taking the first row of a limited query returns the OLDEST,
// which is what made the first version of this guard inert: fed the oldest
// event, "is it within 60s?" is almost always false for any workflow with
// history, so never-fired fired immediately — exactly what the window existed
// to prevent.
//
// Asking instead for events inside the grace window sidesteps the ordering
// problem entirely: if that query returns anything, an event is recent; if it
// returns nothing, every matching event is older than the window and silence is
// meaningful. The set is small enough to scan for a real newest timestamp.
type EventRecency struct {
	// Known is false when the lookup could not be performed. A verdict must
	// never be inferred from an absence it could not measure.
	Known bool
	// WithinGrace is how many matching events fell inside the window.
	WithinGrace int
	// Newest is the newest matching event, set only when one was inside the
	// window. Outside it, the API cannot tell us which was newest.
	Newest time.Time
}

// HealthVerdict classifies one workflow.
type HealthVerdict string

const (
	// HealthFiring means the event occurred and the workflow ran.
	HealthFiring HealthVerdict = "firing"
	// HealthNeverFired means the event occurred and the workflow never ran.
	// This is the failure the product cannot surface.
	HealthNeverFired HealthVerdict = "never-fired"
	// HealthUnverifiable means the event did not occur in the window, so
	// nothing can be concluded.
	HealthUnverifiable HealthVerdict = "unverifiable"
	// HealthNotApplicable covers workflows with no jc_events trigger, and
	// inactive ones, which are not expected to run.
	HealthNotApplicable HealthVerdict = "n/a"
)

// HealthReport is one workflow's assessment.
type HealthReport struct {
	WorkflowID string        `json:"workflow_id"`
	Name       string        `json:"name"`
	Status     string        `json:"status"`
	Trigger    string        `json:"trigger_type"`
	EventType  string        `json:"event_type,omitempty"`
	Events     int           `json:"events_in_window"`
	Runs       int           `json:"runs_in_window"`
	Verdict    HealthVerdict `json:"verdict"`
	Detail     string        `json:"detail"`
	// Conditioned notes that the trigger filters events, so a run count
	// below the event count is expected rather than suspicious.
	Conditioned bool `json:"trigger_has_condition,omitempty"`
	// UnknownEventType flags a type absent from the catalog, which makes a
	// never-fired verdict far more likely to be a typo.
	UnknownEventType bool `json:"unknown_event_type,omitempty"`
	// NewestEvent is the newest matching event INSIDE the grace window, when
	// there was one. Outside the window it is empty, because Directory
	// Insights offers no way to ask which event was newest — see EventRecency.
	NewestEvent string `json:"newest_event,omitempty"`
	// GraceSeconds is the window a workflow is given to respond to an event
	// before its silence counts against it.
	//
	// Reported on EVERY row the guard applies to, whether or not it changed
	// the verdict, so a field-set assertion does not pass or fail depending on
	// which code path ran. A row shape that varies by path is the same defect
	// as a list whose records carry different keys.
	GraceSeconds int `json:"grace_window_seconds,omitempty"`
	// RecentEvents is how many matching events fell inside the grace window.
	RecentEvents int `json:"recent_events_in_grace_window"`
	// WindowStart is where the comparison actually began. It is the later of
	// the requested window and the workflow's creation time — see
	// EffectiveSince for why that clamp is not optional.
	WindowStart string `json:"window_start,omitempty"`
}

// EffectiveSince returns the point from which a workflow can fairly be judged:
// the later of the requested window start and the workflow's own creation.
//
// Without this clamp the check accuses every new workflow. A workflow created
// an hour ago cannot have responded to an event from five days ago, but the
// raw comparison sees "47 events, 0 runs" and calls it broken. That single
// false positive would be enough to make the whole report untrustworthy,
// because the newest workflows are exactly the ones an operator is checking.
//
// An unparseable or absent created_at falls back to the requested window: a
// missing timestamp should not silently narrow the comparison to nothing.
func EffectiveSince(w Workflow, since time.Time) time.Time {
	created, err := time.Parse(time.RFC3339, w.CreatedAt)
	if err != nil {
		return since
	}
	if created.After(since) {
		return created
	}
	return since
}

// AssessHealth builds the verdict for one workflow from its counts.
//
// events is how many times the trigger's event type occurred in the window;
// runs is how many times this workflow ran in the same window. Both counts
// must be taken over the SAME window, and that window must start no earlier
// than EffectiveSince — otherwise a workflow younger than the window reads as
// never-fired.
func AssessHealth(w Workflow, events, runs int, windowStart time.Time, recency EventRecency) HealthReport {
	r := HealthReport{
		WorkflowID: w.ID,
		Name:       w.Name,
		Status:     w.Status,
		Trigger:    w.TriggerType,
		Events:     events,
		Runs:       runs,
	}
	if !windowStart.IsZero() {
		r.WindowStart = windowStart.UTC().Format(time.RFC3339)
	}
	if !recency.Newest.IsZero() {
		r.NewestEvent = recency.Newest.UTC().Format(time.RFC3339)
	}
	if recency.Known {
		r.GraceSeconds = int(TriggerGrace.Seconds())
		r.RecentEvents = recency.WithinGrace
	}

	if w.Status != StatusActive {
		r.Verdict, r.Detail = HealthNotApplicable, "workflow is not active, so it is not expected to run"
		return r
	}
	if w.TriggerType != TriggerEvents {
		r.Verdict = HealthNotApplicable
		r.Detail = "only jc_events workflows can be checked this way; a " + w.TriggerType +
			" trigger has no event stream to compare against"
		return r
	}

	if d, err := ParseDSL(w.DSL); err == nil {
		if t, terr := d.Trigger(); terr == nil {
			r.EventType = t.EventType
			r.Conditioned = t.Condition != ""
			if t.EventType != "" {
				_, known := LookupEventType(t.EventType)
				r.UnknownEventType = !known
			}
		}
	}

	switch {
	case events == 0 && runs > 0:
		// A run is DIRECT proof the event fired, and better proof than the
		// event count: nothing other than the trigger event starts a
		// jc_events workflow, and unlike Directory Insights the runs list is
		// not subject to indexing lag.
		//
		// Without this the tool reported a run and, in the same row, claimed
		// no evidence the event had fired — self-contradictory. It happened
		// in the first ~30 seconds after an event, which is exactly when
		// someone checks whether a workflow they just wired up works, and
		// telling them "unverifiable" while holding proof of firing defeats
		// the tool's only purpose.
		r.Verdict = HealthFiring
		r.Detail = fmt.Sprintf("%s and no indexed %s events yet; the run is proof the event fired "+
			"(Directory Insights indexing lags a few seconds behind the runs list)",
			countOf(runs, "run"), r.EventType)

	case events == 0:
		r.Verdict = HealthUnverifiable
		r.Detail = fmt.Sprintf("no %s events in the window, so a workflow that can never match "+
			"and one whose event simply has not happened are indistinguishable", r.EventType)
		if r.UnknownEventType {
			r.Detail += "; the event type is also absent from the catalog, which makes a typo likely"
		}
		if created, cerr := time.Parse(time.RFC3339, w.CreatedAt); cerr == nil && created.After(windowStart.Add(-time.Second)) {
			r.Detail += "; the workflow was only created " + created.UTC().Format(time.RFC3339) +
				", so the comparison covers its lifetime and nothing longer"
		}

	case runs == 0 && !recency.Known:
		// Recency could not be measured, so silence proves nothing. Falling
		// through to never-fired here would be the THIRD time this check
		// inferred a negative from data it could not obtain: once when
		// Directory Insights had not indexed the event, once when the run had
		// not started, and once here. Absent data is not evidence.
		r.Verdict = HealthUnverifiable
		r.Detail = fmt.Sprintf("%s occurred and this workflow has not run, but how recently the "+
			"most recent one arrived could not be determined, so it cannot be told apart from a "+
			"workflow that simply has not got round to it yet",
			countOf(events, r.EventType+" event"))

	case runs == 0 && recency.WithinGrace > 0:
		// The workflow has not failed to run; it has not yet run. Calling that
		// never-fired is worse than the vagueness it replaced — unverifiable
		// admits it does not know, while never-fired is an actionable alarm
		// asserting a falsehood, to the person least able to dismiss it: the
		// one who just wired the workflow up.
		r.Verdict = HealthUnverifiable
		r.Detail = fmt.Sprintf("%s occurred and this workflow has not run, but %s within the last %ds; "+
			"runs have been observed lagging their event by up to ~20s, so this is too recent to judge",
			countOf(events, r.EventType+" event"),
			countOf(recency.WithinGrace, "arrived"), int(TriggerGrace.Seconds()))

	case runs == 0:
		r.Verdict = HealthNeverFired
		r.Detail = fmt.Sprintf("%s occurred and this workflow never ran",
			countOf(events, r.EventType+" event"))
		if recency.Known {
			r.Detail += fmt.Sprintf("; none arrived in the last %ds, well past the window a run may "+
				"lag its event", int(TriggerGrace.Seconds()))
		}
		switch {
		case r.UnknownEventType:
			r.Detail += "; the event type is absent from the catalog, so check the spelling first"
		case r.Conditioned:
			r.Detail += "; the trigger has a condition, so check whether it can ever match " +
				"before assuming the trigger itself is wrong"
		}

	default:
		r.Verdict = HealthFiring
		r.Detail = fmt.Sprintf("%s against %s", countOf(runs, "run"),
			countOf(events, r.EventType+" event"))
		if r.Conditioned && runs < events {
			r.Detail += "; fewer runs than events is expected here, the trigger has a condition"
		}
	}
	return r
}

// countOf renders a count with its noun, so a report reads "1 group_create
// event" rather than the "1 events" that makes a tool look unfinished.
func countOf(n int, noun string) string {
	return fmt.Sprintf("%d %s%s", n, noun, plural(n))
}

// SortHealth orders reports so the actionable ones come first.
func SortHealth(reports []HealthReport) {
	rank := map[HealthVerdict]int{
		HealthNeverFired:    0,
		HealthUnverifiable:  1,
		HealthFiring:        2,
		HealthNotApplicable: 3,
	}
	sort.SliceStable(reports, func(i, j int) bool {
		if rank[reports[i].Verdict] != rank[reports[j].Verdict] {
			return rank[reports[i].Verdict] < rank[reports[j].Verdict]
		}
		return reports[i].Name < reports[j].Name
	})
}

// RunsWithin counts runs of a workflow that started within the window.
func RunsWithin(runs []Run, workflowID string, since time.Time) int {
	n := 0
	for _, run := range runs {
		if run.WorkflowID != workflowID {
			continue
		}
		// A run whose timestamp will not parse is counted rather than
		// dropped: undercounting produces a false never-fired verdict, which
		// is the expensive direction to be wrong in.
		started, err := time.Parse(time.RFC3339, run.StartedAt)
		if err != nil || !started.Before(since) {
			n++
		}
	}
	return n
}
