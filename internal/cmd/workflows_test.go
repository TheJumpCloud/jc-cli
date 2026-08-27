package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// wfServer mocks the workflow endpoints, faithfully to the live API in the
// ways that matter: /workflows and /workflows/runs answer {totalCount,
// results} while /workflows/templates answers {templates}, a trigger returns
// 202, and a deleted workflow's runs survive.
type wfServer struct {
	*httptest.Server
	workflows map[string]map[string]any
	lastBody  map[string]any
	lastPath  string
	triggered int
	deleted   []string
}

const probeDSL = `{
  "schedule": {"on": {"one": {"with": {"source": "external"}}}},
  "do": [{"listOneUser": {"call": "jc_operation", "with": {
      "operationId": "getApiSystemusers", "version": 1, "queryParams": {"limit": 1}}}}]
}`

func startWFServer(t *testing.T) *wfServer {
	t.Helper()
	s := &wfServer{workflows: map[string]map[string]any{
		"wf-1": {
			"id": "wf-1", "name": "Nightly Audit", "status": "active",
			"trigger_type": "external", "execution_role_id": "role-1",
			"description": "probe", "dsl": json.RawMessage(probeDSL),
		},
		"wf-2": {
			"id": "wf-2", "name": "On User Suspend", "status": "inactive",
			"trigger_type": "jc_events", "execution_role_id": "role-1",
			"dsl": json.RawMessage(`{"schedule":{"on":{"one":{"with":{"source":"jc_events","type":"user_suspended"}}}},
			  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`),
		},
	}}

	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		s.lastPath = p

		switch {
		case p == "/workflows" && r.Method == http.MethodGet:
			results := []map[string]any{}
			for _, wf := range s.workflows {
				results = append(results, wf)
			}
			json.NewEncoder(w).Encode(map[string]any{"totalCount": len(results), "results": results})

		case p == "/workflows" && r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&s.lastBody)
			created := map[string]any{
				"id": "wf-new", "name": s.lastBody["name"], "status": s.lastBody["status"],
				"trigger_type": "external", "dsl": s.lastBody["dsl"],
				"execution_role_id": s.lastBody["execution_role_id"],
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(created)

		case p == "/workflows/templates" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"templates": []map[string]any{
				{"id": "tmpl-1", "name": "Run A Command", "category": "Device Management",
					"description": "d", "dsl": json.RawMessage(`{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
					  "do":[{"run":{"call":"jc_operation","with":{"operationId":"postApiRuncommand","version":1,
					     "bodyParams":{"_id":"REPLACE_WITH_COMMAND_ID"}}}}]}`)},
			}})

		case p == "/workflows/runs" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"totalCount": 1, "results": []map[string]any{
				{"id": "run-1", "workflowId": "gone-1", "name": "Deleted Workflow",
					"status": "completed", "workflowDeletedAt": "2026-08-11T19:43:14.329Z"},
			}})

		case strings.HasPrefix(p, "/workflows/runs/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{
				"id": "run-1", "workflowId": "gone-1", "name": "Deleted Workflow",
				"status": "completed", "workflowDeletedAt": "2026-08-11T19:43:14.329Z",
				"executionDetails": map[string]any{"nodes": []map[string]any{
					{"name": "__trigger", "type": "trigger", "success": true, "is_executed": true, "message": "Workflow invoked."},
					{"name": "listOneUser", "type": "jc_operation", "success": false, "is_executed": true,
						"message": "Task failed.",
						"node_output": map[string]any{"method": "GET", "status": 403,
							"url": "https://console.jumpcloud.com/api/systemusers?limit=1"}},
				}},
			})

		case strings.HasSuffix(p, "/runs") && r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&s.lastBody)
			s.triggered++
			// The live API answers a trigger with 202, not 201.
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]any{
				"id": "run-new", "workflowId": strings.Split(p, "/")[2], "status": "running"})

		case strings.HasPrefix(p, "/roles/") && r.Method == http.MethodGet:
			id := strings.TrimPrefix(p, "/roles/")
			name := "Read Only"
			if id == "role-admin" {
				name = "Administrator"
			}
			json.NewEncoder(w).Encode(map[string]any{"id": id, "name": name})

		case p == "/systemgroups" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"id": "dg-1", "name": "Staging Devices", "type": "system_group"},
			}})

		case p == "/roles" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"id": "role-1", "name": "Read Only"},
				{"id": "role-admin", "name": "Administrator"},
			}})

		case strings.HasPrefix(p, "/workflows/") && r.Method == http.MethodGet:
			id := strings.TrimPrefix(p, "/workflows/")
			wf, ok := s.workflows[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			json.NewEncoder(w).Encode(wf)

		case strings.HasPrefix(p, "/workflows/") && r.Method == http.MethodPut:
			id := strings.TrimPrefix(p, "/workflows/")
			json.NewDecoder(r.Body).Decode(&s.lastBody)
			wf := s.workflows[id]
			// PUT is full replace: whatever arrived becomes the workflow.
			for _, k := range []string{"name", "description", "status", "dsl", "execution_role_id"} {
				if v, ok := s.lastBody[k]; ok {
					wf[k] = v
				} else {
					delete(wf, k)
				}
			}
			json.NewEncoder(w).Encode(wf)

		case strings.HasPrefix(p, "/workflows/") && r.Method == http.MethodDelete:
			id := strings.TrimPrefix(p, "/workflows/")
			s.deleted = append(s.deleted, id)
			delete(s.workflows, id)
			w.WriteHeader(http.StatusNoContent)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func runWFCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

func TestWorkflows_List(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	out, _, err := runWFCmd(t, "workflows", "list")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("list must unwrap {results}: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 workflows, got %d", len(got))
	}
}

