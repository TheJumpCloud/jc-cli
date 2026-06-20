package apple_mdm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"howett.net/plist"
)

// DecodedPolicy is the digested view of a JumpCloud Custom MDM
// Configuration Profile policy ready for round-tripping through the
// TUI's edit flow. We extract just the fields the form cares about
// (schema, current values, redispatch flag, multi-payload guard) and
// drop the rest of the JC response.
//
// The matching apple_mdm.Payload is resolved from the inner plist's
// PayloadType against the embedded catalog. If no schema matches —
// e.g. the policy uses a vendor-specific Apple payload that isn't in
// our vendored Release-v26.4 — Schema.Type is empty and the editor
// path is the only available follow-up.
type DecodedPolicy struct {
	// PolicyID is the JumpCloud policy id (24-char ObjectID). Needed
	// for the eventual PUT /policies/{id}.
	PolicyID string
	// PolicyName is the JC-side policy name. Pre-populates the form's
	// Name field on entry.
	PolicyName string
	// TemplateName is the JumpCloud template name attached to the
	// policy (e.g. "custom_mdm_profile_darwin",
	// "custom_mdm_profile_ios"). Drives the OS family used at PUT
	// time so an iOS-family policy doesn't get accidentally
	// reassigned to the macOS template on edit (Bugbot PR #54
	// review).
	TemplateName string
	// IsMulti is true when the underlying mobileconfig wraps more than
	// one inner payload (CIS-style bundles). Editing those is out of
	// scope for v1 — the TUI shows a "use the Admin Portal" message
	// and bails. v2 would let the operator pick which inner payload
	// to edit.
	IsMulti bool
	// PayloadType is the Apple type of the inner payload (single-
	// payload policies only). Empty when IsMulti is true.
	PayloadType string
	// Schema is the matching catalog entry for PayloadType. Zero
	// Payload when no match — the operator can still drop to the
	// editor, but the form path is unavailable.
	Schema Payload
	// Values is the decoded inner-payload dict, minus the magic 5
	// Payload* keys the emitter owns. Form pre-population reads from
	// this; what comes out the other end goes back through
	// EmitMobileconfig so the Payload* keys are regenerated.
	Values map[string]any
	// Redispatch reflects the policy's redispatchPolicy value. False
	// when the policy template predates the field. Pre-populates the
	// edit form's redispatch toggle.
	Redispatch bool
	// RemovalDisallowed mirrors the envelope's PayloadRemovalDisallowed
	// when the operator originally set it. JumpCloud server-side
	// re-signing may erase this; we honor whatever the GET tells us.
	RemovalDisallowed bool
}

// reservedPayloadKeysSet is the inverse-of-emit list: keys the emitter
// owns and that we therefore strip from Values during decode so the
// form doesn't show them as editable. Mirrors reservedPayloadKey in
// plist.go.
var reservedPayloadKeysSet = map[string]struct{}{
	"PayloadType":        {},
	"PayloadUUID":        {},
	"PayloadVersion":     {},
	"PayloadIdentifier":  {},
	"PayloadDisplayName": {},
}

