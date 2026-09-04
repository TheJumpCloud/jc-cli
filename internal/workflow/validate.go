package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/expr-lang/expr"
)

// Severity separates what will fail from what is merely suspect.
type Severity string

const (
	// Error means the workflow is invalid or will not do what it says.
	Error Severity = "error"
	// Warning means it is accepted but likely wrong.
	Warning Severity = "warning"
)

// Finding is one validation result.
type Finding struct {
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Message  string   `json:"message"`
	// Hint is the concrete fix, when there is one.
	Hint string `json:"hint,omitempty"`
}

func (f Finding) String() string {
	s := fmt.Sprintf("%s: %s: %s", f.Severity, f.Path, f.Message)
	if f.Hint != "" {
		s += "\n    hint: " + f.Hint
	}
	return s
}

// Result is the outcome of validating one DSL document.
type Result struct {
	Findings    []Finding    `json:"findings"`
	TriggerType string       `json:"trigger_type,omitempty"`
	SideEffects []SideEffect `json:"side_effects,omitempty"`
}

// OK reports whether the document is valid. Warnings do not make it invalid.
func (r Result) OK() bool {
	for _, f := range r.Findings {
		if f.Severity == Error {
			return false
		}
	}
	return true
}

// Errors returns only the blocking findings.
func (r Result) Errors() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity == Error {
			out = append(out, f)
		}
	}
	return out
}

// Err renders the blocking findings as a single error, or nil when valid.
func (r Result) Err() error {
	errs := r.Errors()
	if len(errs) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "workflow DSL is invalid (%d problem%s):", len(errs), plural(len(errs)))
	for _, f := range errs {
		fmt.Fprintf(&b, "\n  %s", f.String())
	}
	return fmt.Errorf("%s", b.String())
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Validate checks a DSL document against the rules JumpCloud's Workflows DSL
// guide documents, plus the operationId index. It is deliberately local: the
// API accepts a malformed DSL and fails at run time, so catching problems here
// is the difference between an error at author time and a workflow that
// silently never works.
func ValidateRaw(raw json.RawMessage) Result {
	d, err := ParseDSL(raw)
	if err != nil {
		return Result{Findings: []Finding{{Severity: Error, Path: "dsl", Message: err.Error()}}}
	}
	return Validate(d)
}

// Validate checks a parsed DSL document.
func Validate(d DSL) Result {
	var r Result
	add := func(sev Severity, path, msg, hint string) {
		r.Findings = append(r.Findings, Finding{Severity: sev, Path: path, Message: msg, Hint: hint})
	}

	trigger := validateTrigger(d, add)
	r.TriggerType = trigger.TriggerType()

	tasks := d.Tasks()
	validateTasks(d, tasks, add)
	checkReachability(tasks, add)
	validateExpressions(d, trigger, tasks, add)
	checkDeadStatusGuards(d, add)
	checkInputReferences(d, trigger, add)
	validatePlaceholders(d, add)

	r.SideEffects = d.SideEffects()

	sort.SliceStable(r.Findings, func(i, j int) bool {
		if r.Findings[i].Severity != r.Findings[j].Severity {
			return r.Findings[i].Severity == Error
		}
		return r.Findings[i].Path < r.Findings[j].Path
	})
	return r
}

