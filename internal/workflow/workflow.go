// Package workflow holds the JumpCloud Workflows wire contract and DSL
// validator shared by the CLI (internal/cmd) and the MCP server
// (internal/mcp), so the two surfaces cannot drift on the endpoints, the
// three different list envelopes, or the DSL rules.
//
// The OpenAPI spec declares a workflow's DSL as a bare `dsl: object` — no
// schema, no field list. The real specification is JumpCloud's public
// "Workflows Public API & DSL Guide", vendored at docs/reference/workflows-dsl.md;
// every validation rule in validate.go cites a documented statement from it. What follows is what live probing
// against org 5ec71e8e96bfda0611fc6c5b on 2026-08-27 established beyond, or
// against, both documents:
//
//   - Three list envelopes in one area: /workflows and /workflows/runs answer
//     {totalCount, results}, but /workflows/templates answers {templates:[…]}.
//   - Runs OUTLIVE their workflow. A run for a workflow deleted weeks earlier
//     still lists, carrying workflowDeletedAt. Runs are an audit trail, not a
//     child collection — neither document mentions this.
//   - trigger_type (jc_events | external | scheduler) is response-only; it is
//     derived server-side from the DSL's trigger block and is not accepted on
//     create.
//   - The guide's jc_operation `with` field list omits `version`, but all 33
//     jc_operation steps across the 12 shipped templates carry it, and its
//     value tracks the operation's API version exactly (v1 path → 1, v2 path
//     → 2) with zero mismatches. It is real and fully derivable, so this
//     package emits it and validates it rather than ignoring it.
//   - A completed run carries executionDetails.nodes: a per-step trace with
//     each step's input, output, and the HTTP method, URL and status the step
//     actually called. Undocumented in both the spec and the guide, and the
//     only way to see why a run failed.
//   - Placeholders in shipped templates are REPLACE_WITH_* and may carry a
//     numeric suffix (REPLACE_WITH_GROUP_ID_1).
//   - Half the shipped catalog has side effects: 6 of 12 templates send email
//     and 2 call external connectors. Initialising from a template routinely
//     produces a DSL that needs --allow-side-effects.
package workflow

import (
	"encoding/json"
	"fmt"
)

// Endpoint is the workflow definitions collection.
const Endpoint = "/workflows"

// RunsEndpoint lists every workflow run in the organization. Runs outlive the
// workflow they came from, so this is not scoped under a workflow.
const RunsEndpoint = "/workflows/runs"

// TemplatesEndpoint is the server-served template catalog.
const TemplatesEndpoint = "/workflows/templates"

// WorkflowEndpoint is a single workflow by ID.
func WorkflowEndpoint(id string) string { return Endpoint + "/" + id }

// TriggerEndpoint starts a manual run. It is only meaningful for workflows
// whose trigger source is "external"; the API accepts the call for others but
// nothing happens.
func TriggerEndpoint(id string) string { return WorkflowEndpoint(id) + "/runs" }

// RunEndpoint is a single run by ID.
func RunEndpoint(runID string) string { return RunsEndpoint + "/" + runID }

// TemplateEndpoint is a single template by ID.
func TemplateEndpoint(id string) string { return TemplatesEndpoint + "/" + id }

// Trigger types, as reported by the API on reads. Derived server-side from the
// DSL; never sent on create.
const (
	TriggerEvents    = "jc_events"
	TriggerExternal  = "external"
	TriggerScheduler = "scheduler"
)

// Statuses accepted on create and update.
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)

// ListDefaultFields is the default field subset shown for workflows.
// ListDefaultFields trims the list view. The API returns the full DSL on every
// row, which dominates the payload and is not a list-view concern.
//
// last_ran is back, but COMPUTED rather than passed through: the API sends no
// last-run timestamp, so jc derives it from the runs list on every read. See
// lastran.go for why that override was worth making.
var ListDefaultFields = []string{"id", "name", "status", "trigger_type", "last_ran"}

// RunDefaultFields is the default field subset shown for runs.
var RunDefaultFields = []string{"id", "name", "workflowId", "status", "startedAt", "completedAt"}

// TemplateDefaultFields is the default field subset shown for templates.
var TemplateDefaultFields = []string{"id", "name", "category", "description"}

// Workflow is a saved workflow definition.
type Workflow struct {
	ID              string          `json:"id,omitempty"`
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	DSL             json.RawMessage `json:"dsl"`
	Status          string          `json:"status,omitempty"`
	TriggerType     string          `json:"trigger_type,omitempty"`
	ExecutionRoleID string          `json:"execution_role_id,omitempty"`
	CreatedBy       string          `json:"created_by,omitempty"`
	CreatedAt       string          `json:"created_at,omitempty"`
	UpdatedAt       string          `json:"updated_at,omitempty"`
}

