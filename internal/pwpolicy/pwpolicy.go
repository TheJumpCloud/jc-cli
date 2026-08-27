// Package pwpolicy holds the JumpCloud password-policy wire contract shared by
// the CLI (internal/cmd) and the MCP server (internal/mcp), so the two
// surfaces cannot drift on endpoints, the two different response envelopes, or
// the enable/value field pairing.
//
// Verified live against org 5ec71e8e96bfda0611fc6c5b on 2026-08-27 (KLA-485
// password-policy probe). The OpenAPI spec is wrong or silent in three places:
//
//   - GET /passwordpolicies is NOT in the spec at all, but it exists and is the
//     only way to discover policy IDs. It answers {results:[…]} — a "results"
//     envelope, not the {totalCount, <key>:[…]} used elsewhere in V2.
//   - The list projection is FLAT and SPARSE: policy fields sit at the top
//     level next to objectId/groupCount, and only the fields the policy
//     actually enables are present. It is not the {objectId, policy:{…}}
//     shape returned by every single-policy read, so the two cannot share a
//     decoder. See ListItem vs Policy.
//   - Single reads (by id, by user, by user group) all return the SAME
//     {cached, objectId, groups:[{groupId,name}], policy:{…}} envelope, with
//     the full 33-field policy regardless of which enable* toggles are set.
//
// Write semantics (whether PUT replaces or merges the policy object) were NOT
// verified live: creating and mutating password policies on a real tenant was
// out of scope for the probe. Callers must therefore use MergePolicy, which
// sends the complete policy object with only the requested fields changed.
// That is correct under either semantic, and matches the full-replace
// behaviour already confirmed on sibling V2 endpoints (samba domains, AD
// translation rules, org device settings).
package pwpolicy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Endpoint is the password policy collection.
const Endpoint = "/passwordpolicies"

// PrecedenceEndpoint sets the evaluation order across policies.
const PrecedenceEndpoint = "/passwordpolicies/precedence/set"

// PolicyEndpoint is a single policy by object ID.
func PolicyEndpoint(objectID string) string { return Endpoint + "/" + objectID }

// UserEndpoint is the policy that currently governs a single user.
func UserEndpoint(userID string) string { return Endpoint + "/user/" + userID }

// UserGroupEndpoint is the policy that currently governs a user group.
func UserGroupEndpoint(groupID string) string { return Endpoint + "/usergroup/" + groupID }

// ListDefaultFields is the default field subset shown for the list projection.
// These are the fields the flat list projection always carries.
var ListDefaultFields = []string{"objectId", "name", "precedence", "default", "groupCount", "minLength"}

// Policy is the full policy object carried under the "policy" key of every
// single-policy read and write. Every field is a pointer-free value because
// the API always sends all 33 fields on reads; MergePolicy relies on that to
// round-trip a complete object.
type Policy struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
	Precedence  int    `json:"precedence"`
	// EffectiveDate is server-assigned; it is echoed back on writes.
	EffectiveDate string `json:"effectiveDate,omitempty"`

	// Composition requirements.
	MinLength                           int  `json:"minLength"`
	EnableMinLength                     bool `json:"enableMinLength"`
	NeedsLowercase                      bool `json:"needsLowercase"`
	NeedsUppercase                      bool `json:"needsUppercase"`
	NeedsNumeric                        bool `json:"needsNumeric"`
	NeedsSymbolic                       bool `json:"needsSymbolic"`
	AllowUsernameSubstring              bool `json:"allowUsernameSubstring"`
	DisallowCommonlyUsedPasswords       bool `json:"disallowCommonlyUsedPasswords"`
	DisallowSequentialOrRepetitiveChars bool `json:"disallowSequentialOrRepetitiveChars"`

	// Expiration and history.
	PasswordExpirationInDays       int  `json:"passwordExpirationInDays"`
	EnablePasswordExpirationInDays bool `json:"enablePasswordExpirationInDays"`
	MaxHistory                     int  `json:"maxHistory"`
	EnableMaxHistory               bool `json:"enableMaxHistory"`
	MinChangePeriodInDays          int  `json:"minChangePeriodInDays"`
	EnableMinChangePeriodInDays    bool `json:"enableMinChangePeriodInDays"`

	// Lockout.
	MaxLoginAttempts           int  `json:"maxLoginAttempts"`
	EnableMaxLoginAttempts     bool `json:"enableMaxLoginAttempts"`
	LockoutTimeInSeconds       int  `json:"lockoutTimeInSeconds"`
	EnableLockoutTimeInSeconds bool `json:"enableLockoutTimeInSeconds"`
	ResetLockoutCounterMinutes int  `json:"resetLockoutCounterMinutes"`
	EnableResetLockoutCounter  bool `json:"enableResetLockoutCounter"`

	// Reset and recovery.
	DaysBeforeExpirationToForceReset       int  `json:"daysBeforeExpirationToForceReset"`
	EnableDaysBeforeExpirationToForceReset bool `json:"enableDaysBeforeExpirationToForceReset"`
	DaysAfterExpirationToSelfRecover       int  `json:"daysAfterExpirationToSelfRecover"`
	EnableDaysAfterExpirationToSelfRecover bool `json:"enableDaysAfterExpirationToSelfRecover"`
	EnableRecoveryEmail                    bool `json:"enableRecoveryEmail"`
	AllowUnenrolledMFAPasswordReset        bool `json:"allowUnenrolledMFAPasswordReset"`
	DisplayComplexityOnResetScreen         bool `json:"displayComplexityOnResetScreen"`
}