// validateTrigger enforces that exactly one of the three trigger styles is
// used and that it is complete.
func validateTrigger(d DSL, add func(Severity, string, string, string)) TriggerStyle {
	trigger, err := d.Trigger()
	if err != nil {
		add(Error, "dsl.schedule", err.Error(),
			"use schedule.on.one.with.source = jc_events|external, or schedule.frequency for a timed run")
		return trigger
	}

	// The three styles are mutually exclusive; a schedule carrying both a
	// frequency and a source is ambiguous rather than additive.
	if _, hasOn := d.Schedule["on"]; hasOn && trigger.Frequency != "" {
		add(Error, "dsl.schedule", "schedule declares both a frequency and an on.one trigger",
			"choose exactly one trigger style: jc_events, external, or a frequency schedule")
	}

	switch {
	case trigger.Frequency != "":
		if !ScheduleFrequencies[trigger.Frequency] {
			add(Error, "dsl.schedule.frequency",
				fmt.Sprintf("unknown frequency %q", trigger.Frequency),
				"one of: once, hourly, daily, weekly, monthly (the DSL takes no cron strings)")
		}
		if trigger.Frequency == "once" {
			if _, ok := d.Schedule["start_date"]; !ok {
				add(Error, "dsl.schedule", "a once schedule needs start_date", "e.g. \"start_date\": \"2026-06-01\"")
			}
		}
		if _, ok := d.Schedule["timezone"]; !ok {
			add(Warning, "dsl.schedule", "no timezone set", "add \"timezone\", e.g. \"Etc/UTC\"; otherwise the server default applies")
		}

	case trigger.Source == TriggerEvents:
		if trigger.EventType == "" {
			add(Error, "dsl.schedule.on.one.with", "a jc_events trigger needs a type",
				"set \"type\" to the Directory Insights event to listen for, e.g. \"user_suspended\"")
			break
		}
		// A mistyped event type saves, activates and silently never fires,
		// which is indistinguishable from an event that has not happened yet.
		// Warning rather than erroring because the catalog is a lower bound:
		// a live tenant emitted 30 types the docs do not list.
		if _, known := LookupEventType(trigger.EventType); !known {
			hint := "check the spelling; the API is the authority and this catalog is a lower bound"
			if s := SuggestEventType(trigger.EventType, 3); len(s) > 0 {
				hint = "closest known: " + strings.Join(s, ", ") +
					" — the API is the authority and this catalog is a lower bound"
			}
			add(Warning, "dsl.schedule.on.one.with.type",
				fmt.Sprintf("unknown Directory Insights event type %q", trigger.EventType), hint)
		}

		// A workflow run emits a workflow_run event, so a workflow triggered
		// on workflow_run re-triggers itself on its own completion. Verified
		// live: every run produces one, carrying initiated_by.id = the
		// workflow that ran.
		//
		// A trigger condition is the only thing standing between that and an
		// unbounded loop, so the absence of one is what makes this worth
		// flagging rather than the event type itself — chaining off ANOTHER
		// workflow's run is a legitimate and useful pattern.
		if trigger.EventType == "workflow_run" && trigger.Condition == "" {
			add(Warning, "dsl.schedule.on.one.with.type",
				"this triggers on workflow_run, and a workflow run itself emits a workflow_run event, "+
					"so this workflow will re-trigger on its own completion",
				"add a condition narrowing it to the workflow you mean to chain from — "+
					"initiated_by.id is the id of the workflow that ran, so "+
					`initiated_by.id == "<other workflow id>" stops it retriggering itself`)
		}

	case trigger.Source == TriggerExternal:
		// nothing further required; input.schema is optional

	case trigger.Source == "":
		add(Error, "dsl.schedule.on.one.with", "trigger has no source",
			"jc_events or external. A SCHEDULED workflow does not use this envelope at all: omit `on` entirely and give schedule a flat object — {frequency, interval, day_of_week, time, timezone} — which validates as trigger_type scheduler.")

	default:
		// There are THREE trigger types, and this hint used to name two.
		// explain, health and lint all report trigger_type "scheduler", and
		// lint passes two scheduler templates as clean — so an author who
		// wrote source: "scheduler" by analogy was told by the validator
		// that the thing they were doing does not exist, while the rest of
		// jc showed it working. A hint that is confidently wrong stops
		// correct work, which is worse than no hint.
		add(Error, "dsl.schedule.on.one.with.source",
			fmt.Sprintf("unknown trigger source %q", trigger.Source),
			"jc_events or external. A SCHEDULED workflow does not use this envelope at all: omit `on` entirely and give schedule a flat object — {frequency, interval, day_of_week, time, timezone} — which validates as trigger_type scheduler.")
	}
	return trigger
}

