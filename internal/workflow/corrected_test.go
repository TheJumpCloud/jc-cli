package workflow

import (
	"strings"
	"testing"
)

// The whole point. A corrected template that still trips the validator would
// be no better than the one it replaces, and shipping it would repeat exactly
// the mistake this catalog exists to fix.
func TestCorrectedTemplates_ValidateClean(t *testing.T) {
	all := CorrectedTemplates()
	if len(all) == 0 {
		t.Fatal("the corrected catalog is empty")
	}

	for _, ct := range all {
		d, err := ParseDSL(ct.DSL)
		if err != nil {
			t.Errorf("%s: dsl does not parse: %v", ct.ID, err)
			continue
		}
		// Placeholders are expected — these are still templates.
		res := WithoutPlaceholderFindings(Validate(d))
		for _, f := range res.Findings {
			t.Errorf("%s: %s: %s", ct.ID, f.Severity, f.Message)
		}
	}
}

// Each corrected template must still trace back to what it replaces, or the
// catalog is a pile of unattributed forks.
func TestCorrectedTemplates_Attributed(t *testing.T) {
	for _, ct := range CorrectedTemplates() {
		switch {
		case !strings.HasPrefix(ct.ID, CorrectedIDPrefix):
			t.Errorf("%q must carry the %q prefix, or it cannot be told apart "+
				"from the JumpCloud template it shares a name with", ct.ID, CorrectedIDPrefix)
		case ct.Corrects == "":
			t.Errorf("%s does not say what it corrects", ct.ID)
		case ct.Changes == "":
			t.Errorf("%s does not say what changed", ct.ID)
		}
	}
}

// The correction must be a deletion, not a rewrite. Anything else means the
// catalog has drifted from the original and silently changed behaviour.
func TestCorrectedTemplates_OnlyRemoveTheDeadConjunct(t *testing.T) {
	for _, ct := range CorrectedTemplates() {
		raw := string(ct.DSL)
		if eq200RE.MatchString(raw) {
			t.Errorf("%s still contains a status == 200 test", ct.ID)
		}
		// The live conjuncts the guards depend on must have survived. Every
		// one of these templates guards on something real; if the fix ate
		// that too, the workflow would run when it should not.
		if !strings.Contains(raw, `"if"`) {
			t.Errorf("%s has no guards left at all, so the fix removed too much", ct.ID)
		}
	}
}

func TestFindCorrected(t *testing.T) {
	all := CorrectedTemplates()
	first := all[0]

	// By full ID, by bare slug, and by the original template's name.
	for _, id := range []string{first.ID, strings.TrimPrefix(first.ID, CorrectedIDPrefix), first.Name} {
		got, ok := FindCorrected(id)
		if !ok {
			t.Errorf("FindCorrected(%q) found nothing", id)
			continue
		}
		if got.ID != first.ID {
			t.Errorf("FindCorrected(%q) = %s, want %s", id, got.ID, first.ID)
		}
	}

	if _, ok := FindCorrected("no-such-template"); ok {
		t.Error("an unknown identifier must not resolve")
	}
}

// A defect report is only useful if it can point at the fix.
func TestCorrectionFor(t *testing.T) {
	first := CorrectedTemplates()[0]
	got, ok := CorrectionFor(first.Corrects)
	if !ok {
		t.Fatalf("no correction found for %q", first.Corrects)
	}
	if got.ID != first.ID {
		t.Errorf("CorrectionFor(%q) = %s, want %s", first.Corrects, got.ID, first.ID)
	}
	if _, ok := CorrectionFor("Some Template That Is Fine"); ok {
		t.Error("a template with no correction must not resolve to one")
	}
}

// The catalog is embedded, so a corrupt artifact is a build-time problem that
// must surface in tests rather than as a panic in a user's terminal.
func TestCorrectedTemplates_Embedded(t *testing.T) {
	if len(correctedJSON) == 0 {
		t.Fatal("corrected.json was not embedded")
	}
	if got := len(CorrectedTemplates()); got != 4 {
		t.Errorf("catalog has %d entries, want the 4 defective templates "+
			"lint reports; regenerate with scripts/gen-corrected-templates.py", got)
	}
}
