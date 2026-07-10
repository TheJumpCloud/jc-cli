// Package bundle implements security baseline bundles: named,
// versioned YAML artifacts that group N policy units (Apple
// multi-payload profiles + Windows OMA-URI / registry policies) into
// one apply-able set (KLA-468).
//
// The package deliberately owns no validation logic of its own beyond
// bundle structure: Apple payload values are validated by
// apple_mdm.ComposeConfig.BuildPayloadInstances against the embedded
// schema catalog, and Windows settings/keys by
// windows_mdm.NormalizeAndValidateSettings/Keys — the exact code paths
// the per-policy create commands use, so a bundle that validates here
// creates cleanly there.
//
// Licensing note: builtin bundles must carry a Source block naming
// their origin. CIS Benchmark content (or "CIS" branding) must never
// land in builtin/ — see KLA-468 for the licensing gate; license-safe
// derivations (NIST mSCP, DISA STIG) arrive via KLA-473/474.
package bundle

import (
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// Policy unit types. Each maps onto one JumpCloud custom-policy
// template family.
const (
	UnitAppleProfile    = "apple_profile"    // custom_mdm_profile_darwin / _ios
	UnitWindowsOMAURI   = "windows_oma_uri"  // custom_oma_uri_mdm_windows
	UnitWindowsRegistry = "windows_registry" // custom_registry_keys_policy_windows
)

// Source records where a bundle's content came from — licensing
// provenance is a first-class field because it decides what may ship
// as a builtin (see package doc).
type Source struct {
	// Origin is builtin, user, or imported. The loader stamps it when
	// the author left it blank, so list output always shows one.
	Origin      string `yaml:"origin,omitempty" json:"origin,omitempty"`
	Attribution string `yaml:"attribution,omitempty" json:"attribution,omitempty"`
	License     string `yaml:"license,omitempty" json:"license,omitempty"`
	URL         string `yaml:"url,omitempty" json:"url,omitempty"`
}

// OMAURISetting mirrors windows_mdm.OMAURISetting with YAML tags. The
// wire struct is JSON-only on purpose (its tags are the JumpCloud
// field names); this is the same friendly-name indirection the
// --settings-file JSON and the MCP tools use.
type OMAURISetting struct {
	URI    string `yaml:"uri" json:"uri"`
	Format string `yaml:"format" json:"format"`
	Value  string `yaml:"value" json:"value"`
}

// RegistryKey mirrors windows_mdm.RegistryKey with the friendly field
// names the --key flag syntax uses (location/name/type/data), not the
// customLocation/customValueName wire names.
type RegistryKey struct {
	Location string `yaml:"location" json:"location"`
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"`
	Data     string `yaml:"data" json:"data"`
}

// PolicyUnit is one policy-to-be inside a bundle. Exactly one of
// Profile / Settings / Keys is set, matching Type.
type PolicyUnit struct {
	// Name is required and unique within the bundle; apply composes
	// the tenant policy name as "<bundle>/<unit>".
	Name string `yaml:"name" json:"name"`
	// Type is one of the Unit* constants.
	Type string `yaml:"type" json:"type"`

	// OS applies to apple_profile units only: macOS (default) or iOS.
	OS string `yaml:"os,omitempty" json:"os,omitempty"`
	// Redispatch applies to apple_profile units only; nil means true,
	// matching the compose command's default.
	Redispatch *bool `yaml:"redispatch,omitempty" json:"redispatch,omitempty"`
	// Profile is the multi-payload profile definition, verbatim the
	// shape `jc apple-mdm payloads compose` consumes. Its name may be
	// omitted; Parse defaults it to the unit name.
	Profile *apple_mdm.ComposeConfig `yaml:"profile,omitempty" json:"profile,omitempty"`

	// Settings is the OMA-URI triple list for windows_oma_uri units.
	Settings []OMAURISetting `yaml:"settings,omitempty" json:"settings,omitempty"`

	// Keys is the registry row list for windows_registry units.
	Keys []RegistryKey `yaml:"keys,omitempty" json:"keys,omitempty"`
}

// Bundle is one baseline: metadata + N policy units.
type Bundle struct {
	Name        string       `yaml:"name" json:"name"`
	Version     string       `yaml:"version" json:"version"`
	Description string       `yaml:"description,omitempty" json:"description,omitempty"`
	Source      *Source      `yaml:"source,omitempty" json:"source,omitempty"`
	Policies    []PolicyUnit `yaml:"policies" json:"policies"`
}

// WindowsSettings converts a unit's OMA-URI settings to the wire
// struct windows_mdm validation and policy building consume.
func (u *PolicyUnit) WindowsSettings() []windows_mdm.OMAURISetting {
	out := make([]windows_mdm.OMAURISetting, len(u.Settings))
	for i, s := range u.Settings {
		out[i] = windows_mdm.OMAURISetting{URI: s.URI, Format: s.Format, Value: s.Value}
	}
	return out
}

// WindowsKeys converts a unit's registry rows to the wire struct.
func (u *PolicyUnit) WindowsKeys() []windows_mdm.RegistryKey {
	out := make([]windows_mdm.RegistryKey, len(u.Keys))
	for i, k := range u.Keys {
		out[i] = windows_mdm.RegistryKey{Location: k.Location, ValueName: k.Name, RegType: k.Type, Data: k.Data}
	}
	return out
}

// Platforms reports the distinct OS families the bundle touches, in
// a stable order (macOS, iOS, windows) — list/show summary data.
func (b *Bundle) Platforms() []string {
	var mac, ios, win bool
	for _, u := range b.Policies {
		switch u.Type {
		case UnitAppleProfile:
			if u.OS == "iOS" {
				ios = true
			} else {
				mac = true
			}
		case UnitWindowsOMAURI, UnitWindowsRegistry:
			win = true
		}
	}
	var out []string
	if mac {
		out = append(out, "macOS")
	}
	if ios {
		out = append(out, "iOS")
	}
	if win {
		out = append(out, "windows")
	}
	return out
}

// Parse unmarshals bundle YAML (or JSON — yaml.v3 accepts it) and
// runs structural validation. All structural problems are reported
// together, matching the repo's aggregate-errors convention.
func Parse(data []byte) (*Bundle, error) {
	var b Bundle
	if err := yaml.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parsing bundle: %w", err)
	}
	if err := b.normalizeAndCheck(); err != nil {
		return nil, err
	}
	return &b, nil
}

