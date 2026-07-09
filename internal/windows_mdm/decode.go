package windows_mdm

import (
	"encoding/json"
	"fmt"
)

// Decoding existing Windows custom policies (KLA-464) — the reverse
// of the create path, powering the TUI's list+edit flow. Far simpler
// than the Apple side's decoder: the stored values are structured
// JSON (uriList = array of {uri,format,value} triples, customRegTable
// = array of registry rows), not a base64 plist.

// PolicyKind discriminates the two Windows custom-policy shapes.
type PolicyKind string

const (
	PolicyKindOMAURI   PolicyKind = "oma-uri"
	PolicyKindRegistry PolicyKind = "registry"
)

// DecodedPolicy is an existing JumpCloud Windows custom policy pulled
// apart for editing. Exactly one of Settings/Keys is populated,
// matching Kind.
type DecodedPolicy struct {
	PolicyID     string
	PolicyName   string
	Kind         PolicyKind
	TemplateName string
	// Settings holds the uriList entries (Kind == oma-uri).
	Settings []OMAURISetting
	// Keys holds the customRegTable rows (Kind == registry).
	Keys []RegistryKey
}

// DecodeCustomWindowsPolicy parses a GET /policies/{id} response into
// a DecodedPolicy. Values are located by configFieldName
// (uriList / customRegTable), never by field ID — IDs are per-catalog
// artifacts the same way they are on the create path.
func DecodeCustomWindowsPolicy(raw json.RawMessage) (DecodedPolicy, error) {
	var p struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Template struct {
			Name string `json:"name"`
		} `json:"template"`
		Values []policyConfigValue `json:"values"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return DecodedPolicy{}, fmt.Errorf("decoding policy JSON: %w", err)
	}

	out := DecodedPolicy{
		PolicyID:     p.ID,
		PolicyName:   p.Name,
		TemplateName: p.Template.Name,
	}

	switch p.Template.Name {
	case TemplateNameOMAURI:
		out.Kind = PolicyKindOMAURI
		rawList, err := findConfigField(p.Values, fieldNameOMAURI)
		if err != nil {
			return DecodedPolicy{}, err
		}
		if err := json.Unmarshal(rawList, &out.Settings); err != nil {
			return DecodedPolicy{}, fmt.Errorf("decoding %s entries: %w", fieldNameOMAURI, err)
		}
		if len(out.Settings) == 0 {
			return DecodedPolicy{}, fmt.Errorf("policy %q has an empty %s — nothing to edit", p.Name, fieldNameOMAURI)
		}
	case TemplateNameRegistry:
		out.Kind = PolicyKindRegistry
		rawTable, err := findConfigField(p.Values, fieldNameRegistry)
		if err != nil {
			return DecodedPolicy{}, err
		}
		if err := json.Unmarshal(rawTable, &out.Keys); err != nil {
			return DecodedPolicy{}, fmt.Errorf("decoding %s rows: %w", fieldNameRegistry, err)
		}
		if len(out.Keys) == 0 {
			return DecodedPolicy{}, fmt.Errorf("policy %q has an empty %s — nothing to edit", p.Name, fieldNameRegistry)
		}
	default:
		return DecodedPolicy{}, fmt.Errorf(
			"policy %q uses template %q — not a Windows custom policy (want %s or %s)",
			p.Name, p.Template.Name, TemplateNameOMAURI, TemplateNameRegistry)
	}
	return out, nil
}

// policyConfigValue is one values[] entry of a stored policy.
type policyConfigValue struct {
	ConfigFieldName string          `json:"configFieldName"`
	Value           json.RawMessage `json:"value"`
}

func findConfigField(values []policyConfigValue, name string) (json.RawMessage, error) {
	for _, v := range values {
		if v.ConfigFieldName == name {
			return v.Value, nil
		}
	}
	return nil, fmt.Errorf("policy has no configField named %q — JumpCloud may have renamed the field", name)
}
