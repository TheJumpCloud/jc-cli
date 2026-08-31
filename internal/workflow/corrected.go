package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Corrected copies of the JumpCloud templates that ship with a defect.
//
// The template catalog is the only worked specification of a DSL that has no
// published schema, so whatever the templates do gets copied into real
// workflows. Four of the twelve open a task guard with
// `actions.X.status == 200 &&`, which cannot do what it looks like it does:
//
//   - A non-2xx from any jc_operation halts the whole run, so a guard asking
//     whether an earlier call succeeded is only ever reached when it did.
//   - `== 200` is an equality against ONE success code, so a call returning
//     201 makes it false and silently skips a task that should have run.
//     Observed in a single live run: a task guarded on `status == 200` after
//     a create that returned 201 reported "Skipping — if condition did not
//     match", while `>= 200 && < 300` on the same call executed.
//
// So `jc workflows lint --templates` naming the defect is not enough on its
// own — an operator still needs something safe to copy. These are it.
//
// The correction is a deletion, not a rewrite: every one of those guards ANDs
// the dead test with a live check (a result-set length, a non-empty field),
// and that check is what actually matters. Deleting the conjunct is safe even
// where the guarded call is itself conditional, because a guard referencing a
// SKIPPED task evaluates false without erroring — also verified live, in a run
// where every reference to a skipped task's status and body came out false and
// the run still completed.
//
//go:embed corrected.json
var correctedJSON []byte

// CorrectedTemplate is one repaired template, carrying what it corrects and
// what changed, so the difference is auditable rather than asserted.
type CorrectedTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	// Corrects names the JumpCloud template this replaces.
	Corrects   string `json:"corrects"`
	CorrectsID string `json:"corrects_id,omitempty"`
	// Changes says what was altered and why, in one sentence.
	Changes string          `json:"changes"`
	DSL     json.RawMessage `json:"dsl"`
}

var (
	correctedOnce sync.Once
	corrected     []CorrectedTemplate
)

func loadCorrected() {
	correctedOnce.Do(func() {
		if err := json.Unmarshal(correctedJSON, &corrected); err != nil {
			// The catalog is embedded at build time and covered by a test, so
			// this cannot happen in a shipped binary.
			panic("workflow: corrupt corrected.json: " + err.Error())
		}
	})
}

// CorrectedTemplates returns the repaired catalog.
func CorrectedTemplates() []CorrectedTemplate {
	loadCorrected()
	out := make([]CorrectedTemplate, len(corrected))
	copy(out, corrected)
	return out
}

// FindCorrected resolves a corrected template by ID or name. The "jc:" prefix
// is optional, so both "jc:wipe-device-and-reset-asset-status" and the
// original template's name work.
func FindCorrected(identifier string) (CorrectedTemplate, bool) {
	loadCorrected()
	want := strings.TrimPrefix(identifier, CorrectedIDPrefix)
	for _, t := range corrected {
		if strings.TrimPrefix(t.ID, CorrectedIDPrefix) == want {
			return t, true
		}
	}
	for _, t := range corrected {
		if strings.EqualFold(t.Name, identifier) {
			return t, true
		}
	}
	return CorrectedTemplate{}, false
}

// Source values, shared by every surface that reports where a template came
// from. One vocabulary, defined once.
const (
	// SourceJumpCloud marks a template served by JumpCloud.
	SourceJumpCloud = "jumpcloud"
	// SourceJC marks one of jc's own corrected copies.
	SourceJC = "jc"
)

// CorrectedIDPrefix marks a template as jc's rather than JumpCloud's. The
// prefix matters: a corrected copy shares its NAME with the original, so
// without it the two could not be told apart when resolving.
const CorrectedIDPrefix = "jc:"

// IsCorrectedID reports whether an identifier addresses the corrected catalog.
func IsCorrectedID(identifier string) bool {
	return strings.HasPrefix(identifier, CorrectedIDPrefix)
}

// CorrectionFor returns the corrected replacement for a JumpCloud template
// name, so a defect report can point straight at the fix.
func CorrectionFor(templateName string) (CorrectedTemplate, bool) {
	loadCorrected()
	for _, t := range corrected {
		if strings.EqualFold(t.Corrects, templateName) {
			return t, true
		}
	}
	return CorrectedTemplate{}, false
}

// Describe renders one line naming what a corrected template replaces.
func (t CorrectedTemplate) Describe() string {
	return fmt.Sprintf("%s — corrects JumpCloud's %q: %s", t.ID, t.Corrects, t.Changes)
}

// Template list rows are built HERE, not in each surface.
//
// The CLI and the MCP tool each built this map inline, and the copies drifted:
// one set corrected_by, the other did not. Sharing the construction is what
// makes "the CLI and the MCP tool agree" a property of the code rather than of
// whoever last edited both.

// ServedTemplateRow summarises a JumpCloud template for a list view, without
// its DSL — a full catalog is ~40KB of nested documents. Where jc ships a
// corrected copy, the row names it, because this list is where a caller decides
// what to copy.
func ServedTemplateRow(id, name, category, description string) map[string]any {
	row := map[string]any{
		"id": id, "name": name, "category": category,
		"description": description, "source": SourceJumpCloud,
	}
	if ct, ok := CorrectionFor(name); ok {
		row["corrected_by"] = ct.ID
	}
	return row
}

// CorrectedTemplateRow summarises one of jc's corrected copies.
func CorrectedTemplateRow(ct CorrectedTemplate) map[string]any {
	return map[string]any{
		"id": ct.ID, "name": ct.Name, "category": ct.Category,
		"description": ct.Description, "source": SourceJC,
		"corrects": ct.Corrects, "changes": ct.Changes,
	}
}
