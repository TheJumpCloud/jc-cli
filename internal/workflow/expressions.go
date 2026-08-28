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

	// The external trigger's condition is evaluated against the raw request
	// data, not the workflow context, so `input.` there is silently wrong.
	// The guide calls this out twice; it is the area's sharpest footgun.
	if trigger.Source == TriggerExternal && trigger.Condition != "" {
		if strings.Contains(mustUnwrap(trigger.Condition), "input.") {
			add(Error, "dsl.schedule.on.one.with.condition",
				"an external trigger condition is evaluated against the posted data object, so input.<field> is never bound",
				"drop the input. prefix — write userId, not input.userId")
		}
	}
}

// isPaginationExpr reports whether a path is one of the three positions where
// the `page` context is bound: pagination.update.value, pagination.until, and
// the sibling extract.
func isPaginationExpr(path string) bool {
	return strings.Contains(path, ".pagination.") || strings.HasSuffix(path, ".with.extract")
}

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
			add(Warning, e.Path,
				fmt.Sprintf("this guard tests actions.%s.status, but a non-2xx from %q halts the run before this task is reached",
					ref, ref),
				"the failure branch this appears to handle is unreachable — branch with switch/when BEFORE the fallible call, "+
					"or accept that the run fails. JumpCloud's shipped templates use this idiom too; it does not work there either")
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
