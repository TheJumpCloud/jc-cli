package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The three loops below existed twice each — once in internal/cmd and once
// in internal/mcp — and each copy carried the same defect: a record that
// would not decode was skipped in silence. Duplication is why the defect
// existed twice, so the fix is to have one copy.

// ResolveID turns a workflow name or ID into an ID.
//
// A record that will not decode is an error, not a skip. Skipping one
// turns "I could not read this workflow" into "workflow not found", and
// the caller goes looking for something that is sitting in the list.
//
// Ambiguity was already handled correctly here and is preserved: names are
// not unique, so several matches report every ID rather than picking one.
func ResolveID(rows []json.RawMessage, identifier string) (string, error) {
	var byName []Workflow
	for i, r := range rows {
		w, err := ParseWorkflow(r)
		if err != nil {
			return "", fmt.Errorf("workflow %d of %d did not decode: %w — the record shape "+
				"has changed, and %q would otherwise be reported as not found",
				i+1, len(rows), err, identifier)
		}
		if w.ID == identifier {
			return w.ID, nil
		}
		if strings.EqualFold(w.Name, identifier) {
			byName = append(byName, w)
		}
	}

	switch len(byName) {
	case 0:
		return "", fmt.Errorf("workflow %q not found", identifier)
	case 1:
		return byName[0].ID, nil
	default:
		ids := make([]string, 0, len(byName))
		for _, w := range byName {
			ids = append(ids, w.ID)
		}
		return "", fmt.Errorf("workflow name %q is ambiguous (%d share it: %s); use an ID",
			identifier, len(byName), strings.Join(ids, ", "))
	}
}

// NewestEventTime returns the newest timestamp across matching events.
//
// Callers use this to decide whether a workflow that has never run SHOULD
// have run. Silently skipping an event that will not decode makes the
// newest event look older than it is, or absent entirely — and this
// subsystem has already shipped that bug twice, once returning the oldest
// event as the newest and once letting missing recency read as
// never-fired. On error the caller must record recency as UNKNOWN rather
// than substitute a zero time, which the verdict logic already handles as
// its own case.
func NewestEventTime(events []json.RawMessage) (time.Time, error) {
	var newest time.Time
	for i, raw := range events {
		var e struct {
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return time.Time{}, fmt.Errorf("event %d of %d did not decode: %w",
				i+1, len(events), err)
		}
		if e.Timestamp == "" {
			// Legitimately absent: this event carries no timestamp, which
			// is not the same as one jc could not read.
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			return time.Time{}, fmt.Errorf("event %d of %d has timestamp %q that is not RFC3339: %w",
				i+1, len(events), e.Timestamp, err)
		}
		if ts.After(newest) {
			newest = ts
		}
	}
	return newest, nil
}

// ObservedEventTypes builds the emitted-type histogram the catalog audit
// diffs against.
//
// This one matters most. AuditCatalog reports GAPS — types the tenant
// emitted that the catalog does not list — so a skipped record shrinks the
// observed set and the audit reports fewer gaps. The whole point of the
// tool is catching a catalog that omits things; a decode failure would
// make the omission detector report no omissions.
//
// An empty key stays a legitimate skip: that is a bucket with no type, not
// a record jc could not read.
func ObservedEventTypes(items []json.RawMessage) (map[string]int, error) {
	observed := make(map[string]int, len(items))
	for i, raw := range items {
		var row struct {
			Key   string `json:"key"`
			Count int    `json:"doc_count"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, fmt.Errorf("event-type bucket %d of %d did not decode: %w — the "+
				"audit would otherwise report fewer emitted types than exist, and so "+
				"fewer catalog gaps", i+1, len(items), err)
		}
		if row.Key == "" {
			continue
		}
		observed[row.Key] = row.Count
	}
	return observed, nil
}
