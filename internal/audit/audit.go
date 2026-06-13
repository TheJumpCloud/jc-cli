// Package audit implements cross-resource health checks for a JumpCloud
// org, exposed via `jc audit`. The package is the registry + runner; the
// CLI surface and skill prompts compose against it.
//
// Each check is one Go function tagged with a Category + intrinsic
// Severity. Checks consume a shared *Data bundle (fetched once, in
// parallel) and emit Findings. The runner filters by category + min
// severity, applies the --threshold exit-code policy, and returns a
// sortable result set.
//
// Adding a new check is a one-line Register call from an init() — see
// checks_security.go for the canonical shape.
package audit

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Severity is the impact level a finding represents. Ordered low → high
// so callers can do range comparisons (Severity.AtLeast).
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// severityRank maps each severity to a comparable int. Unknown values
// rank lowest so they don't accidentally trip --threshold gates.
var severityRank = map[Severity]int{
	SeverityInfo:     1,
	SeverityLow:      2,
	SeverityMedium:   3,
	SeverityHigh:     4,
	SeverityCritical: 5,
}

// AtLeast returns true if s is >= other in the standard severity order.
// Used by --severity and --threshold filters.
func (s Severity) AtLeast(other Severity) bool {
	return severityRank[s] >= severityRank[other]
}

// Valid reports whether s is one of the declared constants. Returned
// from CLI flag parsing so an unknown --severity surfaces clearly.
func (s Severity) Valid() bool {
	_, ok := severityRank[s]
	return ok
}

// Category groups checks for filtering and skill scope. Categories are
// curated, not free-form — a typo on a check Category would silently
// orphan it from --category filters.
type Category string

const (
	CategorySecurity   Category = "security"
	CategoryCompliance Category = "compliance"
	CategoryHygiene    Category = "hygiene"
	CategoryIdentity   Category = "identity"
)

// Categories returns the known categories in display order.
func Categories() []Category {
	return []Category{CategorySecurity, CategoryCompliance, CategoryHygiene, CategoryIdentity}
}

// Valid reports whether c is one of the declared constants.
func (c Category) Valid() bool {
	for _, k := range Categories() {
		if k == c {
			return true
		}
	}
	return false
}

// Finding is the atomic unit emitted by a check. One check can emit
// many findings (one per offending resource), or none (the org is
// clean for that check).
//
// ResourceRef uses a "<kind>:<identifier>" convention (admin:foo@bar.com,
// device:host-42, group:contractors) so skills and CI gates can group
// or deduplicate by resource without parsing free-form text.
type Finding struct {
	CheckID         string   `json:"check_id"`
	Title           string   `json:"title"`
	Category        Category `json:"category"`
	Severity        Severity `json:"severity"`
	ResourceRef     string   `json:"resource_ref,omitempty"`
	RemediationHint string   `json:"remediation_hint,omitempty"`
	Detail          string   `json:"detail,omitempty"`
}

// CheckResult captures one check's outcome, including a fatal Error if
// the check couldn't run (e.g. its required API fetch failed). An empty
// Findings slice + empty Error means "ran clean, no issues."
type CheckResult struct {
	CheckID    string    `json:"check_id"`
	Title      string    `json:"title"`
	Category   Category  `json:"category"`
	Findings   []Finding `json:"findings"`
	Error      string    `json:"error,omitempty"`
	DurationMS int64     `json:"duration_ms"`
}

// MaxSeverity returns the highest finding severity in the result, or
// "" if there are none. Useful for status-line rendering and threshold
// comparisons.
func (r CheckResult) MaxSeverity() Severity {
	var max Severity
	for _, f := range r.Findings {
		if max == "" || severityRank[f.Severity] > severityRank[max] {
			max = f.Severity
		}
	}
	return max
}

// AuditCheck is a registered check. ID must be globally unique and
// kebab-case. Run consumes the shared Data bundle and emits findings;
// it should not perform its own API calls.
type AuditCheck struct {
	ID       string
	Title    string
	Category Category
	Run      func(ctx context.Context, d *Data) ([]Finding, error)
}

var registry []AuditCheck

