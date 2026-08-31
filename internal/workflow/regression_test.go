package workflow

import (
	"encoding/json"
	"os"
	"testing"
)

// The work-order regression suite, run by CI instead of by hand.
//
// Three defects in compare-run were found by replaying two real runs, and the
// work order kept that replay as its regression suite — read-only, no tenant
// writes, "it should keep passing". It only kept passing while someone
// remembered to run it. The two traces are checked in under testdata (trimmed
// to the fields the predicate reads, so no tenant data travels with them), and
// the expectations below are the ones the work order states.
//
// Fixtures: dc39d61b-60c5-40de-835a-0f2789c0b1ee (orphan/switch) and
// 6b10726f-c9cb-407f-9e76-f7eadd46123e (halt-on-error). Both runs outlive their
// deleted workflows, so the live originals stay available for re-capture.

func loadRun(t *testing.T, name string) Run {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	run, err := ParseRun(raw)
	if err != nil {
		t.Fatalf("fixture %s does not parse: %v", name, err)
	}
	return run
}

func compareFixture(t *testing.T, dsl string, input map[string]any, fixture string) Comparison {
	t.Helper()
	d, err := ParseDSL(json.RawMessage(dsl))
	if err != nil {
		t.Fatal(err)
	}
	return CompareRun(Simulate(d, input), loadRun(t, fixture))
}

const orphanDSL = `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
  "do":[
    {"router":{"switch":[{"always":{"when":"${ 1 == 1 }","then":"stepC"}},
                         {"never":{"when":"${ 1 == 2 }","then":"stepC"}}]}},
    {"orphanStep":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1,"parameters":{"limit":1}}}},
    {"stepC":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1,"parameters":{"limit":1}}}}
  ]}`

const tier2DSL = `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
  "do":[
    {"expectFailure":{"call":"jc_operation","with":{"operationId":"getApiSystemusersById","version":1,
      "pathParams":{"id":"${ input.deadId }"}}}},
    {"unguardedNextStep":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1,"parameters":{"limit":1}}}},
    {"guardedNextStep":{"if":"${ actions.expectFailure.status == 200 }","call":"jc_operation",
      "with":{"operationId":"getApiSystemusers","version":1,"parameters":{"limit":1}}}}
  ]}`

// Work order §6, orphan run: a routing switch, an unreachable task, and the
// task the switch routes to.
func TestRegression_OrphanRun(t *testing.T) {
	got := compareFixture(t, orphanDSL, map[string]any{"probe": "x"}, "run-orphan.json")

	byTask := map[string]TaskComparison{}
	for _, tc := range got.Tasks {
		byTask[tc.Task] = tc
	}

	for _, want := range []struct {
		task    string
		planned SimStatus
		ran     bool
	}{
		{"router", SimSwitched, true},
		{"orphanStep", SimSkipped, false},
		{"stepC", SimWouldCall, true},
	} {
		tc, ok := byTask[want.task]
		if !ok {
			t.Errorf("%s missing from the comparison", want.task)
			continue
		}
		if tc.Verdict != VerdictAgree {
			t.Errorf("%s verdict = %q (%s), want agree", want.task, tc.Verdict, tc.Detail)
		}
		if tc.Planned != want.planned {
			t.Errorf("%s planned = %q, want %q", want.task, tc.Planned, want.planned)
		}
		if tc.Ran != want.ran {
			t.Errorf("%s ran = %v, want %v", want.task, tc.Ran, want.ran)
		}
	}

	if got.Agree != 3 || got.Diverge != 0 {
		t.Errorf("agree/diverge = %d/%d, want 3/0", got.Agree, got.Diverge)
	}
}

// Work order §6, halt-on-error run. The headline case: a step that got a 404
// RAN — reporting it as skipped is the failed-vs-skipped conflation.
func TestRegression_HaltOnErrorRun(t *testing.T) {
	got := compareFixture(t, tier2DSL,
		map[string]any{"deadId": "000000000000000000000000"}, "run-tier2.json")

	byTask := map[string]TaskComparison{}
	for _, tc := range got.Tasks {
		byTask[tc.Task] = tc
	}

	fail := byTask["expectFailure"]
	if !fail.Ran {
		t.Error("a step carrying method, url, status and a body made its call")
	}
	if !fail.Failed {
		t.Error("expectFailure got a 404 and must be marked failed")
	}
	if fail.Status != "404" {
		t.Errorf("status = %q, want 404", fail.Status)
	}
	if fail.Verdict != VerdictAgree {
		t.Errorf("verdict = %q; the plan was right that a call would be made", fail.Verdict)
	}

	if v := byTask["unguardedNextStep"].Verdict; v != VerdictSkippedButPlannedRun {
		t.Errorf("unguardedNextStep = %q, want skipped-but-planned-run", v)
	}
	if v := byTask["guardedNextStep"].Verdict; v != VerdictUnresolved {
		t.Errorf("guardedNextStep = %q, want unresolved-in-plan", v)
	}
	if !got.RunHalted || got.HaltedAt != "expectFailure" {
		t.Errorf("halt = %v at %q, want true at expectFailure", got.RunHalted, got.HaltedAt)
	}
}

