package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// validateJSON is a helper: most cases are one small DSL document.
func validateJSON(t *testing.T, doc string) Result {
	t.Helper()
	return ValidateRaw(json.RawMessage(doc))
}

// findingAt returns the first finding whose path contains want.
func findingAt(r Result, want string) (Finding, bool) {
	for _, f := range r.Findings {
		if strings.Contains(f.Path, want) {
			return f, true
		}
	}
	return Finding{}, false
}

func hasMessage(r Result, substr string) bool {
	for _, f := range r.Findings {
		if strings.Contains(f.Message, substr) {
			return true
		}
	}
	return false
}

func hasHint(r Result, substr string) bool {
	for _, f := range r.Findings {
		if strings.Contains(f.Hint, substr) {
			return true
		}
	}
	return false
}

// validExternal is a minimal valid external-trigger workflow, used as the base
// for the negative cases so each test changes exactly one thing.
const validExternal = `{
  "schedule": {"on": {"one": {"with": {"source": "external"}}}},
  "do": [{"getUsers": {"call": "jc_operation", "with": {
      "operationId": "getApiSystemusers", "version": 1, "queryParams": {"limit": 1}}}}]
}`

func TestValidate_MinimalExternalWorkflowIsValid(t *testing.T) {
	r := validateJSON(t, validExternal)
	if !r.OK() {
		t.Fatalf("expected valid, got:\n%v", r.Findings)
	}
	if r.TriggerType != TriggerExternal {
		t.Errorf("TriggerType = %q, want external", r.TriggerType)
	}
	if len(r.SideEffects) != 0 {
		t.Errorf("a jc_operation-only workflow has no side effects, got %+v", r.SideEffects)
	}
}

func TestValidate_ScheduledAndEventTriggers(t *testing.T) {
	scheduled := `{"schedule": {"frequency": "daily", "time": "07:00", "timezone": "Etc/UTC"},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`
	if r := validateJSON(t, scheduled); !r.OK() {
		t.Errorf("scheduled workflow should be valid: %v", r.Findings)
	} else if r.TriggerType != TriggerScheduler {
		t.Errorf("TriggerType = %q, want scheduler", r.TriggerType)
	}

	event := `{"schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "user_suspended"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`
	if r := validateJSON(t, event); !r.OK() {
		t.Errorf("event workflow should be valid: %v", r.Findings)
	} else if r.TriggerType != TriggerEvents {
		t.Errorf("TriggerType = %q, want jc_events", r.TriggerType)
	}
}

func TestValidate_TriggerStylesAreMutuallyExclusive(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"frequency": "daily", "on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)
	if r.OK() {
		t.Fatal("a schedule with both a frequency and an on trigger must be rejected")
	}
	if !hasMessage(r, "both a frequency and an on.one trigger") {
		t.Errorf("unexpected findings: %v", r.Findings)
	}
}

func TestValidate_EventTriggerNeedsType(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "jc_events"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)
	if r.OK() || !hasMessage(r, "needs a type") {
		t.Errorf("a jc_events trigger with no event type must be rejected: %v", r.Findings)
	}
}

func TestValidate_EmptyDoRejected(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}}, "do": []}`)
	if r.OK() || !hasMessage(r, "at least one task") {
		t.Errorf("empty do must be rejected: %v", r.Findings)
	}
}

func TestValidate_UnknownOperationIDSuggests(t *testing.T) {
	r := validateJSON(t, strings.Replace(validExternal, "getApiSystemusers", "getApiSystemusrs", 1))
	if r.OK() {
		t.Fatal("a typo'd operationId must be rejected")
	}
	f, ok := findingAt(r, "operationId")
	if !ok {
		t.Fatalf("no operationId finding: %v", r.Findings)
	}
	if !strings.Contains(f.Hint, "getApiSystemusers") {
		t.Errorf("hint should suggest the real id, got %q", f.Hint)
	}
}

func TestValidate_LegacyOperationIDIsNamedAsSuch(t *testing.T) {
	r := validateJSON(t, strings.Replace(validExternal, "getApiSystemusers", "systemusers_list", 1))
	if r.OK() {
		t.Fatal("a legacy snake_case id must be rejected")
	}
	if !hasMessage(r, "deprecated snake_case") {
		t.Errorf("the legacy form should be called out explicitly: %v", r.Findings)
	}
	f, _ := findingAt(r, "operationId")
	if !strings.Contains(f.Hint, "getApiSystemusers") {
		t.Errorf("hint should name the replacement, got %q", f.Hint)
	}
}

func TestValidate_VersionMustMatchAPIVersion(t *testing.T) {
	// getApiSystemusers is a v1 operation; version 2 is wrong.
	r := validateJSON(t, strings.Replace(validExternal, `"version": 1`, `"version": 2`, 1))
	if r.OK() {
		t.Fatal("a version that disagrees with the operation must be rejected")
	}
	f, ok := findingAt(r, "version")
	if !ok || !strings.Contains(f.Message, "v1 operation") {
		t.Errorf("unexpected findings: %v", r.Findings)
	}
}

func TestValidate_MissingVersionWarnsWithTheRightNumber(t *testing.T) {
	r := validateJSON(t, strings.Replace(validExternal, `"version": 1, `, "", 1))
	if !r.OK() {
		t.Fatalf("a missing version is a warning, not an error: %v", r.Errors())
	}
	f, ok := findingAt(r, "version")
	if !ok || f.Severity != Warning || !strings.Contains(f.Hint, `"version": 1`) {
		t.Errorf("expected a warning naming version 1, got %+v", f)
	}
}

func TestValidate_PathParamsRequiredForTemplatedPath(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusersById", "version": 1}}}]}`)
	if r.OK() || !hasMessage(r, "needs pathParams") {
		t.Errorf("an operation with {id} in its path needs pathParams: %v", r.Findings)
	}
}

