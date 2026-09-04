package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func rawList(t *testing.T, items ...string) []json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, len(items))
	for i, s := range items {
		out[i] = json.RawMessage(s)
	}
	return out
}

// A workflow jc cannot read must not present as a workflow that is not
// there. The caller then goes looking for something sitting in the list.
func TestResolveID_UndecodableRecordIsNotReportedAsNotFound(t *testing.T) {
	rows := rawList(t, `"a bare string, not a workflow object"`)
	_, err := ResolveID(rows, "nightly-offboard")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The message must LEAD with what happened. It may go on to mention
	// "not found" — explaining what the caller would otherwise have seen is
	// the useful half — so a substring check on that phrase would be
	// checking this test's own explanatory wording.
	if !strings.HasPrefix(err.Error(), "workflow 1 of 1 did not decode") {
		t.Errorf("error should lead with the decode failure, got: %v", err)
	}
}

// Ambiguity was already correct here and must stay so: workflow names are
// not unique, and picking one of two silently is the defect this avoided.
func TestResolveID_AmbiguousNameStillListsEveryID(t *testing.T) {
	rows := rawList(t,
		`{"id":"aaaaaaaaaaaaaaaaaaaaaaa1","name":"nightly"}`,
		`{"id":"aaaaaaaaaaaaaaaaaaaaaaa2","name":"nightly"}`)
	_, err := ResolveID(rows, "nightly")
	if err == nil {
		t.Fatal("two workflows share that name and one was returned anyway")
	}
	for _, want := range []string{"ambiguous", "aaaaaaaaaaaaaaaaaaaaaaa1", "aaaaaaaaaaaaaaaaaaaaaaa2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message missing %q: %v", want, err)
		}
	}
}

func TestResolveID_GenuineMissStillSaysNotFound(t *testing.T) {
	rows := rawList(t, `{"id":"aaaaaaaaaaaaaaaaaaaaaaa1","name":"nightly"}`)
	_, err := ResolveID(rows, "no-such-workflow")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want a not-found error, got: %v", err)
	}
}

// The catalog audit reports gaps — types emitted that the catalog lacks.
// A skipped record shrinks the observed set, so the tool built to catch a
// catalog that omits things would report no omissions.
func TestObservedEventTypes_UndecodableBucketIsReported(t *testing.T) {
	_, err := ObservedEventTypes(rawList(t,
		`{"key":"user.create","doc_count":4}`,
		`{"key":{"nested":"object"},"doc_count":2}`))
	if err == nil {
		t.Fatal("a bucket that would not decode was skipped — the audit would " +
			"report fewer emitted types, and so fewer catalog gaps, than exist")
	}
	if !strings.Contains(err.Error(), "fewer catalog gaps") {
		t.Errorf("error should name the consequence, got: %v", err)
	}
}

// An empty key is a bucket with no type, not a record jc could not read.
func TestObservedEventTypes_EmptyKeyStaysALegitimateSkip(t *testing.T) {
	got, err := ObservedEventTypes(rawList(t,
		`{"key":"user.create","doc_count":4}`,
		`{"key":"","doc_count":9}`))
	if err != nil {
		t.Fatalf("an empty key is not drift: %v", err)
	}
	if len(got) != 1 || got["user.create"] != 4 {
		t.Errorf("got %v, want just user.create=4", got)
	}
}

// This subsystem has shipped an absence-read-as-negative twice. A record
// that will not decode must not quietly make the newest event look older.
func TestNewestEventTime_ReportsRatherThanSkipping(t *testing.T) {
	if _, err := NewestEventTime(rawList(t, `"not an event object"`)); err == nil {
		t.Fatal("an undecodable event was skipped; the newest event would look older than it is")
	}
	if _, err := NewestEventTime(rawList(t,
		`{"timestamp":"2026-09-04T10:00:00Z"}`,
		`{"timestamp":"yesterday"}`)); err == nil {
		t.Fatal("an unparseable timestamp was skipped")
	}
}

func TestNewestEventTime_PicksTheNewestAndToleratesAnAbsentTimestamp(t *testing.T) {
	got, err := NewestEventTime(rawList(t,
		`{"timestamp":"2026-09-01T10:00:00Z"}`,
		`{"other":"field"}`,
		`{"timestamp":"2026-09-04T10:00:00Z"}`))
	if err != nil {
		t.Fatalf("an event with no timestamp is not drift: %v", err)
	}
	if got.Format("2006-01-02") != "2026-09-04" {
		t.Errorf("got %v, want the newest", got)
	}
}
