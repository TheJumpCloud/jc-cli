package workflow

import (
	"encoding/json"
	"sort"
	"testing"
)

// Cross-surface parity, asserted on FIELD SETS rather than values.
//
// Two bugs came from the CLI and the MCP tool building the same object
// independently: one set corrected_by and the other did not, and the two tools
// spelled the same distinction as `source` in one place and a "(jc corrected)"
// suffix on `kind` in the other. Both were invisible to a value comparison and
// both would have failed this.
//
// The rule these encode: an object that two surfaces emit is CONSTRUCTED once,
// in this package. A test asserting the shape is what stops the constructors
// being quietly re-inlined later.

func keysOf(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func assertKeys(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Errorf("field set = %v, want %v", got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("field set = %v, want %v", got, want)
			return
		}
	}
}

// A served template row, as both surfaces emit it.
func TestParity_ServedTemplateRow(t *testing.T) {
	// A template with no correction.
	plain := ServedTemplateRow("t1", "Some Clean Template", "cat", "desc")
	assertKeys(t, keysOf(t, plain), "id", "name", "category", "description", "source")
	if plain["source"] != SourceJumpCloud {
		t.Errorf("source = %v, want %q", plain["source"], SourceJumpCloud)
	}

	// A template jc corrects gains exactly one field, and it is the pointer
	// to the fix — the thing whose absence from the MCP tool was the bug.
	corrected := CorrectedTemplates()[0]
	withFix := ServedTemplateRow("t2", corrected.Corrects, "cat", "desc")
	assertKeys(t, keysOf(t, withFix),
		"id", "name", "category", "description", "source", "corrected_by")
	if withFix["corrected_by"] != corrected.ID {
		t.Errorf("corrected_by = %v, want %q", withFix["corrected_by"], corrected.ID)
	}
}

// jc's own rows carry what they replace and what changed.
func TestParity_CorrectedTemplateRow(t *testing.T) {
	row := CorrectedTemplateRow(CorrectedTemplates()[0])
	assertKeys(t, keysOf(t, row),
		"id", "name", "category", "description", "source", "corrects", "changes")
	if row["source"] != SourceJC {
		t.Errorf("source = %v, want %q", row["source"], SourceJC)
	}
}

// The lint sweep must use the SAME source vocabulary as the template list. It
// did not: lint said kind "template (jc corrected)" while the list said
// source "jc", so a caller consuming both learned two vocabularies for one
// distinction.
func TestParity_LintUsesTheTemplateListVocabulary(t *testing.T) {
	served := LintTemplate("t1", "Some Template", []byte(
		`{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
		  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`))
	if served.Source != SourceJumpCloud {
		t.Errorf("served template source = %q, want %q", served.Source, SourceJumpCloud)
	}
	if served.Kind != "template" {
		t.Errorf("kind should say WHAT it is, not who wrote it: %q", served.Kind)
	}

	for _, sub := range LintCorrected() {
		if sub.Source != SourceJC {
			t.Errorf("%s source = %q, want %q", sub.ID, sub.Source, SourceJC)
		}
		if sub.Kind != "template" {
			t.Errorf("%s kind = %q; the source field carries the distinction", sub.ID, sub.Kind)
		}
	}

	// And the row builder must agree with the lint subject about the value.
	row := CorrectedTemplateRow(CorrectedTemplates()[0])
	if row["source"] != LintCorrected()[0].Source {
		t.Errorf("the two tools disagree about source: %v vs %q",
			row["source"], LintCorrected()[0].Source)
	}
}

// The summary must say where its subjects came from: a bare "16 checked" over
// a 12-template catalog reads as though the catalog grew.
func TestParity_SummaryCountsBySource(t *testing.T) {
	subs := append(
		[]LintSubject{{Kind: "template", Source: SourceJumpCloud, Name: "a"}},
		LintCorrected()...)
	got := Summarize(subs)

	if got.Checked != len(subs) {
		t.Errorf("Checked = %d, want %d", got.Checked, len(subs))
	}
	if got.CheckedBySource[SourceJumpCloud] != 1 {
		t.Errorf("jumpcloud count = %d, want 1", got.CheckedBySource[SourceJumpCloud])
	}
	if got.CheckedBySource[SourceJC] != len(CorrectedTemplates()) {
		t.Errorf("jc count = %d, want %d",
			got.CheckedBySource[SourceJC], len(CorrectedTemplates()))
	}
	// The breakdown must account for every checked subject.
	total := 0
	for _, n := range got.CheckedBySource {
		total += n
	}
	if total != got.Checked {
		t.Errorf("breakdown sums to %d but %d were checked", total, got.Checked)
	}
}
