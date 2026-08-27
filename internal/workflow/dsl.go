package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Call types a task may invoke. Three of the four have side effects that reach
// outside JumpCloud, which is why they are named here rather than treated as
// opaque strings.
const (
	// CallJCOperation invokes a JumpCloud API operation by operationId.
	CallJCOperation = "jc_operation"
	// CallEmailAddresses sends real email to explicit addresses.
	CallEmailAddresses = "sendEmailsToAddresses"
	// CallEmailChannel sends real email to notification channels.
	CallEmailChannel = "sendEmailsToChannel"
	// CallConnector performs an arbitrary HTTP request against a configured
	// third-party connector.
	CallConnector = "connector_operation"
)

// SideEffectCalls are the call types that reach outside JumpCloud: they mail
// people or call external endpoints. Creating a workflow containing one is
// gated behind an explicit opt-in.
var SideEffectCalls = map[string]string{
	CallEmailAddresses: "sends email to explicit addresses",
	CallEmailChannel:   "sends email to notification channels",
	CallConnector:      "calls an external connector endpoint",
}

// KnownCalls is every call type the DSL defines.
var KnownCalls = map[string]bool{
	CallJCOperation:    true,
	CallEmailAddresses: true,
	CallEmailChannel:   true,
	CallConnector:      true,
}

// ScheduleFrequencies are the time-based trigger frequencies. The DSL takes a
// structured schedule; there are no cron strings.
var ScheduleFrequencies = map[string]bool{
	"once": true, "hourly": true, "daily": true, "weekly": true, "monthly": true,
}

// ControlTargets are the reserved `then` targets that are not task names.
var ControlTargets = map[string]bool{"continue": true, "exit": true, "end": true}

// placeholderRE matches the REPLACE_WITH_* markers the shipped templates leave
// for the author to fill. The numeric suffix is real: templates that bind
// several groups use REPLACE_WITH_GROUP_ID_1, _2, _3.
var placeholderRE = regexp.MustCompile(`REPLACE_WITH_[A-Za-z0-9_]+`)

// exprInterpolationRE finds ${ ... } spans. Expressions may also appear bare
// (the DSL accepts both forms), so this only locates the wrapped ones.
var exprInterpolationRE = regexp.MustCompile(`\$\{([^}]*)\}`)

// DSL is a workflow definition document.
type DSL struct {
	Input    map[string]any   `json:"input,omitempty"`
	Schedule map[string]any   `json:"schedule"`
	Do       []map[string]any `json:"do"`
}

// ParseDSL decodes a DSL document, rejecting unknown top-level keys so a
// misplaced field is reported rather than silently ignored by the server.
func ParseDSL(raw json.RawMessage) (DSL, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return DSL{}, fmt.Errorf("dsl is not a JSON object: %w", err)
	}
	var unknown []string
	for k := range probe {
		switch k {
		case "input", "schedule", "do":
		default:
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return DSL{}, fmt.Errorf("unknown top-level dsl key(s): %s (expected input, schedule, do)",
			strings.Join(unknown, ", "))
	}

	var d DSL
	if err := json.Unmarshal(raw, &d); err != nil {
		return DSL{}, fmt.Errorf("parsing dsl: %w", err)
	}
	return d, nil
}

// TriggerStyle describes which of the three mutually exclusive trigger styles
// a DSL uses.
type TriggerStyle struct {
	// Source is "jc_events" or "external" for event and manual triggers, and
	// empty for a time-based schedule.
	Source string
	// Frequency is set for a time-based schedule.
	Frequency string
	// Condition is the trigger's gating expression, when present.
	Condition string
	// EventType is the Directory Insights event a jc_events trigger listens
	// for.
	EventType string
}

// TriggerType maps the style onto the trigger_type the API reports back.
func (t TriggerStyle) TriggerType() string {
	switch {
	case t.Frequency != "":
		return TriggerScheduler
	case t.Source != "":
		return t.Source
	}
	return ""
}

