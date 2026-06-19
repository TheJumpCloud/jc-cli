package apple_mdm

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// ParseValuePairs parses --values key=value pairs into a string map.
// Order-preserving via slice input (Cobra StringSliceVar). Returns an
// error on missing '=' separators so a typo doesn't silently drop a
// value the operator thought they set.
func ParseValuePairs(pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		i := strings.IndexByte(p, '=')
		if i <= 0 {
			return nil, fmt.Errorf("invalid --values entry %q: want key=value", p)
		}
		key := strings.TrimSpace(p[:i])
		val := p[i+1:]
		if key == "" {
			return nil, fmt.Errorf("invalid --values entry %q: empty key", p)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("duplicate --values key %q", key)
		}
		out[key] = val
	}
	return out, nil
}

// MergeValues combines --values-file JSON with --values key=value
// pairs. Scalar pairs win on collision (more specific by virtue of
// being on the command line). The file shape is a flat JSON object;
// nested values must come from the file (--values is scalar-only by
// design — bash quoting nested JSON is a UX disaster).
func MergeValues(fileValues map[string]any, pairs map[string]string) map[string]any {
	merged := make(map[string]any, len(fileValues)+len(pairs))
	for k, v := range fileValues {
		merged[k] = v
	}
	// Pairs come in as untyped strings; coerce happens later against
	// the schema in CoerceAndValidate. Stash them here so collisions
	// resolve cleanly.
	for k, v := range pairs {
		merged[k] = v
	}
	return merged
}

// CoerceAndValidate walks the payload schema and converts each
// user-supplied value to the right Go type, validating against the
// schema's Range/RangeList/Presence as it goes. Returns the typed map
// ready for EmitMobileconfig, or an aggregated error with one line
// per problem.
//
// Coercion contract:
//   - String fields accept any string (no conversion).
//   - Boolean fields accept Go bool, or strings "true"/"false" (case-insensitive).
//   - Integer fields accept Go int/float64 (whole), or strings parseable as int.
//   - Real fields accept Go int/float64, or strings parseable as float.
//   - Array/Dictionary fields accept the corresponding Go types
//     verbatim (only --values-file can supply these).
//   - Data fields accept strings (already base64-encoded).
//
// Required keys missing from values produce errors. Unknown keys
// (not in the schema) produce errors — typos shouldn't silently ship
// to a device.
func CoerceAndValidate(p Payload, values map[string]any) (map[string]any, error) {
	// Build a fast lookup so we can detect unknown keys.
	known := make(map[string]Key, len(p.Keys))
	for _, k := range p.Keys {
		known[k.Name] = k
	}

	var errs []string
	out := make(map[string]any, len(values))

	// Check each supplied value against the schema.
	for name, raw := range values {
		k, ok := known[name]
		if !ok {
			errs = append(errs, fmt.Sprintf("unknown key %q for payloadtype %s", name, p.Type))
			continue
		}
		v, err := coerceOne(k, raw)
		if err != nil {
			errs = append(errs, fmt.Sprintf("key %q: %v", name, err))
			continue
		}
		if err := validateConstraints(k, v); err != nil {
			errs = append(errs, fmt.Sprintf("key %q: %v", name, err))
			continue
		}
		out[name] = v
	}

	// Required keys must be supplied.
	for _, k := range p.Keys {
		if strings.ToLower(k.Presence) != "required" {
			continue
		}
		if _, ok := out[k.Name]; !ok {
			errs = append(errs, fmt.Sprintf("required key %q missing", k.Name))
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("value validation failed for %s:\n  - %s",
			p.Type, strings.Join(errs, "\n  - "))
	}
	return out, nil
}

// coerceOne converts a raw value (Go-typed from JSON or string from
// CLI) into the typed shape Apple's schema expects.
func coerceOne(k Key, raw any) (any, error) {
	switch k.Type {
	case "string":
		switch x := raw.(type) {
		case string:
			return x, nil
		default:
			// Allow JSON numbers/bools to render as strings when the
			// schema is loose. Apple sometimes uses string-typed
			// fields for numeric-looking IDs (e.g. SSIDs).
			return fmt.Sprintf("%v", x), nil
		}

	case "boolean":
		switch x := raw.(type) {
		case bool:
			return x, nil
		case string:
			b, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(x)))
			if err != nil {
				return nil, fmt.Errorf("boolean expected, got %q", x)
			}
			return b, nil
		}
		return nil, fmt.Errorf("boolean expected, got %T", raw)

	case "integer":
		switch x := raw.(type) {
		case int:
			return x, nil
		case int64:
			return int(x), nil
		case float64:
			if x != float64(int64(x)) {
				return nil, fmt.Errorf("integer expected, got non-whole %v", x)
			}
			return int(x), nil
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(x))
			if err != nil {
				return nil, fmt.Errorf("integer expected, got %q", x)
			}
			return n, nil
		}
		return nil, fmt.Errorf("integer expected, got %T", raw)

	case "real":
		switch x := raw.(type) {
		case float32:
			return float64(x), nil
		case float64:
			return x, nil
		case int:
			return float64(x), nil
		case int64:
			return float64(x), nil
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			if err != nil {
				return nil, fmt.Errorf("real expected, got %q", x)
			}
			return f, nil
		}
		return nil, fmt.Errorf("real expected, got %T", raw)

	case "data":
		// Accept already-base64-encoded strings. The validator
		// doesn't decode/re-encode — that would force a roundtrip
		// for the user-uploaded mobileconfig case (PR3) where we
		// already have the bytes pre-encoded.
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("data expected (base64 string), got %T", raw)
		}
		return s, nil

	case "date":
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("date expected (RFC3339 string), got %T", raw)
		}
		return s, nil

	case "array":
		// Arrays come from --values-file only; the validator passes
		// them through and emitter handles nested element typing via
		// inferType.
		switch x := raw.(type) {
		case []any:
			return x, nil
		case string:
			// Allow a JSON-encoded array as a fallback so the user
			// can pass arrays via --values key='["a","b"]' if they
			// really want.
			return parseJSONScalar(x, "array")
		}
		return nil, fmt.Errorf("array expected, got %T", raw)

	case "dictionary":
		switch x := raw.(type) {
		case map[string]any:
			return x, nil
		case string:
			return parseJSONScalar(x, "dictionary")
		}
		return nil, fmt.Errorf("dictionary expected, got %T", raw)

	case "any", "":
		// Schema is silent — accept whatever the user provided.
		return raw, nil

	default:
		return nil, fmt.Errorf("unsupported schema type %q", k.Type)
	}
}

