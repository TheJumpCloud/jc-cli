package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/expr-lang/expr"
)

// Local dry run: what a workflow WOULD call, resolved against real input.
//
// What this is: a call planner. It walks the task list, evaluates conditions
// with the same Expr engine the DSL uses, resolves ${ } interpolation in
// parameters, and reports each step with its resolved parameters — marking
// every write, email and connector call as stubbed.
//
// What this is NOT: an oracle for engine semantics. It cannot tell you what
// JumpCloud's runtime does, because it implements this package's reading of
// the DSL rather than the engine's. Anything it says about halt-on-error,
// branch selection or operator handling is a restatement of assumptions, not
// evidence. Those questions were settled by running real workflows, and that
// remains the only way to settle new ones.
//
// It is worth having anyway, because the common question is not "what are the
// engine's semantics" but "given this input, which objects would this touch
// and with what parameters" — and answering that needs no created workflow, no
// active status, and no write-capable role.

// SimStatus is what happened to one step in a dry run.
type SimStatus string

const (
	// SimWouldCall is a read that the caller may execute for real.
	SimWouldCall SimStatus = "would-call"
	// SimStubbed is a write, email or connector call: never executed.
	SimStubbed SimStatus = "stubbed"
	// SimSkipped is a step a guard or branch excluded.
	SimSkipped SimStatus = "skipped"
	// SimSwitched is a switch node, which evaluates its cases and routes. It
	// is a form of running, not of being skipped.
	SimSwitched SimStatus = "switched"
	// SimUnresolved is a step whose parameters could not be resolved,
	// usually because they reference a prior step's response body that a dry
	// run does not have.
	SimUnresolved SimStatus = "unresolved"
)

// SimStep is one planned step.
type SimStep struct {
	Task   string    `json:"task"`
	Status SimStatus `json:"status"`
	// Call is the DSL call type.
	Call string `json:"call,omitempty"`
	// Operation renders the JumpCloud operation as METHOD /path.
	Operation string `json:"operation,omitempty"`
	// Method is the HTTP method, which is what decides read versus write.
	Method string `json:"method,omitempty"`
	// Params are the resolved path, query and body parameters.
	Params map[string]any `json:"params,omitempty"`
	// Why explains a skip, a stub, or a failure to resolve.
	Why string `json:"why,omitempty"`
}

// SimResult is a dry run.
type SimResult struct {
	Steps []SimStep `json:"steps"`
	// Caveat is carried in the payload deliberately: a caller reading only
	// the result must still see what this cannot tell them.
	Caveat string `json:"caveat"`
}