// validateTasks enforces the structural rules on the task list.
func validateTasks(d DSL, tasks []Task, add func(Severity, string, string, string)) {
	if len(d.Do) == 0 {
		add(Error, "dsl.do", "a workflow needs at least one task", "")
		return
	}

	// Task names in document order, so forward-only reference checks can ask
	// "does this target appear later?".
	position := map[string]int{}
	for i, t := range tasks {
		if _, dup := position[t.Name]; dup {
			add(Error, t.Path, fmt.Sprintf("duplicate task name %q", t.Name),
				"task names must be unique; they are how `then` targets and ${ actions.<name> } refer to a step")
			continue
		}
		position[t.Name] = i
	}

	for i, t := range tasks {
		if t.Body == nil {
			add(Error, t.Path, "task body is not an object", "")
			continue
		}
		validateTask(t, i, position, add)
	}
}

func validateTask(t Task, index int, position map[string]int, add func(Severity, string, string, string)) {
	_, isLoop := t.Body["for"]
	_, isSwitch := t.Body["switch"]
	call := t.Call()

	switch {
	case isLoop:
		validateLoop(t, add)
	case isSwitch:
		validateSwitch(t, index, position, add)
	case call == "":
		add(Error, t.Path, "task does nothing: no call, for, or switch", "")
	case !KnownCalls[call]:
		add(Error, t.Path+".call", fmt.Sprintf("unknown call type %q", call),
			"one of: jc_operation, sendEmailsToAddresses, sendEmailsToChannel, connector_operation "+
				"(sendEmailsToChannel is documented but appears in none of the shipped templates)")
	default:
		validateCall(t, add)
	}

	if then, ok := t.Body["then"].(string); ok {
		validateThen(then, t.Path+".then", index, position, add)
	}
}

// validateLoop enforces the loop rules: an iterable, a non-empty body, and no
// nesting (the engine does not support nested loops).
func validateLoop(t Task, add func(Severity, string, string, string)) {
	if t.Depth > 0 {
		add(Error, t.Path, "nested for loops are not supported",
			"collect with pagination + extract, then iterate once")
	}
	loop, ok := t.Body["for"].(map[string]any)
	if !ok {
		add(Error, t.Path+".for", "for must be an object with each and in", "")
		return
	}
	if each, _ := loop["each"].(string); each == "" {
		add(Error, t.Path+".for.each", "for.each must name the iterator variable", "e.g. \"each\": \"user\"")
	}
	if _, ok := loop["in"]; !ok {
		add(Error, t.Path+".for.in", "for.in must name the collection to iterate", "e.g. \"in\": \"${ actions.listAllUsers }\"")
	}
	body, ok := t.Body["do"].([]any)
	if !ok || len(body) == 0 {
		add(Error, t.Path+".do", "a for loop body needs at least one task", "")
	}
}

// validateSwitch enforces that every branch has a forward target.
func validateSwitch(t Task, index int, position map[string]int, add func(Severity, string, string, string)) {
	branches, ok := t.Body["switch"].([]any)
	if !ok || len(branches) == 0 {
		add(Error, t.Path+".switch", "switch must be a non-empty list of branches", "")
		return
	}
	for i, raw := range branches {
		branch, ok := raw.(map[string]any)
		if !ok {
			add(Error, fmt.Sprintf("%s.switch[%d]", t.Path, i), "branch is not an object", "")
			continue
		}
		for name, rawCase := range branch {
			p := fmt.Sprintf("%s.switch[%d].%s", t.Path, i, name)
			c, ok := rawCase.(map[string]any)
			if !ok {
				add(Error, p, "branch case is not an object", "")
				continue
			}
			then, _ := c["then"].(string)
			if then == "" {
				add(Error, p+".then", "branch has no then target", "name a task defined after this switch")
				continue
			}
			validateThen(then, p+".then", index, position, add)
			if _, hasWhen := c["when"]; !hasWhen && name != "default" {
				add(Warning, p, "branch has no when condition", "only the default branch may omit when")
			}
		}
	}
}