func TestWorkflows_TemplatesUseTheirOwnEnvelope(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	out, _, err := runWFCmd(t, "workflows", "templates", "list")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("templates must unwrap {templates}: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0]["id"] != "tmpl-1" {
		t.Errorf("unexpected templates: %s", out)
	}
	// The list view must not carry the DSL bodies.
	if _, hasDSL := got[0]["dsl"]; hasDSL {
		t.Error("template list should omit the DSL — the full catalog is far too large for a list view")
	}
}

func TestWorkflows_TemplatesInitReportsPlaceholders(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	out, errOut, err := runWFCmd(t, "workflows", "templates", "init", "tmpl-1")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("init must emit a workflow document: %v\n%s", err, out)
	}
	if doc["status"] != "inactive" {
		t.Errorf("an initialised workflow must start inactive, got %v", doc["status"])
	}
	// The placeholder report goes to stderr so stdout stays pipeable, and
	// names each marker's KIND — that is what makes --set usable without
	// going and reading the template.
	if !strings.Contains(errOut, "COMMAND_ID") {
		t.Errorf("init should report what still needs filling, stderr was: %s", errOut)
	}
	if !strings.Contains(errOut, "a JumpCloud command") {
		t.Errorf("the report should say what the marker expects, stderr was: %s", errOut)
	}
	if !strings.Contains(errOut, "--set accepts a name") {
		t.Errorf("a resolvable marker should say so, stderr was: %s", errOut)
	}
	if strings.Contains(out, "placeholder(s) to fill") {
		t.Error("the placeholder report must not pollute stdout")
	}
}

func TestWorkflows_ValidateRejectsUnfilledTemplate(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	path := writeTemp(t, `{"name":"x","dsl":`+`{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[{"run":{"call":"jc_operation","with":{"operationId":"postApiRuncommand","version":1,
	     "bodyParams":{"_id":"REPLACE_WITH_COMMAND_ID"}}}}]}}`)

	_, errOut, err := runWFCmd(t, "workflows", "validate", path, "-o", "table")
	if err == nil {
		t.Fatal("an unfilled placeholder must make validate fail")
	}
	if !strings.Contains(errOut, "REPLACE_WITH_COMMAND_ID") {
		t.Errorf("stderr should name the placeholder: %s", errOut)
	}
}

func TestWorkflows_ValidateAcceptsAValidDocument(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	path := writeTemp(t, `{"name":"probe","dsl":`+probeDSL+`}`)
	out, _, err := runWFCmd(t, "workflows", "validate", path, "-o", "table")
	if err != nil {
		t.Fatalf("valid document should pass: %v", err)
	}
	if !strings.Contains(out, "valid") {
		t.Errorf("expected a success line, got %q", out)
	}
}