func TestValidate_UnfilledPlaceholderReportedWithPath(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"run": {"call": "jc_operation", "with": {"operationId": "postApiRuncommand", "version": 1,
	     "bodyParams": {"_id": "REPLACE_WITH_COMMAND_ID"}}}}]}`)
	if r.OK() {
		t.Fatal("an unfilled placeholder must block creation")
	}
	f, ok := findingAt(r, "bodyParams._id")
	if !ok {
		t.Fatalf("placeholder finding should carry its JSON path: %v", r.Findings)
	}
	if !strings.Contains(f.Message, "REPLACE_WITH_COMMAND_ID") {
		t.Errorf("finding should name the marker, got %q", f.Message)
	}
}

func TestValidate_PlaceholderWithNumericSuffix(t *testing.T) {
	// Templates binding several groups use REPLACE_WITH_GROUP_ID_1, _2, _3.
	r := validateJSON(t, strings.Replace(validExternal,
		`"queryParams": {"limit": 1}`, `"queryParams": {"x": "REPLACE_WITH_GROUP_ID_2"}`, 1))
	if !hasMessage(r, "REPLACE_WITH_GROUP_ID_2") {
		t.Errorf("numeric-suffixed placeholders must be detected: %v", r.Findings)
	}
}

func TestValidate_NestedForRejected(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"outer": {"for": {"each": "u", "in": "${ input.users }"}, "do": [
	     {"inner": {"for": {"each": "g", "in": "${ u.groups }"}, "do": [
	        {"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}}]}}]}`)
	if r.OK() || !hasMessage(r, "nested for loops are not supported") {
		t.Errorf("nested loops must be rejected: %v", r.Findings)
	}
}

func TestValidate_EmptyLoopBodyRejected(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"outer": {"for": {"each": "u", "in": "${ input.users }"}, "do": []}}]}`)
	if r.OK() || !hasMessage(r, "loop body needs at least one task") {
		t.Errorf("an empty loop body must be rejected: %v", r.Findings)
	}
}

func TestValidate_ThenMustBeForwardOnly(t *testing.T) {
	backward := `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"first": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}},
	    {"second": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}, "then": "first"}}]}`
	r := validateJSON(t, backward)
	if r.OK() || !hasMessage(r, "not forward of this task") {
		t.Errorf("backward then targets must be rejected: %v", r.Findings)
	}

	// The reserved control targets are always allowed.
	ok := strings.Replace(backward, `"then": "first"`, `"then": "end"`, 1)
	if r := validateJSON(t, ok); !r.OK() {
		t.Errorf("then: end must be accepted: %v", r.Findings)
	}
}

func TestValidate_ThenTargetMustExist(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}, "then": "nope"}}]}`)
	if r.OK() || !hasMessage(r, "is not a task in this workflow") {
		t.Errorf("an unknown then target must be rejected: %v", r.Findings)
	}
}

// The same binding rule holds for jc_events. Proven live: creating a workflow
// whose group_create trigger tested `input.resource.name` was rejected with
// "failed to compile expression: unknown name input". Every shipped template
// agrees, writing association.op / changes / userId with no prefix at all.
func TestValidate_EventConditionMustNotUseInputPrefix(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "group_create", "condition": "input.resource.name != \"\""}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)
	if r.OK() || !hasMessage(r, "input.<field> is never bound") {
		t.Errorf("input. in an event trigger condition must be rejected: %v", r.Findings)
	}
	if !hasHint(r, "resource.name") {
		t.Errorf("the hint should show the event-payload form, not the external one: %v", r.Findings)
	}

	// The unprefixed form is what the server accepts.
	good := `{"schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "group_create", "condition": "resource.name != \"\""}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`
	if r := validateJSON(t, good); !r.OK() {
		t.Errorf("a bare field reference is correct here: %v", r.Findings)
	}
}