// ParseFile reads and parses a bundle file from disk.
func ParseFile(path string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading bundle %s: %w", path, err)
	}
	b, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return b, nil
}

// MarshalYAML serializes a bundle back to YAML (export path).
func MarshalYAML(b *Bundle) ([]byte, error) {
	return yaml.Marshal(b)
}

// normalizeAndCheck enforces bundle structure and normalizes the few
// convenience defaults (unit OS casing, profile name fallback). It
// deliberately does NOT validate payload values or Windows formats —
// that's Validate's job, which needs the Apple catalog.
func (b *Bundle) normalizeAndCheck() error {
	var errs []string
	if b.Name == "" {
		errs = append(errs, "'name' is required")
	}
	if b.Version == "" {
		errs = append(errs, "'version' is required")
	}
	if len(b.Policies) == 0 {
		errs = append(errs, "'policies' must contain at least one entry")
	}

	seen := make(map[string]bool, len(b.Policies))
	for i := range b.Policies {
		u := &b.Policies[i]
		at := fmt.Sprintf("policies[%d]", i)
		if u.Name == "" {
			errs = append(errs, at+": 'name' is required")
		} else {
			at = fmt.Sprintf("policies[%d] (%s)", i, u.Name)
			if seen[u.Name] {
				errs = append(errs, at+": duplicate unit name")
			}
			seen[u.Name] = true
		}

		hasProfile, hasSettings, hasKeys := u.Profile != nil, len(u.Settings) > 0, len(u.Keys) > 0

		switch u.Type {
		case UnitAppleProfile:
			if !hasProfile {
				errs = append(errs, at+": type apple_profile requires a 'profile' block")
			} else if u.Profile.Name == "" {
				// Author convenience: the unit name doubles as the
				// profile display name unless overridden.
				u.Profile.Name = u.Name
			}
			if hasSettings || hasKeys {
				errs = append(errs, at+": apple_profile takes 'profile' only — remove 'settings'/'keys'")
			}
			switch strings.ToLower(u.OS) {
			case "", "macos":
				u.OS = "macOS"
			case "ios":
				u.OS = "iOS"
			default:
				errs = append(errs, fmt.Sprintf("%s: os %q: want macOS or iOS", at, u.OS))
			}
		case UnitWindowsOMAURI:
			if !hasSettings {
				errs = append(errs, at+": type windows_oma_uri requires a non-empty 'settings' list")
			}
			if hasProfile || hasKeys {
				errs = append(errs, at+": windows_oma_uri takes 'settings' only — remove 'profile'/'keys'")
			}
			errs = append(errs, checkNoAppleFields(at, u)...)
		case UnitWindowsRegistry:
			if !hasKeys {
				errs = append(errs, at+": type windows_registry requires a non-empty 'keys' list")
			}
			if hasProfile || hasSettings {
				errs = append(errs, at+": windows_registry takes 'keys' only — remove 'profile'/'settings'")
			}
			errs = append(errs, checkNoAppleFields(at, u)...)
		case "":
			errs = append(errs, at+": 'type' is required (apple_profile, windows_oma_uri, or windows_registry)")
		default:
			errs = append(errs, fmt.Sprintf("%s: unknown type %q (want apple_profile, windows_oma_uri, or windows_registry)", at, u.Type))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("bundle validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// checkNoAppleFields flags Apple-only fields on Windows units — a
// silent ignore would make the author think os/redispatch applied.
func checkNoAppleFields(at string, u *PolicyUnit) []string {
	var errs []string
	if u.OS != "" {
		errs = append(errs, at+": 'os' applies to apple_profile units only")
	}
	if u.Redispatch != nil {
		errs = append(errs, at+": 'redispatch' applies to apple_profile units only")
	}
	return errs
}
