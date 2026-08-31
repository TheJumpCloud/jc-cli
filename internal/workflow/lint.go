package workflow

import (
	"encoding/json"
	"sort"
	"strings"
)

// Sweep linting — validating everything at once rather than one file at a time.
//
// Validation on a single file answers "is this one right?". The more useful
// question on a live tenant is "which of the things already running are
// wrong?", and nobody asks it, because it means exporting every workflow and
// validating each by hand.
//
// The template catalog matters just as much. Templates are the only worked
// examples of a DSL with no published schema, so an idiom that appears in them
// gets copied into real workflows. Linting the catalog says, concretely, which
// of the examples are safe to copy.

// WithoutPlaceholderFindings drops the findings that exist only because
// REPLACE_WITH_* markers are still in place.
//
// A template is SUPPOSED to have placeholders, so reporting them as errors
// makes every template look broken and buries the findings that matter — the
// template's own defects, which anyone copying it inherits. Linting the
// catalog raw reports 10 of 12 as failing when the real number of defective
// ones is far smaller.
//
// This is only correct for templates. On a real workflow an unfilled marker is
// a genuine error: it means someone shipped a template without finishing it.
func WithoutPlaceholderFindings(r Result) Result {
	out := r
	out.Findings = nil
	for _, f := range r.Findings {
		if strings.Contains(f.Message, markerPrefix) || strings.Contains(f.Hint, markerPrefix) {
			continue
		}
		out.Findings = append(out.Findings, f)
	}
	return out
}