func TestValidate_ExternalConditionMustNotUseInputPrefix(t *testing.T) {
	// The guide flags this twice: an external trigger's condition is
	// evaluated against the posted data object, so input.<field> never binds.
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external", "condition": "input.userId != \"\""}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)
	if r.OK() || !hasMessage(r, "input.<field> is never bound") {
		t.Errorf("input. in an external trigger condition must be rejected: %v", r.Findings)
	}

	// Without the prefix it is correct.
	good := `{"schedule": {"on": {"one": {"with": {"source": "external", "condition": "userId != \"\""}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`
	if r := validateJSON(t, good); !r.OK() {
		t.Errorf("a bare field reference is correct here: %v", r.Findings)
	}
}

func TestValidate_ExpressionMustCompile(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"if": "${ input.x == }", "call": "jc_operation",
	     "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)
	if r.OK() || !hasMessage(r, "does not compile") {
		t.Errorf("a syntactically broken expression must be rejected: %v", r.Findings)
	}
}

// && is accepted, and must not be flagged.
//
// Every shipped template writes its trigger conditions with &&. A live
// experiment on 2026-08-27 ran two workflows differing only in the operator,
// in both positions operators appear — a task-level `if` and an external
// trigger condition — and all four runs completed with the guarded step
// executing. Warning on this flagged the entire shipped catalogue for a
// non-issue.
func TestValidate_GoOperatorsAreAccepted(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"if": "${ input.x == 1 && input.y == 2 }", "call": "jc_operation",
	     "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)
	if !r.OK() {
		t.Errorf("&& must not be an error: %v", r.Errors())
	}
	for _, f := range r.Findings {
		if strings.Contains(f.Message, "&&") || strings.Contains(f.Hint, "and / or / not") {
			t.Errorf("&& must not be flagged at all, got %+v", f)
		}
	}

	// The keyword form is equally fine; neither is preferred by the linter.
	r = validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"if": "${ input.x == 1 and input.y == 2 }", "call": "jc_operation",
	     "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)
	if len(r.Findings) != 0 {
		t.Errorf("the keyword form should produce no findings either: %v", r.Findings)
	}
}

func TestValidate_ActionsMustReferAnEarlierTask(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"first": {"call": "jc_operation", "with": {"operationId": "getApiSystemusersById", "version": 1,
	        "pathParams": {"id": "${ actions.second.body.id }"}}}},
	    {"second": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)
	if r.OK() || !hasMessage(r, "has not run yet") {
		t.Errorf("a forward actions reference must be rejected: %v", r.Findings)
	}
}

func TestValidate_ActionsInsideLoopSeesEarlierSibling(t *testing.T) {
	// Regression: the task-position lookup must resolve to the innermost
	// enclosing task, not the loop, or every sibling in a loop body looks
	// like it runs later than itself.
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"loop": {"for": {"each": "u", "in": "${ input.users }"}, "do": [
	      {"getOne": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}},
	      {"useIt": {"call": "jc_operation", "with": {"operationId": "getApiSystemusersById", "version": 1,
	          "pathParams": {"id": "${ actions.getOne.body.id }"}}}}]}}]}`)
	if !r.OK() {
		t.Errorf("a backward reference inside a loop body is legal: %v", r.Errors())
	}
}

func TestValidate_UnknownActionsReference(t *testing.T) {
	r := validateJSON(t, strings.Replace(validExternal,
		`"queryParams": {"limit": 1}`, `"queryParams": {"x": "${ actions.ghost.body.id }"}`, 1))
	if r.OK() || !hasMessage(r, "does not name a task") {
		t.Errorf("an unknown actions reference must be rejected: %v", r.Findings)
	}
}

func TestValidate_PageOnlyInPaginationExpressions(t *testing.T) {
	r := validateJSON(t, strings.Replace(validExternal,
		`"queryParams": {"limit": 1}`, `"queryParams": {"x": "${ page.request.queryParams.skip }"}`, 1))
	if r.OK() || !hasMessage(r, "page is only available inside pagination") {
		t.Errorf("page outside pagination must be rejected: %v", r.Findings)
	}
}

func TestValidate_ExtractCountsAsAPaginationExpression(t *testing.T) {
	// Regression: extract sits beside the pagination block, not inside it,
	// but the guide lists it as a pagination expression, so `page` is bound.
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {
	      "operationId": "getApiSystemusers", "version": 1,
	      "queryParams": {"limit": 10, "skip": 0},
	      "pagination": {"update": {"in": "queryParams", "key": "skip",
	          "value": "${ page.request.queryParams.skip + 10 }"},
	        "until": "${ len(page.response.body.results) == 0 }"},
	      "extract": "${ page.response.body.results }"}}}]}`)
	if !r.OK() {
		t.Errorf("page in extract is legal: %v", r.Errors())
	}
}