// writeMethods are the HTTP methods that change something.
var writeMethods = map[string]bool{"POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// changesState reports whether an operation mutates anything.
//
// Method alone is not enough: JumpCloud's search endpoints are POSTs that read
// (POST /api/search/systemusers carries its filter in the body). Stubbing
// those would break the search-then-act shape, which is exactly the shape a
// dry run is most useful for — the plan would show nothing found and every
// downstream step skipped.
func changesState(op Operation) bool {
	if !writeMethods[op.Method] {
		return false
	}
	if strings.Contains(op.Path, "/search/") {
		return false
	}
	return true
}

// Simulate plans what a DSL would do for a given trigger input.
//
// It never performs a request. Reads are reported as would-call with their
// resolved parameters so the caller can decide whether to run them; writes,
// emails and connector calls are reported as stubbed.
func Simulate(d DSL, input map[string]any) SimResult {
	res := SimResult{Caveat: "A local plan, not a prediction of engine behaviour: " +
		"branch selection, halt-on-error and expression semantics are this tool's reading of the DSL, " +
		"not observations of JumpCloud's runtime. Verify behaviour with a real run."}

	env := map[string]any{"input": input, "actions": map[string]any{}}

	// A switch names the next node explicitly. Branch targets it did not
	// choose are skipped, and so are tasks NO branch targets that sit after a
	// task which jumps away — execution follows the jump graph, not the order
	// of the do list.
	//
	// This comment previously said such tasks "still run in array order,
	// verified live". That was wrong and has been reverted; see
	// UnreachableTasks for the ground truth that settled it. The planner
	// shares that analysis with the validator now, so the two cannot drift
	// again — while they disagreed, comparing a plan against a real run
	// reported a divergence that was only the planner's blind spot.
	branchTargets := map[string]bool{}
	for _, t := range d.Tasks() {
		if branches, ok := t.Body["switch"].([]any); ok {
			for _, rawBranch := range branches {
				branch, _ := rawBranch.(map[string]any)
				for _, rawCase := range branch {
					c, _ := rawCase.(map[string]any)
					if then, ok := c["then"].(string); ok && !ControlTargets[then] {
						branchTargets[then] = true
					}
				}
			}
		}
	}

	unreachable := UnreachableTasks(d.Tasks())

	chosen := map[string]bool{}
	// undecided holds branch targets belonging to a switch whose `when` could
	// not be evaluated in a dry run. They are NOT skipped — nobody knows
	// whether they run — and saying "skipped" produces a false
	// ran-but-planned-skip, which is the one verdict this tool tells you to
	// act on.
	undecided := map[string]bool{}
	for _, t := range d.Tasks() {
		step := SimStep{Task: t.Name, Call: t.Call()}

		// A task the jump graph never reaches does not run, whatever its
		// position in the do list.
		if unreachable[t.Name] {
			step.Status = SimSkipped
			step.Why = "unreachable: no branch targets it and the preceding task jumps elsewhere"
			res.Steps = append(res.Steps, step)
			continue
		}

		// A branch target runs only if some switch selected it — unless the
		// switch could not be evaluated at all, in which case its targets are
		// unresolved rather than skipped.
		if branchTargets[t.Name] && !chosen[t.Name] && undecided[t.Name] {
			step.Status = SimUnresolved
			step.Why = "the switch routing here reads data a dry run does not have, " +
				"so whether this branch is taken cannot be predicted"
			res.Steps = append(res.Steps, step)
			continue
		}
		if branchTargets[t.Name] && !chosen[t.Name] {
			step.Status = SimSkipped
			step.Why = "a branch target no evaluated switch selected"
			res.Steps = append(res.Steps, step)
			continue
		}

		if branches, ok := t.Body["switch"].([]any); ok {
			// A switch that routes has RUN. Choosing a branch is what a switch
			// does when it executes; what it routes TO is the branch decision,
			// and is not a statement about the switch itself.
			//
			// Marking it skipped put every DSL containing a switch — which is
			// most non-trivial ones — into ran-but-planned-skip, the very
			// verdict this tool names as the one worth acting on. A systematic
			// false positive there teaches people to ignore it, which defeats
			// the tool.
			step.Status = SimSwitched
			outcome := pickBranch(branches, env)
			switch {
			case len(outcome.unevaluable) > 0:
				// A `when` that reads a prior step's response body cannot be
				// evaluated without one. The if-guard path has always
				// reported that as unresolved; this one used to discard the
				// error and fall through to the default, so the plan named a
				// branch it had not chosen and the comparison called the
				// engine wrong.
				for _, target := range outcome.targets {
					undecided[target] = true
				}
				step.Why = "cannot route: " + strings.Join(outcome.unevaluable, "; ")
			case outcome.target != "":
				chosen[outcome.target] = true
				step.Why = "chose " + outcome.target + " (" + outcome.why + ")"
			default:
				step.Why = "no branch matched, so nothing downstream was selected"
			}
			res.Steps = append(res.Steps, step)
			continue
		}

		if cond, ok := t.Body["if"].(string); ok {
			pass, err := evalBool(cond, env)
			if err != nil {
				step.Status = SimUnresolved
				step.Why = "guard did not evaluate: " + err.Error()
				res.Steps = append(res.Steps, step)
				continue
			}
			if !pass {
				step.Status = SimSkipped
				step.Why = "guard evaluated false"
				res.Steps = append(res.Steps, step)
				continue
			}
		}

		switch t.Call() {
		case CallJCOperation:
			id := t.OperationID()
			op, known := LookupOperation(id)
			if !known {
				step.Status = SimUnresolved
				step.Why = "unknown operationId " + id
				break
			}
			step.Operation, step.Method = op.Describe(), op.Method
			step.Params = resolveParams(t.With(), env)

			// A plan whose parameters the API will reject is not a plan.
			// Simulate used to print "would-call" with the wrong parameter
			// name echoed back, directly beside the correct path — the
			// contradiction was visible in one row and unremarked, and the
			// create then failed with HTTP 400.
			if why := pathParamProblem(op, t.With()); why != "" {
				step.Status = SimUnresolved
				step.Why = why
				break
			}

			if changesState(op) {
				step.Status = SimStubbed
				step.Why = op.Method + " changes state and is never executed by a dry run"
			} else {
				step.Status = SimWouldCall
				if writeMethods[op.Method] {
					step.Why = "a " + op.Method + " that reads (search endpoint)"
				}
			}

		case CallEmailAddresses, CallEmailChannel:
			step.Status = SimStubbed
			step.Why = "sends real email"
			step.Params = resolveParams(t.With(), env)

		case CallConnector:
			step.Status = SimStubbed
			step.Why = "calls an external connector endpoint"
			step.Params = resolveParams(t.With(), env)

		default:
			step.Status = SimSkipped
			step.Why = "no call"
		}

		res.Steps = append(res.Steps, step)
	}
	return res
}

// branchOutcome is what the planner could work out about a switch.
type branchOutcome struct {
	// target is the branch selected, when one could be.
	target string
	why    string
	// unevaluable holds the `when` expressions that could not be evaluated at
	// all. Non-empty means the routing is unknown, which is different from no
	// branch matching.
	unevaluable []string
	// targets is every branch target of this switch, needed when the routing
	// is unknown and all of them become undecided.
	targets []string
}

// pickBranch works out which branch a switch takes, and reports honestly when
// it cannot.
//
// It used to swallow the evaluation error and fall through to the default, so
// a switch reading a prior step's response body — which is what the corrected
// templates all do — silently produced a plan that named the default branch.
// The plan then disagreed with every run that branched any other way, and
// reported the ENGINE as wrong.
func pickBranch(branches []any, env map[string]any) branchOutcome {
	var out branchOutcome
	var fallback string
	for _, rawBranch := range branches {
		branch, ok := rawBranch.(map[string]any)
		if !ok {
			continue
		}
		for name, rawCase := range branch {
			c, ok := rawCase.(map[string]any)
			if !ok {
				continue
			}
			then, _ := c["then"].(string)
			if then != "" && !ControlTargets[then] {
				out.targets = append(out.targets, then)
			}
			when, hasWhen := c["when"].(string)
			if !hasWhen {
				fallback = then
				continue
			}
			pass, err := evalBool(when, env)
			if err != nil {
				out.unevaluable = append(out.unevaluable,
					"case "+name+" ("+when+"): "+err.Error())
				continue
			}
			if pass && out.target == "" {
				out.target, out.why = then, "case "+name+" matched"
			}
		}
	}
	if out.target == "" && fallback != "" && len(out.unevaluable) == 0 {
		out.target, out.why = fallback, "default"
	}
	return out
}

func evalBool(src string, env map[string]any) (bool, error) {
	unwrapped, _ := unwrapExpr(src)
	out, err := expr.Eval(unwrapped, env)
	if err != nil {
		return false, err
	}
	b, ok := out.(bool)
	if !ok {
		return false, fmt.Errorf("expression is %T, not a boolean", out)
	}
	return b, nil
}

// resolveParams substitutes ${ } references in a call's parameter blocks.
// A reference a dry run cannot know — typically a prior step's response body —
// is left as its literal text rather than guessed at.
func resolveParams(with map[string]any, env map[string]any) map[string]any {
	if with == nil {
		return nil
	}
	out := map[string]any{}
	for _, block := range []string{"pathParams", "queryParams", "bodyParams", "recipients", "message"} {
		v, ok := with[block]
		if !ok {
			continue
		}
		out[block] = resolveValue(v, env)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func resolveValue(v any, env map[string]any) any {
	switch t := v.(type) {
	case string:
		if src, whole := unwrapExpr(t); whole {
			if got, err := expr.Eval(src, env); err == nil {
				return got
			}
			return t
		}
		return exprInterpolationRE.ReplaceAllStringFunc(t, func(m string) string {
			inner := strings.TrimSpace(m[2 : len(m)-1])
			if got, err := expr.Eval(inner, env); err == nil {
				return fmt.Sprint(got)
			}
			return m
		})
	case map[string]any:
		out := map[string]any{}
		for k, vv := range t {
			out[k] = resolveValue(vv, env)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, vv := range t {
			out = append(out, resolveValue(vv, env))
		}
		return out
	}
	return v
}

// SimulateRaw is Simulate over a raw DSL document.
func SimulateRaw(raw json.RawMessage, input map[string]any) (SimResult, error) {
	d, err := ParseDSL(raw)
	if err != nil {
		return SimResult{}, err
	}
	return Simulate(d, input), nil
}

// pathParamProblem describes a pathParams object the operation will reject,
// or "" when it is fine. Shares ComparePathParams with validate so the two
// cannot disagree about the same document.
func pathParamProblem(op Operation, with map[string]any) string {
	missing, unexpected, ok := ComparePathParams(op, with)
	if !ok {
		return "pathParams is not an object"
	}
	if len(missing) == 0 {
		return ""
	}
	if len(unexpected) == 1 && len(missing) == 1 {
		return fmt.Sprintf("path parameter %q is not set; %q was supplied instead (%s)",
			missing[0], unexpected[0], op.Path)
	}
	return fmt.Sprintf("path parameters not set: %s (%s)",
		strings.Join(missing, ", "), op.Path)
}
