package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

// The DSL guide recommends the keyword forms (and / or / not) over C-style
// && / || / !, naming them as the usual cause of "invalid runtime expression
// format". This package used to warn on that.
//
// It no longer does. All 12 shipped templates write their trigger conditions
// with &&, and a live experiment on 2026-08-27 settled it: two workflows
// identical but for the operator, in BOTH positions the operators appear —
// a task-level `if` and an external trigger condition — executed identically,
// all four runs completing with the guarded step running. The guide's advice
// is conservative, and warning on it flagged every shipped template for a
// non-issue, which is worse than silence. Whatever produces "invalid runtime
// expression format" is not this.

// actionsRefRE finds ${ actions.<name> } references, which must resolve to a
// task defined earlier in the document.
var actionsRefRE = regexp.MustCompile(`\bactions\.([A-Za-z_][A-Za-z0-9_]*)`)

// pageRefRE finds `page` references, which are only in scope inside pagination
// expressions.
var pageRefRE = regexp.MustCompile(`\bpage\.`)

// Expressions collects every Expr source in the document with its position and
// the result type that position requires.
func (d DSL) Expressions() []Expression {
	var out []Expression

	if trigger, err := d.Trigger(); err == nil && trigger.Condition != "" {
		out = append(out, Expression{
			Source: mustUnwrap(trigger.Condition),
			Path:   "dsl.schedule.on.one.with.condition",
			Kind:   ExprBool,
		})
	}

	for _, t := range d.Tasks() {
		if t.Body == nil {
			continue
		}
		if raw, ok := t.Body["if"].(string); ok {
			out = append(out, Expression{Source: mustUnwrap(raw), Path: t.Path + ".if", Kind: ExprBool})
		}
		if loop, ok := t.Body["for"].(map[string]any); ok {
			if raw, ok := loop["in"].(string); ok {
				out = append(out, Expression{Source: mustUnwrap(raw), Path: t.Path + ".for.in", Kind: ExprArray})
			}
		}
		if branches, ok := t.Body["switch"].([]any); ok {
			for i, rawBranch := range branches {
				branch, ok := rawBranch.(map[string]any)
				if !ok {
					continue
				}
				for name, rawCase := range branch {
					c, ok := rawCase.(map[string]any)
					if !ok {
						continue
					}
					if w, ok := c["when"].(string); ok {
						out = append(out, Expression{
							Source: mustUnwrap(w),
							Path:   fmt.Sprintf("%s.switch[%d].%s.when", t.Path, i, name),
							Kind:   ExprBool,
						})
					}
				}
			}
		}

		with := t.With()
		if with == nil {
			continue
		}
		if raw, ok := with["extract"].(string); ok {
			out = append(out, Expression{Source: mustUnwrap(raw), Path: t.Path + ".with.extract", Kind: ExprArray})
		}
		if pag, ok := with["pagination"].(map[string]any); ok {
			if raw, ok := pag["until"].(string); ok {
				out = append(out, Expression{
					Source: mustUnwrap(raw), Path: t.Path + ".with.pagination.until", Kind: ExprBool,
				})
			}
			if update, ok := pag["update"].(map[string]any); ok {
				if raw, ok := update["value"].(string); ok {
					out = append(out, Expression{
						Source: mustUnwrap(raw), Path: t.Path + ".with.pagination.update.value", Kind: ExprAny,
					})
				}
			}
		}
		// Parameter blocks carry interpolated expressions in arbitrary
		// nesting, so they are collected by walking rather than by name.
		for _, block := range []string{"pathParams", "queryParams", "bodyParams"} {
			if params, ok := with[block]; ok {
				out = append(out, interpolatedIn(params, t.Path+".with."+block)...)
			}
		}
	}
	return out
}

// interpolatedIn finds ${ ... } spans inside a parameter block. These may be
// whole-value expressions or embedded in a larger string; both compile the
// same way, but an embedded one places no constraint on the result type.
func interpolatedIn(v any, path string) []Expression {
	var out []Expression
	walkStrings(v, path, func(s, p string) {
		if src, whole := unwrapExpr(s); whole {
			out = append(out, Expression{Source: src, Path: p, Kind: ExprAny})
			return
		}
		for _, m := range exprInterpolationRE.FindAllStringSubmatch(s, -1) {
			out = append(out, Expression{
				Source: strings.TrimSpace(m[1]), Path: p, Kind: ExprAny, Interpolated: true,
			})
		}
	})
	return out
}

func mustUnwrap(s string) string {
	src, _ := unwrapExpr(s)
	return src
}

