package workflow

import "testing"

func subject(name string, sevs ...Severity) LintSubject {
	s := LintSubject{Kind: "workflow", Name: name}
	for _, sev := range sevs {
		s.Result.Findings = append(s.Result.Findings, Finding{Severity: sev, Message: "x"})
	}
	return s
}

// The first line of a sweep should be the thing worth acting on.
func TestSummarize_WorstFirst(t *testing.T) {
	got := Summarize([]LintSubject{
		subject("clean"),
		subject("warn", Warning),
		{Kind: "workflow", Name: "unparseable", Skipped: "dsl will not parse"},
		subject("broken", Error, Warning),
	})

	want := []string{"broken", "warn", "unparseable", "clean"}
	for i, n := range want {
		if got.Subjects[i].Name != n {
			t.Errorf("position %d = %q, want %q", i, got.Subjects[i].Name, n)
		}
	}
}

// A workflow that could not be parsed is an open question, not a pass, so it
// must never be counted as clean.
func TestSummarize_SkippedIsNotClean(t *testing.T) {
	got := Summarize([]LintSubject{
		subject("ok"),
		{Kind: "template", Name: "bad json", Skipped: "dsl will not parse"},
	})
	if got.Clean != 1 {
		t.Errorf("Clean = %d, want 1 — a skipped subject is not a passing one", got.Clean)
	}
	if got.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", got.Skipped)
	}
	if got.Checked != 2 {
		t.Errorf("Checked = %d, want 2", got.Checked)
	}
}

// Every subject lands in exactly one bucket, so the totals can be trusted to
// add up in the summary line.
func TestSummarize_BucketsPartition(t *testing.T) {
	got := Summarize([]LintSubject{
		subject("a", Error), subject("b", Error, Error),
		subject("c", Warning), subject("d"), subject("e"),
		{Name: "f", Skipped: "no"},
	})
	if sum := got.Clean + got.Errors + got.Warnings + got.Skipped; sum != got.Checked {
		t.Errorf("buckets sum to %d but %d were checked", sum, got.Checked)
	}
	if got.Errors != 2 || got.Warnings != 1 || got.Clean != 2 || got.Skipped != 1 {
		t.Errorf("got errors=%d warnings=%d clean=%d skipped=%d",
			got.Errors, got.Warnings, got.Clean, got.Skipped)
	}
}

// A subject with both errors and warnings counts once, as an error — otherwise
// the summary line double-counts and reads as more problems than exist.
func TestSummarize_ErrorOutranksWarning(t *testing.T) {
	got := Summarize([]LintSubject{subject("both", Error, Warning, Warning)})
	if got.Errors != 1 || got.Warnings != 0 {
		t.Errorf("errors=%d warnings=%d, want 1 and 0", got.Errors, got.Warnings)
	}
	if s := got.Subjects[0]; s.Errors() != 1 || s.Warnings() != 2 {
		t.Errorf("per-subject counts should still be exact: %d/%d", s.Errors(), s.Warnings())
	}
}

// A template is supposed to have placeholders. Counting them as errors makes
// every template look broken and buries the findings that matter — and it
// would contradict the single-template path, which does filter them.
func TestWithoutPlaceholderFindings(t *testing.T) {
	res := Result{
		TriggerType: "jc_events",
		Findings: []Finding{
			{Severity: Error, Path: "a", Message: "unfilled template placeholder REPLACE_WITH_GROUP_ID"},
			{Severity: Warning, Path: "b", Message: "this guard can never see a failure"},
			{Severity: Error, Path: "c", Message: "something", Hint: "replace REPLACE_WITH_PATH first"},
		},
	}

	got := WithoutPlaceholderFindings(res)
	if len(got.Findings) != 1 {
		t.Fatalf("kept %d findings, want 1: %+v", len(got.Findings), got.Findings)
	}
	if got.Findings[0].Path != "b" {
		t.Errorf("kept the wrong finding: %+v", got.Findings[0])
	}
	if got.TriggerType != "jc_events" {
		t.Error("the rest of the result must survive the filter")
	}
	// The input must not be mutated: the caller may still want the raw result.
	if len(res.Findings) != 3 {
		t.Errorf("the original result was mutated: %d findings left", len(res.Findings))
	}
}

// The real catalog is the reason this exists: linting it raw reported 10 of 12
// templates as failing, when the placeholders causing that are exactly what a
// template is meant to contain.
func TestWithoutPlaceholderFindings_LeavesRealDefects(t *testing.T) {
	res := Result{Findings: []Finding{
		{Severity: Error, Message: "unfilled template placeholder REPLACE_WITH_COMMAND_ID"},
		{Severity: Error, Message: "unfilled template placeholder REPLACE_WITH_WORKFLOW_ID"},
	}}
	if got := WithoutPlaceholderFindings(res); !got.OK() {
		t.Errorf("a template whose only problems are placeholders is not broken: %+v", got.Findings)
	}
}