// parseJSONScalar handles the --values key='[1,2,3]' case where the
// user passed a JSON-encoded array or dict as a string.
func parseJSONScalar(s, expect string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("%s expected, JSON parse failed: %w", expect, err)
	}
	if expect == "array" {
		if _, ok := v.([]any); !ok {
			return nil, fmt.Errorf("array expected, JSON value is %T", v)
		}
	}
	if expect == "dictionary" {
		if _, ok := v.(map[string]any); !ok {
			return nil, fmt.Errorf("dictionary expected, JSON value is %T", v)
		}
	}
	return v, nil
}

// validateConstraints checks the value against schema-declared rangelist
// (enum) and range (numeric bounds). Per-key descriptions for
// constraint violations name the actual value so the operator can see
// which one tripped — much more useful than "value out of range."
func validateConstraints(k Key, v any) error {
	if len(k.RangeList) > 0 {
		if !inRangeList(v, k.RangeList) {
			vals := make([]string, 0, len(k.RangeList))
			for _, x := range k.RangeList {
				vals = append(vals, fmt.Sprintf("%v", x))
			}
			return fmt.Errorf("value %v not in allowed list {%s}", v, strings.Join(vals, ", "))
		}
	}
	if k.Range != nil {
		// Compare against ranges as float64 to keep the code simple;
		// the schema's Range.Min/Max are loosely typed (int or float
		// from YAML).
		f, ok := toFloat(v)
		if !ok {
			return fmt.Errorf("range constraint requires numeric value, got %T", v)
		}
		if min, ok := toFloat(k.Range.Min); ok && f < min {
			return fmt.Errorf("value %v below minimum %v", v, k.Range.Min)
		}
		if max, ok := toFloat(k.Range.Max); ok && f > max {
			return fmt.Errorf("value %v above maximum %v", v, k.Range.Max)
		}
	}
	return nil
}

// inRangeList uses reflect.DeepEqual so strings, bools, ints, and
// floats all compare correctly. Apple's rangelists carry the literal
// YAML type (e.g. `rangelist: [foo, bar]` parses as []string,
// `rangelist: [1, 2, 3]` parses as []int).
func inRangeList(v any, allowed []any) bool {
	for _, a := range allowed {
		if reflect.DeepEqual(v, a) {
			return true
		}
	}
	return false
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}
