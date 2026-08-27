package screen

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/workflow"
)

const authorTemplateDSL = `{
  "schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "association_change",
     "condition": "association.connection.from.object_id in [\"REPLACE_WITH_DEVICE_GROUP_ID\"]"}}}},
  "do": [{"runCommandOnDevice": {"call": "jc_operation", "with": {
     "operationId": "postApiRuncommand", "version": 1,
     "bodyParams": {"_id": "REPLACE_WITH_COMMAND_ID"}}}}]
}`

const authorEmailDSL = `{
  "schedule": {"on": {"one": {"with": {"source": "external"}}}},
  "do": [{"mail": {"call": "sendEmailsToAddresses", "with": {
     "message": {"subject": "s", "body": "b"},
     "recipients": {"to_addresses": ["REPLACE_WITH_IT_OPS_EMAIL"]}}}}]
}`

func authorScreen(t *testing.T, dsl string) *WorkflowAuthoringScreen {
	t.Helper()
	s := NewWorkflowAuthoringScreen(workflow.Template{
		ID: "tmpl-1", Name: "Run A Command", Description: "d",
		DSL: json.RawMessage(dsl),
	})
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	return s
}

func TestWorkflowAuthoring_RowsCarryPlaceholderKinds(t *testing.T) {
	s := authorScreen(t, authorTemplateDSL)
	if len(s.rows) != 2 {
		t.Fatalf("expected a row per marker, got %d", len(s.rows))
	}
	// Sorted, so the order is stable between runs.
	if s.rows[0].Marker != "REPLACE_WITH_COMMAND_ID" || s.rows[1].Marker != "REPLACE_WITH_DEVICE_GROUP_ID" {
		t.Fatalf("rows not in sorted order: %+v", s.rows)
	}
	if s.rows[0].Kind.Kind != workflow.KindCommand {
		t.Errorf("COMMAND_ID row kind = %q", s.rows[0].Kind.Kind)
	}
	// The device-group marker must NOT fall through to the user-group rule.
	if s.rows[1].Kind.Kind != workflow.KindDeviceGroup {
		t.Errorf("DEVICE_GROUP_ID row kind = %q, want device-group", s.rows[1].Kind.Kind)
	}
}

func TestWorkflowAuthoring_NameStageRejectsEmpty(t *testing.T) {
	s := authorScreen(t, authorTemplateDSL)
	s.input.SetValue("   ")
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.stage != wfAuthorStageName {
		t.Error("an empty name must not advance the flow")
	}
	if s.err == "" {
		t.Error("an empty name should say why it was rejected")
	}
}

func TestWorkflowAuthoring_CannotContinueWithUnfilledMarkers(t *testing.T) {
	s := authorScreen(t, authorTemplateDSL)
	s.input.SetValue("My Workflow")
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // name -> fill

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if s.stage != wfAuthorStageFill {
		t.Errorf("continuing with unfilled markers must stay on the fill stage, got %v", s.stage)
	}
	if !strings.Contains(s.err, "COMMAND_ID") {
		t.Errorf("the error should name what is missing, got %q", s.err)
	}
}

func TestWorkflowAuthoring_FillProducesACompleteDSL(t *testing.T) {
	s := authorScreen(t, authorTemplateDSL)
	s.rows[0].Value, s.rows[0].Display = "6139919b03c9b24d0b8f3ef1", "Restart Spooler"
	s.rows[1].Value, s.rows[1].Display = "6912793ab839ce0001735a0e", "Staging Devices"

	if err := s.applyFills(); err != nil {
		t.Fatalf("applyFills: %v", err)
	}
	if strings.Contains(string(s.dsl), "REPLACE_WITH_") {
		t.Errorf("markers survived: %s", s.dsl)
	}
	// The marker in the trigger CONDITION must be substituted too, not just
	// the one in the task parameters.
	if !strings.Contains(string(s.dsl), "6912793ab839ce0001735a0e") {
		t.Errorf("the condition's marker was not filled: %s", s.dsl)
	}

	s.validate()
	if !s.result.OK() {
		t.Errorf("the filled workflow should validate: %v", s.result.Errors())
	}
}

// The fill view shows the chosen NAME, with the resolved ID alongside — an
// operator should not have to recognise a 24-hex string.
func TestWorkflowAuthoring_FillViewShowsNamesNotIDs(t *testing.T) {
	s := authorScreen(t, authorTemplateDSL)
	s.stage = wfAuthorStageFill
	s.rows[0].Value, s.rows[0].Display = "6139919b03c9b24d0b8f3ef1", "Restart Spooler"

	view := s.View()
	if !strings.Contains(view, "Restart Spooler") {
		t.Errorf("the chosen name should be shown:\n%s", view)
	}
	if !strings.Contains(view, "6139919b03c9b24d0b8f3ef1") {
		t.Errorf("the resolved ID should be shown alongside:\n%s", view)
	}
	// An unanswered row should say what it wants.
	if !strings.Contains(view, "a JumpCloud device group") {
		t.Errorf("an unfilled row should describe what it expects:\n%s", view)
	}
}

