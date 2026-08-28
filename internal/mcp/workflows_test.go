package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const wfProbeDSLJSON = `{
  "schedule": {"on": {"one": {"with": {"source": "external"}}}},
  "do": [{"listOneUser": {"call": "jc_operation", "with": {
      "operationId": "getApiSystemusers", "version": 1, "queryParams": {"limit": 1}}}}]
}`

type wfMock struct {
	*httptest.Server
	lastBody  map[string]any
	triggered int
	deleted   []string
}

func startWorkflowV2Server(t *testing.T) *wfMock {
	t.Helper()
	// Keep the resolver's name→ID cache inside the test's temp dir, so
	// fixture role IDs never reach the developer's real cache or another
	// test.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := &wfMock{}
	external := map[string]any{
		"id": "wf-1", "name": "Nightly Audit", "status": "active",
		"trigger_type": "external", "execution_role_id": "role-1",
		"dsl": mustDecode(t, wfProbeDSLJSON),
	}
	events := map[string]any{
		"id": "wf-2", "name": "On User Suspend", "status": "active",
		"trigger_type": "jc_events", "execution_role_id": "role-1",
		"dsl": json.RawMessage(`{"schedule":{"on":{"one":{"with":{"source":"jc_events","type":"user_suspended"}}}},
		  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`),
	}

	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")

		switch {
		case p == "/workflows" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"totalCount": 2,
				"results": []map[string]any{external, events}})

		case p == "/workflows" && r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&m.lastBody)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": "wf-new", "name": m.lastBody["name"]})

		case p == "/workflows/templates" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"templates": []map[string]any{
				{"id": "tmpl-1", "name": "Run A Command", "category": "Device Management",
					"dsl": json.RawMessage(`{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
					  "do":[{"run":{"call":"jc_operation","with":{"operationId":"postApiRuncommand","version":1,
					     "bodyParams":{"_id":"REPLACE_WITH_COMMAND_ID"}}}}]}`)},
			}})

		case p == "/workflows/runs" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"totalCount": 1, "results": []map[string]any{
				{"id": "run-1", "workflowId": "gone-1", "status": "completed",
					"workflowDeletedAt": "2026-08-11T19:43:14.329Z"}}})

		case p == "/roles" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"id": "role-1", "name": "Read Only"}}})

		case strings.HasPrefix(p, "/roles/") && r.Method == http.MethodGet:
			// users.readonly only. That permits getApiSystemusers (which
			// lists it) but NOT deleteApiSystemusersById (which needs
			// "users" or "users.delete") — the asymmetry the scope check
			// exists to catch.
			json.NewEncoder(w).Encode(map[string]any{
				"id": "role-1", "name": "Read Only",
				"scopes": []string{"users.readonly"}})

		case p == "/workflows/wf-1" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(external)

		case p == "/workflows/wf-2" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(events)

		case strings.HasSuffix(p, "/runs") && r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&m.lastBody)
			m.triggered++
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]any{"id": "run-new", "status": "running"})

		case strings.HasPrefix(p, "/workflows/") && r.Method == http.MethodPut:
			json.NewDecoder(r.Body).Decode(&m.lastBody)
			json.NewEncoder(w).Encode(external)

		case strings.HasPrefix(p, "/workflows/") && r.Method == http.MethodDelete:
			m.deleted = append(m.deleted, strings.TrimPrefix(p, "/workflows/"))
			w.WriteHeader(http.StatusNoContent)

		default:
			t.Errorf("unexpected: %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(m.Close)
	return m
}

func TestMCPWorkflows_List(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_list", map[string]any{}))
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("list must unwrap {results}: %v\n%s", err, out)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 workflows, got %d", len(rows))
	}
}

func TestMCPWorkflows_TemplatesListOmitsDSL(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_templates_list", map[string]any{}))
	var rows []map[string]any
	json.Unmarshal([]byte(out), &rows)
	if len(rows) != 1 {
		t.Fatalf("expected 1 template: %s", out)
	}
	if _, hasDSL := rows[0]["dsl"]; hasDSL {
		t.Error("the list view must omit DSL bodies — the catalog is far too large otherwise")
	}
}

