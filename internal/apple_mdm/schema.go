// Package apple_mdm parses Apple's MDM Configuration Profile schemas
// (vendored from https://github.com/apple/device-management) into a
// typed Go model the CLI can browse, render, and use to emit valid
// .mobileconfig payloads.
//
// The vendored YAML files live under schemas/<Release-tag>/profiles/
// and are embedded into the binary at build time via go:embed (see
// catalog.go) — the binary has zero runtime dependency on the Apple
// repo. To refresh, see schemas/<tag>/NOTICE.md.
package apple_mdm

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Payload is one Apple Configuration Profile schema (one YAML file
// under mdm/profiles/). Fields mirror Apple's schema.yaml meta-schema —
// see https://github.com/apple/device-management/blob/release/docs/schema.md
// for the prose spec.
type Payload struct {
	// ID is the per-file catalog identifier, derived from the source
	// filename (no .yaml extension). Distinct from Type because Apple
	// ships multiple variants under the same payloadtype — e.g. the
	// MCX (Managed Client) schemas split across com.apple.MCX.yaml,
	// com.apple.MCX(FileVault2).yaml, com.apple.MCX(WiFi).yaml, all of
	// which share payloadtype "com.apple.MCX". ID is what makes them
	// individually addressable.
	ID string `json:"id"`
	// Type is Apple's canonical PayloadType written into the
	// .mobileconfig — e.g. "com.apple.wifi.managed". May collide
	// across ID values (see above). For lookups where the caller
	// has the canonical type, Catalog.ByType returns the first match
	// deterministically; the per-variant ID resolves the ambiguity.
	Type string `json:"type"`
	// Title is a short human-readable label, e.g. "Wi-Fi".
	Title string `json:"title,omitempty"`
	// Description is a one-sentence prose summary, suitable for list
	// rendering.
	Description string `json:"description,omitempty"`
	// SupportedOS is the platform support matrix declared at payload
	// level. Per-key SupportedOS entries inherit from this and may
	// override individual fields — see Key.EffectiveSupport.
	SupportedOS SupportedOS `json:"supported_os,omitempty"`
	// Keys is the ordered list of payload keys (PayloadContent dict
	// entries). Order matches the YAML for stable rendering.
	Keys []Key `json:"keys,omitempty"`
}

// SupportedOS is the per-platform support matrix. Keys are Apple's
// canonical platform names: "iOS", "macOS", "tvOS", "visionOS",
// "watchOS". Absence of a key means the platform isn't supported by
// the schema at all (vs. Introduced == "n/a" which means "the platform
// exists but this payload isn't applicable").
type SupportedOS map[string]OSSupport

// OSSupport carries the metadata that gates a payload (or key) on a
// specific platform. Most fields are optional; only Introduced is
// always populated in the source.
type OSSupport struct {
	// Introduced is the OS version the payload first shipped on, e.g.
	// "10.7" for macOS. The sentinel "n/a" means the platform exists
	// but doesn't support this payload/key.
	Introduced string `yaml:"introduced" json:"introduced,omitempty"`
	// Deprecated marks the version Apple stopped recommending the
	// payload/key. Empty if still current.
	Deprecated string `yaml:"deprecated" json:"deprecated,omitempty"`
	// Removed marks the version Apple dropped support entirely. Empty
	// if still functional.
	Removed string `yaml:"removed" json:"removed,omitempty"`

	// Multiple: whether the payload may appear more than once in a
	// single configuration profile (e.g. multiple Wi-Fi networks).
	Multiple bool `yaml:"multiple" json:"multiple,omitempty"`
	// Supervised: whether the payload only applies on supervised
	// devices (enforced via Apple Configurator or DEP).
	Supervised bool `yaml:"supervised" json:"supervised,omitempty"`
	// RequiresDEP: whether the device must be enrolled via Automated
	// Device Enrollment (formerly DEP).
	RequiresDEP bool `yaml:"requiresdep" json:"requires_dep,omitempty"`
	// UserApprovedMDM: whether the payload only applies after the user
	// has explicitly approved MDM (macOS user-approved MDM workflow).
	UserApprovedMDM bool `yaml:"userapprovedmdm" json:"user_approved_mdm,omitempty"`
	// AllowManualInstall: whether the payload can be installed by
	// double-clicking a .mobileconfig (vs MDM-only).
	AllowManualInstall bool `yaml:"allowmanualinstall" json:"allow_manual_install,omitempty"`
	// DeviceChannel / UserChannel: which MDM enrollment channel
	// accepts the payload.
	DeviceChannel bool `yaml:"devicechannel" json:"device_channel,omitempty"`
	UserChannel   bool `yaml:"userchannel" json:"user_channel,omitempty"`

	// SharedIPad / UserEnrollment carry nested mode flags. We only
	// surface the raw map; downstream consumers that care about
	// supervised-iPad or user-enrollment scoping can read it.
	SharedIPad     map[string]any `yaml:"sharedipad" json:"shared_ipad,omitempty"`
	UserEnrollment map[string]any `yaml:"userenrollment" json:"user_enrollment,omitempty"`
}

// Available returns true if the OS supports this payload/key at all.
// Apple uses Introduced=="n/a" as the "supported platform but not this
// payload" sentinel; an absent map entry means "this platform isn't
// even in the matrix." Both are unavailable.
func (s OSSupport) Available() bool {
	return s.Introduced != "" && s.Introduced != "n/a"
}

// Key is one entry under payloadkeys. Apple's schema supports
// arbitrarily nested subkeys (an array of dicts, where each dict has
// its own keys) — we model this recursively.
type Key struct {
	// Name is the key as it appears in the emitted plist, e.g.
	// "AutoJoin" or "SSID_STR".
	Name string `json:"name"`
	// Title is an optional human-readable label.
	Title string `json:"title,omitempty"`
	// Type is Apple's type tag with the angle brackets stripped. One
	// of: "string", "integer", "real", "boolean", "data", "array",
	// "dictionary", "date", "any". Raw source has "<boolean>" form;
	// we normalize on parse so downstream consumers don't have to.
	Type string `json:"type,omitempty"`
	// Presence is "required" or "optional". The Apple meta-schema also
	// permits "deprecated" and "conditional" — we surface them
	// verbatim.
	Presence string `json:"presence,omitempty"`
	// Default is the documented default value when the key is omitted.
	// Type matches Key.Type (bool for boolean, string for string, etc.).
	Default any `json:"default,omitempty"`
	// Content is the key's prose description. May be multi-line
	// markdown. Used by `jc apple-mdm payloads show`.
	Content string `json:"content,omitempty"`
	// RangeList is an enumeration of valid values (Apple's
	// "rangelist:"). Empty for keys that accept any value.
	RangeList []any `json:"range_list,omitempty"`
	// Range is an inclusive numeric range; nil for keys that aren't
	// numerically bounded.
	Range *Range `json:"range,omitempty"`
	// ValueType is a semantic refinement on Type (e.g. "email", "url",
	// "hostname", "regex", "uuid"). Empty for keys without a
	// semantic refinement.
	ValueType string `json:"value_type,omitempty"`
	// SupportedOS is the per-key support matrix. Keys inherit from the
	// parent Payload.SupportedOS; explicit entries here OVERRIDE the
	// inherited values for the matching platform. Use
	// Key.EffectiveSupport to resolve the merged view.
	SupportedOS SupportedOS `json:"supported_os,omitempty"`
	// Subkeys: for array/dictionary types, the nested key schema. An
	// array<dictionary> ships with Subkeys describing each dict's
	// shape; an array<string> ships with a single Subkey describing
	// the string constraints.
	Subkeys []Key `json:"subkeys,omitempty"`
}

// Range is an inclusive numeric range used for validating integer and
// real-typed keys. Min/Max are typed any so we can carry both ints and
// floats verbatim from the YAML — emitter casts based on Key.Type.
type Range struct {
	Min any `yaml:"min" json:"min,omitempty"`
	Max any `yaml:"max" json:"max,omitempty"`
}

// EffectiveSupport returns the per-platform support for this key,
// merging Key.SupportedOS over Payload.SupportedOS. Apple's schema
// allows a key to either fully override a platform entry or just
// shadow specific fields; we apply key-level entries as full
// replacements (Apple's docs say partial-merge semantics are
// undefined, and in practice every key either omits a platform
// entirely or replaces it whole).
func (k Key) EffectiveSupport(parent SupportedOS) SupportedOS {
	merged := make(SupportedOS, len(parent))
	for os, sup := range parent {
		merged[os] = sup
	}
	for os, sup := range k.SupportedOS {
		merged[os] = sup
	}
	return merged
}

// rawPayloadFile is the on-disk YAML shape — we unmarshal into this,
// then normalize into the exported Payload model. Keeping the raw form
// internal lets us evolve the public types without churning the YAML
// loader.
type rawPayloadFile struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Payload     struct {
		PayloadType string      `yaml:"payloadtype"`
		SupportedOS SupportedOS `yaml:"supportedOS"`
	} `yaml:"payload"`
	PayloadKeys []rawKey `yaml:"payloadkeys"`
}