// validateThen enforces the forward-only rule on jump targets.
func validateThen(target, path string, index int, position map[string]int, add func(Severity, string, string, string)) {
	if ControlTargets[target] {
		return
	}
	pos, ok := position[target]
	if !ok {
		add(Error, path, fmt.Sprintf("then target %q is not a task in this workflow", target),
			"targets must be a task name, or one of: continue, exit, end")
		return
	}
	if pos <= index {
		add(Error, path, fmt.Sprintf("then target %q is not forward of this task", target),
			"then targets must be forward-only; backward jumps are rejected")
	}
}

// validateCall checks the per-call-type `with` block.
func validateCall(t Task, add func(Severity, string, string, string)) {
	with := t.With()
	if with == nil {
		add(Error, t.Path+".with", fmt.Sprintf("%s needs a with block", t.Call()), "")
		return
	}

	switch t.Call() {
	case CallJCOperation:
		validateJCOperation(t, with, add)
	case CallEmailAddresses, CallEmailChannel:
		validateEmail(t, with, add)
	case CallConnector:
		validateConnector(t, with, add)
	}
}

func validateJCOperation(t Task, with map[string]any, add func(Severity, string, string, string)) {
	id, _ := with["operationId"].(string)
	if id == "" {
		add(Error, t.Path+".with.operationId", "jc_operation needs an operationId", "")
		return
	}

	op, known := LookupOperation(id)
	if !known {
		hint := ""
		if s := SuggestOperation(id, 3); len(s) > 0 {
			hint = "did you mean: " + strings.Join(s, ", ")
		}
		if looksLegacyID(id) {
			msg := fmt.Sprintf("operationId %q is not a known operation, and looks like a deprecated snake_case id", id)
			if hint == "" {
				hint = "JumpCloud migrated these to standardized camelCase ids"
			} else {
				hint += " (JumpCloud migrated snake_case ids to camelCase)"
			}
			add(Error, t.Path+".with.operationId", msg, hint)
			return
		}
		add(Error, t.Path+".with.operationId", fmt.Sprintf("unknown operationId %q", id), hint)
		return
	}

	// version is absent from the guide's field list, but every jc_operation
	// step in every shipped template carries it and its value always matches
	// the operation's API version. Treat it as required and checkable.
	want := op.APIVersion()
	switch v, ok := with["version"]; {
	case !ok:
		add(Warning, t.Path+".with.version",
			fmt.Sprintf("no version set; %s is a v%d operation", id, want),
			fmt.Sprintf("add \"version\": %d", want))
	default:
		got, isNum := toInt(v)
		if !isNum {
			add(Error, t.Path+".with.version", fmt.Sprintf("version must be a number, got %T", v), "")
		} else if got != want {
			add(Error, t.Path+".with.version",
				fmt.Sprintf("version %d does not match %s (%s is a v%d operation)", got, id, op.Path, want),
				fmt.Sprintf("set \"version\": %d", want))
		}
	}

	// A path with {placeholders} needs pathParams to fill them.
	if strings.Contains(op.Path, "{") {
		if _, ok := with["pathParams"]; !ok {
			add(Error, t.Path+".with.pathParams",
				fmt.Sprintf("%s needs pathParams for %s", id, op.Path),
				"supply one entry per {segment} in the path")
		}
	}

	if pag, ok := with["pagination"]; ok {
		validatePagination(t, pag, add)
	}
}