func TestMCPWorkflows_TemplatesInitNamesPlaceholders(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_templates_init", map[string]any{"identifier": "tmpl-1"}))
	var doc map[string]any
	json.Unmarshal([]byte(out), &doc)
	markers, _ := doc["placeholders"].([]any)
	if len(markers) != 1 || markers[0] != "REPLACE_WITH_COMMAND_ID" {
		t.Errorf("init must tell the caller what to fill: %s", out)
	}
	if doc["status"] != "inactive" {
		t.Errorf("initialised workflows start inactive, got %v", doc["status"])
	}
}

func TestMCPWorkflows_ValidateReportsFindings(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_validate", map[string]any{
		"dsl": mustDecode(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
		  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiNonsense","version":1}}}]}`),
	}))
	if !strings.Contains(out, "unknown operationId") {
		t.Errorf("validate should reject an unknown operationId: %s", out)
	}
}

func TestMCPWorkflows_ValidateAcceptsGoodDSL(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_validate", map[string]any{
		"dsl": mustDecode(t, wfProbeDSLJSON),
	}))
	var res map[string]any
	json.Unmarshal([]byte(out), &res)
	if res["trigger_type"] != "external" {
		t.Errorf("expected an external trigger: %s", out)
	}
	findings, _ := res["findings"].([]any)
	if len(findings) != 0 {
		t.Errorf("a clean DSL should produce no findings: %s", out)
	}
}

func TestMCPWorkflows_ExplainResolvesOperationIDs(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_explain", map[string]any{
		"dsl": mustDecode(t, wfProbeDSLJSON),
	}))
	if !strings.Contains(out, "GET /api/systemusers") {
		t.Errorf("explain must resolve operationIds to real endpoints: %s", out)
	}
}

func TestMCPWorkflows_ExplainByIdentifier(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_explain", map[string]any{"identifier": "Nightly Audit"}))
	if !strings.Contains(out, "GET /api/systemusers") {
		t.Errorf("explain should also work on a deployed workflow: %s", out)
	}
}

func TestMCPWorkflows_CreatePlanDoesNotWrite(t *testing.T) {
	srv := startWorkflowV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_create", map[string]any{
		"name": "probe", "dsl": mustDecode(t, wfProbeDSLJSON), "role": "Read Only",
	}))
	var m map[string]any
	json.Unmarshal([]byte(out), &m)
	if m["plan"] != true {
		t.Fatalf("expected a plan: %s", out)
	}
	if srv.lastBody != nil {
		t.Error("plan mode must not write")
	}
}

func TestMCPWorkflows_CreateRefusesSideEffects(t *testing.T) {
	srv := startWorkflowV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	emailDSL := mustDecode(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[{"mail":{"call":"sendEmailsToAddresses","with":{
	     "message":{"subject":"s","body":"b"},
	     "recipients":{"to_addresses":["ops@example.com"]}}}}]}`)

	out := getResultText(t, callTool(t, cs, "workflows_create", map[string]any{
		"name": "mailer", "dsl": emailDSL, "role": "Read Only", "execute": true,
	}))
	if !strings.Contains(out, "allow_side_effects") {
		t.Errorf("a mailing workflow must be refused without the opt-in: %s", out)
	}
	if !strings.Contains(out, "ops@example.com") {
		t.Errorf("the refusal should name who would be mailed: %s", out)
	}
	if srv.lastBody != nil {
		t.Error("nothing should have been sent")
	}

	// With the opt-in it goes through.
	callTool(t, cs, "workflows_create", map[string]any{
		"name": "mailer", "dsl": emailDSL, "role": "Read Only",
		"allow_side_effects": true, "execute": true,
	})
	if srv.lastBody == nil {
		t.Error("expected a create after opting in")
	}
}

func TestMCPWorkflows_CreateRejectsInvalidDSL(t *testing.T) {
	srv := startWorkflowV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_create", map[string]any{
		"name": "bad", "role": "Read Only", "execute": true,
		"dsl": mustDecode(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},"do":[]}`),
	}))
	if !strings.Contains(out, "at least one task") {
		t.Errorf("an empty do must be rejected locally: %s", out)
	}
	if srv.lastBody != nil {
		t.Error("nothing should have been sent")
	}
}

