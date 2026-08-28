package workflow

import (
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
	// Kind is "workflow" or "template".
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
	// Status and Role are set for workflows; a template has neither.
	Status string `json:"status,omitempty"`
	Role   string `json:"execution_role,omitempty"`
	// Skipped explains why nothing could be checked, when that happened —
	// a DSL that will not parse cannot be validated, and reporting that as
	// "clean" would be worse than saying nothing.
	Skipped string `json:"skipped,omitempty"`
	// CorrectedBy names jc's repaired copy, when this subject has findings
	// and a correction exists. Reporting a defect without naming the fix
	// leaves the operator exactly where they started.
	CorrectedBy string `json:"corrected_by,omitempty"`
	Result      Result `json:"result"`
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
}

// Summarize computes the totals and orders subjects worst-first, so the
// output's first line is the one worth acting on.
func Summarize(subjects []LintSubject) LintSummary {
	s := LintSummary{Subjects: subjects, Checked: len(subjects)}
	for _, sub := range subjects {
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