func TestValidate_PaginationNeedsUntil(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {
	      "operationId": "getApiSystemusers", "version": 1,
	      "pagination": {"update": {"in": "queryParams", "key": "skip", "value": "${ 1 }"}}}}}]}`)
	if r.OK() || !hasMessage(r, "needs an until condition") {
		t.Errorf("pagination without until must be rejected — it is the usual runaway cause: %v", r.Findings)
	}
}

func TestValidate_EmailRequiresSubjectBodyAndRecipients(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"mail": {"call": "sendEmailsToAddresses", "with": {"message": {"subject": ""}}}}]}`)
	if r.OK() {
		t.Fatal("an incomplete email step must be rejected")
	}
	for _, want := range []string{"needs a subject", "needs a body", "needs recipients"} {
		if !hasMessage(r, want) {
			t.Errorf("missing finding %q in %v", want, r.Findings)
		}
	}
}

func TestValidate_SideEffectsAreEnumeratedWithTargets(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"mail": {"call": "sendEmailsToAddresses", "with": {
	        "message": {"subject": "s", "body": "b"},
	        "recipients": {"to_addresses": ["ops@example.com", "sec@example.com"]}}}},
	    {"ext": {"call": "connector_operation", "with": {
	        "id": "c1", "httpMethod": "POST", "endpointPath": "/incident"}}}]}`)
	if !r.OK() {
		t.Fatalf("this workflow is valid, just side-effecting: %v", r.Errors())
	}
	if len(r.SideEffects) != 2 {
		t.Fatalf("expected 2 side effects, got %+v", r.SideEffects)
	}
	var mail, ext SideEffect
	for _, se := range r.SideEffects {
		switch se.Call {
		case CallEmailAddresses:
			mail = se
		case CallConnector:
			ext = se
		}
	}
	if len(mail.Targets) != 2 || mail.Targets[0] != "ops@example.com" {
		t.Errorf("email recipients must be listed so a reviewer sees who is mailed: %+v", mail)
	}
	if len(ext.Targets) != 1 || !strings.Contains(ext.Targets[0], "POST /incident") {
		t.Errorf("connector target must name the endpoint: %+v", ext)
	}
}

func TestValidate_UnknownCallType(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"call": "run_shell", "with": {}}}]}`)
	if r.OK() || !hasMessage(r, "unknown call type") {
		t.Errorf("an unknown call type must be rejected: %v", r.Findings)
	}
}

func TestValidate_DuplicateTaskNames(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}},
	    {"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)
	if r.OK() || !hasMessage(r, "duplicate task name") {
		t.Errorf("duplicate task names must be rejected — they are how references resolve: %v", r.Findings)
	}
}

func TestResult_ErrIsNilWhenOnlyWarnings(t *testing.T) {
	r := Result{Findings: []Finding{{Severity: Warning, Path: "p", Message: "m"}}}
	if !r.OK() || r.Err() != nil {
		t.Error("warnings must not make a document invalid")
	}
	r.Findings = append(r.Findings, Finding{Severity: Error, Path: "p2", Message: "boom"})
	if r.OK() || r.Err() == nil {
		t.Error("an error must make a document invalid")
	}
	if !strings.Contains(r.Err().Error(), "boom") {
		t.Errorf("Err should carry the messages: %v", r.Err())
	}
}

// The execution role is the security model: validation otherwise confirms an
// operation exists, not that the workflow may call it, so a destructive
// operationId passes silently. This is what turns that into an author-time
// finding.
func TestCheckScopes_FlagsWhatTheRoleCannotDo(t *testing.T) {
	d, err := ParseDSL(json.RawMessage(`{
	  "schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"read": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}},
	    {"destroy": {"call": "jc_operation", "with": {"operationId": "deleteApiSystemusersById",
	        "version": 1, "pathParams": {"id": "x"}}}}
	  ]}`))
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}

	// A read-only scope set: permits the list, not the delete.
	gaps := CheckScopes(d, []string{"users.readonly"})
	if len(gaps) != 1 {
		t.Fatalf("expected exactly the delete to be flagged, got %+v", gaps)
	}
	if gaps[0].Task != "destroy" || gaps[0].OperationID != "deleteApiSystemusersById" {
		t.Errorf("wrong step flagged: %+v", gaps[0])
	}
	if len(gaps[0].Needs) == 0 {
		t.Error("a gap must say which scopes would permit it")
	}

	// Holding ANY one declared scope is sufficient.
	if gaps := CheckScopes(d, []string{"users.readonly", "users.delete"}); len(gaps) != 0 {
		t.Errorf("users.delete should permit the delete, got %+v", gaps)
	}

	// No scopes at all flags everything with declared scopes.
	if gaps := CheckScopes(d, nil); len(gaps) != 2 {
		t.Errorf("an empty role should flag both steps, got %d", len(gaps))
	}
}