func TestMCPWorkflows_UpdateSendsCompleteObject(t *testing.T) {
	srv := startWorkflowV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	callTool(t, cs, "workflows_update", map[string]any{
		"identifier": "wf-1", "name": "Renamed", "execute": true,
	})
	if srv.lastBody["name"] != "Renamed" {
		t.Errorf("name = %v", srv.lastBody["name"])
	}
	if _, ok := srv.lastBody["dsl"]; !ok {
		t.Error("PUT is full-replace, so update must resend the DSL")
	}
	if srv.lastBody["execution_role_id"] != "role-1" {
		t.Errorf("the execution role must survive an update: %v", srv.lastBody["execution_role_id"])
	}
}

func TestMCPWorkflows_TriggerRefusesNonExternal(t *testing.T) {
	srv := startWorkflowV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_trigger", map[string]any{
		"identifier": "wf-2", "execute": true,
	}))
	if !strings.Contains(out, "jc_events") {
		t.Errorf("a non-external workflow must be refused: %s", out)
	}
	if srv.triggered != 0 {
		t.Error("nothing should have been posted")
	}
}

func TestMCPWorkflows_TriggerPlanListsWhatWouldRun(t *testing.T) {
	srv := startWorkflowV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_trigger", map[string]any{"identifier": "wf-1"}))
	if !strings.Contains(out, "/api/systemusers") {
		t.Errorf("the plan must say what would actually execute: %s", out)
	}
	if srv.triggered != 0 {
		t.Error("plan mode must not trigger")
	}
}

func TestMCPWorkflows_TriggerWrapsDataEnvelope(t *testing.T) {
	srv := startWorkflowV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	callTool(t, cs, "workflows_trigger", map[string]any{
		"identifier": "wf-1", "data": map[string]any{"note": "hi"}, "execute": true,
	})
	if srv.triggered != 1 {
		t.Fatalf("expected one trigger, got %d", srv.triggered)
	}
	data, _ := srv.lastBody["data"].(map[string]any)
	if data["note"] != "hi" {
		t.Errorf("run input must be wrapped in a data envelope: %#v", srv.lastBody)
	}
}

func TestMCPWorkflows_RunsSurviveDeletion(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_runs_list", map[string]any{}))
	if !strings.Contains(out, "workflowDeletedAt") {
		t.Errorf("a run whose workflow was deleted must still list: %s", out)
	}
}

func TestMCPWorkflows_ReadOnlyModeBlocksWrites(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{ReadOnly: true})

	for name, args := range map[string]map[string]any{
		"workflows_create":  {"name": "x", "dsl": mustDecode(t, wfProbeDSLJSON), "role": "Read Only", "execute": true},
		"workflows_update":  {"identifier": "wf-1", "name": "y", "execute": true},
		"workflows_delete":  {"identifier": "wf-1", "execute": true},
		"workflows_trigger": {"identifier": "wf-1", "execute": true},
	} {
		out := getResultText(t, callTool(t, cs, name, args))
		if !strings.Contains(out, "read-only") {
			t.Errorf("%s should be refused in read-only mode, got: %s", name, out)
		}
	}
}

// mustDecode turns a DSL literal into the decoded object MCP arguments carry.
func mustDecode(t *testing.T, doc string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("bad test DSL: %v", err)
	}
	return m
}

