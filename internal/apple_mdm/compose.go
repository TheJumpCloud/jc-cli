package apple_mdm

import (
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ComposeConfig describes a multi-payload Configuration profile —
// the input shape for `jc apple-mdm payloads compose`. Mirrors the
// envelope+payloads structure ProfileCreator-style tools emit, but
// expressed in YAML/JSON so it version-controls cleanly alongside
// runbooks.
//
// One file → one Configuration envelope → N inner payloads. The
// fields here map 1:1 to EnvelopeOpts + []PayloadInstance so loading
// is a thin walk over Apple's schema catalog with per-payload value
// validation.
type ComposeConfig struct {
	// Name is the profile's display name (shown in System Settings →
	// Profiles, and used as the policy name on --create-policy).
	// Required — the validator rejects an empty name to avoid
	// shipping an unlabeled profile.
	Name string `yaml:"name" json:"name"`

	// Identifier is the profile's reverse-DNS identifier. Optional;
	// EnvelopeOpts auto-generates `jc.<uuid>` when blank, matching
	// the create-policy single-payload behavior.
	Identifier string `yaml:"identifier,omitempty" json:"identifier,omitempty"`

	// Organization is rendered into the profile metadata. Optional.
	Organization string `yaml:"organization,omitempty" json:"organization,omitempty"`

	// RemovalDisallowed prevents end users from removing the profile
	// via System Settings — requires MDM unenroll. Same flag the
	// single-payload create-policy --removal-disallowed sets.
	RemovalDisallowed bool `yaml:"removal_disallowed,omitempty" json:"removal_disallowed,omitempty"`

	// Payloads is the ordered list of inner payloads. Each entry
	// names an Apple PayloadType, supplies values, and may override
	// the per-payload display name. Order is preserved verbatim into
	// PayloadContent so the same .mobileconfig produced today is
	// produced tomorrow.
	Payloads []ComposePayload `yaml:"payloads" json:"payloads"`
}

// ComposePayload is one entry under ComposeConfig.Payloads. Mirrors
// PayloadInstance but with the schema referenced by string Type
// instead of the resolved Payload struct — the loader does the
// catalog lookup.
type ComposePayload struct {
	// Type is the Apple PayloadType (e.g. "com.apple.security.firewall").
	// For ambiguous types (com.apple.MCX has 6 variants) the loader
	// errors with the same disambiguation message as `payloads show`;
	// the operator should switch to the catalog ID in that case.
	Type string `yaml:"type" json:"type"`

	// ID is an optional catalog ID override (the filename-derived
	// identifier, e.g. "com.apple.MCX(EnergySaver)"). When set, this
	// disambiguates MCX-style variants. Type is still required for
	// human-readable config readability.
	ID string `yaml:"id,omitempty" json:"id,omitempty"`

	// DisplayName overrides the per-payload PayloadDisplayName. When
	// blank the emitter falls back to Schema.Title (the same default
	// the single-payload `template` command uses).
	DisplayName string `yaml:"display_name,omitempty" json:"display_name,omitempty"`

	// Values maps Apple key names to user-supplied values. Same
	// shape CoerceAndValidate consumes elsewhere in this package.
	// May be nil/empty if every required key has a schema default.
	Values map[string]any `yaml:"values,omitempty" json:"values,omitempty"`
}

// LoadComposeConfig reads a compose-profile YAML/JSON file from disk
// and unmarshals it into a ComposeConfig. yaml.v3 accepts JSON as a
// strict YAML subset, so this handles both extensions without sniffing.
func LoadComposeConfig(path string) (*ComposeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading compose config %s: %w", path, err)
	}
	var cfg ComposeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing compose config %s: %w", path, err)
	}
	return &cfg, nil
}

