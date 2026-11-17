// Package featuresettings reads the organization feature settings
// (GET /api/v2/feature-settings): per-feature toggles and parameters such as
// whether the AI Gateway ("mcp"), the user MCP server ("user_mcp") or identity
// risk detection are enabled.
//
// Live contract (probed 2026-11-17, test org):
//
//   - The response is {"features": [{name, category, settings: [{name, value}]}]}.
//     value is any JSON (bool, number, string, object, array).
//   - Only two filter fields work: name and category. The spec's own example
//     (feature.name:eq:mcp&setting.name:eq:enabled) returns HTTP 500, as does
//     every other unknown field. The server answers an unsupported field with a
//     500, not a 400.
//   - Only eq is reliable. ne:mcp returned mcp itself and dropped two other
//     features, in:mcp|user_mcp returned only mcp, and search:mcp missed
//     user_mcp. So this package sends eq filters and nothing else.
//   - Values are case sensitive (category:eq:AI matches nothing).
//
// The PUT /feature-settings/{feature}/{setting} write exists in the spec but is
// not implemented: it flips org-wide feature switches (the AI Gateway itself
// among them) and was not probed.
package featuresettings

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
)

// Endpoint is the V2 path of the feature-settings list.
const Endpoint = "/feature-settings"

var namePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// ValidName reports whether s looks like a feature or category name
// (lower-case letters, digits and underscores, as every live name is).
func ValidName(s string) bool { return namePattern.MatchString(s) }

// ErrInvalidName explains a rejected --name/--category value.
func ErrInvalidName(kind, s string) error {
	return fmt.Errorf("%q is not a valid feature %s — expected lower-case letters, digits and underscores (e.g. mcp, user_mcp, ai)", s, kind)
}

// Path builds the list path with eq filters for the non-empty arguments.
// Callers validate the values with ValidName first.
func Path(name, category string) string {
	v := url.Values{}
	if name != "" {
		v.Add("filter", "name:eq:"+name)
	}
	if category != "" {
		v.Add("filter", "category:eq:"+category)
	}
	if len(v) == 0 {
		return Endpoint
	}
	return Endpoint + "?" + v.Encode()
}

// Setting is one name/value pair of a feature.
type Setting struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// Feature is one feature with its settings.
type Feature struct {
	Name     string    `json:"name"`
	Category string    `json:"category"`
	Settings []Setting `json:"settings"`
}

// Parse decodes the {"features": [...]} envelope. It returns each feature as
// the server sent it (raw) alongside the decoded form.
func Parse(raw json.RawMessage) ([]json.RawMessage, []Feature, error) {
	var env struct {
		Features []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, nil, fmt.Errorf("unexpected feature-settings response: not a {features} envelope: %w", err)
	}
	if env.Features == nil {
		return nil, nil, fmt.Errorf("unexpected feature-settings response: no features field")
	}
	decoded := make([]Feature, 0, len(env.Features))
	for i, f := range env.Features {
		var ft Feature
		if err := json.Unmarshal(f, &ft); err != nil {
			return nil, nil, fmt.Errorf("unexpected feature-settings response: feature %d: %w", i, err)
		}
		decoded = append(decoded, ft)
	}
	return env.Features, decoded, nil
}

// Flatten renders features as one row per setting:
// {feature, category, setting, value}, for tables and CSV.
func Flatten(features []Feature) ([]json.RawMessage, error) {
	var rows []json.RawMessage
	for _, f := range features {
		for _, s := range f.Settings {
			b, err := json.Marshal(map[string]any{
				"feature":  f.Name,
				"category": f.Category,
				"setting":  s.Name,
				"value":    s.Value,
			})
			if err != nil {
				return nil, err
			}
			rows = append(rows, b)
		}
	}
	return rows, nil
}

// FlatFields is the column order for Flatten rows.
var FlatFields = []string{"feature", "category", "setting", "value"}