// validateExpressions compiles every expression the way the engine does, and
// checks the scoping rules the guide documents.
func validateExpressions(d DSL, trigger TriggerStyle, tasks []Task, add func(Severity, string, string, string)) {
	// Task positions, so ${ actions.<name> } can be checked as a backward
	// reference: a task cannot read output a later task has not produced.
	position := map[string]int{}
	for i, t := range tasks {
		if _, ok := position[t.Name]; !ok {
			position[t.Name] = i
		}
	}
	// Longest match wins: a path inside a loop body is prefixed by both the
	// loop task and the inner task, and it is the inner one that is "here".
	taskAt := func(path string) (int, bool) {
		best, bestLen, found := 0, -1, false
		for i, t := range tasks {
			if (strings.HasPrefix(path, t.Path+".") || path == t.Path) && len(t.Path) > bestLen {
				best, bestLen, found = i, len(t.Path), true
			}
		}
		return best, found
	}

	for _, e := range d.Expressions() {
		if e.Source == "" {
			add(Error, e.Path, "empty expression", "")
			continue
		}

		// An unfilled placeholder is reported once, by the placeholder pass;
		// it would also fail to compile, which is noise.
		if placeholderRE.MatchString(e.Source) {
			continue
		}

		if err := compileExpr(e.Source, e.Kind); err != nil {
			hint := ""
			if e.Kind == ExprBool {
				hint = "this position must evaluate to a boolean"
			}
			add(Error, e.Path, fmt.Sprintf("expression does not compile: %v", err), hint)
			continue
		}

		// page is only bound inside pagination expressions. extract counts:
		// the guide lists it with update.value and until, even though it sits
		// beside the pagination block rather than inside it.
		if pageRefRE.MatchString(e.Source) && !isPaginationExpr(e.Path) {
			add(Error, e.Path, "page is only available inside pagination expressions",
				"reference actions.<task> or input instead")
		}

		// actions.<name> must name a task that has already run.
		for _, m := range actionsRefRE.FindAllStringSubmatch(e.Source, -1) {
			ref := m[1]
			refPos, known := position[ref]
			if !known {
				add(Error, e.Path, fmt.Sprintf("actions.%s does not name a task in this workflow", ref),
					"reference a task by its key in the do list")
				continue
			}
			if here, ok := taskAt(e.Path); ok && refPos >= here {
				add(Error, e.Path,
					fmt.Sprintf("actions.%s refers to a task that has not run yet", ref),
					"a task can only read output from a task defined earlier")
			}
		}
	}

	// A trigger condition is evaluated against the event or request payload
	// itself, with the payload's fields bound at the TOP LEVEL — so `input.`
	// there is never bound. The guide calls this out for external triggers;
	// a live create proved the same holds for jc_events, which rejected
	//   input.resource.name == "..."
	// with `failed to compile expression: unknown name input`. JumpCloud's own
	// templates confirm the shape: they write association.op, changes, userId
	// and workflow.id with no prefix at all.
	if trigger.Condition != "" && strings.Contains(mustUnwrap(trigger.Condition), "input.") {
		what := "the posted data object"
		example := "write userId, not input.userId"
		if trigger.Source == TriggerEvents {
			what = "the event payload"
			example = "write resource.name, not input.resource.name"
		}
		add(Error, "dsl.schedule.on.one.with.condition",
			"a trigger condition is evaluated against "+what+
				", whose fields are bound at the top level, so input.<field> is never bound",
			"drop the input. prefix — "+example)
	}
}

// isPaginationExpr reports whether a path is one of the three positions where
// the `page` context is bound: pagination.update.value, pagination.until, and
// the sibling extract.
func isPaginationExpr(path string) bool {
	return strings.Contains(path, ".pagination.") || strings.HasSuffix(path, ".with.extract")
}

// eq200RE finds the specific form `actions.X.status == 200`, which is worse
// than a merely-dead test: it can suppress a task that succeeded with 201.
var eq200RE = regexp.MustCompile(`\bactions\.[A-Za-z_][A-Za-z0-9_]*\.status\s*==\s*200\b`)

// statusGuardRE finds a reference to a prior step's HTTP status.
var statusGuardRE = regexp.MustCompile(`\bactions\.([A-Za-z_][A-Za-z0-9_]*)\.status\b`)

