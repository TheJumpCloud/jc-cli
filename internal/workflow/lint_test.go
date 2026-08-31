package workflow

import (
	"encoding/json"
	"testing"
)

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

// Reporting a defect without naming the fix leaves the reader where they
// started — and this sweep is where they choose what to copy.
//
// Regression: the CLI set corrected_by while the MCP tool did not, because the
// loop was duplicated in both surfaces instead of shared. An agent running the
// sweep saw four defective templates and no pointer to the corrected copies
// shipping in the same binary. Both now call LintTemplate.
func TestLintTemplate_NamesTheCorrection(t *testing.T) {
	target := CorrectedTemplates()[0]

	// The template this corrects, reconstructed with the defect that earns a
	// finding: a status guard after a fallible call.
	defective := []byte(`{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[
	    {"fetch":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}},
	    {"guarded":{"if":"${ actions.fetch.status == 200 }","call":"jc_operation",
	      "with":{"operationId":"getApiSystemusers","version":1}}}
	  ]}`)

	sub := LintTemplate("tmpl-1", target.Corrects, defective)
	if len(sub.Result.Findings) == 0 {
		t.Fatal("this template has a status guard and must produce a finding")
	}
	if sub.CorrectedBy != target.ID {
		t.Errorf("corrected_by = %q, want %q — the fix ships in this binary and must be named",
			sub.CorrectedBy, target.ID)
	}
}

// A clean template must not advertise a correction: the pointer is only useful
// where there is something to fix.
func TestLintTemplate_CleanTemplateNamesNoCorrection(t *testing.T) {
	clean := []byte(`{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`)

	sub := LintTemplate("tmpl-2", CorrectedTemplates()[0].Corrects, clean)
	if sub.CorrectedBy != "" {
		t.Errorf("a clean template needs no correction pointer, got %q", sub.CorrectedBy)
	}
}

// The corrected copies are linted in the same sweep that reports the defects
// they fix, so the output proves them — and a copy that ever drifted would be
// caught by the very tool it exists to answer.
func TestLintCorrected_AllClean(t *testing.T) {
	subs := LintCorrected()
	if len(subs) != len(CorrectedTemplates()) {
		t.Fatalf("linted %d of %d corrected copies", len(subs), len(CorrectedTemplates()))
	}
	for _, s := range subs {
		if s.Skipped != "" {
			t.Errorf("%s was not checked: %s", s.ID, s.Skipped)
		}
		for _, f := range s.Result.Findings {
			t.Errorf("%s is meant to be the FIXED copy: %s: %s", s.ID, f.Severity, f.Message)
		}
		if s.Corrects == "" {
			t.Errorf("%s does not say what it replaces", s.ID)
		}
		// A corrected copy must never point at itself for a fix.
		if s.CorrectedBy != "" {
			t.Errorf("%s should not carry corrected_by", s.ID)
		}
	}
}

// A linted workflow must be correlatable to the object workflows_get returns.
//
// lint reported only `execution_role` as a NAME while get/list reported only
// `execution_role_id` as an ID — a different field name AND a different
// representation for one fact, so joining the two needed a separate roles
// lookup. It was invisible until the tenant had a workflow to compare.
func TestLintWorkflow_CarriesTheRoleIDForCorrelation(t *testing.T) {
	w := Workflow{
		ID: "wf-1", Name: "probe", Status: StatusActive, TriggerType: TriggerEvents,
		ExecutionRoleID: "5f4fd6954485cfa33af14d14",
		DSL: []byte(`{"schedule":{"on":{"one":{"with":{"source":"jc_events","type":"user_create"}}}},
		  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`),
	}

	sub, _, ok := LintWorkflow(w)
	if !ok {
		t.Fatalf("should lint: %s", sub.Skipped)
	}
	if sub.ExecutionRoleID != w.ExecutionRoleID {
		t.Errorf("execution_role_id = %q, want the id workflows_get reports (%q)",
			sub.ExecutionRoleID, w.ExecutionRoleID)
	}

	// trigger_type must sit where get/list put it, not one level down.
	if sub.TriggerType != TriggerEvents {
		t.Errorf("trigger_type = %q at the top level, want %q — get and list put it there",
			sub.TriggerType, TriggerEvents)
	}

	raw, err := json.Marshal(sub)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"execution_role_id", "trigger_type"} {
		if _, ok := m[k]; !ok {
			t.Errorf("lint must emit %q at the top level so a field-set diff against "+
				"workflows_get does not flag it", k)
		}
	}
}