// Group is a user group bound to a policy.
type Group struct {
	GroupID string `json:"groupId"`
	Name    string `json:"name"`
}

// Detail is the envelope returned by every single-policy read: by object ID,
// by user, and by user group all share it.
type Detail struct {
	Cached   bool    `json:"cached"`
	ObjectID string  `json:"objectId"`
	Groups   []Group `json:"groups"`
	Policy   Policy  `json:"policy"`
}

// GroupIDs returns just the bound group IDs, in the order the API listed them.
// Writes carry groupIds while reads carry groups, so an update that means to
// leave bindings alone has to translate between the two.
func (d Detail) GroupIDs() []string {
	if len(d.Groups) == 0 {
		return nil
	}
	ids := make([]string, 0, len(d.Groups))
	for _, g := range d.Groups {
		ids = append(ids, g.GroupID)
	}
	return ids
}

// ListItem is the flat, sparse list projection. It is deliberately separate
// from Policy: the list omits every field the policy does not enable, so a
// zero here means "not reported", not "set to zero". Use Detail for real
// values.
type ListItem struct {
	ObjectID   string `json:"objectId"`
	Name       string `json:"name"`
	Precedence int    `json:"precedence"`
	Default    bool   `json:"default"`
	GroupCount int    `json:"groupCount"`
}

// ParseList unwraps the {results:[…]} envelope returned by GET /passwordpolicies.
func ParseList(raw json.RawMessage) ([]json.RawMessage, error) {
	var env struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parsing password policy list: %w", err)
	}
	return env.Results, nil
}

// ParseListItems decodes the list projection into typed items.
func ParseListItems(raw json.RawMessage) ([]ListItem, error) {
	rows, err := ParseList(raw)
	if err != nil {
		return nil, err
	}
	items := make([]ListItem, 0, len(rows))
	for _, r := range rows {
		var it ListItem
		if err := json.Unmarshal(r, &it); err != nil {
			return nil, fmt.Errorf("parsing password policy list item: %w", err)
		}
		items = append(items, it)
	}
	return items, nil
}

// ParseDetail decodes a single-policy read.
func ParseDetail(raw json.RawMessage) (Detail, error) {
	var d Detail
	if err := json.Unmarshal(raw, &d); err != nil {
		return Detail{}, fmt.Errorf("parsing password policy: %w", err)
	}
	return d, nil
}

// Body builds the request body shared by create (POST) and update (PUT).
// groupIDs is sent whenever it is non-nil, so an update that read the current
// bindings can pass them straight back and leave them untouched.
func Body(p Policy, groupIDs []string) map[string]any {
	body := map[string]any{"policy": p}
	if groupIDs != nil {
		body["groupIds"] = groupIDs
	}
	return body
}

// PrecedenceEntry is one element of the precedence/set array body, which is a
// bare JSON array rather than an object.
type PrecedenceEntry struct {
	ObjectID   string `json:"objectId"`
	Precedence int    `json:"precedence"`
}

// BatchDeleteBody is the body of DELETE /passwordpolicies.
func BatchDeleteBody(objectIDs []string) map[string]any {
	return map[string]any{"objectIds": objectIDs}
}

// ValueEnables pairs each value field with the boolean that switches it on.
// The API stores the two independently, so a value written without its enable
// flag is silently inert — setting minLength=16 while enableMinLength is false
// changes nothing a user would notice. ApplyChanges closes that trap.
var ValueEnables = map[string]string{
	"minLength":                        "enableMinLength",
	"passwordExpirationInDays":         "enablePasswordExpirationInDays",
	"maxHistory":                       "enableMaxHistory",
	"minChangePeriodInDays":            "enableMinChangePeriodInDays",
	"maxLoginAttempts":                 "enableMaxLoginAttempts",
	"lockoutTimeInSeconds":             "enableLockoutTimeInSeconds",
	"resetLockoutCounterMinutes":       "enableResetLockoutCounter",
	"daysBeforeExpirationToForceReset": "enableDaysBeforeExpirationToForceReset",
	"daysAfterExpirationToSelfRecover": "enableDaysAfterExpirationToSelfRecover",
}

