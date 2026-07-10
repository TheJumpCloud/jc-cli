package bundle

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// Unit drift states.
const (
	StateInSync  = "in-sync"
	StateDrifted = "drifted"
	StateMissing = "missing"
)

// UnitStatus is one unit's drift verdict.
type UnitStatus struct {
	Unit       string   `json:"unit"`
	PolicyName string   `json:"policy_name"`
	PolicyID   string   `json:"policy_id,omitempty"`
	State      string   `json:"state"`
	Diffs      []string `json:"diffs,omitempty"`
}

// StatusReport is the full tenant-vs-bundle comparison.
type StatusReport struct {
	Bundle          string `json:"bundle"`
	Version         string `json:"version"`
	PolicyGroupID   string `json:"policy_group_id"`
	PolicyGroupName string `json:"policy_group_name"`
	// MatchedByMarker is true when the policy group was found via its
	// bundle:<name>@<version> description marker (rename-proof); false
	// means the default-name fallback matched.
	MatchedByMarker bool         `json:"matched_by_marker"`
	Units           []UnitStatus `json:"units"`
	// Orphans are policy-group members whose names match no bundle
	// unit — usually leftovers from a bundle edit.
	Orphans []string `json:"orphans,omitempty"`
	InSync  bool     `json:"in_sync"`
}

// Status compares the tenant's applied state against the bundle
// definition. Read-only: policy-group lookup → member list → per-policy
// GET + decode + diff.
func Status(ctx context.Context, client *api.V2Client, b *Bundle, cat *apple_mdm.Catalog) (*StatusReport, error) {
	gid, gname, byMarker, err := findAppliedPolicyGroup(ctx, client, b)
	if err != nil {
		return nil, err
	}
	report := &StatusReport{
		Bundle:          b.Name,
		Version:         b.Version,
		PolicyGroupID:   gid,
		PolicyGroupName: gname,
		MatchedByMarker: byMarker,
	}

	memberIDs, err := listPolicyGroupMembers(ctx, client, gid)
	if err != nil {
		return nil, err
	}

	// Fetch every member once; index the raw responses by policy name.
	type member struct {
		id  string
		raw json.RawMessage
	}
	byName := make(map[string]member, len(memberIDs))
	for _, id := range memberIDs {
		raw, err := client.Get(ctx, "/policies/"+id)
		if err != nil {
			return nil, fmt.Errorf("fetching policy %s: %w", id, err)
		}
		var meta struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("decoding policy %s: %w", id, err)
		}
		byName[meta.Name] = member{id: id, raw: raw}
	}

	claimed := make(map[string]bool, len(b.Policies))
	allSync := true
	for i := range b.Policies {
		u := &b.Policies[i]
		us := UnitStatus{Unit: u.Name, PolicyName: PolicyName(b, u.Name)}
		m, ok := byName[us.PolicyName]
		if !ok {
			us.State = StateMissing
			allSync = false
			report.Units = append(report.Units, us)
			continue
		}
		claimed[us.PolicyName] = true
		us.PolicyID = m.id

		diffs, err := diffUnit(u, m.raw, cat)
		if err != nil {
			return nil, fmt.Errorf("diffing %q against policy %s: %w", u.Name, m.id, err)
		}
		if len(diffs) == 0 {
			us.State = StateInSync
		} else {
			us.State = StateDrifted
			us.Diffs = diffs
			allSync = false
		}
		report.Units = append(report.Units, us)
	}

	for name := range byName {
		if !claimed[name] {
			report.Orphans = append(report.Orphans, name)
		}
	}
	sort.Strings(report.Orphans)
	report.InSync = allSync && len(report.Orphans) == 0
	return report, nil
}

// findAppliedPolicyGroup locates the bundle's policy group: first by
// the provenance marker in the description (survives renames), then by
// the default name (covers pre-marker or hand-made groups).
func findAppliedPolicyGroup(ctx context.Context, client *api.V2Client, b *Bundle) (id, name string, byMarker bool, err error) {
	result, err := client.ListAll(ctx, "/policygroups", api.V2ListOptions{})
	if err != nil {
		return "", "", false, fmt.Errorf("listing policy groups: %w", err)
	}
	marker := ProvenanceMarker(b)
	defaultName := DefaultPolicyGroupName(b)
	var nameMatch struct{ id, name string }
	for _, raw := range result.Data {
		var g struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(raw, &g); err != nil {
			continue
		}
		if g.Description == marker {
			return g.ID, g.Name, true, nil
		}
		if g.Name == defaultName && nameMatch.id == "" {
			nameMatch.id, nameMatch.name = g.ID, g.Name
		}
	}
	if nameMatch.id != "" {
		return nameMatch.id, nameMatch.name, false, nil
	}
	return "", "", false, fmt.Errorf(
		"no policy group found for bundle %s v%s (looked for description %q, then name %q) — was the bundle applied?",
		b.Name, b.Version, marker, defaultName)
}

