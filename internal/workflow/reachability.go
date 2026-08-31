package workflow

// Shared reachability analysis.
//
// Execution follows the jump graph, not the order of the do list: a task no
// branch targets, sitting after a task that jumps away, is never reached. This
// was established by ground truth outside the trace — a switch jumped past a
// task that would have created a user group, and afterwards the group did not
// exist. See RunState for the full evidence.
//
// The validator and the planner must agree on this. They did not: the validator
// warned that such a task never runs while the planner predicted it WOULD run,
// so a comparison against a real run reported a divergence that was the
// planner's own blind spot rather than anything about the workflow.

// JumpTargets returns every task named by a `then`, at the top level, whether
// from a task or from a switch case.
func JumpTargets(top []Task) map[string]bool {
	targeted := map[string]bool{}
	note := func(v any) {
		if then, ok := v.(string); ok && then != "" && !ControlTargets[then] {
			targeted[then] = true
		}
	}
	for _, t := range top {
		if t.Body == nil {
			continue
		}
		note(t.Body["then"])
		branches, ok := t.Body["switch"].([]any)
		if !ok {
			continue
		}
		for _, rawBranch := range branches {
			branch, ok := rawBranch.(map[string]any)
			if !ok {
				continue
			}
			for _, rawCase := range branch {
				c, ok := rawCase.(map[string]any)
				if !ok {
					continue
				}
				note(c["then"])
			}
		}
	}
	return targeted
}

// JumpsAway reports whether a task always transfers control elsewhere, so the
// task after it is never reached by fall-through.
func JumpsAway(t Task) bool {
	if t.Body == nil {
		return false
	}
	if _, ok := t.Body["switch"]; ok {
		return true
	}
	if then, ok := t.Body["then"].(string); ok {
		return then != "" && then != "continue"
	}
	return false
}

// UnreachableTasks returns the names of top-level tasks that no branch targets
// and that follow a task which jumps away — tasks the engine silently never
// runs.
func UnreachableTasks(tasks []Task) map[string]bool {
	var top []Task
	for _, t := range tasks {
		if t.Depth == 0 {
			top = append(top, t)
		}
	}
	out := map[string]bool{}
	if len(top) < 2 {
		return out
	}
	targeted := JumpTargets(top)
	for i := 1; i < len(top); i++ {
		if !targeted[top[i].Name] && JumpsAway(top[i-1]) {
			out[top[i].Name] = true
		}
	}
	return out
}