// Work order §6, template sweep — the counts and the pointers, offline.
func TestRegression_TemplateSweepInvariants(t *testing.T) {
	// The served catalog is not available offline, so this covers the half
	// that is: jc's corrected copies, and the pointer resolving both ways.
	corrected := LintCorrected()
	if len(corrected) != 4 {
		t.Fatalf("expected 4 corrected copies, got %d", len(corrected))
	}
	for _, s := range corrected {
		if len(s.Result.Findings) != 0 {
			t.Errorf("%s must lint clean: %+v", s.ID, s.Result.Findings)
		}
		if s.Source != SourceJC {
			t.Errorf("%s source = %q, want %q", s.ID, s.Source, SourceJC)
		}
		if s.Corrects == "" {
			t.Errorf("%s does not name what it replaces", s.ID)
		}
		// Round-trip closure: the original points here, and this points back.
		back, ok := CorrectionFor(s.Corrects)
		if !ok || back.ID != s.ID {
			t.Errorf("%s does not round-trip through CorrectionFor(%q)", s.ID, s.Corrects)
		}
	}

	summary := Summarize(corrected)
	if summary.CheckedBySource[SourceJC] != 4 {
		t.Errorf("checked_by_source[jc] = %d, want 4", summary.CheckedBySource[SourceJC])
	}
}

const paginationDSL = `{"schedule":{"on":{"one":{"with":{"source":"external","condition":"probe != \"\""}}}},
  "input":{"schema":{"format":"json","document":{"type":"object","required":["probe"],
    "properties":{"probe":{"type":"string"}}}}},
  "do":[
    {"paginateUsers":{"call":"jc_operation","with":{
      "operationId":"getApiSystemusers","version":1,
      "queryParams":{"limit":2,"skip":0},
      "pagination":{"update":{"in":"queryParams","key":"skip",
        "value":"${ page.request.queryParams.skip + 2 }"},
        "until":"${ len(page.response.body.results) == 0 }"},
      "extract":"${ page.response.body.results }"}}},
    {"bigBody":{"call":"jc_operation","with":{
      "operationId":"getApiSystems","version":1,"queryParams":{"limit":100}}}}
  ]}`

// The only observed node where the two halves of the run/skip predicate come
// apart: node_output is present, status is ABSENT.
//
// A truncated node does not carry a trimmed version of the normal shape — it
// carries a different object entirely: {_preview, _truncated, pageCount,
// totalItems}, with no body, status, method or url. Keying on node_output
// gives the right answer (it ran); keying on status would read it as skipped,
// which is exactly the refactor this fixture exists to make fail loudly.
//
// From run 575ebe87-a1b2-4252-93cf-6b5d097564dd. Note the asymmetry, worth
// knowing: the 7-item EXTRACTED list was truncated while the substantially
// larger raw device body in the same run was not — truncation applies to
// extract output, not to raw response bodies.
func TestRegression_TruncatedNodeRanWithoutAStatus(t *testing.T) {
	got := compareFixture(t, paginationDSL, map[string]any{"probe": "x"}, "run-truncated.json")

	byTask := map[string]TaskComparison{}
	for _, tc := range got.Tasks {
		byTask[tc.Task] = tc
	}

	trunc, ok := byTask["paginateUsers"]
	if !ok {
		t.Fatalf("paginateUsers missing: %+v", got.Tasks)
	}
	if !trunc.Ran {
		t.Error("a truncated node carries node_output, so it ran — do not key on status")
	}
	if trunc.Status != "" {
		t.Errorf("a truncated node has no status; got %q", trunc.Status)
	}
	if trunc.Verdict != VerdictAgree {
		t.Errorf("paginateUsers verdict = %q (%s), want agree", trunc.Verdict, trunc.Detail)
	}

	// The untruncated sibling still reports its status normally.
	if big := byTask["bigBody"]; !big.Ran || big.Status != "200" {
		t.Errorf("bigBody ran=%v status=%q, want true and 200", big.Ran, big.Status)
	}

	if got.Agree != 2 || got.Diverge != 0 {
		t.Errorf("agree/diverge = %d/%d, want 2/0", got.Agree, got.Diverge)
	}
}