// Run is one execution of a workflow. WorkflowDeletedAt is set when the
// workflow it came from has since been deleted; the run itself survives.
type Run struct {
	ID                string           `json:"id"`
	WorkflowID        string           `json:"workflowId"`
	Name              string           `json:"name,omitempty"`
	Status            string           `json:"status"`
	StartedAt         string           `json:"startedAt,omitempty"`
	CompletedAt       string           `json:"completedAt,omitempty"`
	Error             string           `json:"error,omitempty"`
	WorkflowDeletedAt string           `json:"workflowDeletedAt,omitempty"`
	Input             map[string]any   `json:"input,omitempty"`
	ExecutionDetails  ExecutionDetails `json:"executionDetails,omitempty"`
}

// ExecutionDetails is the per-step trace a completed run carries. Neither the
// OpenAPI spec nor the DSL guide mentions it, but it is the only place the
// outcome of an individual step is visible — including the HTTP status and URL
// each jc_operation actually called. It is what makes a failed run
// diagnosable.
type ExecutionDetails struct {
	Nodes []RunNode `json:"nodes,omitempty"`
}

// RunNode is one executed step. The first node is always the synthetic
// "__trigger" node carrying the run's input.
type RunNode struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Success     bool        `json:"success"`
	IsExecuted  bool        `json:"is_executed"`
	Message     string      `json:"message,omitempty"`
	TriggerType string      `json:"trigger_type,omitempty"`
	Truncated   bool        `json:"is_output_truncated,omitempty"`
	NodeInput   any         `json:"node_input,omitempty"`
	NodeOutput  *NodeOutput `json:"node_output,omitempty"`
	// IfCondition is present on a node whose task-level `if` was evaluated,
	// carrying the expression and its result. It is a stronger skip signal
	// than the message, being structured rather than English prose.
	IfCondition *IfCondition `json:"if_condition,omitempty"`
}

// IfCondition is a task guard as the engine evaluated it.
type IfCondition struct {
	Expression string `json:"expression"`
	Result     bool   `json:"result"`
}

// NodeOutput is what a step produced. Its shape depends on the node type: a
// jc_operation reports method/url/status/body, while an email node reports
// notification_type and a STRING status ("success").
type NodeOutput struct {
	Method string `json:"method,omitempty"`
	// Status is an HTTP status for a jc_operation and a word for an email
	// node, so it cannot be typed as an int — see TraceStatus.
	Status TraceStatus `json:"status,omitempty"`
	URL    string      `json:"url,omitempty"`
	Body   any         `json:"body,omitempty"`
	// NotificationType appears on email nodes.
	NotificationType string `json:"notification_type,omitempty"`
}

// TraceStatus is a step's status, which the engine reports as a NUMBER for an
// API call and as a STRING for an email send.
//
// Typing this as an int made `jc workflows runs get --trace` fail outright on
// any run containing an email step — the trace view, which is the only place a
// failed run's cause is visible, was unusable for exactly the workflows that
// send notifications. Observed as {"notification_type":"workflows_common",
// "status":"success"}.
type TraceStatus struct {
	// Code is the HTTP status when the engine reported a number, else 0.
	Code int
	// Text is the word the engine reported, else empty.
	Text string
}

// UnmarshalJSON accepts either form rather than failing on the unexpected one.
func (s *TraceStatus) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		return json.Unmarshal(b, &s.Text)
	}
	return json.Unmarshal(b, &s.Code)
}

// MarshalJSON round-trips whichever form was received.
func (s TraceStatus) MarshalJSON() ([]byte, error) {
	if s.Text != "" {
		return json.Marshal(s.Text)
	}
	return json.Marshal(s.Code)
}

// String renders the status for a human.
func (s TraceStatus) String() string {
	if s.Text != "" {
		return s.Text
	}
	if s.Code != 0 {
		return fmt.Sprintf("%d", s.Code)
	}
	return ""
}

// OK reports whether the status indicates success, in either form.
func (s TraceStatus) OK() bool {
	if s.Text != "" {
		return s.Text == "success"
	}
	return s.Code >= 200 && s.Code < 300
}