// Register adds a check to the global registry. Called from init() in
// each checks_*.go file. Duplicate IDs panic at registration time — a
// silent overwrite would make findings invisible without an obvious
// failure mode.
func Register(c AuditCheck) {
	for _, existing := range registry {
		if existing.ID == c.ID {
			panic(fmt.Sprintf("audit: duplicate check ID %q", c.ID))
		}
	}
	if !c.Category.Valid() {
		panic(fmt.Sprintf("audit: check %q has invalid category %q", c.ID, c.Category))
	}
	registry = append(registry, c)
}

// All returns a copy of the registry so callers can mutate the slice
// (sort, filter) without corrupting the package-level state.
func All() []AuditCheck {
	out := make([]AuditCheck, len(registry))
	copy(out, registry)
	return out
}

// RunOptions configures a Run invocation. Empty Categories means "all";
// empty MinSeverity means "no filter."
type RunOptions struct {
	Categories  []Category
	MinSeverity Severity
}

// match returns true if the check survives the category filter.
func (o RunOptions) matchCategory(cat Category) bool {
	if len(o.Categories) == 0 {
		return true
	}
	for _, want := range o.Categories {
		if want == cat {
			return true
		}
	}
	return false
}

// Run executes the registered checks against the supplied Data bundle.
// Checks run sequentially (cheap, the data is already in memory) so the
// runner can preserve registration order in the output and so a single
// check's error doesn't race against another's progress reporting.
//
// Each check is bounded by ctx; cancellation is checked between checks
// and surfaces immediately as ctx.Err().
func Run(ctx context.Context, d *Data, opts RunOptions) ([]CheckResult, error) {
	var results []CheckResult
	for _, c := range registry {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if !opts.matchCategory(c.Category) {
			continue
		}
		start := time.Now()
		findings, err := c.Run(ctx, d)
		duration := time.Since(start).Milliseconds()

		res := CheckResult{
			CheckID:    c.ID,
			Title:      c.Title,
			Category:   c.Category,
			DurationMS: duration,
		}
		if err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		if opts.MinSeverity != "" && opts.MinSeverity.Valid() {
			findings = filterBySeverity(findings, opts.MinSeverity)
		}
		res.Findings = findings
		results = append(results, res)
	}
	return results, nil
}

func filterBySeverity(in []Finding, min Severity) []Finding {
	out := in[:0]
	for _, f := range in {
		if f.Severity.AtLeast(min) {
			out = append(out, f)
		}
	}
	return out
}

// SortByCategoryAndSeverity orders results for human display: category
// in canonical order, then highest-severity-first within each category,
// then check ID for stable tiebreak.
func SortByCategoryAndSeverity(results []CheckResult) {
	catOrder := make(map[Category]int)
	for i, c := range Categories() {
		catOrder[c] = i
	}
	sort.SliceStable(results, func(i, j int) bool {
		ci, cj := catOrder[results[i].Category], catOrder[results[j].Category]
		if ci != cj {
			return ci < cj
		}
		si, sj := severityRank[results[i].MaxSeverity()], severityRank[results[j].MaxSeverity()]
		if si != sj {
			return si > sj
		}
		return results[i].CheckID < results[j].CheckID
	})
}

// AnyFindingAtLeast returns true if any result contains a finding at or
// above the given severity. Used by --exit-code/--threshold to derive
// the CLI exit status.
func AnyFindingAtLeast(results []CheckResult, threshold Severity) bool {
	if !threshold.Valid() {
		return false
	}
	for _, r := range results {
		for _, f := range r.Findings {
			if f.Severity.AtLeast(threshold) {
				return true
			}
		}
	}
	return false
}

// AnyCheckError returns true if any check failed to run (its Run
// returned a non-nil error, typically because its required data wasn't
// fetched). Distinct from findings — a degraded audit shouldn't look
// like a clean one. Used alongside AnyFindingAtLeast for --exit-code
// so a partial fetch can't silently green-light a CI gate.
func AnyCheckError(results []CheckResult) bool {
	for _, r := range results {
		if r.Error != "" {
			return true
		}
	}
	return false
}