// Trigger reads the trigger style out of the schedule block. It reports an
// error only for a schedule that names no recognisable style at all; deeper
// checks belong to Validate.
func (d DSL) Trigger() (TriggerStyle, error) {
	if d.Schedule == nil {
		return TriggerStyle{}, fmt.Errorf("dsl.schedule is missing")
	}

	if f, ok := d.Schedule["frequency"].(string); ok && f != "" {
		return TriggerStyle{Frequency: f}, nil
	}

	with, ok := scheduleWith(d.Schedule)
	if !ok {
		return TriggerStyle{}, fmt.Errorf(
			"dsl.schedule names no trigger: expected schedule.frequency, or schedule.on.one.with.source")
	}
	ts := TriggerStyle{}
	ts.Source, _ = with["source"].(string)
	ts.Condition, _ = with["condition"].(string)
	ts.EventType, _ = with["type"].(string)
	return ts, nil
}

// scheduleWith reaches schedule.on.one.with, the nesting the event and manual
// triggers share.
func scheduleWith(schedule map[string]any) (map[string]any, bool) {
	on, ok := schedule["on"].(map[string]any)
	if !ok {
		return nil, false
	}
	one, ok := on["one"].(map[string]any)
	if !ok {
		return nil, false
	}
	with, ok := one["with"].(map[string]any)
	return with, ok
}

// Task is one entry of a `do` list, flattened out of its single-key wrapper.
type Task struct {
	// Name is the task's key, which is how other tasks reference it in
	// `then` targets and ${ actions.<name> } expressions.
	Name string
	// Path is the JSON path to the task, for error messages.
	Path string
	// Body is the task object.
	Body map[string]any
	// Depth is 0 for top-level tasks and 1 inside a for-loop body.
	Depth int
	// Parent is the enclosing loop task's name, empty at the top level.
	Parent string
}

// Call returns the task's call type, if it makes one.
func (t Task) Call() string {
	c, _ := t.Body["call"].(string)
	return c
}

// With returns the task's `with` block.
func (t Task) With() map[string]any {
	w, _ := t.Body["with"].(map[string]any)
	return w
}

// OperationID returns the jc_operation operationId, if this is one.
func (t Task) OperationID() string {
	if t.Call() != CallJCOperation {
		return ""
	}
	id, _ := t.With()["operationId"].(string)
	return id
}

// Describe renders the task as one line: a jc_operation resolved to the real
// endpoint it calls, and the other call types named for what they do. Shared
// so the CLI's `explain` and the TUI's review cannot describe a step
// differently.
func (t Task) Describe() string {
	if _, isLoop := t.Body["for"]; isLoop {
		loop, _ := t.Body["for"].(map[string]any)
		each, _ := loop["each"].(string)
		in, _ := loop["in"].(string)
		return fmt.Sprintf("%s: for each %s in %s", t.Name, each, in)
	}
	if _, isSwitch := t.Body["switch"]; isSwitch {
		return fmt.Sprintf("%s: branch", t.Name)
	}

	switch t.Call() {
	case CallJCOperation:
		id := t.OperationID()
		if op, ok := LookupOperation(id); ok {
			return fmt.Sprintf("%s: %s", t.Name, op.Describe())
		}
		return fmt.Sprintf("%s: %s (unknown operation)", t.Name, id)
	case CallEmailAddresses, CallEmailChannel:
		return fmt.Sprintf("%s: send email", t.Name)
	case CallConnector:
		with := t.With()
		method, _ := with["httpMethod"].(string)
		path, _ := with["endpointPath"].(string)
		return fmt.Sprintf("%s: external connector %s %s", t.Name, method, path)
	}
	return fmt.Sprintf("%s: (no action)", t.Name)
}

// Tasks flattens the `do` list into tasks in document order, descending one
// level into for-loop bodies. Nested loops are not supported by the engine, so
// depth never exceeds 1 in a valid document; a deeper nesting is still
// reported here so Validate can name it.
func (d DSL) Tasks() []Task {
	var out []Task
	walkDo(d.Do, "dsl.do", 0, "", &out)
	return out
}

func walkDo(do []map[string]any, path string, depth int, parent string, out *[]Task) {
	for i, entry := range do {
		for name, rawBody := range entry {
			body, _ := rawBody.(map[string]any)
			p := fmt.Sprintf("%s[%d].%s", path, i, name)
			t := Task{Name: name, Path: p, Body: body, Depth: depth, Parent: parent}
			*out = append(*out, t)
			if body == nil {
				continue
			}
			if _, isLoop := body["for"]; isLoop {
				*out = append(*out, loopBody(body, p, depth, name)...)
			}
		}
	}
}

