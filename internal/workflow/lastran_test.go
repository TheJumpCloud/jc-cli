package workflow

import (
	"encoding/json"
	"testing"
)

func TestLastRanByWorkflow_TakesTheMostRecent(t *testing.T) {
	got := LastRanByWorkflow([]Run{
		{WorkflowID: "w1", StartedAt: "2026-08-31T10:00:00Z"},
		{WorkflowID: "w1", StartedAt: "2026-08-31T12:00:00Z"}, // newest
		{WorkflowID: "w1", StartedAt: "2026-08-31T11:00:00Z"},
		{WorkflowID: "w2", StartedAt: "2026-08-30T09:00:00Z"},
	})

	if got["w1"] != "2026-08-31T12:00:00Z" {
		t.Errorf("w1 = %q, want the newest run", got["w1"])
	}
	if got["w2"] != "2026-08-30T09:00:00Z" {
		t.Errorf("w2 = %q", got["w2"])
	}
	if _, ok := got["w3"]; ok {
		t.Error("a workflow with no runs must be absent, not empty")
	}
}

// An unparseable timestamp is still evidence the workflow ran. Dropping it
// would report "never run" for one that demonstrably did — the same
// undercounting mistake RunsWithin avoids.
func TestLastRanByWorkflow_KeepsUnparseableTimestamps(t *testing.T) {
	got := LastRanByWorkflow([]Run{{WorkflowID: "w1", StartedAt: "not-a-time"}})
	if got["w1"] != "not-a-time" {
		t.Errorf("w1 = %q, want the raw value kept", got["w1"])
	}
}

func TestAnnotateLastRan(t *testing.T) {
	row := json.RawMessage(`{"id":"w1","name":"probe","status":"active"}`)
	out := AnnotateLastRan(row, map[string]string{"w1": "2026-08-31T12:00:00Z"})

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got[LastRanKey] != "2026-08-31T12:00:00Z" {
		t.Errorf("last_ran = %v", got[LastRanKey])
	}
	// Everything else must survive untouched.
	if got["name"] != "probe" || got["status"] != "active" {
		t.Errorf("the rest of the object was disturbed: %v", got)
	}
}

// Absent says "never ran" more honestly than an empty string, and matches what
// the documentation described before the field was removed for not existing.
func TestAnnotateLastRan_OmitsWhenNeverRun(t *testing.T) {
	row := json.RawMessage(`{"id":"w2","name":"probe"}`)
	out := AnnotateLastRan(row, map[string]string{"w1": "2026-08-31T12:00:00Z"})

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if _, present := got[LastRanKey]; present {
		t.Errorf("a workflow that never ran must not carry last_ran: %v", got)
	}
}

// A failure to read runs must not lose the workflow list. Degrading to no
// last_ran is fine; dropping rows is not.
func TestAnnotateLastRan_DegradesWithoutRunData(t *testing.T) {
	rows := []json.RawMessage{
		json.RawMessage(`{"id":"w1","name":"a"}`),
		json.RawMessage(`{"id":"w2","name":"b"}`),
	}
	out := AnnotateAllLastRan(rows, nil)
	if len(out) != 2 {
		t.Fatalf("got %d rows, want 2", len(out))
	}
	for i, r := range out {
		if string(r) != string(rows[i]) {
			t.Errorf("row %d was altered: %s", i, r)
		}
	}

	// Nor may a malformed row be dropped.
	bad := []json.RawMessage{json.RawMessage(`"not an object"`)}
	if got := AnnotateAllLastRan(bad, map[string]string{"w1": "t"}); len(got) != 1 {
		t.Errorf("a row that will not parse must be handed back, got %d rows", len(got))
	}
}