// DecodeCustomMDMPolicy parses the raw JSON response from
// `GET /policies/{id}` and produces a DecodedPolicy. Tolerant of older
// policies that predate the redispatchPolicy field; missing wire
// pieces collapse to zero values rather than errors.
//
// The function does NOT need the live catalog (the matching is a
// pure lookup); it's the caller's responsibility to make sure
// apple_mdm.Default() has loaded by the time DecodedPolicy.Schema is
// consumed.
func DecodeCustomMDMPolicy(raw []byte) (DecodedPolicy, error) {
	// Structural unmarshal of just the fields we care about. Going
	// through a typed shape keeps the parser tolerant of JC adding
	// new fields to the policy response without churning this code.
	var resp struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Template struct {
			Name string `json:"name"`
		} `json:"template"`
		Values []struct {
			ConfigFieldID   string `json:"configFieldID"`
			ConfigFieldName string `json:"configFieldName"`
			Value           any    `json:"value"`
		} `json:"values"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return DecodedPolicy{}, fmt.Errorf("parsing policy JSON: %w", err)
	}

	d := DecodedPolicy{
		PolicyID:     resp.ID,
		PolicyName:   resp.Name,
		TemplateName: resp.Template.Name,
	}

	var plistBase64 string
	for _, v := range resp.Values {
		switch v.ConfigFieldName {
		case "payload":
			if s, ok := v.Value.(string); ok {
				plistBase64 = s
			}
		case "redispatchPolicy":
			if b, ok := v.Value.(bool); ok {
				d.Redispatch = b
			}
		}
	}
	if plistBase64 == "" {
		return d, fmt.Errorf("policy %s has no payload values entry", resp.ID)
	}

	plistBytes, err := base64.StdEncoding.DecodeString(plistBase64)
	if err != nil {
		return d, fmt.Errorf("decoding base64 payload: %w", err)
	}

	envelope, err := parsePlistEnvelope(plistBytes)
	if err != nil {
		return d, fmt.Errorf("parsing plist envelope: %w", err)
	}

	if removalDisallowed, ok := envelope["PayloadRemovalDisallowed"].(bool); ok {
		d.RemovalDisallowed = removalDisallowed
	}

	contents, ok := envelope["PayloadContent"].([]any)
	if !ok || len(contents) == 0 {
		return d, fmt.Errorf("plist envelope has no PayloadContent array")
	}
	if len(contents) > 1 {
		d.IsMulti = true
		return d, nil
	}
	inner, ok := contents[0].(map[string]any)
	if !ok {
		return d, fmt.Errorf("PayloadContent[0] is not a dictionary (got %T)", contents[0])
	}
	d.PayloadType, _ = inner["PayloadType"].(string)
	d.Values = stripReservedPayloadKeys(inner)

	// Catalog lookup. Tolerant of empty PayloadType (the field can be
	// absent on malformed but parseable plists) and of catalog misses
	// (vendored release lags behind Apple's latest).
	//
	// For ambiguous PayloadTypes (com.apple.MCX is the canonical
	// case — 6 catalog variants share that type for EnergySaver /
	// FileVault2 / TimeServer / WiFi / Accounts / Mobility), pick
	// the variant whose Keys best match the policy's actual inner
	// values. Pre-fix (Bugbot PR #54 re-review) we used ByType which
	// returns first-wins; an EnergySaver policy could decode against
	// the WiFi variant's schema, leading to wrong field display in
	// the form and dropped keys on save.
	if cat, err := Default(); err == nil && d.PayloadType != "" {
		variants := cat.VariantsOf(d.PayloadType)
		d.Schema = pickBestSchemaVariant(variants, d.Values)
	}
	return d, nil
}

// pickBestSchemaVariant returns the catalog variant whose Keys have
// the largest name overlap with the policy's actual values. For
// payload types with only one variant the choice is trivial; for
// ambiguous ones (com.apple.MCX) the operator's edited keys are the
// strongest hint we have at the right schema.
func pickBestSchemaVariant(variants []Payload, values map[string]any) Payload {
	switch len(variants) {
	case 0:
		return Payload{}
	case 1:
		return variants[0]
	}
	bestIdx, bestScore := 0, -1
	for i, v := range variants {
		keyNames := make(map[string]struct{}, len(v.Keys))
		for _, k := range v.Keys {
			keyNames[k.Name] = struct{}{}
		}
		score := 0
		for k := range values {
			if _, ok := keyNames[k]; ok {
				score++
			}
		}
		if score > bestScore {
			bestScore, bestIdx = score, i
		}
	}
	return variants[bestIdx]
}

// parsePlistEnvelope unmarshals the XML plist into the loose
// map[string]any shape the form path uses. We don't go through a
// typed struct because the Apple Configuration envelope is keyed by
// well-known string keys and we want the inner-payload dict to come
// out as map[string]any (matching what EmitMobileconfig accepts on
// the way back in).
func parsePlistEnvelope(data []byte) (map[string]any, error) {
	var envelope map[string]any
	if _, err := plist.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if envelope == nil {
		return nil, fmt.Errorf("plist root is empty")
	}
	return envelope, nil
}

// OSFamilyFromTemplateName extracts the JumpCloud OS family from a
// custom_mdm_profile_<family> template name. Returns the family
// suffix on success; empty string if the template name doesn't match
// the expected prefix.
//
//	"custom_mdm_profile_darwin"  → "darwin"
//	"custom_mdm_profile_ios"     → "ios"
//	anything else                → ""
func OSFamilyFromTemplateName(templateName string) string {
	const prefix = "custom_mdm_profile_"
	if len(templateName) <= len(prefix) || templateName[:len(prefix)] != prefix {
		return ""
	}
	return templateName[len(prefix):]
}

// stripReservedPayloadKeys returns a shallow copy of inner with the
// Payload* keys the emitter regenerates removed. Editing those would
// silently change PayloadType (the most dangerous case) or invalidate
// the profile identifier; the form shouldn't see them at all.
func stripReservedPayloadKeys(inner map[string]any) map[string]any {
	out := make(map[string]any, len(inner))
	for k, v := range inner {
		if _, reserved := reservedPayloadKeysSet[k]; reserved {
			continue
		}
		out[k] = v
	}
	return out
}
