package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// liveRunsResponse is the verbatim body GET /workflows/runs returned from org
// 5ec71e8e96bfda0611fc6c5b on 2026-08-27. The run's workflow had been deleted
// two months earlier and the run still lists: runs are an audit trail that
// outlives its workflow, which neither the spec nor the DSL guide mentions.
const liveRunsResponse = `{
  "results": [
    {
      "completedAt": "2026-05-06T18:42:01.470Z",
      "id": "de8cba84-49c6-4156-8461-c1eee0ec1ac0",
      "name": "test schedule",
      "startedAt": "2026-05-06T18:42:00.364Z",
      "status": "completed",
      "workflowDeletedAt": "2026-08-11T19:43:14.329Z",
      "workflowId": "69fb7e2909118200019812ca"
    }
  ],
  "totalCount": 1
}`

// liveTemplatesResponse is the shape GET /workflows/templates returns — a
// "templates" envelope, unlike the {totalCount, results} used by every other
// list in this area.
const liveTemplatesResponse = `{
  "templates": [
    {
      "category": "Device Management",
      "description": "Runs a targeted command script.",
      "dsl": {"do": [], "schedule": {}},
      "id": "tmpl-1",
      "name": "Execute Command on Device Group Change"
    }
  ]
}`

func TestEndpoints(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{Endpoint, "/workflows"},
		{RunsEndpoint, "/workflows/runs"},
		{TemplatesEndpoint, "/workflows/templates"},
		{WorkflowEndpoint("w1"), "/workflows/w1"},
		{TriggerEndpoint("w1"), "/workflows/w1/runs"},
		{RunEndpoint("r1"), "/workflows/runs/r1"},
		{TemplateEndpoint("t1"), "/workflows/templates/t1"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestParseRuns_SurvivesDeletedWorkflow(t *testing.T) {
	runs, err := ParseRuns(json.RawMessage(liveRunsResponse))
	if err != nil {
		t.Fatalf("ParseRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	if runs[0].WorkflowDeletedAt == "" {
		t.Error("workflowDeletedAt must survive decoding — it is how a caller knows the run outlived its workflow")
	}
	if !runs[0].Done() {
		t.Error("a completed run must report Done")
	}
}

func TestRunDone(t *testing.T) {
	for status, want := range map[string]bool{
		"completed": true, "failed": true, "cancelled": true,
		"running": false, "queued": false, "": false,
	} {
		if got := (Run{Status: status}).Done(); got != want {
			t.Errorf("Done(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestParseTemplates_UsesItsOwnEnvelope(t *testing.T) {
	ts, err := ParseTemplates(json.RawMessage(liveTemplatesResponse))
	if err != nil {
		t.Fatalf("ParseTemplates: %v", err)
	}
	if len(ts) != 1 || ts[0].ID != "tmpl-1" || ts[0].Category != "Device Management" {
		t.Errorf("unexpected templates: %+v", ts)
	}
	// The runs/workflows envelope must NOT decode a template list.
	if rows, _ := ParseList(json.RawMessage(liveTemplatesResponse)); len(rows) != 0 {
		t.Error("templates use a different envelope; ParseList must not silently succeed on one")
	}
}

func TestCreateBody_OmitsTriggerType(t *testing.T) {
	b := CreateBody(Workflow{
		Name: "w", DSL: json.RawMessage(`{}`), ExecutionRoleID: "r1",
		TriggerType: "external", ID: "should-not-be-sent",
	})
	for _, k := range []string{"trigger_type", "id"} {
		if _, ok := b[k]; ok {
			t.Errorf("%s is server-derived and must not be sent on create", k)
		}
	}
	if b["execution_role_id"] != "r1" {
		t.Errorf("execution_role_id missing: %#v", b)
	}
}

func TestUpdateBody_AlwaysCarriesDSL(t *testing.T) {
	// PUT is full-replace, so the DSL must go every time even when only the
	// name changed.
	b := UpdateBody(Workflow{Name: "renamed", DSL: json.RawMessage(`{"do":[]}`)})
	if _, ok := b["dsl"]; !ok {
		t.Error("update must always send the dsl")
	}
	if b["name"] != "renamed" {
		t.Errorf("name = %v", b["name"])
	}
}

func TestTriggerBody(t *testing.T) {
	raw, _ := json.Marshal(TriggerBody(map[string]any{"userId": "u1"}))
	if string(raw) != `{"data":{"userId":"u1"}}` {
		t.Errorf("trigger body = %s", raw)
	}
	raw, _ = json.Marshal(TriggerBody(nil))
	if string(raw) != `{"data":{}}` {
		t.Errorf("nil data must still send an empty data object, got %s", raw)
	}
}

func TestOperationIndex_Loaded(t *testing.T) {
	if n := OperationCount(); n < 700 {
		t.Fatalf("operation index looks truncated: %d entries", n)
	}
	op, ok := LookupOperation("postApiRuncommand")
	if !ok {
		t.Fatal("postApiRuncommand missing from the index")
	}
	if op.Method != "POST" || op.Path != "/api/runCommand" {
		t.Errorf("unexpected operation: %+v", op)
	}
	if op.APIVersion() != 1 {
		t.Errorf("APIVersion = %d, want 1 for a v1 path", op.APIVersion())
	}
	v2, _ := LookupOperation("postApiV2Workflows")
	if v2.APIVersion() != 2 {
		t.Errorf("APIVersion = %d, want 2 for a /api/v2 path", v2.APIVersion())
	}
}

func TestSuggestOperation(t *testing.T) {
	// A near-miss typo should surface the real id.
	got := SuggestOperation("getApiSystemuser", 3)
	if !contains(got, "getApiSystemusers") {
		t.Errorf("typo suggestion missed the obvious match: %v", got)
	}
	// A legacy snake_case id should point at its camelCase replacement.
	got = SuggestOperation("systemusers_list", 5)
	if len(got) == 0 {
		t.Error("legacy id produced no suggestions at all")
	}
}

func TestLooksLegacyID(t *testing.T) {
	if !looksLegacyID("systemusers_list") {
		t.Error("snake_case id should be flagged as legacy")
	}
	if looksLegacyID("getApiSystemusers") {
		t.Error("camelCase id must not be flagged")
	}
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

func TestParseDSL_RejectsUnknownTopLevelKey(t *testing.T) {
	_, err := ParseDSL(json.RawMessage(`{"schedule":{},"do":[],"tasks":[]}`))
	if err == nil {
		t.Fatal("want an error for an unknown top-level key")
	}
	if !strings.Contains(err.Error(), "tasks") {
		t.Errorf("error should name the offending key, got %v", err)
	}
}

func TestRunNode_Describe(t *testing.T) {
	ok := RunNode{Name: "getUser", IsExecuted: true, Success: true, Message: "Task completed."}
	ok.NodeOutput = &struct {
		Method string `json:"method"`
		Status int    `json:"status"`
		URL    string `json:"url"`
		Body   any    `json:"body"`
	}{Method: "GET", Status: 200, URL: "https://example.com/api/systemusers"}
	if got := ok.Describe(); !strings.Contains(got, "[ok]") || !strings.Contains(got, "GET https://example.com/api/systemusers → 200") {
		t.Errorf("Describe = %q", got)
	}

	failed := RunNode{Name: "boom", IsExecuted: true, Success: false}
	if got := failed.Describe(); !strings.Contains(got, "FAILED") {
		t.Errorf("a failed step must be marked: %q", got)
	}

	skipped := RunNode{Name: "never", IsExecuted: false}
	if got := skipped.Describe(); !strings.Contains(got, "skipped") {
		t.Errorf("an unexecuted step must be marked skipped: %q", got)
	}

	// A paginated step aggregates several requests and reports no single
	// status; rendering "→ 0" would invent one.
	paginated := RunNode{Name: "listAll", IsExecuted: true, Success: true, Message: "Task completed."}
	paginated.NodeOutput = &struct {
		Method string `json:"method"`
		Status int    `json:"status"`
		URL    string `json:"url"`
		Body   any    `json:"body"`
	}{}
	if got := paginated.Describe(); strings.Contains(got, "→ 0") {
		t.Errorf("a paginated step must not report a status of 0: %q", got)
	}
}

func TestRun_FailedNode(t *testing.T) {
	r := Run{ExecutionDetails: ExecutionDetails{Nodes: []RunNode{
		{Name: "a", IsExecuted: true, Success: true},
		{Name: "b", IsExecuted: true, Success: false},
		{Name: "c", IsExecuted: true, Success: false},
	}}}
	n, ok := r.FailedNode()
	if !ok || n.Name != "b" {
		t.Errorf("FailedNode should return the FIRST failure, got %+v %v", n, ok)
	}
	if _, ok := (Run{}).FailedNode(); ok {
		t.Error("a run with no trace has no failed node")
	}
}