func TestWorkflows_ValidateAcceptsABareDSL(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	// A file with no "dsl" key is treated as the DSL itself, so a
	// hand-written fragment still validates.
	path := writeTemp(t, probeDSL)
	if _, _, err := runWFCmd(t, "workflows", "validate", path, "-o", "table"); err != nil {
		t.Fatalf("a bare DSL document should validate: %v", err)
	}
}

func TestWorkflows_ExplainResolvesOperationIDs(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	path := writeTemp(t, `{"name":"probe","dsl":`+probeDSL+`}`)
	out, _, err := runWFCmd(t, "workflows", "explain", path)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	// The point of explain: an opaque operationId becomes a real endpoint.
	if !strings.Contains(out, "GET /api/systemusers") {
		t.Errorf("explain should resolve the operationId to its endpoint: %s", out)
	}
	if !strings.Contains(out, "manual") {
		t.Errorf("explain should describe the trigger: %s", out)
	}
}

func TestWorkflows_CreateSendsRoleAndDefaultsInactive(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)

	path := writeTemp(t, `{"name":"probe","dsl":`+probeDSL+`}`)
	if _, _, err := runWFCmd(t, "workflows", "create", "--file", path, "--role", "Read Only", "--force"); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if srv.lastBody["execution_role_id"] != "role-1" {
		t.Errorf("execution_role_id = %v, want role-1", srv.lastBody["execution_role_id"])
	}
	if srv.lastBody["status"] != "inactive" {
		t.Errorf("a new workflow must default to inactive, got %v", srv.lastBody["status"])
	}
	if _, sent := srv.lastBody["trigger_type"]; sent {
		t.Error("trigger_type is server-derived and must not be sent")
	}
}

func TestWorkflows_CreateRefusesInvalidDSL(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)

	path := writeTemp(t, `{"name":"bad","dsl":{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiNonsense","version":1}}}]}}`)
	_, _, err := runWFCmd(t, "workflows", "create", "--file", path, "--role", "Read Only", "--force")
	if err == nil {
		t.Fatal("an unknown operationId must be rejected before the API is called")
	}
	if srv.lastBody != nil {
		t.Error("nothing should have been sent")
	}
}

func TestWorkflows_CreateRefusesSideEffectsWithoutOptIn(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)

	emailDSL := `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[{"mail":{"call":"sendEmailsToAddresses","with":{
	     "message":{"subject":"s","body":"b"},
	     "recipients":{"to_addresses":["ops@example.com"]}}}}]}`
	path := writeTemp(t, `{"name":"mailer","dsl":`+emailDSL+`}`)

	_, _, err := runWFCmd(t, "workflows", "create", "--file", path, "--role", "Read Only", "--force")
	if err == nil {
		t.Fatal("a DSL that emails people must not be creatable without an explicit opt-in")
	}
	if !strings.Contains(err.Error(), "ops@example.com") {
		t.Errorf("the refusal should name who would be mailed, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--allow-side-effects") {
		t.Errorf("the refusal should name the opt-in flag, got: %v", err)
	}
	if srv.lastBody != nil {
		t.Error("nothing should have been sent")
	}

	// With the flag it goes through.
	if _, _, err := runWFCmd(t, "workflows", "create", "--file", path,
		"--role", "Read Only", "--allow-side-effects", "--force"); err != nil {
		t.Fatalf("with the opt-in it should create: %v", err)
	}
	if srv.lastBody == nil {
		t.Error("expected a create call after opting in")
	}
}

func TestWorkflows_CreatePlanWarnsOnAdminRole(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)

	path := writeTemp(t, `{"name":"probe","dsl":`+probeDSL+`}`)
	out, errOut, _ := runWFCmd(t, "workflows", "create", "--file", path, "--role", "Administrator", "--plan")
	combined := out + errOut
	if !strings.Contains(combined, "administrator role is a broad standing grant") {
		t.Errorf("binding an admin role to an unattended workflow should be called out: %s", combined)
	}
	if srv.lastBody != nil {
		t.Error("--plan must not write")
	}
}

