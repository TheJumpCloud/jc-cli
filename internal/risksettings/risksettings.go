// Package risksettings reads and changes the Access Risk (identity risk
// detection) settings: GET /access-risk-settings and PUT
// /access-risk-settings/{name}. Probed 2026-11-19:
//
//   - The listing is {features: [{name: identity_risk_detection, category:
//     itdr, settings: [{name, value}, ...]}]}, about 30 settings, including
//     the master "enabled" switch.
//   - PUT takes {"value": v} and answers {name, value}. The value is typed:
//     a number sent as a string is a 400 "invalid feature/setting or value";
//     an unknown name is a 400 "invalid setting".
//
// So a change is typed from the setting's current value, and read back.
package risksettings

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const Path = "/access-risk-settings"

// SettingPath is one setting's update path.
func SettingPath(name string) string { return Path + "/" + name }

// Setting is one name/value pair.
type Setting struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// Parse flattens the listing into settings, carrying the feature name.
func Parse(raw []byte) ([]Setting, error) {
	var env struct {
		Features *[]struct {
			Name     string    `json:"name"`
			Settings []Setting `json:"settings"`
		} `json:"features"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("the access risk settings response is not an object: %w", err)
	}
	if env.Features == nil {
		return nil, fmt.Errorf("the access risk settings response has no features")
	}
	var out []Setting
	for _, f := range *env.Features {
		out = append(out, f.Settings...)
	}
	return out, nil
}

// Find returns one setting by name.
func Find(settings []Setting, name string) (Setting, error) {
	for _, s := range settings {
		if s.Name == name {
			return s, nil
		}
	}
	return Setting{}, fmt.Errorf("no access risk setting %q", name)
}

// Coerce turns the text a person typed into a value of the same JSON type as
// the setting's current value. A list's elements take the type of its current
// first element; a setting with no value (null) has no type to follow, so it
// takes a JSON literal only.
func Coerce(current any, input string) (any, error) {
	input = strings.TrimSpace(input)
	switch cur := current.(type) {
	case bool:
		b, err := strconv.ParseBool(input)
		if err != nil {
			return nil, fmt.Errorf("this setting is true or false, not %q", input)
		}
		return b, nil
	case float64:
		n, err := strconv.ParseInt(input, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("this setting is a whole number, not %q", input)
		}
		return n, nil
	case string:
		return input, nil
	case nil:
		var v any
		if err := json.Unmarshal([]byte(input), &v); err != nil {
			return nil, fmt.Errorf("this setting has no value yet, so jc cannot tell its type: give a JSON "+
				"literal (true, 90, \"text\" or [\"a\",\"b\"]), not %q", input)
		}
		return v, nil
	case []any:
		var items []any
		if strings.HasPrefix(input, "[") {
			if err := json.Unmarshal([]byte(input), &items); err != nil {
				return nil, fmt.Errorf("this setting is a list; give a JSON array or comma-separated values: %w", err)
			}
		} else {
			items = []any{}
			for _, p := range strings.Split(input, ",") {
				if p = strings.TrimSpace(p); p != "" {
					items = append(items, p)
				}
			}
		}
		if len(cur) == 0 {
			// No element to follow. Text is the common case (IP ranges); a
			// list that reads as numbers is ambiguous, so ask for JSON.
			if !strings.HasPrefix(input, "[") {
				for _, it := range items {
					if _, err := strconv.ParseFloat(it.(string), 64); err == nil {
						return nil, fmt.Errorf("this list is empty, so jc cannot tell whether %q is a number "+
							"or text: give a JSON array, e.g. [13335] or [\"13335\"]", it)
					}
				}
			}
			return items, nil
		}
		out := make([]any, len(items))
		for i, it := range items {
			s, isText := it.(string)
			if !isText {
				b, _ := json.Marshal(it)
				s = string(b)
			}
			v, err := Coerce(cur[0], s)
			if err != nil {
				return nil, fmt.Errorf("list element %d: %w", i+1, err)
			}
			out[i] = v
		}
		return out, nil
	}
	return nil, fmt.Errorf("this setting has a value type jc does not know how to set")
}

// Equal compares a read-back value with the one sent.
func Equal(got, want any) bool {
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	return string(a) == string(b)
}