func TestWorkflowAuthoring_ReviewBlocksCreateWhenInvalid(t *testing.T) {
	s := authorScreen(t, authorTemplateDSL)
	// Never filled, so validation fails on the placeholders.
	s.validate()
	s.stage = wfAuthorStageReview
	s.roleName = "Read Only"

	if s.result.OK() {
		t.Fatal("an unfilled template must not validate")
	}
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.stage == wfAuthorStageCreating {
		t.Error("Enter must not create while validation is failing")
	}

	view := s.View()
	if !strings.Contains(view, "cannot create") {
		t.Errorf("review should say creation is blocked:\n%s", view)
	}
	if strings.Contains(view, "Enter create") {
		t.Errorf("the footer must not offer create while invalid:\n%s", view)
	}
}

// A side-effecting workflow needs a second, explicit confirmation — the TUI
// equivalent of --allow-side-effects.
func TestWorkflowAuthoring_SideEffectsNeedExtraConfirmation(t *testing.T) {
	s := authorScreen(t, authorEmailDSL)
	s.rows[0].Value, s.rows[0].Display = "ops@example.com", "ops@example.com"
	if err := s.applyFills(); err != nil {
		t.Fatalf("applyFills: %v", err)
	}
	s.validate()
	if !s.result.OK() {
		t.Fatalf("this workflow is valid, just side-effecting: %v", s.result.Errors())
	}
	if len(s.result.SideEffects) == 0 {
		t.Fatal("the email step should be reported as a side effect")
	}

	s.stage = wfAuthorStageReview
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.stage != wfAuthorStageConfirmRisk {
		t.Fatalf("Enter should route to the risk confirmation, got %v", s.stage)
	}
	view := s.View()
	if !strings.Contains(view, "reaches outside JumpCloud") {
		t.Errorf("the confirmation should say why it is being asked:\n%s", view)
	}
	if !strings.Contains(view, "ops@example.com") {
		t.Errorf("the confirmation should name who gets mailed:\n%s", view)
	}

	// Declining goes back rather than creating.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if s.stage != wfAuthorStageReview {
		t.Errorf("declining should return to review, got %v", s.stage)
	}
}

func TestWorkflowAuthoring_AdminRoleIsCalledOut(t *testing.T) {
	s := authorScreen(t, authorTemplateDSL)
	s.stage = wfAuthorStageReview
	s.roleName = "Administrator"
	s.validate()

	if !strings.Contains(s.View(), "broad standing grant") {
		t.Errorf("binding an admin role to an unattended workflow should be called out:\n%s", s.View())
	}

	s.roleName = "Read Only"
	if strings.Contains(s.View(), "broad standing grant") {
		t.Errorf("a least-privilege role should not warn:\n%s", s.View())
	}
}

func TestPickerEntryForKind(t *testing.T) {
	// Commands are a V1 resource; using the V2 client would request
	// /api/v2/commands, which does not exist.
	e, ok := pickerEntryForKind(workflow.KindCommand)
	if !ok {
		t.Fatal("commands should be pickable")
	}
	if e.ClientType != tui.ClientV1 {
		t.Errorf("commands picker must use the V1 client, got %v", e.ClientType)
	}
	if e.ListEndpoint != "/commands" {
		t.Errorf("endpoint = %q", e.ListEndpoint)
	}

	if e, ok := pickerEntryForKind(workflow.KindWorkflow); !ok || e.ResponseKey != "results" {
		t.Errorf("the workflows picker must unwrap the results envelope: %+v", e)
	}

	// Free text has no list to pick from and must fall back to a prompt.
	if _, ok := pickerEntryForKind(workflow.KindFreeText); ok {
		t.Error("free text must not offer a picker")
	}
}

func TestWorkflowAuthoring_FreeTextMarkerPromptsInstead(t *testing.T) {
	s := authorScreen(t, authorEmailDSL)
	s.stage = wfAuthorStageFill
	s.cursor = 0

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.stage != wfAuthorStageFreeText {
		t.Fatalf("a free-text marker should open a prompt, got %v", s.stage)
	}
	if !strings.Contains(s.View(), "an email address to notify") {
		t.Errorf("the prompt should describe what is wanted:\n%s", s.View())
	}

	s.input.SetValue("ops@example.com")
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.rows[0].Value != "ops@example.com" {
		t.Errorf("the typed value should be recorded, got %q", s.rows[0].Value)
	}
	if s.stage != wfAuthorStageFill {
		t.Errorf("accepting should return to the fill list, got %v", s.stage)
	}
}