func validatePagination(t Task, raw any, add func(Severity, string, string, string)) {
	p := t.Path + ".with.pagination"
	pag, ok := raw.(map[string]any)
	if !ok {
		add(Error, p, "pagination must be an object", "")
		return
	}
	update, ok := pag["update"].(map[string]any)
	if !ok {
		add(Error, p+".update", "pagination needs an update block", "")
	} else {
		in, _ := update["in"].(string)
		switch in {
		case "pathParams", "queryParams", "bodyParams":
		case "":
			add(Error, p+".update.in", "pagination.update.in is required", "one of: pathParams, queryParams, bodyParams")
		default:
			add(Error, p+".update.in", fmt.Sprintf("unknown pagination target %q", in),
				"one of: pathParams, queryParams, bodyParams")
		}
		if key, _ := update["key"].(string); key == "" {
			add(Error, p+".update.key", "pagination.update.key is required", "the parameter to advance, e.g. skip or cursor")
		}
		if _, ok := update["value"]; !ok {
			add(Error, p+".update.value", "pagination.update.value is required", "an expression producing the next page's value")
		}
	}
	if _, ok := pag["until"]; !ok {
		// The guide names a missing until as the usual cause of runaway
		// iteration, so this is an error rather than a warning.
		add(Error, p+".until", "pagination needs an until condition",
			"without it the loop runs until the iteration cap, e.g. \"${ len(page.response.body.results) == 0 }\"")
	}
}

func validateEmail(t Task, with map[string]any, add func(Severity, string, string, string)) {
	msg, ok := with["message"].(map[string]any)
	if !ok {
		add(Error, t.Path+".with.message", "email needs a message block", "")
	} else {
		if s, _ := msg["subject"].(string); s == "" {
			add(Error, t.Path+".with.message.subject", "email needs a subject", "")
		}
		if b, _ := msg["body"].(string); b == "" {
			add(Error, t.Path+".with.message.body", "email needs a body", "")
		}
	}

	recipients, ok := with["recipients"].(map[string]any)
	if !ok {
		add(Error, t.Path+".with.recipients", "email needs recipients", "")
		return
	}
	key := "to_addresses"
	if t.Call() == CallEmailChannel {
		key = "channel_object_ids"
	}
	list, _ := recipients[key].([]any)
	if len(list) == 0 {
		add(Error, t.Path+".with.recipients."+key,
			fmt.Sprintf("%s needs a non-empty %s list", t.Call(), key), "")
	}
}

func validateConnector(t Task, with map[string]any, add func(Severity, string, string, string)) {
	if id, _ := with["id"].(string); id == "" {
		add(Error, t.Path+".with.id", "connector_operation needs the connector object id", "")
	}
	method, _ := with["httpMethod"].(string)
	if method == "" {
		add(Error, t.Path+".with.httpMethod", "connector_operation needs an httpMethod", "e.g. GET or POST")
	}
	if p, _ := with["endpointPath"].(string); p == "" {
		add(Error, t.Path+".with.endpointPath", "connector_operation needs an endpointPath", "")
	}
}