// The scope check is the reason this exists: without it, validation confirms
// an operation exists, not that the role may call it, so a destructive
// operationId passes silently.
func TestMCPWorkflows_ValidateWithRoleFlagsScopeGaps(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	destructive := mustDecode(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[{"destroy":{"call":"jc_operation","with":{"operationId":"deleteApiSystemusersById",
	     "version":1,"pathParams":{"id":"x"}}}}]}`)

	// Without a role, the destructive step draws no comment at all.
	out := getResultText(t, callTool(t, cs, "workflows_validate", map[string]any{"dsl": destructive}))
	if strings.Contains(out, "may not permit") {
		t.Errorf("no role was given, so no scope finding is possible:\n%s", out)
	}

	// With one, it is named.
	out = getResultText(t, callTool(t, cs, "workflows_validate", map[string]any{
		"dsl": destructive, "role": "Read Only",
	}))
	if !strings.Contains(out, "deleteApiSystemusersById") || !strings.Contains(out, "may not permit") {
		t.Errorf("the scope gap should be reported:\n%s", out)
	}
	// Advisory only — the spec's scope list is a lower bound.
	var res struct {
		Findings []struct {
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result is not a validation report: %v\n%s", err, out)
	}
	for _, f := range res.Findings {
		if strings.Contains(f.Message, "may not permit") && f.Severity != "warning" {
			t.Errorf("a scope gap must be a warning, got %q", f.Severity)
		}
	}
}

// create already resolves a role, so the gaps belong in its plan — the last
// point before a run-time permission failure.
func TestMCPWorkflows_CreatePlanReportsScopeGaps(t *testing.T) {
	srv := startWorkflowV2Server(t)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "workflows_create", map[string]any{
		"name": "destroyer", "role": "Read Only",
		"dsl": mustDecode(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
		  "do":[{"destroy":{"call":"jc_operation","with":{"operationId":"deleteApiSystemusersById",
		     "version":1,"pathParams":{"id":"x"}}}}]}`),
	}))
	if !strings.Contains(out, "scope_gaps") {
		t.Errorf("the plan should carry the scope gaps:\n%s", out)
	}
	if !strings.Contains(out, "the API will reject the create if so") {
		t.Errorf("the plan should say what happens next:\n%s", out)
	}
	if srv.lastBody != nil {
		t.Error("plan mode must not write")
	}
}

// The catalog is 341 entries and the payload field list is ~16 per entry.
// Returning both unfiltered was ~174KB in a single tool result — roughly 45k
// tokens — which is not a reasonable thing to hand a model.
func TestMCPWorkflows_EventTypesOmitsFieldsOnABroadBrowse(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	type row struct {
		EventType     string   `json:"event_type"`
		PayloadFields []string `json:"payload_fields"`
	}
	type resp struct {
		Matched    int    `json:"matched"`
		Note       string `json:"note"`
		EventTypes []row  `json:"event_types"`
	}
	decode := func(s string) resp {
		t.Helper()
		var r resp
		if err := json.Unmarshal([]byte(s), &r); err != nil {
			t.Fatalf("not a listing: %v", err)
		}
		return r
	}

	broadRaw := getResultText(t, callTool(t, cs, "workflows_event_types", map[string]any{}))
	broad := decode(broadRaw)
	for _, r := range broad.EventTypes {
		if len(r.PayloadFields) > 0 {
			t.Fatalf("an unfiltered browse must not carry per-event field lists (%d bytes total)", len(broadRaw))
		}
	}
	// And it must say why, and how to get them.
	if !strings.Contains(broad.Note, "Narrow with service or search") {
		t.Errorf("note = %q", broad.Note)
	}

	// Narrowing returns the fields, which are the point of asking.
	narrowRaw := getResultText(t, callTool(t, cs, "workflows_event_types", map[string]any{
		"service": "access_management",
	}))
	narrow := decode(narrowRaw)
	if len(narrow.EventTypes) == 0 {
		t.Fatal("expected matches for access_management")
	}
	if len(narrow.EventTypes[0].PayloadFields) == 0 {
		t.Error("a narrowed query should carry the fields")
	}
	if narrow.Note != "" {
		t.Errorf("a narrowed query should not carry the omission note: %q", narrow.Note)
	}
	if len(narrowRaw) >= len(broadRaw) {
		t.Errorf("a narrowed query should be smaller: narrow=%d broad=%d", len(narrowRaw), len(broadRaw))
	}
}