// A gap is a warning, never an error: the spec's x-scopes is a lower bound —
// the live API accepted a scope for postApiRuncommand that the spec omits — so
// blocking on it would reject workflows that actually run.
func TestValidateWithRole_GapsAreAdvisory(t *testing.T) {
	d, _ := ParseDSL(json.RawMessage(`{
	  "schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"destroy": {"call": "jc_operation", "with": {"operationId": "deleteApiSystemusersById",
	      "version": 1, "pathParams": {"id": "x"}}}}]}`))

	r := ValidateWithRole(d, "Read Only", []string{"users.readonly"})
	if !r.OK() {
		t.Errorf("a scope gap must not make the workflow invalid: %v", r.Errors())
	}

	var found bool
	for _, f := range r.Findings {
		if strings.Contains(f.Message, "Read Only") && strings.Contains(f.Message, "deleteApiSystemusersById") {
			found = true
			if f.Severity != Warning {
				t.Errorf("severity = %v, want warning", f.Severity)
			}
			if !strings.Contains(f.Hint, "lower bound") {
				t.Errorf("the hint should say the spec list is not authoritative: %q", f.Hint)
			}
		}
	}
	if !found {
		t.Errorf("no scope finding produced: %+v", r.Findings)
	}
}

func TestOperation_PermittedBy(t *testing.T) {
	op, ok := LookupOperation("postApiRuncommand")
	if !ok {
		t.Fatal("postApiRuncommand missing")
	}
	if len(op.Scopes) == 0 {
		t.Fatal("the index must carry x-scopes")
	}
	if !op.PermittedBy(map[string]bool{"commands": true}) {
		t.Error("holding a declared scope must permit the operation")
	}
	if op.PermittedBy(map[string]bool{"unrelated.scope": true}) {
		t.Error("an unrelated scope must not permit it")
	}
}

// A task no branch targets NEVER RUNS — execution follows the jump graph, not
// the order of the do list.
//
// This reverses an earlier reading of this repo's, which had it running in
// array order. That reading came from trusting the run trace, where an
// untargeted task reports is_executed=true, success=true and "Task completed."
// exactly like one that ran. Settled instead with an observable outside the
// trace: a switch jumped past an untargeted task that would have CREATED A
// USER GROUP, and afterwards the group did not exist, while the jump target's
// group did. The trace lies here; the tenant does not.
func TestValidate_UntargetedTaskNeverRuns(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"router": {"switch": [{"always": {"when": "${ 1 == 1 }", "then": "target"}}]}},
	    {"orphan": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}},
	    {"target": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}
	  ]}`)

	f, ok := findingAt(r, "orphan")
	if !ok {
		t.Fatalf("an untargeted task after a jump must be flagged: %v", r.Findings)
	}
	if !strings.Contains(f.Message, "silently never run") {
		t.Errorf("the message must say the task does not run: %q", f.Message)
	}
	// The opposite claim shipped once and had to be reverted; keep it out.
	if strings.Contains(f.Message, "array order") || strings.Contains(f.Hint, "STILL execute") {
		t.Errorf("this is the reverted claim, it must not come back: %q / %q", f.Message, f.Hint)
	}
	if !strings.Contains(f.Hint, "jump graph") {
		t.Errorf("the hint should name the real rule: %q", f.Hint)
	}
}

// Plain sequential tasks fall through and are all reachable.
func TestValidate_SequentialTasksAreReachable(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"first": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}},
	    {"second": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}},
	    {"third": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}
	  ]}`)
	if hasMessage(r, "not targeted by any then") {
		t.Errorf("fall-through tasks are reachable: %v", r.Findings)
	}
}

// A then chain that reaches every task must not warn — the check is
// conservative on purpose, and a false unreachability warning is worse than
// none.
func TestValidate_ThenChainIsReachable(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}, "then": "b"}},
	    {"b": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}
	  ]}`)
	if hasMessage(r, "not targeted by any then") {
		t.Errorf("a then target is reachable: %v", r.Findings)
	}
}

// Every shipped template must stay silent: they are the only worked examples,
// and warning on them would train people to ignore the warning.
func TestValidate_ReachabilityQuietOnRealTemplateShapes(t *testing.T) {
	// The shape used by "Wipe Device and Reset Asset Status": a switch whose
	// branches target the tasks that follow it, in order.
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "association_change"}}}},
	  "do": [
	    {"route": {"switch": [
	       {"isMac": {"when": "${ input.os == \"mac\" }", "then": "macPath"}},
	       {"default": {"then": "otherPath"}}]}},
	    {"macPath": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}, "then": "common"}},
	    {"otherPath": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}, "then": "common"}},
	    {"common": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}
	  ]}`)
	if hasMessage(r, "not targeted by any then") {
		t.Errorf("a normal switch-and-converge shape must not warn: %v", r.Findings)
	}
}