func loopBody(body map[string]any, path string, depth int, name string) []Task {
	raw, ok := body["do"].([]any)
	if !ok {
		return nil
	}
	inner := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		if m, ok := e.(map[string]any); ok {
			inner = append(inner, m)
		}
	}
	var out []Task
	walkDo(inner, path+".do", depth+1, name, &out)
	return out
}

// Placeholders returns every unfilled REPLACE_WITH_* marker in the document,
// each with the JSON path where it sits, in document order.
type Placeholder struct {
	Marker string
	Path   string
}

// Placeholders walks the whole document, since markers appear in task
// parameters, schedule conditions, and email bodies alike.
func (d DSL) Placeholders() []Placeholder {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil
	}
	var out []Placeholder
	walkStrings(generic, "dsl", func(s, path string) {
		for _, m := range placeholderRE.FindAllString(s, -1) {
			out = append(out, Placeholder{Marker: m, Path: path})
		}
	})
	return out
}

// walkStrings visits every string in a decoded JSON document with its path.
func walkStrings(v any, path string, fn func(s, path string)) {
	switch t := v.(type) {
	case string:
		fn(t, path)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkStrings(t[k], path+"."+k, fn)
		}
	case []any:
		for i, e := range t {
			walkStrings(e, fmt.Sprintf("%s[%d]", path, i), fn)
		}
	}
}

// SideEffect is one step that reaches outside JumpCloud.
type SideEffect struct {
	// Task is the task's name.
	Task string
	// Call is the call type.
	Call string
	// What describes the effect in one phrase.
	What string
	// Targets are the concrete recipients or endpoints, so a reviewer sees
	// who gets mailed and what gets called rather than only that it happens.
	Targets []string
}

// SideEffects lists every step that sends email or calls an external
// connector, with its concrete targets.
func (d DSL) SideEffects() []SideEffect {
	var out []SideEffect
	for _, t := range d.Tasks() {
		call := t.Call()
		what, ok := SideEffectCalls[call]
		if !ok {
			continue
		}
		out = append(out, SideEffect{
			Task: t.Name, Call: call, What: what, Targets: sideEffectTargets(call, t.With()),
		})
	}
	return out
}

func sideEffectTargets(call string, with map[string]any) []string {
	if with == nil {
		return nil
	}
	switch call {
	case CallEmailAddresses, CallEmailChannel:
		recipients, _ := with["recipients"].(map[string]any)
		if recipients == nil {
			return nil
		}
		key := "to_addresses"
		if call == CallEmailChannel {
			key = "channel_object_ids"
		}
		list, _ := recipients[key].([]any)
		out := make([]string, 0, len(list))
		for _, v := range list {
			out = append(out, fmt.Sprint(v))
		}
		return out
	case CallConnector:
		method, _ := with["httpMethod"].(string)
		path, _ := with["endpointPath"].(string)
		id, _ := with["id"].(string)
		return []string{strings.TrimSpace(method + " " + path + " (connector " + id + ")")}
	}
	return nil
}

// Expression is one Expr source found in the document.
type Expression struct {
	// Source is the expression text, unwrapped from ${ }.
	Source string
	// Path is where it sits, for error messages.
	Path string
	// Kind constrains what the expression must evaluate to.
	Kind ExprKind
	// Interpolated marks an expression embedded in a larger string, which
	// may legitimately be any type.
	Interpolated bool
}

// ExprKind is the result type an expression position requires.
type ExprKind int

const (
	// ExprAny places no constraint on the result.
	ExprAny ExprKind = iota
	// ExprBool must evaluate to a boolean: `if`, `when`, `until`.
	ExprBool
	// ExprArray must evaluate to an iterable: `for.in`, `extract`.
	ExprArray
)

// unwrapExpr strips a lone ${ ... } wrapper. The DSL accepts expressions with
// or without it, so both forms reach the compiler identically.
func unwrapExpr(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "${") || !strings.HasSuffix(trimmed, "}") {
		return trimmed, false
	}
	inner := trimmed[2 : len(trimmed)-1]
	// A string with two separate spans is interpolation, not one expression.
	if strings.Contains(inner, "${") {
		return trimmed, false
	}
	return strings.TrimSpace(inner), true
}
