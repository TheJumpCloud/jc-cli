package resolve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
)

func raws(t *testing.T, objs ...string) []json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, len(objs))
	for i, s := range objs {
		out[i] = json.RawMessage(s)
	}
	return out
}

// The sharpest case: the record matched by name, but its ID could not be
// read. Reporting "not found" there is a lie about something jc just
// saw, and it sends the operator looking for a resource that is sitting
// in front of them.
func TestMatchByName_MatchedRecordWithUnreadableIDIsAnError(t *testing.T) {
	cfg := ResourceConfig{NameField: "username", IDField: "_id"}
	cases := map[string]string{
		"id is an object": `{"username":"ada","_id":{"$oid":"5ec9ce0000c9510e358c9918"}}`,
		"id is absent":    `{"username":"ada"}`,
	}
	for name, rec := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := matchByName(raws(t, rec), "ada", cfg)
			if err == nil {
				t.Fatalf("a record that matched by name but had no readable ID was "+
					"skipped, leaving %d matches — this reports as \"not found\" for a "+
					"user that plainly exists", len(res.matches))
			}
			if !strings.Contains(err.Error(), "ada") {
				t.Errorf("error should name the resource it found, got: %v", err)
			}
		})
	}
}

// The aggregate case. A rename upstream makes every record unreadable,
// and "not found" alone is indistinguishable from a genuine absence.
func TestMatchByName_NotFoundSaysWhenRecordsWereUnreadable(t *testing.T) {
	cfg := ResourceConfig{NameField: "username", IDField: "_id"}
	res, err := matchByName(raws(t,
		`{"userName":"ada","_id":"1"}`, // field renamed upstream
		`{"userName":"bob","_id":"2"}`,
	), "ada", cfg)
	if err != nil {
		t.Fatalf("unreadable records must not fail the lookup outright: %v", err)
	}
	if res.unreadable != 2 || res.total != 2 {
		t.Fatalf("got unreadable=%d total=%d, want 2/2", res.unreadable, res.total)
	}
	msg := res.notFound("ada", cfg).Error()
	if !strings.Contains(msg, "2 of 2") {
		t.Errorf("the not-found error must disclose the unreadable count, got: %s", msg)
	}
}

// The counterpart, and the reason unreadable is only reported on zero
// matches: a normal heterogeneous listing must not produce a scary
// message when the lookup actually worked.
func TestMatchByName_UnreadableRecordsAreSilentWhenSomethingMatched(t *testing.T) {
	cfg := ResourceConfig{NameField: "username", IDField: "_id"}
	res, err := matchByName(raws(t,
		`"a bare string, not a record"`,
		`{"username":"ada","_id":"5ec9ce0000c9510e358c9918"}`,
	), "ada", cfg)
	if err != nil {
		t.Fatalf("matchByName: %v", err)
	}
	if len(res.matches) != 1 || res.matches[0].ID != "5ec9ce0000c9510e358c9918" {
		t.Fatalf("got %+v, want the one good match", res.matches)
	}
}

// A name that simply differs is the filter working, not a failure. If it
// counted as unreadable, every lookup on a large directory would append
// a spurious schema warning to its not-found error and the real signal
// would be worthless.
func TestMatchByName_NonMatchingNamesDoNotCountAsUnreadable(t *testing.T) {
	cfg := ResourceConfig{NameField: "username", IDField: "_id"}
	res, err := matchByName(raws(t,
		`{"username":"bob","_id":"1"}`,
		`{"username":"carol","_id":"2"}`,
	), "ada", cfg)
	if err != nil {
		t.Fatalf("matchByName: %v", err)
	}
	if res.unreadable != 0 {
		t.Fatalf("unreadable=%d, want 0 — plain non-matches are not drift", res.unreadable)
	}
	if msg := res.notFound("ada", cfg).Error(); strings.Contains(msg, "record shape") {
		t.Errorf("a clean not-found must not mention the record shape, got: %s", msg)
	}
}

// V1 and V2 held identical copies of this loop, which is why the defect
// existed twice. They now share one implementation; this asserts both
// entry points actually reach it, so a future divergence has to break a
// test rather than quietly reintroduce the bug in one of them.
func TestBothResolvers_ReportADriftedRecordRatherThanNotFound(t *testing.T) {
	cfg := ResourceConfig{
		ListEndpoint: "/systemusers",
		NameField:    "username",
		IDField:      "_id",
	}

	t.Run("v1", func(t *testing.T) {
		ts := startTestServer(t, "/systemusers", []map[string]any{
			{"username": "ada", "_id": map[string]any{"$oid": "5ec9ce0000c9510e358c9918"}},
		})
		defer ts.Close()
		assertDriftReported(t, func() (string, error) {
			return NewResolver(testV1Client(ts.URL)).Resolve(context.Background(), "ada", cfg)
		})
	})

	t.Run("v2", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"username":"ada","_id":{"$oid":"5ec9ce0000c9510e358c9918"}}]`))
		}))
		defer ts.Close()
		client := api.NewV2ClientWithKey("test-key")
		client.BaseURL = ts.URL
		assertDriftReported(t, func() (string, error) {
			return NewV2Resolver(client).Resolve(context.Background(), "ada", cfg)
		})
	})
}

func assertDriftReported(t *testing.T, resolve func() (string, error)) {
	t.Helper()
	id, err := resolve()
	if err == nil {
		t.Fatalf("resolver returned %q for a record whose ID would not decode; "+
			"it must not report success or \"not found\" for a resource it can see", id)
	}
	if !strings.Contains(err.Error(), "cannot return an ID") {
		t.Errorf("error should explain the drift, got: %v", err)
	}
}