// A mistyped trigger type saves, activates and silently never fires — the
// worst failure shape the DSL offers, because it is indistinguishable from an
// event that has not happened yet.
func TestValidate_UnknownEventTypeWarnsWithNearMiss(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "user_suspend"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)

	// A warning, never an error: the catalog is a lower bound, and a live
	// tenant emitted 30 types the documentation does not list.
	if !r.OK() {
		t.Fatalf("an unknown event type must not make the workflow invalid: %v", r.Errors())
	}
	if !hasMessage(r, `unknown Directory Insights event type "user_suspend"`) {
		t.Fatalf("the type should be flagged: %v", r.Findings)
	}
	// The realistic failure is a typo, so the near miss is what makes it
	// actionable.
	f, _ := findingAt(r, "with.type")
	if !strings.Contains(f.Hint, "user_suspended") {
		t.Errorf("hint should name the near miss, got %q", f.Hint)
	}
	if !strings.Contains(f.Hint, "lower bound") {
		t.Errorf("hint must say the catalog is not authoritative, got %q", f.Hint)
	}
}

// Every event type JumpCloud's own templates use must stay silent. Warning on
// the only worked examples anyone has would teach people to ignore it.
func TestValidate_TemplateEventTypesAreSilent(t *testing.T) {
	for _, et := range []string{
		"association_change",
		"access_management_association_change",
		"user_activated",
		"user_create",
		"user_suspended", // from the DSL guide
		"system_update",  // from the DSL guide
	} {
		r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "`+et+`"}}}},
		  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)
		if hasMessage(r, "unknown Directory Insights event type") {
			t.Errorf("%s is used by a shipped template or the guide and must not warn: %v", et, r.Findings)
		}
	}
}

func TestEventTypeCatalog(t *testing.T) {
	if n := EventTypeCount(); n < 300 {
		t.Fatalf("catalog looks truncated: %d entries", n)
	}
	e, ok := LookupEventType("user_suspended")
	if !ok {
		t.Fatal("user_suspended missing from the catalog")
	}
	if e.Describe == "" {
		t.Error("an entry must carry what the event means, or the listing is useless")
	}

	// The service axis lines up with api.ValidInsightsServices.
	if got := EventTypes("access_management", ""); len(got) == 0 {
		t.Error("service filter returned nothing for access_management")
	}
	// Search matches the description, not only the name.
	if got := EventTypes("", "suspended"); len(got) == 0 {
		t.Error("substring search found nothing for 'suspended'")
	}
	if got := EventTypes("", "zzz-no-such-thing"); len(got) != 0 {
		t.Errorf("a nonsense search should match nothing, got %d", len(got))
	}
}

func TestSuggestEventType(t *testing.T) {
	if got := SuggestEventType("user_suspend", 3); len(got) == 0 || got[0] != "user_suspended" {
		t.Errorf("a one-character typo should suggest the real type first, got %v", got)
	}
	// Nonsense should not produce confident noise.
	if got := SuggestEventType("zzzzzzzzzzzzzzzzzzzz", 3); len(got) != 0 {
		t.Errorf("an invented name should suggest nothing, got %v", got)
	}
}