func TestWorkflows_UpdateSendsCompleteObject(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)

	if _, _, err := runWFCmd(t, "workflows", "update", "wf-1", "--name", "Renamed", "--force"); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if srv.lastBody["name"] != "Renamed" {
		t.Errorf("name = %v", srv.lastBody["name"])
	}
	// PUT is full-replace, so a rename must still carry the DSL and the role
	// or the server drops them.
	if _, ok := srv.lastBody["dsl"]; !ok {
		t.Error("update must resend the DSL — PUT is full-replace")
	}
	if srv.lastBody["execution_role_id"] != "role-1" {
		t.Errorf("update must preserve the execution role, got %v", srv.lastBody["execution_role_id"])
	}
}

func TestWorkflows_UpdateNoFlags(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	_, _, err := runWFCmd(t, "workflows", "update", "wf-1", "--force")
	if err == nil || !strings.Contains(err.Error(), "no changes requested") {
		t.Errorf("expected a no-changes error, got %v", err)
	}
}

func TestWorkflows_DeleteKeepsRuns(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)

	out, _, err := runWFCmd(t, "workflows", "delete", "Nightly Audit", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(srv.deleted) != 1 || srv.deleted[0] != "wf-1" {
		t.Errorf("deleted = %v", srv.deleted)
	}
	if !strings.Contains(out, "deleted") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestWorkflows_TriggerRefusesNonExternal(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)

	_, _, err := runWFCmd(t, "workflows", "trigger", "wf-2", "--force")
	if err == nil {
		t.Fatal("a jc_events workflow cannot be manually triggered")
	}
	if !strings.Contains(err.Error(), "jc_events") {
		t.Errorf("the error should name the trigger type, got %v", err)
	}
	if srv.triggered != 0 {
		t.Error("nothing should have been posted")
	}
}

func TestWorkflows_TriggerSendsDataEnvelope(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)

	if _, _, err := runWFCmd(t, "workflows", "trigger", "wf-1", "--data", `{"note":"hi"}`, "--force"); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if srv.triggered != 1 {
		t.Fatalf("expected one trigger, got %d", srv.triggered)
	}
	data, _ := srv.lastBody["data"].(map[string]any)
	if data["note"] != "hi" {
		t.Errorf("run input must be wrapped in a data envelope, got %#v", srv.lastBody)
	}
}

func TestWorkflows_TriggerPlanListsSteps(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)

	out, errOut, _ := runWFCmd(t, "workflows", "trigger", "wf-1", "--plan")
	combined := out + errOut
	if !strings.Contains(combined, "/api/systemusers") {
		t.Errorf("the plan should say what will actually run: %s", combined)
	}
	if srv.triggered != 0 {
		t.Error("--plan must not trigger")
	}
}

func TestWorkflows_TriggerBadData(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	_, _, err := runWFCmd(t, "workflows", "trigger", "wf-1", "--data", "not json", "--force")
	if err == nil || !strings.Contains(err.Error(), "invalid --data JSON") {
		t.Errorf("expected a JSON error, got %v", err)
	}
}

func TestWorkflows_RunsSurviveTheirWorkflow(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	out, _, err := runWFCmd(t, "workflows", "runs", "list")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var runs []map[string]any
	json.Unmarshal([]byte(out), &runs)
	if len(runs) != 1 || runs[0]["workflowDeletedAt"] == nil {
		t.Errorf("a run whose workflow was deleted must still list: %s", out)
	}
}

func TestWorkflows_RunTraceNamesTheFailingStep(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	out, _, err := runWFCmd(t, "workflows", "runs", "get", "run-1", "--trace")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	// The trace is the only place a failure's cause shows up.
	if !strings.Contains(out, "FAILED") || !strings.Contains(out, "listOneUser") {
		t.Errorf("trace should mark the failed step: %s", out)
	}
	if !strings.Contains(out, "403") {
		t.Errorf("trace should carry the HTTP status the step got back: %s", out)
	}
	if !strings.Contains(out, "First failure: listOneUser") {
		t.Errorf("trace should name the first failure: %s", out)
	}
	if !strings.Contains(out, "retained") {
		t.Errorf("trace should note the workflow was deleted: %s", out)
	}
}

// writeTemp writes content to a temp file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/wf.json"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	return path
}

