package identityrisk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// SettingsEndpoint is the Access Risk Detection settings collection. GET lists
// every setting of the identity_risk_detection feature; PUT on a child sets one.
const SettingsEndpoint = "/access-risk-settings"

// SettingEndpoint addresses one setting for a PUT.
func SettingEndpoint(name string) string { return SettingsEndpoint + "/" + url.PathEscape(name) }

// Setting is one row of the settings listing.
type Setting struct {
	Name     string          `json:"name"`
	Value    json.RawMessage `json:"value"`
	Category string          `json:"category,omitempty"`
}

// ParseSettings reads {"features":[{"name","category","settings":...}]} into
// one row per setting. The spec does not say what "settings" holds, so both
// plausible shapes are accepted: an object keyed by setting name, or a list of
// {"name","value"} objects. Rows are sorted by name for stable output.
func ParseSettings(raw json.RawMessage) ([]Setting, error) {
	var env struct {
		Features []struct {
			Name     string          `json:"name"`
			Category string          `json:"category"`
			Settings json.RawMessage `json:"settings"`
		} `json:"features"`
	}
	if err := decodeEnvelope(raw, &env, "access risk settings"); err != nil {
		return nil, err
	}
	var out []Setting
	for _, f := range env.Features {
		s := bytes.TrimSpace(f.Settings)
		if len(s) == 0 || string(s) == "null" {
			continue
		}
		cat := f.Category
		if cat == "" {
			cat = f.Name
		}
		switch s[0] {
		case '{':
			var m map[string]json.RawMessage
			if err := json.Unmarshal(s, &m); err != nil {
				return nil, fmt.Errorf("access risk settings: unreadable settings object: %w", err)
			}
			for k, v := range m {
				out = append(out, Setting{Name: k, Value: v, Category: cat})
			}
		case '[':
			var l []struct {
				Name  string          `json:"name"`
				Value json.RawMessage `json:"value"`
			}
			if err := json.Unmarshal(s, &l); err != nil {
				return nil, fmt.Errorf("access risk settings: unreadable settings list: %w", err)
			}
			for _, e := range l {
				out = append(out, Setting{Name: e.Name, Value: e.Value, Category: cat})
			}
		default:
			return nil, fmt.Errorf("access risk settings: unexpected settings shape in feature %q", f.Name)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// SettingNames lists the names, for the "valid names" error.
func SettingNames(rows []Setting) []string {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names
}

// FindSetting returns the named setting or an error listing the valid names.
func FindSetting(rows []Setting, name string) (Setting, error) {
	for _, r := range rows {
		if r.Name == name {
			return r, nil
		}
	}
	return Setting{}, fmt.Errorf("unknown access risk setting %q — the server returned: %s",
		name, strings.Join(SettingNames(rows), ", "))
}

// ParseValue parses --value / --value-file content as exactly one JSON value.
func ParseValue(text string) (json.RawMessage, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("the value is empty: it must be one JSON value, e.g. true or [\"203.0.113.0/24\"]")
	}
	dec := json.NewDecoder(strings.NewReader(text))
	var v json.RawMessage
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("the value is not valid JSON (%v) — quote strings, e.g. '\"text\"'", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("the value is not valid JSON: more than one JSON value given")
	}
	return v, nil
}

// JSONType names the JSON type of a raw value.
func JSONType(v json.RawMessage) string {
	s := bytes.TrimSpace(v)
	if len(s) == 0 {
		return "null"
	}
	switch s[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	}
	return "number"
}

// CheckType rejects a new value whose JSON type differs from the current one.
func CheckType(name string, current, next json.RawMessage) error {
	ct, nt := JSONType(current), JSONType(next)
	if ct == "null" || ct == nt {
		return nil
	}
	return fmt.Errorf("setting %q holds a %s, but the new value is a %s — the JSON type must match", name, ct, nt)
}

// SameValue compares two raw JSON values semantically (whitespace and key
// order do not matter).
func SameValue(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return bytes.Equal(xb, yb)
}

// DisableWarning is shown when the master toggle is being switched off.
const DisableWarning = "This will switch Access Risk Detection off for the whole organization."

// IsDisabling reports whether the change turns the master toggle off.
func IsDisabling(name string, next json.RawMessage) bool {
	return name == "enabled" && string(bytes.TrimSpace(next)) == "false"
}