// LintSubject is one thing that was linted.
type LintSubject struct {
	// Kind is "workflow" or "template" — WHAT the subject is.
	Kind string `json:"kind"`
	// Source is "jumpcloud" or "jc" — WHO wrote it. It uses the same
	// vocabulary as workflows_templates_list deliberately: encoding one
	// distinction two ways (there: source, here: a "(jc corrected)" suffix on
	// kind) made a caller consuming both tools learn two vocabularies for the
	// same fact. Presentation may still render it prettily; the DATA says it
	// once.
	Source string `json:"source,omitempty"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	// Status and Role are set for workflows; a template has neither.
	Status string `json:"status,omitempty"`
	// ExecutionRoleID and Role are BOTH emitted, using the same field names
	// workflows_get and workflows_list use for the same facts.
	//
	// lint previously reported only `execution_role` as a NAME while get and
	// list reported only `execution_role_id` as an ID — a different field name
	// AND a different representation for one fact, so correlating a linted
	// workflow to its role needed a separate lookup. Same class as the
	// source/kind divergence fixed for templates; it was invisible until the
	// tenant had a workflow to compare.
	//
	// The id is the durable half: ids are stable, names are not, so it is the
	// join key between the two tools.
	//
	// The reverse — a resolved role NAME on workflows_get/list — is
	// deliberately NOT added. Those return the API object, and composing a
	// field into a passthrough is the same mistake as normalising `admin`
	// would have been: it trades a visible gap for an invisible jc-only
	// value. A lint subject is jc's own object and may carry both.
	ExecutionRoleID string `json:"execution_role_id,omitempty"`
	Role            string `json:"execution_role,omitempty"`
	// Skipped explains why nothing could be checked, when that happened —
	// a DSL that will not parse cannot be validated, and reporting that as
	// "clean" would be worse than saying nothing.
	Skipped string `json:"skipped,omitempty"`
	// CorrectedBy names jc's repaired copy, when this subject has findings
	// and a correction exists. Reporting a defect without naming the fix
	// leaves the operator exactly where they started.
	CorrectedBy string `json:"corrected_by,omitempty"`
	// TriggerType is lifted to the top level to sit where workflows_get and
	// workflows_list put it. It was reachable only as result.trigger_type,
	// so the same name lived at two depths and any top-level field-set diff
	// flagged it.
	TriggerType string `json:"trigger_type,omitempty"`
	// Corrects is set on jc's own corrected copies, naming what they replace.
	Corrects string `json:"corrects,omitempty"`
	Result   Result `json:"result"`
}

// Errors and Warnings count this subject's findings by severity.
func (s LintSubject) Errors() int   { return s.count(Error) }
func (s LintSubject) Warnings() int { return s.count(Warning) }

func (s LintSubject) count(sev Severity) int {
	n := 0
	for _, f := range s.Result.Findings {
		if f.Severity == sev {
			n++
		}
	}
	return n
}

// LintSummary is the whole sweep.
type LintSummary struct {
	Subjects []LintSubject `json:"subjects"`
	// Totals over every subject.
	Checked  int `json:"checked"`
	Clean    int `json:"clean"`
	Errors   int `json:"with_errors"`
	Warnings int `json:"with_warnings"`
	Skipped  int `json:"skipped"`
	// CheckedBySource breaks the total down, because a bare "16 checked" over
	// a 12-template catalog reads as though the catalog grew. Four of those
	// are jc's corrected duplicates of four of the twelve.
	CheckedBySource map[string]int `json:"checked_by_source,omitempty"`
}

// Summarize computes the totals and orders subjects worst-first, so the
// output's first line is the one worth acting on.
func Summarize(subjects []LintSubject) LintSummary {
	s := LintSummary{Subjects: subjects, Checked: len(subjects)}
	for _, sub := range subjects {
		if sub.Source != "" {
			if s.CheckedBySource == nil {
				s.CheckedBySource = map[string]int{}
			}
			s.CheckedBySource[sub.Source]++
		}
		switch {
		case sub.Skipped != "":
			s.Skipped++
		case sub.Errors() > 0:
			s.Errors++
		case sub.Warnings() > 0:
			s.Warnings++
		default:
			s.Clean++
		}
	}
	sort.SliceStable(s.Subjects, func(i, j int) bool {
		a, b := s.Subjects[i], s.Subjects[j]
		if ra, rb := lintRank(a), lintRank(b); ra != rb {
			return ra < rb
		}
		if a.Errors() != b.Errors() {
			return a.Errors() > b.Errors()
		}
		if a.Warnings() != b.Warnings() {
			return a.Warnings() > b.Warnings()
		}
		return a.Name < b.Name
	})
	return s
}

// lintRank orders the categories. Errors first, then warnings, then the
// subjects that could not be checked at all — those sit above the clean ones
// because an unchecked workflow is an open question, not a pass.
func lintRank(s LintSubject) int {
	switch {
	case s.Errors() > 0:
		return 0
	case s.Warnings() > 0:
		return 1
	case s.Skipped != "":
		return 2
	default:
		return 3
	}
}

// The lint subjects below are built HERE rather than in each surface.
//
// They were duplicated once, and the copies drifted immediately: the CLI set
// CorrectedBy on a defective template while the MCP tool did not, so an agent
// running the sweep saw four defective templates and no pointer to the fix that
// ships in the same binary. Sharing the construction is the only thing that
// keeps the two honest.

// LintTemplate validates one template from the served catalog.
//
// Placeholders are NOT counted against it: a template is supposed to have them,
// and counting them reported 10 of 12 as failing while burying the real
// defects. Where jc ships a corrected copy of a template that has findings, the
// subject names it — reporting a defect without naming the fix leaves the
// reader where they started, and this sweep is where they were choosing what to
// copy.
func LintTemplate(id, name string, dsl json.RawMessage) LintSubject {
	sub := LintSubject{Kind: "template", Source: SourceJumpCloud, ID: id, Name: name}
	d, err := ParseDSL(dsl)
	if err != nil {
		sub.Skipped = "dsl could not be parsed: " + err.Error()
		return sub
	}
	sub.Result = WithoutPlaceholderFindings(Validate(d))
	if ct, ok := CorrectionFor(name); ok && len(sub.Result.Findings) > 0 {
		sub.CorrectedBy = ct.ID
	}
	return sub
}

// LintCorrected validates jc's own corrected copies.
//
// They are linted in the SAME sweep that reports the defects they correct, so
// the output proves the corrections rather than merely asserting them — and so
// a corrected copy that ever drifted would be caught by the tool it exists to
// answer.
func LintCorrected() []LintSubject {
	all := CorrectedTemplates()
	subjects := make([]LintSubject, 0, len(all))
	for _, ct := range all {
		sub := LintSubject{Kind: "template", Source: SourceJC, ID: ct.ID, Name: ct.Name, Corrects: ct.Corrects}
		d, err := ParseDSL(ct.DSL)
		if err != nil {
			sub.Skipped = "dsl could not be parsed: " + err.Error()
			subjects = append(subjects, sub)
			continue
		}
		sub.Result = WithoutPlaceholderFindings(Validate(d))
		subjects = append(subjects, sub)
	}
	return subjects
}

// LintWorkflow validates one workflow. Scope checking needs a role lookup, so
// it stays with the caller; ApplyRole folds the result back in.
func LintWorkflow(w Workflow) (LintSubject, DSL, bool) {
	sub := LintSubject{Kind: "workflow", ID: w.ID, Name: w.Name, Status: w.Status,
		ExecutionRoleID: w.ExecutionRoleID}
	d, err := ParseDSL(w.DSL)
	if err != nil {
		sub.Skipped = "dsl could not be parsed: " + err.Error()
		return sub, DSL{}, false
	}
	sub.Result = Validate(d)
	sub.TriggerType = sub.Result.TriggerType
	return sub, d, true
}