// listPolicyGroupMembers returns the policy IDs in a policy group.
func listPolicyGroupMembers(ctx context.Context, client *api.V2Client, groupID string) ([]string, error) {
	raw, err := client.Get(ctx, "/policygroups/"+groupID+"/members")
	if err != nil {
		return nil, fmt.Errorf("listing policy group members: %w", err)
	}
	var members []struct {
		To struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"to"`
	}
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, fmt.Errorf("decoding policy group members: %w", err)
	}
	ids := make([]string, 0, len(members))
	for _, m := range members {
		if m.To.Type == "policy" && m.To.ID != "" {
			ids = append(ids, m.To.ID)
		}
	}
	return ids, nil
}

// diffUnit compares one bundle unit against its tenant policy.
func diffUnit(u *PolicyUnit, raw json.RawMessage, cat *apple_mdm.Catalog) ([]string, error) {
	switch u.Type {
	case UnitAppleProfile:
		return diffAppleUnit(u, raw, cat)
	case UnitWindowsOMAURI:
		return diffOMAURIUnit(u, raw)
	case UnitWindowsRegistry:
		return diffRegistryUnit(u, raw)
	}
	return nil, fmt.Errorf("unknown unit type %q", u.Type)
}

// diffAppleUnit compares payload-by-payload: expected instances are
// rebuilt from the compose config through the same validation path
// apply used, then compared against the decoded profile as
// canonicalized values (never base64 bytes — PayloadUUIDs regenerate
// on every emit).
func diffAppleUnit(u *PolicyUnit, raw json.RawMessage, cat *apple_mdm.Catalog) ([]string, error) {
	decoded, err := apple_mdm.DecodeProfilePayloads(raw)
	if err != nil {
		return nil, err
	}

	instances, _, err := u.Profile.BuildPayloadInstances(cat)
	if err != nil {
		return nil, err
	}

	var diffs []string
	if len(decoded.Payloads) != len(instances) {
		diffs = append(diffs, fmt.Sprintf("payload count: tenant has %d, bundle defines %d",
			len(decoded.Payloads), len(instances)))
		return diffs, nil
	}
	for i, inst := range instances {
		got := decoded.Payloads[i]
		if got.Type != inst.Schema.Type {
			diffs = append(diffs, fmt.Sprintf("payload[%d]: tenant type %s, bundle type %s",
				i, got.Type, inst.Schema.Type))
			continue
		}
		if !jsonEqual(got.Values, inst.Values) {
			diffs = append(diffs, fmt.Sprintf("payload[%d] (%s): values differ — tenant %s, bundle %s",
				i, got.Type, canonJSON(got.Values), canonJSON(inst.Values)))
		}
	}

	// Redispatch only exists on the darwin template; iOS policies
	// always decode false, so comparing there would be a false drift.
	if u.OS == "macOS" {
		want := u.Redispatch == nil || *u.Redispatch
		if decoded.Redispatch != want {
			diffs = append(diffs, fmt.Sprintf("redispatch: tenant %v, bundle %v", decoded.Redispatch, want))
		}
	}
	return diffs, nil
}

// diffOMAURIUnit set-diffs settings keyed by URI.
func diffOMAURIUnit(u *PolicyUnit, raw json.RawMessage) ([]string, error) {
	decoded, err := windows_mdm.DecodeCustomWindowsPolicy(raw)
	if err != nil {
		return nil, err
	}
	if decoded.Kind != windows_mdm.PolicyKindOMAURI {
		return []string{fmt.Sprintf("tenant policy is %s, bundle unit is OMA-URI", decoded.Kind)}, nil
	}
	expected, err := windows_mdm.NormalizeAndValidateSettings(u.WindowsSettings())
	if err != nil {
		return nil, err
	}

	got := make(map[string]windows_mdm.OMAURISetting, len(decoded.Settings))
	for _, s := range decoded.Settings {
		got[s.URI] = s
	}
	var diffs []string
	seen := make(map[string]bool, len(expected))
	for _, want := range expected {
		seen[want.URI] = true
		g, ok := got[want.URI]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("setting %s: missing on tenant", want.URI))
			continue
		}
		if g.Format != want.Format || g.Value != want.Value {
			diffs = append(diffs, fmt.Sprintf("setting %s: tenant %s=%q, bundle %s=%q",
				want.URI, g.Format, g.Value, want.Format, want.Value))
		}
	}
	for _, s := range decoded.Settings {
		if !seen[s.URI] {
			diffs = append(diffs, fmt.Sprintf("setting %s: on tenant but not in bundle", s.URI))
		}
	}
	return diffs, nil
}

// diffRegistryUnit set-diffs rows keyed by location + value name.
func diffRegistryUnit(u *PolicyUnit, raw json.RawMessage) ([]string, error) {
	decoded, err := windows_mdm.DecodeCustomWindowsPolicy(raw)
	if err != nil {
		return nil, err
	}
	if decoded.Kind != windows_mdm.PolicyKindRegistry {
		return []string{fmt.Sprintf("tenant policy is %s, bundle unit is registry", decoded.Kind)}, nil
	}
	expected, err := windows_mdm.NormalizeAndValidateKeys(u.WindowsKeys())
	if err != nil {
		return nil, err
	}

	rowKey := func(k windows_mdm.RegistryKey) string { return k.Location + `\` + k.ValueName }
	got := make(map[string]windows_mdm.RegistryKey, len(decoded.Keys))
	for _, k := range decoded.Keys {
		got[rowKey(k)] = k
	}
	var diffs []string
	seen := make(map[string]bool, len(expected))
	for _, want := range expected {
		seen[rowKey(want)] = true
		g, ok := got[rowKey(want)]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("key %s: missing on tenant", rowKey(want)))
			continue
		}
		if g.RegType != want.RegType || g.Data != want.Data {
			diffs = append(diffs, fmt.Sprintf("key %s: tenant %s=%q, bundle %s=%q",
				rowKey(want), g.RegType, g.Data, want.RegType, want.Data))
		}
	}
	for _, k := range decoded.Keys {
		if !seen[rowKey(k)] {
			diffs = append(diffs, fmt.Sprintf("key %s: on tenant but not in bundle", rowKey(k)))
		}
	}
	return diffs, nil
}

// jsonEqual compares two values by canonical JSON — encoding/json
// sorts map keys, and numeric types that differ only in Go
// representation (int64 vs uint64 vs float64 from the plist and
// schema-coercion paths) marshal identically.
func jsonEqual(a, b any) bool {
	return canonJSON(a) == canonJSON(b)
}

func canonJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
