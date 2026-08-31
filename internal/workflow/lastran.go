package workflow

import (
	"encoding/json"
	"time"
)

// last_ran: a computed field, added deliberately.
//
// This BREAKS a rule the rest of this package follows — that a passthrough
// object is returned exactly as the API sent it, and jc does not compose
// fields into it. That rule exists for a good reason: a computed value mixed
// into a passthrough is indistinguishable from an upstream one, so the next
// person debugging a discrepancy cannot tell whether JumpCloud or jc produced
// it. It is why a resolved role NAME was declined on workflows_get, and why
// `admin` and `_id` were left as the API sends them.
//
// The override is deliberate and narrow, for reasons the rest do not have:
//
//   - The field was DOCUMENTED for a long time and did not exist. Callers
//     planned around it and wrote against nothing. Removing the mention (in
//     #119) fixed the lie; it did not give anyone the timestamp they wanted.
//   - "When did this last run" is the question the whole health area answers,
//     and answering it currently requires a second tool call and a join the
//     caller has to write.
//   - Unlike a role name, it cannot go stale in a confusing way: it is derived
//     from the runs list on every read, not cached.
//
// Cost is one extra request per call, not per workflow: the whole runs list is
// fetched once and joined in memory.
//
// It is computed from the RUNS LIST rather than from Directory Insights.
// DI does carry a workflow_run event (initiated_by.id is the workflow, and
// resource.id the run), but it lags the engine by seconds, and a "last ran"
// that trails reality is worse than one that does not exist.

// LastRanKey is the field name, matching what the documentation promised
// before the field was removed for not existing.
const LastRanKey = "last_ran"

// LastRanByWorkflow returns the most recent run start per workflow id.
//
// Runs outlive their workflow, so this may name workflows that no longer
// exist; callers join on the ids they hold rather than iterating this.
func LastRanByWorkflow(runs []Run) map[string]string {
	newest := map[string]time.Time{}
	raw := map[string]string{}
	for _, r := range runs {
		if r.WorkflowID == "" || r.StartedAt == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, r.StartedAt)
		if err != nil {
			// An unparseable timestamp is still evidence the workflow ran, so
			// keep it if nothing better has been seen. Dropping it would
			// report "never run" for a workflow that demonstrably did.
			if _, seen := raw[r.WorkflowID]; !seen {
				raw[r.WorkflowID] = r.StartedAt
			}
			continue
		}
		if prev, seen := newest[r.WorkflowID]; !seen || ts.After(prev) {
			newest[r.WorkflowID] = ts
			raw[r.WorkflowID] = r.StartedAt
		}
	}
	return raw
}

// AnnotateLastRan injects last_ran into a workflow object, and is a no-op when
// the workflow has never run — an absent field says "never" more honestly than
// an empty string, and matches what the documentation described.
func AnnotateLastRan(row json.RawMessage, lastRan map[string]string) json.RawMessage {
	if len(lastRan) == 0 {
		return row
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(row, &obj); err != nil {
		// Not an object: hand it back untouched rather than lose it.
		return row
	}
	var id string
	if rawID, ok := obj["id"]; ok {
		_ = json.Unmarshal(rawID, &id)
	}
	ts, ok := lastRan[id]
	if !ok || ts == "" {
		return row
	}
	enc, err := json.Marshal(ts)
	if err != nil {
		return row
	}
	obj[LastRanKey] = enc
	out, err := json.Marshal(obj)
	if err != nil {
		return row
	}
	return out
}

// AnnotateAllLastRan applies AnnotateLastRan across a list.
func AnnotateAllLastRan(rows []json.RawMessage, lastRan map[string]string) []json.RawMessage {
	if len(lastRan) == 0 {
		return rows
	}
	out := make([]json.RawMessage, len(rows))
	for i, row := range rows {
		out[i] = AnnotateLastRan(row, lastRan)
	}
	return out
}