// A non-2xx halts the whole run — verified live, where a deliberate 404 left
// both following tasks reporting "Not executed — workflow failed at a prior
// task". So an `if` testing whether an earlier call succeeded only ever runs
// when it did: the failure branch it appears to handle is unreachable.
func TestValidate_DeadStatusGuardWarns(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"fetch": {"call": "jc_operation", "with": {"operationId": "getApiSystemusersById",
	        "version": 1, "pathParams": {"id": "${ input.id }"}}}},
	    {"guarded": {"if": "${ actions.fetch.status != 500 }", "call": "jc_operation",
	        "with": {"operationId": "getApiSystemusers", "version": 1}}}
	  ]}`)

	if !r.OK() {
		t.Fatalf("the DSL is well-formed, just built on a broken idiom: %v", r.Errors())
	}
	if !hasMessage(r, "halts the run before this task is reached") {
		t.Fatalf("the dead guard should be flagged: %v", r.Findings)
	}
	f, _ := findingAt(r, "guarded.if")
	// The message must own the fact that it contradicts the shipped
	// templates, or it reads as a bug in the linter.
	if !strings.Contains(f.Hint, "shipped templates use this idiom too") {
		t.Errorf("hint should acknowledge the templates do this: %q", f.Hint)
	}
	if !strings.Contains(f.Hint, "BEFORE the fallible call") {
		t.Errorf("hint should name the working pattern: %q", f.Hint)
	}
	// The fix is a deletion, not a restructure. Every shipped template ANDs
	// the dead status test with a live check that must survive.
	if !strings.Contains(f.Hint, "keep the rest of the guard") {
		t.Errorf("hint should say to delete the conjunct and keep the rest: %q", f.Hint)
	}
}

// `status == 200` is worse than merely dead, and gets its own message.
//
// A non-2xx already halted the run, so the only thing the test can still do is
// come out FALSE on a successful 201 or 204 and silently skip the task.
// Verified live in one run: a task guarded on `status == 200` after a create
// that returned 201 reported "Skipping — if condition did not match", while
// `>= 200 && < 300` on the same call executed.
func TestValidate_StatusEquals200CanSilentlySkip(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"create": {"call": "jc_operation", "with": {"operationId": "postApiV2Usergroups",
	        "version": 2, "bodyParams": {"name": "x"}}}},
	    {"guarded": {"if": "${ actions.create.status == 200 && input.go }", "call": "jc_operation",
	        "with": {"operationId": "getApiSystemusers", "version": 1}}}
	  ]}`)

	f, ok := findingAt(r, "guarded.if")
	if !ok {
		t.Fatalf("== 200 after a fallible call must be flagged: %v", r.Findings)
	}
	if !strings.Contains(f.Message, "silently skip") {
		t.Errorf("the message should lead with the suppression risk, not redundancy: %q", f.Message)
	}
	if !strings.Contains(f.Message, "201") {
		t.Errorf("the message should name the status that triggers it: %q", f.Message)
	}
	if !strings.Contains(f.Hint, "delete the `actions.create.status == 200 &&` conjunct") {
		t.Errorf("the hint should name the exact text to delete: %q", f.Hint)
	}
	if !strings.Contains(f.Hint, ">= 200 && < 300") {
		t.Errorf("the hint should give the safe form for a real status test: %q", f.Hint)
	}
}

// A switch placed BEFORE the fallible call is the pattern that works, and must
// not be flagged.
func TestValidate_SwitchBeforeTheCallIsNotFlagged(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"lookup": {"call": "jc_operation", "with": {"operationId": "postApiSearchSystemusers",
	        "version": 1, "bodyParams": {"filter": {}}}}},
	    {"route": {"switch": [
	        {"found": {"when": "${ len(actions.lookup.body.results) > 0 }", "then": "detail"}},
	        {"default": {"then": "detail"}}]}},
	    {"detail": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}
	  ]}`)
	if hasMessage(r, "halts the run before this task") {
		t.Errorf("branching on a body BEFORE the fallible call is the working pattern: %v", r.Findings)
	}
}

// Guarding on something other than a status is unrelated and must stay silent.
func TestValidate_NonStatusGuardIsNotFlagged(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [
	    {"fetch": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}},
	    {"guarded": {"if": "${ actions.fetch.body.totalCount > 0 }", "call": "jc_operation",
	        "with": {"operationId": "getApiSystemusers", "version": 1}}}
	  ]}`)
	if hasMessage(r, "halts the run before this task") {
		t.Errorf("a body guard is legitimate — only status guards are dead: %v", r.Findings)
	}
}

// A condition naming a field the event does not carry evaluates false forever
// — the same silent-never-fires failure as a mistyped event type, one layer
// down.
func TestValidate_UnknownEventPayloadFieldWarns(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "group_create"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusersById",
	      "version": 1, "pathParams": {"id": "${ input.resourse.id }"}}}}]}`)

	if !r.OK() {
		t.Fatalf("the field map is a lower bound, so this must not be an error: %v", r.Errors())
	}
	if !hasMessage(r, "input.resourse is not carried by a group_create event payload") {
		t.Fatalf("the bad field should be flagged: %v", r.Findings)
	}
	f, _ := findingAt(r, "pathParams.id")
	if !strings.Contains(f.Hint, "closest available: resource") {
		t.Errorf("hint should name the near miss, got %q", f.Hint)
	}
	if !strings.Contains(f.Hint, "silently never matches") {
		t.Errorf("hint should say what goes wrong, got %q", f.Hint)
	}
}

// The fields the shipped templates actually use must stay silent.
func TestValidate_RealEventFieldsAreSilent(t *testing.T) {
	for _, ref := range []string{
		"input.resource.id",
		"input.association.connection.to.object_id",
		"input.initiated_by.email",
		"input.auth_method",
		"input.changes[0].to",
	} {
		r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "association_change"}}}},
		  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers",
		      "version": 1, "queryParams": {"x": "${ `+ref+` }"}}}}]}`)
		if hasMessage(r, "is not carried by") {
			t.Errorf("%s is a real payload field and must not warn: %v", ref, r.Findings)
		}
	}
}