// validatePlaceholders reports every REPLACE_WITH_* marker left unfilled.
func validatePlaceholders(d DSL, add func(Severity, string, string, string)) {
	for _, p := range d.Placeholders() {
		add(Error, p.Path, fmt.Sprintf("unfilled template placeholder %s", p.Marker),
			"replace it with a real value before creating the workflow")
	}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

// compileExpr compiles one Expr source the way the workflow engine does, so a
// syntax error surfaces at author time rather than at run time.
func compileExpr(src string, kind ExprKind) error {
	opts := []expr.Option{}
	if kind == ExprBool {
		opts = append(opts, expr.AsBool())
	}
	_, err := expr.Compile(src, opts...)
	return err
}

// ScopeGap is one step whose operation no held scope permits.
type ScopeGap struct {
	// Task is the step's name.
	Task string `json:"task"`
	// OperationID is what it calls.
	OperationID string `json:"operation_id"`
	// Describe renders the operation as METHOD /path.
	Describe string `json:"describe"`
	// Needs are the scopes that would permit it; any one suffices.
	Needs []string `json:"needs"`
}

// CheckScopes reports the steps a role's scopes do not obviously permit.
//
// The execution role is the only thing between an unattended workflow and the
// API — validation otherwise checks that an operation EXISTS, not that the
// workflow may call it, so deleteApiSystemusersById passes silently. Comparing
// the DSL's operations against a named role's scopes moves that from a run-time
// surprise to an author-time finding.
//
// A gap is advisory, not disqualifying. The spec's x-scopes is a lower bound:
// the live API accepted a scope for postApiRuncommand that the spec omits, so
// a role holding none of the declared scopes may still be permitted. Reporting
// a gap as an error would block workflows that actually work.
func CheckScopes(d DSL, roleScopes []string) []ScopeGap {
	held := make(map[string]bool, len(roleScopes))
	for _, s := range roleScopes {
		held[s] = true
	}

	var gaps []ScopeGap
	seen := map[string]bool{}
	for _, t := range d.Tasks() {
		id := t.OperationID()
		if id == "" || seen[t.Name] {
			continue
		}
		seen[t.Name] = true

		op, ok := LookupOperation(id)
		if !ok || len(op.Scopes) == 0 {
			continue
		}
		if op.PermittedBy(held) {
			continue
		}
		gaps = append(gaps, ScopeGap{
			Task: t.Name, OperationID: id, Describe: op.Describe(), Needs: op.Scopes,
		})
	}
	return gaps
}

// ValidateWithRole runs Validate and adds a scope finding per step the role
// does not obviously permit. roleName is used only in the message.
func ValidateWithRole(d DSL, roleName string, roleScopes []string) Result {
	r := Validate(d)
	for _, g := range CheckScopes(d, roleScopes) {
		r.Findings = append(r.Findings, Finding{
			Severity: Warning,
			Path:     "dsl.do." + g.Task,
			Message: fmt.Sprintf("role %q may not permit %s (%s)",
				roleName, g.OperationID, g.Describe),
			Hint: "needs one of: " + strings.Join(g.Needs, ", ") +
				" — the API is the authority; the spec's scope list is a lower bound",
		})
	}
	return r
}

// checkReachability warns about tasks no jump targets.
//
// The execution model, established by live probing on 2026-08-28 with two
// probes differing only in where the task sat relative to a switch's target:
//
//   - A task named by a `then` or a switch branch belongs to the jump graph.
//     Only the chosen branch's target runs; the others are skipped. A switch's
//     unchosen default target did not appear in its run trace at all.
//   - A task named by NOTHING is not part of that graph. It executes in array
//     order regardless of any switch around it — verified both between a
//     switch and its target, and after it.
//
// So an un-targeted task is the dangerous case, and dangerous in the opposite
// direction to the obvious guess: someone who writes a switch to route AROUND
// a step finds it running anyway. This warns about exactly that, and the hint
// says so — an earlier version asserted the reverse and would have told an
// author a destructive step was safely bypassed when it was not.
func checkReachability(tasks []Task, add func(Severity, string, string, string)) {
	// Only reason about top-level tasks. Loop bodies have their own ordering
	// and no jumps into them.
	var top []Task
	for _, t := range tasks {
		if t.Depth == 0 {
			top = append(top, t)
		}
	}
	if len(top) < 2 {
		return
	}

	targeted := JumpTargets(top)

	for i := 1; i < len(top); i++ {
		t := top[i]
		if targeted[t.Name] || !JumpsAway(top[i-1]) {
			continue
		}
		add(Warning, t.Path,
			fmt.Sprintf("task %q is not targeted by any then, but %q jumps elsewhere, so it will silently never run",
				t.Name, top[i-1].Name),
			"execution follows the jump graph, not the order of the do list. Give some branch an explicit then "+
				"naming this task, or move it out of the do list")
	}
}