// checkDeadStatusGuards warns about conditions that test a prior step's HTTP
// status in a position that can never see a failure.
//
// A non-2xx from any jc_operation halts the whole run: verified live, where a
// deliberate 404 on step one left both following tasks reporting
// "Not executed — workflow failed at a prior task". So an `if` that asks
// whether an earlier call succeeded is only ever evaluated when it did — the
// failure branch it appears to handle is unreachable.
//
// This flags JumpCloud's own templates, which is correct: every one of them
// guards downstream steps this way, and the idiom cannot work. The message
// says so, because a warning that contradicts the only worked examples needs
// to explain itself or it reads as a bug in the linter.
//
// A `when` inside a switch placed BEFORE the fallible call is the working
// pattern and is deliberately not flagged: at that point nothing has failed.
func checkDeadStatusGuards(d DSL, add func(Severity, string, string, string)) {
	tasks := d.Tasks()

	// Which tasks make a fallible call, and in what order.
	position := map[string]int{}
	fallible := map[string]bool{}
	for i, t := range tasks {
		if _, seen := position[t.Name]; !seen {
			position[t.Name] = i
		}
		if t.Call() == CallJCOperation || t.Call() == CallConnector {
			fallible[t.Name] = true
		}
	}

	for _, e := range d.Expressions() {
		// Only task-level `if` guards are dead. A switch `when` may sit
		// before the call it guards, which is the pattern that works.
		if !strings.HasSuffix(e.Path, ".if") {
			continue
		}
		here, ok := taskIndexForPath(tasks, e.Path)
		if !ok {
			continue
		}
		for _, m := range statusGuardRE.FindAllStringSubmatch(e.Source, -1) {
			ref := m[1]
			refPos, known := position[ref]
			if !known || !fallible[ref] || refPos >= here {
				continue
			}
			// Equality against 200 is the dangerous form. Because a non-2xx
			// already halted the run, the only thing this test can still do
			// is come out FALSE on a successful 201 or 204 and silently skip
			// the task. Proven live: a task guarded on `status == 200` after
			// a create returning 201 reported "Skipping — if condition did
			// not match", while `>= 200 && < 300` in the same run executed.
			if eq200RE.MatchString(e.Source) {
				add(Warning, e.Path,
					fmt.Sprintf("this guard tests actions.%s.status == 200, which cannot detect failure and CAN silently skip this task: "+
						"a non-2xx from %q already halted the run, so the only remaining effect is to come out false on a successful 201 or 204",
						ref, ref),
					fmt.Sprintf("delete the `actions.%s.status == 200 &&` conjunct and keep the rest of the guard, which is doing the real work. "+
						"If you genuinely need a status test, use >= 200 && < 300 — verified live, where == 200 skipped a task after a 201 and the range did not",
						ref))
				continue
			}
			add(Warning, e.Path,
				fmt.Sprintf("this guard tests actions.%s.status, but a non-2xx from %q halts the run before this task is reached",
					ref, ref),
				fmt.Sprintf("the failure branch this appears to handle is unreachable — delete the actions.%s.status conjunct and keep the rest of the guard, "+
					"or branch with switch/when BEFORE the fallible call. JumpCloud's shipped templates use this idiom too; it does not work there either", ref))
		}
	}
}

// taskIndexForPath resolves an expression path to the innermost task holding
// it. Longest match wins, as a path inside a loop body is prefixed by both.
func taskIndexForPath(tasks []Task, path string) (int, bool) {
	best, bestLen, found := 0, -1, false
	for i, t := range tasks {
		if (strings.HasPrefix(path, t.Path+".") || path == t.Path) && len(t.Path) > bestLen {
			best, bestLen, found = i, len(t.Path), true
		}
	}
	return best, found
}

// inputRefRE finds the top-level field of an ${ input.<field> } reference.
var inputRefRE = regexp.MustCompile(`\binput\.([A-Za-z_][A-Za-z0-9_]*)`)

// checkInputReferences warns about ${ input.<field> } references the trigger
// cannot satisfy.
//
// Two sources, depending on the trigger:
//
//   - external: the workflow declares its own JSON input schema, and the API
//     enforces it when a run is started. A field the schema does not declare
//     can never arrive.
//   - jc_events: the payload shape comes from the field map, built from a
//     captured trigger payload. That map is a lower bound, so this stays a
//     warning.
//
// Either way a bad reference evaluates false forever, and the workflow simply
// never matches — the same invisible failure as a mistyped event type, one
// layer down.
func checkInputReferences(d DSL, trigger TriggerStyle, add func(Severity, string, string, string)) {
	var (
		allowed map[string]bool
		source  string
	)

	switch {
	case trigger.Source == TriggerExternal:
		fields, declared := d.inputSchemaFields()
		if !declared {
			// No schema declared: anything may be posted, nothing to check.
			return
		}
		allowed = map[string]bool{}
		for _, f := range fields {
			allowed[f] = true
		}
		source = "the declared input schema"

	case trigger.Source == TriggerEvents:
		allowed = map[string]bool{}
		for _, f := range EventFields(trigger.EventType) {
			allowed[f] = true
		}
		source = "a " + trigger.EventType + " event payload"

	default:
		// A scheduled trigger has no input to reference.
		return
	}

	reported := map[string]bool{}
	for _, e := range d.Expressions() {
		// The trigger condition of an external workflow is evaluated against
		// the raw data object, not the input context; that case has its own
		// finding.
		if strings.HasSuffix(e.Path, "with.condition") {
			continue
		}
		for _, m := range inputRefRE.FindAllStringSubmatch(e.Source, -1) {
			field := m[1]
			if allowed[field] || reported[e.Path+field] {
				continue
			}
			reported[e.Path+field] = true

			hint := "a reference the trigger cannot satisfy evaluates false forever, so the workflow silently never matches"
			if s := suggestField(field, allowed); s != "" {
				hint = "closest available: " + s + " — " + hint
			}
			add(Warning, e.Path,
				"input."+field+" is not carried by "+source, hint)
		}
	}
}

// suggestField returns the nearest available field name, for the typo case.
func suggestField(field string, allowed map[string]bool) string {
	best, bestD := "", 1<<30
	for f := range allowed {
		d := levenshtein(strings.ToLower(field), strings.ToLower(f))
		if d < bestD {
			best, bestD = f, d
		}
	}
	if bestD > len(field)/2+1 {
		return ""
	}
	return best
}