// Settable lists every policy field a caller may change, as JSON keys.
// ApplyChanges rejects anything outside this set, so a typo fails loudly
// instead of being dropped on the floor by the server.
var Settable = map[string]bool{
	"name": true, "description": true,
	"minLength": true, "needsLowercase": true, "needsUppercase": true,
	"needsNumeric": true, "needsSymbolic": true, "allowUsernameSubstring": true,
	"disallowCommonlyUsedPasswords": true, "disallowSequentialOrRepetitiveChars": true,
	"passwordExpirationInDays": true, "maxHistory": true, "minChangePeriodInDays": true,
	"maxLoginAttempts": true, "lockoutTimeInSeconds": true, "resetLockoutCounterMinutes": true,
	"daysBeforeExpirationToForceReset": true, "daysAfterExpirationToSelfRecover": true,
	"enableRecoveryEmail": true, "allowUnenrolledMFAPasswordReset": true,
	"displayComplexityOnResetScreen": true,
}

func init() {
	// Every enable* toggle is settable on its own, so a caller can switch a
	// requirement off without having to restate its value.
	for _, enable := range ValueEnables {
		Settable[enable] = true
	}
}

// ApplyChanges returns a copy of cur with changes applied by JSON key.
//
// It is the single merge path for both surfaces: read the current policy, hand
// it here with only what changed, and send the complete result back. That is
// correct whether the API's PUT replaces or merges, which live probing did not
// establish (see the package doc).
//
// Setting a value field also switches on its paired enable flag unless the
// caller set that flag explicitly in the same call — so `--min-length 16`
// takes effect, while `--min-length 16 --enable-min-length=false` still lets a
// caller stage a value they have not turned on yet.
func ApplyChanges(cur Policy, changes map[string]any) (Policy, error) {
	if len(changes) == 0 {
		return cur, fmt.Errorf("no fields to change")
	}

	unknown := make([]string, 0)
	for k := range changes {
		if !Settable[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return cur, fmt.Errorf("unknown password policy field(s): %s", strings.Join(unknown, ", "))
	}

	raw, err := json.Marshal(cur)
	if err != nil {
		return cur, fmt.Errorf("encoding current policy: %w", err)
	}
	var merged map[string]any
	if err := json.Unmarshal(raw, &merged); err != nil {
		return cur, fmt.Errorf("decoding current policy: %w", err)
	}

	for k, v := range changes {
		merged[k] = v
		if enable, ok := ValueEnables[k]; ok {
			if _, explicit := changes[enable]; !explicit {
				merged[enable] = true
			}
		}
	}

	out, err := json.Marshal(merged)
	if err != nil {
		return cur, fmt.Errorf("encoding merged policy: %w", err)
	}
	var next Policy
	if err := json.Unmarshal(out, &next); err != nil {
		return cur, fmt.Errorf("decoding merged policy: %w", err)
	}
	return next, nil
}

// Diff reports the fields that differ between two policies, as human-readable
// "field: old -> new" lines, sorted for stable plan and confirmation output.
func Diff(before, after Policy) []string {
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	var bm, am map[string]any
	_ = json.Unmarshal(b, &bm)
	_ = json.Unmarshal(a, &am)

	var out []string
	for k, av := range am {
		bv, ok := bm[k]
		if ok && fmt.Sprintf("%v", bv) == fmt.Sprintf("%v", av) {
			continue
		}
		out = append(out, fmt.Sprintf("%s: %v -> %v", k, bv, av))
	}
	sort.Strings(out)
	return out
}

// Describe renders a one-line summary of the requirements a policy enforces,
// skipping every setting its enable flag leaves off.
func (p Policy) Describe() string {
	var parts []string
	if p.EnableMinLength {
		parts = append(parts, fmt.Sprintf("min length %d", p.MinLength))
	}
	for _, c := range []struct {
		on   bool
		name string
	}{
		{p.NeedsLowercase, "lowercase"},
		{p.NeedsUppercase, "uppercase"},
		{p.NeedsNumeric, "numeric"},
		{p.NeedsSymbolic, "symbolic"},
	} {
		if c.on {
			parts = append(parts, c.name)
		}
	}
	if p.EnablePasswordExpirationInDays {
		parts = append(parts, fmt.Sprintf("expires after %dd", p.PasswordExpirationInDays))
	}
	if p.EnableMaxHistory {
		parts = append(parts, fmt.Sprintf("last %d reused blocked", p.MaxHistory))
	}
	if p.EnableMaxLoginAttempts {
		parts = append(parts, fmt.Sprintf("lockout after %d attempts", p.MaxLoginAttempts))
	}
	if len(parts) == 0 {
		return "no requirements enabled"
	}
	return strings.Join(parts, ", ")
}

// FindByName returns the first policy whose name matches, case-insensitively.
// Policy names are not unique in the API, and the default policy ships with an
// empty name, so callers should prefer object IDs where they have them.
func FindByName(items []ListItem, name string) (ListItem, bool) {
	for _, it := range items {
		if strings.EqualFold(it.Name, name) {
			return it, true
		}
	}
	return ListItem{}, false
}