// BuildPayloadInstances resolves each ComposePayload against the
// catalog, runs CoerceAndValidate on its values, and returns the
// ready-to-emit []PayloadInstance plus the matching EnvelopeOpts.
//
// Validation is aggressive: every error across every payload is
// reported together rather than failing on the first one. Apple's
// schemas are detailed enough that the operator wants to see every
// invalid key in one pass, not iterate one-fix-per-edit.
//
// The returned PayloadInstance slice preserves config order, which
// preserves the emitted PayloadContent order. Some Apple payloads
// rely on order (e.g. SSO extension before identity providers); we
// take what the operator wrote rather than reorganizing.
func (c *ComposeConfig) BuildPayloadInstances(cat *Catalog) ([]PayloadInstance, EnvelopeOpts, error) {
	if c.Name == "" {
		return nil, EnvelopeOpts{}, fmt.Errorf("compose config: 'name' is required")
	}
	if len(c.Payloads) == 0 {
		return nil, EnvelopeOpts{}, fmt.Errorf("compose config: 'payloads' must contain at least one entry")
	}

	env := EnvelopeOpts{
		DisplayName:       c.Name,
		Identifier:        c.Identifier,
		Organization:      c.Organization,
		RemovalDisallowed: c.RemovalDisallowed,
	}

	instances := make([]PayloadInstance, 0, len(c.Payloads))
	var errs []string

	for i, p := range c.Payloads {
		// Empty type catches a common operator error (missed entry
		// in YAML) early with a useful position.
		if p.Type == "" {
			errs = append(errs, fmt.Sprintf("payloads[%d]: 'type' is required", i))
			continue
		}
		schema, err := resolveComposePayload(cat, p)
		if err != nil {
			errs = append(errs, fmt.Sprintf("payloads[%d] (%s): %v", i, p.Type, err))
			continue
		}
		typed, err := CoerceAndValidate(schema, p.Values)
		if err != nil {
			errs = append(errs, fmt.Sprintf("payloads[%d] (%s): %v", i, p.Type, err))
			continue
		}
		instances = append(instances, PayloadInstance{
			Schema:      schema,
			Values:      typed,
			DisplayName: p.DisplayName,
		})
	}

	if len(errs) > 0 {
		return nil, EnvelopeOpts{}, fmt.Errorf("compose config validation failed:\n  - %s",
			strings.Join(errs, "\n  - "))
	}
	return instances, env, nil
}

// UnsupportedPayloadTypes returns the payload types among instances
// that do not declare support for the given Apple platform ("macOS",
// "iOS" — the catalog's verbatim keys). Callers refuse to create a
// policy when this is non-empty: JumpCloud would accept the profile
// but the device silently ignores unsupported payloads (Bugbot PR #51
// / #59 lineage). Shared by compose create-policy and bundle apply.
func UnsupportedPayloadTypes(instances []PayloadInstance, applePlatform string) []string {
	var unsupported []string
	for _, p := range instances {
		sup, ok := p.Schema.SupportedOS[applePlatform]
		if !ok || !sup.Available() {
			unsupported = append(unsupported, p.Schema.Type)
		}
	}
	return unsupported
}

// resolveComposePayload picks the right catalog entry for a compose
// payload, preferring an explicit ID when supplied, falling back to
// PayloadType lookup with the same disambiguation as `payloads show`.
func resolveComposePayload(cat *Catalog, p ComposePayload) (Payload, error) {
	if p.ID != "" {
		schema, ok := cat.ByID(p.ID)
		if !ok {
			return Payload{}, fmt.Errorf("no catalog entry with ID %q", p.ID)
		}
		// Cross-check: ID's PayloadType must match the operator's
		// declared Type. A mismatch is almost always a copy-paste
		// error and would silently ship the wrong shape.
		if schema.Type != p.Type {
			return Payload{}, fmt.Errorf(
				"ID %q resolves to payloadtype %q, but config declares %q",
				p.ID, schema.Type, p.Type)
		}
		return schema, nil
	}

	variants := cat.VariantsOf(p.Type)
	switch len(variants) {
	case 0:
		return Payload{}, fmt.Errorf("no payload with type %q in catalog (release %s)",
			p.Type, cat.Release)
	case 1:
		return variants[0], nil
	default:
		ids := make([]string, 0, len(variants))
		for _, v := range variants {
			ids = append(ids, v.ID)
		}
		return Payload{}, fmt.Errorf(
			"payloadtype %q is ambiguous (%d variants); set 'id' to one of: %s",
			p.Type, len(variants), strings.Join(ids, ", "))
	}
}