type rawKey struct {
	Key         string      `yaml:"key"`
	Title       string      `yaml:"title"`
	Type        string      `yaml:"type"`
	Presence    string      `yaml:"presence"`
	Default     any         `yaml:"default"`
	Content     string      `yaml:"content"`
	RangeList   []any       `yaml:"rangelist"`
	Range       *Range      `yaml:"range"`
	ValueType   string      `yaml:"valuetype"`
	SupportedOS SupportedOS `yaml:"supportedOS"`
	Subkeys     []rawKey    `yaml:"subkeys"`
}

// ParsePayload loads one Apple schema YAML and normalizes it into a
// Payload. id should be the per-file catalog identifier (filename
// without extension); source is only used in error messages.
func ParsePayload(id, source string, data []byte) (Payload, error) {
	var raw rawPayloadFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Payload{}, fmt.Errorf("%s: yaml: %w", source, err)
	}
	if raw.Payload.PayloadType == "" {
		return Payload{}, fmt.Errorf("%s: missing payload.payloadtype", source)
	}

	p := Payload{
		ID:          id,
		Type:        raw.Payload.PayloadType,
		Title:       raw.Title,
		Description: raw.Description,
		SupportedOS: raw.Payload.SupportedOS,
		Keys:        make([]Key, 0, len(raw.PayloadKeys)),
	}
	for _, rk := range raw.PayloadKeys {
		p.Keys = append(p.Keys, normalizeKey(rk))
	}
	return p, nil
}

// normalizeKey converts a rawKey into the exported Key, stripping the
// angle brackets from the type tag and recursing into Subkeys.
func normalizeKey(rk rawKey) Key {
	k := Key{
		Name:        rk.Key,
		Title:       rk.Title,
		Type:        unwrapType(rk.Type),
		Presence:    rk.Presence,
		Default:     rk.Default,
		Content:     rk.Content,
		RangeList:   rk.RangeList,
		Range:       rk.Range,
		ValueType:   rk.ValueType,
		SupportedOS: rk.SupportedOS,
	}
	if len(rk.Subkeys) > 0 {
		k.Subkeys = make([]Key, len(rk.Subkeys))
		for i, sk := range rk.Subkeys {
			k.Subkeys[i] = normalizeKey(sk)
		}
	}
	return k
}

// unwrapType strips the angle-bracket wrapper Apple uses for type tags
// (`<boolean>` → `boolean`). Unknown shapes pass through verbatim so
// the caller can detect parser drift instead of getting a silent
// default.
func unwrapType(t string) string {
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
		return strings.TrimSuffix(strings.TrimPrefix(t, "<"), ">")
	}
	return t
}