func TestParseSetFlags(t *testing.T) {
	got, err := parseSetFlags([]string{
		"COMMAND_ID=abc",
		"REPLACE_WITH_DEVICE_GROUP_ID=def",
	})
	if err != nil {
		t.Fatalf("parseSetFlags: %v", err)
	}
	// Both the bare and the full form must land on the same key.
	if got["REPLACE_WITH_COMMAND_ID"] != "abc" {
		t.Errorf("bare marker not normalized: %#v", got)
	}
	if got["REPLACE_WITH_DEVICE_GROUP_ID"] != "def" {
		t.Errorf("full marker not kept: %#v", got)
	}

	// A value containing '=' is legitimate and must survive.
	got, err = parseSetFlags([]string{"HEADER_VALUE=a=b"})
	if err != nil {
		t.Fatalf("parseSetFlags: %v", err)
	}
	if got["REPLACE_WITH_HEADER_VALUE"] != "a=b" {
		t.Errorf("only the first = separates: %#v", got)
	}

	if _, err := parseSetFlags([]string{"no-equals"}); err == nil {
		t.Error("want an error for a --set without =")
	}
}

func TestWorkflows_TemplatesInitSetFillsPlaceholder(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)
	overrideV1Client(t, srv.URL)

	// The template's marker is REPLACE_WITH_COMMAND_ID; a 24-hex value needs
	// no resolution and must pass straight through.
	out, errOut, err := runWFCmd(t, "workflows", "templates", "init", "tmpl-1",
		"--set", "COMMAND_ID=6139919b03c9b24d0b8f3ef1")
	if err != nil {
		t.Fatalf("Execute error: %v (stderr %s)", err, errOut)
	}
	if strings.Contains(out, "REPLACE_WITH_") {
		t.Errorf("the placeholder should be filled:\n%s", out)
	}
	if !strings.Contains(out, "6139919b03c9b24d0b8f3ef1") {
		t.Errorf("the value should be substituted:\n%s", out)
	}
	// Nothing left, so nothing to report.
	if strings.Contains(errOut, "still to fill") {
		t.Errorf("no placeholders should remain, stderr: %s", errOut)
	}
}

func TestWorkflows_TemplatesInitSetRejectsUnknownMarker(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startWFServer(t).URL)

	_, _, err := runWFCmd(t, "workflows", "templates", "init", "tmpl-1", "--set", "NOPE=x")
	if err == nil {
		t.Fatal("want an error for a marker this template does not have")
	}
	// The error must say what IS available, or the fix is guesswork.
	if !strings.Contains(err.Error(), "REPLACE_WITH_COMMAND_ID") {
		t.Errorf("error should list the real markers: %v", err)
	}
}

func TestResolvePlaceholderValue_PassesThroughIDsAndFreeText(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)

	client, err := newV2Client()
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	// A 24-hex ID needs no lookup.
	got, err := resolvePlaceholderValue(context.Background(), client,
		"REPLACE_WITH_COMMAND_ID", "6139919b03c9b24d0b8f3ef1")
	if err != nil || got != "6139919b03c9b24d0b8f3ef1" {
		t.Errorf("an ID should pass through untouched: %q %v", got, err)
	}

	// Free text is literal, never resolved.
	got, err = resolvePlaceholderValue(context.Background(), client,
		"REPLACE_WITH_IT_OPS_EMAIL", "ops@example.com")
	if err != nil || got != "ops@example.com" {
		t.Errorf("free text should be taken literally: %q %v", got, err)
	}
}

// The file's status must survive when --status is not passed. Assigning the
// flag unconditionally discarded it, and the workflow then failed to trigger
// with "workflow is not active" and nothing pointing back at the cause.
func TestWorkflows_CreateHonoursStatusFromFile(t *testing.T) {
	setupUsersTest(t)
	srv := startWFServer(t)
	overrideV2Client(t, srv.URL)

	path := writeTemp(t, `{"name":"probe","status":"active","dsl":`+probeDSL+`}`)
	if _, _, err := runWFCmd(t, "workflows", "create", "--file", path, "--role", "Read Only", "--force"); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if srv.lastBody["status"] != "active" {
		t.Errorf("status = %v; the file's value must survive when --status is absent", srv.lastBody["status"])
	}

	// The flag still wins when both are given.
	srv.lastBody = nil
	if _, _, err := runWFCmd(t, "workflows", "create", "--file", path,
		"--role", "Read Only", "--status", "inactive", "--force"); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if srv.lastBody["status"] != "inactive" {
		t.Errorf("status = %v; --status must override the file", srv.lastBody["status"])
	}
}
