package workday

import (
	"encoding/json"
	"strings"
	"testing"
)

// VERIFIED live: an org with no Workday integration returns a bare [], not an
// envelope with a zero count. Every sibling area in this API returns an
// envelope, so this is the fact most likely to be mis-assumed by the next
// person, and the one worth pinning hardest.
func TestParseList_EmptyOrgReturnsABareArray(t *testing.T) {
	rows, err := ParseList(json.RawMessage(`[]`))
	if err != nil {
		t.Fatalf("the probed empty response must parse: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

func TestParseList_ArrayOfRecords(t *testing.T) {
	rows, err := ParseList(json.RawMessage(`[{"id":"608b8ce3232e114d1f20e195"},{"id":"63ee0b7c7bafab0001c62301"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("got %d rows, want 2", len(rows))
	}
}

// If an envelope ever appears where a bare array was, that must be loud. The
// record shape here has never been seen with data in it, so the first person
// to run this against a real integration should get a specific failure rather
// than something that looks like a working empty tenant.
func TestParseList_AnEnvelopeWhereAnArrayWasIsReported(t *testing.T) {
	_, err := ParseList(json.RawMessage(`{"results":[],"totalCount":0}`))
	if err == nil {
		t.Fatal("an envelope was accepted where a bare array was probed; an empty " +
			"result here is indistinguishable from an org with no integration")
	}
	if !strings.Contains(err.Error(), "bare array") {
		t.Errorf("the error should say what was probed, got: %v", err)
	}
}

func TestIsObjectID(t *testing.T) {
	for s, want := range map[string]bool{
		"608b8ce3232e114d1f20e195": true,
		"not-an-id":                false,
		"":                         false,
		"608b8ce3232e114d1f20e19":  false, // 23 chars
	} {
		if got := IsObjectID(s); got != want {
			t.Errorf("IsObjectID(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestErrNotObjectID_SaysWhereToFindTheID(t *testing.T) {
	err := ErrNotObjectID("workday-prod")
	for _, want := range []string{"workday-prod", "24-character", "jc workday list"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message missing %q: %v", want, err)
		}
	}
}

// The endpoints that could not be exercised are recorded rather than
// implemented on a guess. If someone implements them, this test should be
// updated in the same change — that is the point of it failing loudly when
// the map empties.
func TestUnprobed_RecordsWhatCouldNotBeVerified(t *testing.T) {
	if len(Unprobed) != 2 {
		t.Errorf("want the two unprobed endpoints recorded, got %d", len(Unprobed))
	}
	for k, v := range Unprobed {
		if !strings.Contains(v, "unobserved") {
			t.Errorf("%s: the note should say the shape was never seen: %s", k, v)
		}
	}
}
