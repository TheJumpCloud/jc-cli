package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func simulate(t *testing.T, doc string, input map[string]any) SimResult {
	t.Helper()
	res, err := SimulateRaw(json.RawMessage(doc), input)
	if err != nil {
		t.Fatalf("SimulateRaw: %v", err)
	}
	return res
}

func stepNamed(r SimResult, name string) (SimStep, bool) {
	for _, s := range r.Steps {
		if s.Task == name {
			return s, true
		}
	}
	return SimStep{}, false
}

// The point of a dry run: see what a destructive workflow would touch, with
// real resolved parameters, without touching anything.
func TestSimulate_StubsWritesAndResolvesParams(t *testing.T) {
	r := simulate(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[
	    {"nuke":{"call":"jc_operation","with":{"operationId":"deleteApiSystemusersById","version":1,
	        "pathParams":{"id":"${ input.victim }"}}}},
	    {"tell":{"call":"sendEmailsToAddresses","with":{
	        "message":{"subject":"s","body":"b"},
	        "recipients":{"to_addresses":["ops@example.com"]}}}}]}`,
		map[string]any{"victim": "5ec7221c4d224c0e309577e7"})

	nuke, ok := stepNamed(r, "nuke")
	if !ok {
		t.Fatal("nuke step missing")
	}
	if nuke.Status != SimStubbed {
		t.Errorf("a DELETE must be stubbed, got %q", nuke.Status)
	}
	// The resolved parameter is the whole value: it says WHICH object.
	pp, _ := nuke.Params["pathParams"].(map[string]any)
	if pp["id"] != "5ec7221c4d224c0e309577e7" {
		t.Errorf("the input reference should be resolved, got %#v", pp)
	}

	mail, _ := stepNamed(r, "tell")
	if mail.Status != SimStubbed {
		t.Errorf("email must be stubbed, got %q", mail.Status)
	}
	if !strings.Contains(mail.Why, "email") {
		t.Errorf("why = %q", mail.Why)
	}
}

// A search is a POST that reads. Stubbing it would break the search-then-act
// shape, which is the shape a dry run is most useful for.
func TestSimulate_SearchPostCountsAsARead(t *testing.T) {
	r := simulate(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[{"find":{"call":"jc_operation","with":{"operationId":"postApiSearchSystemusers","version":1,
	      "bodyParams":{"filter":{"and":[{"username":"${ input.u }"}]}}}}}]}`,
		map[string]any{"u": "alice"})

	find, _ := stepNamed(r, "find")
	if find.Status != SimWouldCall {
		t.Errorf("a search POST should be a read, got %q (%s)", find.Status, find.Why)
	}
	if !strings.Contains(find.Why, "search endpoint") {
		t.Errorf("why should explain the exception, got %q", find.Why)
	}
	// And the filter must carry the resolved value, not the template.
	if !strings.Contains(mustJSON(find.Params), "alice") {
		t.Errorf("input reference not resolved into the body: %s", mustJSON(find.Params))
	}
}

// Guards are evaluated with real input, so the plan reflects the branch that
// would actually be taken.
func TestSimulate_EvaluatesGuardsAgainstInput(t *testing.T) {
	doc := `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[
	    {"always":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}},
	    {"maybe":{"if":"${ input.go == \"yes\" }","call":"jc_operation",
	        "with":{"operationId":"getApiSystems","version":1}}}]}`

	on := simulate(t, doc, map[string]any{"go": "yes"})
	if s, _ := stepNamed(on, "maybe"); s.Status != SimWouldCall {
		t.Errorf("a passing guard should let the step through, got %q", s.Status)
	}

	off := simulate(t, doc, map[string]any{"go": "no"})
	s, _ := stepNamed(off, "maybe")
	if s.Status != SimSkipped {
		t.Errorf("a failing guard should skip, got %q", s.Status)
	}
	if !strings.Contains(s.Why, "guard evaluated false") {
		t.Errorf("why = %q", s.Why)
	}
}

// Only the chosen branch target runs; the others are skipped. Matches the
// live-verified model.
func TestSimulate_FollowsTheChosenBranch(t *testing.T) {
	r := simulate(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[
	    {"route":{"switch":[
	       {"hit":{"when":"${ input.go == \"yes\" }","then":"chosen"}},
	       {"default":{"then":"other"}}]}},
	    {"chosen":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}},
	    {"other":{"call":"jc_operation","with":{"operationId":"getApiSystems","version":1}}}]}`,
		map[string]any{"go": "yes"})

	if s, _ := stepNamed(r, "chosen"); s.Status != SimWouldCall {
		t.Errorf("the chosen target should run, got %q", s.Status)
	}
	if s, _ := stepNamed(r, "other"); s.Status != SimSkipped {
		t.Errorf("an unchosen branch target should be skipped, got %q", s.Status)
	}
}

// A reference a dry run cannot know — a prior step's response body — must be
// reported as unresolved rather than guessed at.
func TestSimulate_UnknowableReferenceIsNotInvented(t *testing.T) {
	r := simulate(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[
	    {"first":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}},
	    {"second":{"if":"${ actions.first.status == 200 }","call":"jc_operation",
	        "with":{"operationId":"getApiSystems","version":1}}}]}`, nil)

	s, _ := stepNamed(r, "second")
	if s.Status != SimUnresolved {
		t.Errorf("a guard on an unavailable prior body should be unresolved, got %q (%s)", s.Status, s.Why)
	}
}

// The caveat travels in the payload: a caller reading only the result must
// still see what this cannot tell them.
func TestSimulate_CarriesItsOwnCaveat(t *testing.T) {
	r := simulate(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`, nil)
	if !strings.Contains(r.Caveat, "not a prediction of engine behaviour") {
		t.Errorf("caveat = %q", r.Caveat)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