// Describe renders one step of a run trace as a single line.
func (n RunNode) Describe() string {
	// Rendered from State, not from is_executed/success/message, all three of
	// which are identical between a task that ran and one a branch routed
	// around. See RunState for how that was established.
	state, why := n.State()
	mark := map[RunState]string{
		RunStateRan:     "ok",
		RunStateSkipped: "skipped",
		RunStateFailed:  "FAILED",
		RunStateUnknown: "unclear",
	}[state]

	s := fmt.Sprintf("[%s] %s", mark, n.Name)
	// A paginated step aggregates several requests, so it reports no single
	// method/URL/status; printing "→ 0" there would invent one.
	if n.NodeOutput != nil && n.NodeOutput.Status.String() != "" {
		if n.NodeOutput.URL != "" {
			s += fmt.Sprintf("  %s %s → %s", n.NodeOutput.Method, n.NodeOutput.URL, n.NodeOutput.Status)
		} else {
			// An email node has a status but no method or URL.
			s += "  → " + n.NodeOutput.Status.String()
		}
	}
	// For a skip, the derived reason is what the operator needs: the upstream
	// message says "Task completed." on a task that made no call.
	switch {
	case state == RunStateSkipped || state == RunStateUnknown:
		s += "  (" + why + ")"
	case n.Message != "":
		s += "  (" + n.Message + ")"
	}
	if n.Truncated {
		s += "  [output truncated]"
	}
	return s
}

// FailedNode returns the first step that ran and did not succeed, which is
// where a failed run went wrong.
func (r Run) FailedNode() (RunNode, bool) {
	for _, n := range r.ExecutionDetails.Nodes {
		if n.IsExecuted && !n.Success {
			return n, true
		}
	}
	return RunNode{}, false
}

// Done reports whether the run has reached a terminal state, so a caller
// polling after a manual trigger knows when to stop.
func (r Run) Done() bool {
	switch r.Status {
	case "completed", "failed", "cancelled", "error", "timeout":
		return true
	}
	return false
}

// Template is one entry in the server-served catalog.
type Template struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Category    string          `json:"category,omitempty"`
	DSL         json.RawMessage `json:"dsl"`
}

// ParseList unwraps the {totalCount, results:[…]} envelope used by /workflows
// and /workflows/runs.
func ParseList(raw json.RawMessage) ([]json.RawMessage, error) {
	var env struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parsing workflow list: %w", err)
	}
	return env.Results, nil
}

// ParseRuns decodes the runs list into typed runs.
func ParseRuns(raw json.RawMessage) ([]Run, error) {
	rows, err := ParseList(raw)
	if err != nil {
		return nil, err
	}
	runs := make([]Run, 0, len(rows))
	for _, r := range rows {
		var run Run
		if err := json.Unmarshal(r, &run); err != nil {
			return nil, fmt.Errorf("parsing workflow run: %w", err)
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// ParseTemplates unwraps the {templates:[…]} envelope. The catalog uses a
// different envelope from every other list in this area.
func ParseTemplates(raw json.RawMessage) ([]Template, error) {
	var env struct {
		Templates []Template `json:"templates"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parsing workflow templates: %w", err)
	}
	return env.Templates, nil
}

// ParseWorkflow decodes a single workflow.
func ParseWorkflow(raw json.RawMessage) (Workflow, error) {
	var w Workflow
	if err := json.Unmarshal(raw, &w); err != nil {
		return Workflow{}, fmt.Errorf("parsing workflow: %w", err)
	}
	return w, nil
}

// ParseRun decodes a single run.
func ParseRun(raw json.RawMessage) (Run, error) {
	var r Run
	if err := json.Unmarshal(raw, &r); err != nil {
		return Run{}, fmt.Errorf("parsing workflow run: %w", err)
	}
	return r, nil
}

// CreateBody builds the POST /workflows body. trigger_type is deliberately
// absent: the API derives it from the DSL and rejects nothing, but sending it
// would imply the caller controls it.
func CreateBody(w Workflow) map[string]any {
	body := map[string]any{
		"name":              w.Name,
		"dsl":               w.DSL,
		"execution_role_id": w.ExecutionRoleID,
	}
	if w.Description != "" {
		body["description"] = w.Description
	}
	if w.Status != "" {
		body["status"] = w.Status
	}
	return body
}

// UpdateBody builds the PUT /workflows/{id} body. V2 PUTs in this API are
// full-replace, so callers must read the current workflow and pass a complete
// object rather than only what changed.
func UpdateBody(w Workflow) map[string]any {
	body := map[string]any{"dsl": w.DSL}
	if w.Name != "" {
		body["name"] = w.Name
	}
	if w.Description != "" {
		body["description"] = w.Description
	}
	if w.Status != "" {
		body["status"] = w.Status
	}
	if w.ExecutionRoleID != "" {
		body["execution_role_id"] = w.ExecutionRoleID
	}
	return body
}

// TriggerBody wraps the caller's input as the {data} envelope /runs expects.
func TriggerBody(data map[string]any) map[string]any {
	if data == nil {
		data = map[string]any{}
	}
	return map[string]any{"data": data}
}