// An external trigger declares its own schema, and the API enforces it, so a
// field the schema omits can never arrive.
func TestValidate_InputNotInDeclaredSchemaWarns(t *testing.T) {
	r := validateJSON(t, `{
	  "input": {"schema": {"format": "json", "document": {"type": "object",
	      "required": ["userId"], "properties": {"userId": {"type": "string"}}}}},
	  "schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusersById",
	      "version": 1, "pathParams": {"id": "${ input.userIdd }"}}}}]}`)

	if !hasMessage(r, "input.userIdd is not carried by the declared input schema") {
		t.Fatalf("a field outside the schema should be flagged: %v", r.Findings)
	}
	f, _ := findingAt(r, "pathParams.id")
	if !strings.Contains(f.Hint, "closest available: userId") {
		t.Errorf("hint should name the near miss, got %q", f.Hint)
	}

	// The declared field itself is fine.
	ok := validateJSON(t, `{
	  "input": {"schema": {"format": "json", "document": {"type": "object",
	      "properties": {"userId": {"type": "string"}}}}},
	  "schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusersById",
	      "version": 1, "pathParams": {"id": "${ input.userId }"}}}}]}`)
	if hasMessage(ok, "is not carried by") {
		t.Errorf("a declared field must not warn: %v", ok.Findings)
	}
}

// With no schema declared, anything may be posted — there is nothing to check
// against, and guessing would be noise.
func TestValidate_NoInputSchemaMeansNoInputChecking(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "external"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers",
	      "version": 1, "queryParams": {"x": "${ input.anythingAtAll }"}}}}]}`)
	if hasMessage(r, "is not carried by") {
		t.Errorf("without a declared schema there is nothing to validate against: %v", r.Findings)
	}
}

func TestEventFields(t *testing.T) {
	base := EventFields("user_create")
	for _, want := range []string{"resource", "changes", "initiated_by", "auth_method", "geoip"} {
		if !KnownEventField("user_create", want) {
			t.Errorf("%q missing from the common envelope: %v", want, base)
		}
	}
	// Association events carry the association itself; ordinary ones do not.
	if !KnownEventField("association_change", "association") {
		t.Error("association_change should expose association")
	}
	if KnownEventField("user_create", "association") {
		t.Error("a non-association event should not expose association")
	}
	// The suffix rule must cover the whole family.
	if !KnownEventField("access_management_association_change", "association") {
		t.Error("the suffix match should cover access_management_association_change")
	}
}

// The workflows service emits its own event types, and the documentation omits
// them entirely. They matter more than a typical gap because they are
// TRIGGERABLE: without them, a workflow chaining off another workflow's run is
// reported as having an unknown event type — a false alarm from the one check
// that exists to catch real typos.
//
// Verified on a live tenant 2026-08-31: 10 workflow_create, 10 workflow_delete
// and 11 workflow_run events, service "workflows", none in the parsed catalog.
func TestValidate_WorkflowsServiceEventTypesAreKnown(t *testing.T) {
	for _, typ := range []string{"workflow_create", "workflow_delete", "workflow_run"} {
		if _, known := LookupEventType(typ); !known {
			t.Errorf("%q is emitted by this tenant and must not be reported as unknown", typ)
		}
	}
}

// A workflow run emits a workflow_run event, so triggering on workflow_run
// without a condition means re-triggering on your own completion.
func TestValidate_WorkflowRunTriggerWarnsAboutSelfRetrigger(t *testing.T) {
	r := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "workflow_run"}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)

	if !hasMessage(r, "re-trigger on its own completion") {
		t.Fatalf("an unconditioned workflow_run trigger must be flagged: %v", r.Findings)
	}
	if !hasHint(r, "initiated_by.id") {
		t.Errorf("the hint must name the field that breaks the loop: %v", r.Findings)
	}
	// It is a warning, not an error: the API accepts it and the pattern is
	// legitimate once narrowed.
	f, _ := findingAt(r, "with.type")
	if f.Severity != Warning {
		t.Errorf("severity = %v, want warning", f.Severity)
	}

	// Chaining off ANOTHER workflow is the useful case and must stay quiet.
	chained := validateJSON(t, `{"schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "workflow_run",
	    "condition": "initiated_by.id == \"6a95a7bc1218f500017769c8\""}}}},
	  "do": [{"a": {"call": "jc_operation", "with": {"operationId": "getApiSystemusers", "version": 1}}}]}`)
	if hasMessage(chained, "re-trigger on its own completion") {
		t.Errorf("a narrowed workflow_run trigger is a legitimate pattern: %v", chained.Findings)
	}
}
