package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Placeholder kinds. A kind names WHAT a marker wants, not how to find it:
// this package deliberately does not import internal/resolve, so each surface
// maps a kind onto its own resolver (the CLI and MCP to a ResourceConfig, the
// TUI to a registry entry it can open as a picker).
const (
	KindCommand     = "command"
	KindUserGroup   = "user-group"
	KindDeviceGroup = "device-group"
	KindPolicy      = "policy"
	KindAppleMDM    = "apple-mdm"
	KindWorkflow    = "workflow"
	KindFreeText    = "free-text"
)

// PlaceholderKind describes what a REPLACE_WITH_* marker expects.
type PlaceholderKind struct {
	// Kind is one of the Kind* constants.
	Kind string `json:"kind"`
	// Describe is a one-phrase explanation for help text and prompts.
	Describe string `json:"describe"`
	// Resolvable reports whether a name can be looked up for this kind. Free
	// text is not resolvable and is taken literally.
	Resolvable bool `json:"resolvable"`
}

// placeholderRule maps a marker suffix to a kind.
//
// Order matters and is the whole reason this is a slice rather than a map. The
// markers overlap: DEVICE_GROUP_ID ends with GROUP_ID, and
// FAILED_POLICY_DEVICE_GROUP_ID contains both POLICY and DEVICE_GROUP. Testing
// longest-first means the specific rule wins over the general one; reordering
// this list silently misclassifies markers, so placeholders_test.go pins every
// marker the shipped templates actually use.
var placeholderRules = []struct {
	suffix string
	kind   PlaceholderKind
}{
	{"DEVICE_GROUP_ID", PlaceholderKind{KindDeviceGroup, "a JumpCloud device group", true}},
	{"APPLE_MDM_ID", PlaceholderKind{KindAppleMDM, "an Apple MDM configuration", true}},
	{"WORKFLOW_ID", PlaceholderKind{KindWorkflow, "another workflow", true}},
	{"COMMAND_ID", PlaceholderKind{KindCommand, "a JumpCloud command", true}},
	{"POLICY_ID", PlaceholderKind{KindPolicy, "a JumpCloud policy", true}},
	{"GROUP_ID", PlaceholderKind{KindUserGroup, "a JumpCloud user group", true}},
}

// freeTextDescriptions give the unresolvable markers a useful prompt instead
// of a bare "free text".
var freeTextDescriptions = map[string]string{
	"DEPARTMENT_NAME": "a department name, matched against the user's department attribute",
	"HEADER_VALUE":    "an HTTP header value sent to the connector",
	"IT_OPS_EMAIL":    "an email address to notify",
	"PATH":            "the connector endpoint path, e.g. /api/v1/incidents",
	"CONNECTOR":       "a JumpCloud connector object ID (no jc command lists these yet)",
	"STATUS_ID":       "an asset status ID",
}

// markerPrefix is the prefix every template placeholder carries.
const markerPrefix = "REPLACE_WITH_"

// numericSuffixRE matches the trailing index on markers like
// REPLACE_WITH_GROUP_ID_1, which templates use when they bind several groups.
var numericSuffixRE = regexp.MustCompile(`_\d+$`)

// ClassifyPlaceholder reports what a marker expects. It accepts either the
// full marker or the bare name, so a caller can pass what a user typed.
func ClassifyPlaceholder(marker string) PlaceholderKind {
	bare := strings.TrimPrefix(strings.TrimSpace(marker), markerPrefix)
	bare = numericSuffixRE.ReplaceAllString(bare, "")

	for _, r := range placeholderRules {
		if strings.HasSuffix(bare, r.suffix) {
			return r.kind
		}
	}

	desc := freeTextDescriptions[bare]
	if desc == "" {
		desc = "a literal value"
	}
	return PlaceholderKind{Kind: KindFreeText, Describe: desc}
}

// NormalizeMarker returns the full REPLACE_WITH_* form of a marker name, so
// --set COMMAND_ID and --set REPLACE_WITH_COMMAND_ID mean the same thing.
func NormalizeMarker(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		return ""
	}
	if strings.HasPrefix(n, markerPrefix) {
		return n
	}
	return markerPrefix + n
}

// PlaceholderKinds returns every distinct marker in the document with what it
// expects, in sorted order so output and prompts are stable.
func (d DSL) PlaceholderKinds() map[string]PlaceholderKind {
	out := map[string]PlaceholderKind{}
	for _, p := range d.Placeholders() {
		if _, seen := out[p.Marker]; !seen {
			out[p.Marker] = ClassifyPlaceholder(p.Marker)
		}
	}
	return out
}

// PlaceholderMarkers returns the distinct markers, sorted.
func (d DSL) PlaceholderMarkers() []string {
	kinds := d.PlaceholderKinds()
	out := make([]string, 0, len(kinds))
	for m := range kinds {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Fill substitutes marker values throughout the document.
//
// Substitution is textual because markers appear anywhere a string can — task
// parameters, schedule conditions, email bodies, connector paths — and a typed
// walk would have to know every one of those positions.
//
// It is all-or-nothing: an unknown key, or a marker left unfilled, is an error
// and nothing is returned. A half-filled workflow is worse than none, because
// it would fail at run time rather than here.
func (d DSL) Fill(values map[string]string) (json.RawMessage, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("encoding dsl: %w", err)
	}

	present := d.PlaceholderKinds()

	var unknown []string
	normalized := make(map[string]string, len(values))
	for k, v := range values {
		marker := NormalizeMarker(k)
		if _, ok := present[marker]; !ok {
			unknown = append(unknown, k)
			continue
		}
		normalized[marker] = v
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("this template has no placeholder(s) %s; it has: %s",
			strings.Join(unknown, ", "), strings.Join(d.PlaceholderMarkers(), ", "))
	}

	// Longest marker first: REPLACE_WITH_GROUP_ID is a prefix of
	// REPLACE_WITH_GROUP_ID_1, so replacing the short one first would corrupt
	// the long one into a value followed by a stray "_1".
	markers := make([]string, 0, len(normalized))
	for m := range normalized {
		markers = append(markers, m)
	}
	sort.Slice(markers, func(i, j int) bool {
		if len(markers[i]) != len(markers[j]) {
			return len(markers[i]) > len(markers[j])
		}
		return markers[i] < markers[j]
	})

	out := string(raw)
	for _, m := range markers {
		// The value goes through JSON encoding so quotes and backslashes in a
		// name cannot break out of the string they land in. Strip exactly the
		// two delimiting quotes rather than trimming: a value ENDING in a
		// quote would otherwise lose its escaping and leave a dangling
		// backslash.
		enc, err := json.Marshal(normalized[m])
		if err != nil {
			return nil, fmt.Errorf("encoding value for %s: %w", m, err)
		}
		out = strings.ReplaceAll(out, m, string(enc[1:len(enc)-1]))
	}

	filled := json.RawMessage(out)
	remaining, err := ParseDSL(filled)
	if err != nil {
		return nil, fmt.Errorf("filled dsl no longer parses: %w", err)
	}
	if left := remaining.PlaceholderMarkers(); len(left) > 0 {
		return nil, fmt.Errorf("still unfilled: %s", strings.Join(left, ", "))
	}
	return filled, nil
}
