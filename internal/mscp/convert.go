package mscp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/bundle"
)

// Report says what the conversion did — surfaced by the import command
// and folded into the bundle description so the honest-subset caveat
// travels with the artifact.
type Report struct {
	BaselineTitle string
	TotalRules    int
	// Converted rules contributed at least one payload key.
	Converted int
	// ShellOnly rules have no mobileconfig enforcement (mSCP enforces
	// them via shell check/fix scripts) — out of MDM's reach.
	ShellOnly []string
	// Units lists the generated policy units (one per PayloadType).
	Units []string
	// RawTypes are payload types emitted in raw mode (no vendored
	// Apple schema, or merged content spanning schema variants).
	RawTypes []string
}

// Convert turns one mSCP baseline into a bundle: rules are filtered to
// the mobileconfig-enforceable subset, $ODV placeholders are resolved
// per the baseline's parent_values column, and payload keys are merged
// per PayloadType into one policy unit each (the same per-type profile
// layout mSCP's own generator emits).
//
// Payload types whose merged values validate against the embedded
// Apple catalog become normal catalog-checked payloads; the rest —
// preference domains Apple ships no schema for (com.apple.Safari),
// merged com.apple.MCX spanning catalog variants, nested
// com.apple.ManagedClient.preferences — are emitted raw.
func Convert(rules map[string]*Rule, b *Baseline, bundleName, origin string) (*bundle.Bundle, *Report, error) {
	cat, err := apple_mdm.Default()
	if err != nil {
		return nil, nil, err
	}

	report := &Report{BaselineTitle: b.Title}

	// Merge payload keys per type, preserving first-seen type order so
	// regeneration is deterministic. Track which rule set each key so
	// genuine conflicts (same key, different values) fail loudly.
	type keyOrigin struct {
		rule  string
		value any
	}
	merged := map[string]map[string]any{}
	owners := map[string]map[string]keyOrigin{}
	var typeOrder []string

	for _, id := range b.RuleIDs() {
		report.TotalRules++
		r, ok := rules[id]
		if !ok {
			return nil, nil, fmt.Errorf("baseline references unknown rule %q", id)
		}
		if !r.Mobileconfig || len(r.MobileconfigInfo) == 0 {
			report.ShellOnly = append(report.ShellOnly, id)
			continue
		}
		report.Converted++
		for pt, content := range r.MobileconfigInfo {
			if merged[pt] == nil {
				merged[pt] = map[string]any{}
				owners[pt] = map[string]keyOrigin{}
				typeOrder = append(typeOrder, pt)
			}
			for k, v := range content {
				resolved, err := resolveODV(r, b.ParentValues, v)
				if err != nil {
					return nil, nil, err
				}
				if prev, exists := owners[pt][k]; exists {
					// List-valued keys union-merge: rules contribute
					// items independently (SkipSetupItems is the
					// canonical case — one rule adds AppleID, another
					// iCloudStorage). mSCP's own generator concatenates
					// these the same way. Scalars must agree.
					if unioned, ok := mergeLists(merged[pt][k], resolved); ok {
						merged[pt][k] = unioned
						continue
					}
					if fmt.Sprintf("%v", prev.value) != fmt.Sprintf("%v", resolved) {
						return nil, nil, fmt.Errorf(
							"baseline %s: rules %s and %s set %s/%s to different values (%v vs %v)",
							b.Title, prev.rule, r.ID, pt, k, prev.value, resolved)
					}
					continue
				}
				owners[pt][k] = keyOrigin{rule: r.ID, value: resolved}
				merged[pt][k] = resolved
			}
		}
	}

	if len(typeOrder) == 0 {
		return nil, nil, fmt.Errorf("baseline %s has no mobileconfig-enforceable rules", b.Title)
	}

	out := &bundle.Bundle{
		Name:    bundleName,
		Version: SnapshotTag,
		Source: &bundle.Source{
			Origin: origin,
			Attribution: fmt.Sprintf(
				"Derived from the NIST macOS Security Compliance Project (usnistgov/macos_security, tag %s): %s. "+
					"Covers the configuration-profile-enforceable subset: %d of %d baseline rules; the other %d rules require mSCP's shell-based checks outside MDM.",
				SnapshotTag, b.Title, report.Converted, report.TotalRules, len(report.ShellOnly)),
			License: "CC BY 4.0",
			URL:     "https://github.com/usnistgov/macos_security",
		},
		Description: fmt.Sprintf(
			"macOS hardening baseline derived from NIST mSCP %s (%s). One policy per payload domain; profile-enforceable subset (%d of %d rules).",
			b.ParentValues, SnapshotTag, report.Converted, report.TotalRules),
	}

	for _, pt := range typeOrder {
		values := merged[pt]
		raw := !validatesAgainstCatalog(cat, pt, values)
		if raw {
			report.RawTypes = append(report.RawTypes, pt)
		}
		out.Policies = append(out.Policies, bundle.PolicyUnit{
			Name: pt,
			Type: bundle.UnitAppleProfile,
			OS:   "macOS",
			Profile: &apple_mdm.ComposeConfig{
				Name: bundleName + ": " + pt,
				Payloads: []apple_mdm.ComposePayload{{
					Type:   pt,
					Raw:    raw,
					Values: values,
				}},
			},
		})
		report.Units = append(report.Units, pt)
	}
	sort.Strings(report.RawTypes)

	// The converter validates its own output — a generated baseline
	// that fails bundle validation must never reach disk.
	if err := bundle.Validate(out, cat); err != nil {
		return nil, nil, fmt.Errorf("generated bundle failed validation: %w", err)
	}
	return out, report, nil
}

// mergeLists returns the order-preserving, deduplicated union of two
// values when BOTH are YAML sequences; ok is false otherwise (scalars
// fall through to the equality check).
func mergeLists(existing, incoming any) (any, bool) {
	a, aok := existing.([]any)
	b, bok := incoming.([]any)
	if !aok || !bok {
		return nil, false
	}
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]any, 0, len(a)+len(b))
	for _, list := range [][]any{a, b} {
		for _, item := range list {
			key := fmt.Sprintf("%v", item)
			if !seen[key] {
				seen[key] = true
				out = append(out, item)
			}
		}
	}
	return out, true
}

// validatesAgainstCatalog reports whether the merged values can go
// through the catalog-checked (non-raw) compose path: the type must
// resolve to exactly ONE catalog variant (compose refuses ambiguous
// types without an explicit id — merged mSCP payloads like
// com.apple.MCX deliberately span variants) and that variant must
// accept the values.
func validatesAgainstCatalog(cat *apple_mdm.Catalog, payloadType string, values map[string]any) bool {
	variants := cat.VariantsOf(payloadType)
	if len(variants) != 1 {
		return false
	}
	_, err := apple_mdm.CoerceAndValidate(variants[0], values)
	return err == nil
}

// Summary renders the report as the import command's stderr recap.
func (r *Report) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Baseline: %s\n", r.BaselineTitle)
	fmt.Fprintf(&b, "Rules: %d total → %d profile-enforceable (converted), %d shell-only (skipped)\n",
		r.TotalRules, r.Converted, len(r.ShellOnly))
	fmt.Fprintf(&b, "Policy units: %d (one per payload domain)\n", len(r.Units))
	if len(r.RawTypes) > 0 {
		fmt.Fprintf(&b, "Raw payloads (no vendored Apple schema; emitted verbatim): %s\n",
			strings.Join(r.RawTypes, ", "))
	}
	return b.String()
}
