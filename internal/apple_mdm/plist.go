package apple_mdm

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Hand-rolled plist XML emitter for the constrained Configuration
// Profile subset. The subset is small (8 value types, one envelope
// shape) and the file format is stable — a 200-line emitter beats a
// runtime dep on howett.net/plist for the typed control we need
// (deterministic key order, predictable indentation, injectable UUID
// generation for tests).
//
// What's supported: string, integer, real, boolean, data (base64),
// date (RFC3339), array, dict. The `any` Apple type infers from the
// Go type at emit time.

// plistHeader is Apple's required preamble for every .mobileconfig.
const plistHeader = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
`

// plistFooter closes the root <plist> tag.
const plistFooter = `</plist>
`

// EnvelopeOpts configures the Configuration envelope around one or
// more inner payloads. Mirrors the fields JumpCloud writes when it
// re-signs an uploaded mobileconfig (PayloadDisplayName,
// PayloadIdentifier, PayloadOrganization, PayloadRemovalDisallowed).
type EnvelopeOpts struct {
	// DisplayName is the human-readable name shown in System Settings
	// → Profiles. Should match the policy name JumpCloud will assign.
	DisplayName string
	// Identifier is the profile's reverse-DNS identifier — typically
	// "com.<org>.<purpose>". Empty value autogenerates a "jc.<uuid>"
	// form so each call produces a unique profile that won't shadow
	// an existing one on the device.
	Identifier string
	// Organization is the rendered Organization name in the profile
	// metadata. Optional.
	Organization string
	// RemovalDisallowed: if true, end users can't remove the profile
	// via System Settings — requires MDM unenroll. Matches what
	// JumpCloud sets server-side for managed profiles.
	RemovalDisallowed bool
}

// PayloadInstance is one inner payload to emit inside the envelope's
// PayloadContent array. Schema gates type coercion + the magic 5 keys
// (PayloadType, PayloadUUID, PayloadVersion, PayloadIdentifier,
// PayloadDisplayName); Values supplies user-set keys.
type PayloadInstance struct {
	// Schema is the parsed Apple schema for this payload. PayloadType
	// is sourced from Schema.Type.
	Schema Payload
	// Values is keyed by schema Key.Name and carries Go-typed values
	// the validator coerced from --values / --values-file. Use
	// CoerceValues to build this from string scalars.
	Values map[string]any
	// DisplayName overrides the default per-payload PayloadDisplayName
	// (Schema.Title). Optional.
	DisplayName string
}

// EmitMobileconfig writes a complete .mobileconfig XML document to w
// containing one Configuration envelope around the supplied payload
// instances. The envelope wraps each instance's keys in its own dict
// inside the PayloadContent array, exactly matching what JumpCloud
// re-signs and pushes to devices.
//
// UUIDs use crypto/rand by default; tests override via the newUUID
// package var.
func EmitMobileconfig(w io.Writer, env EnvelopeOpts, payloads []PayloadInstance) error {
	if len(payloads) == 0 {
		return fmt.Errorf("at least one payload is required")
	}
	if _, err := io.WriteString(w, plistHeader); err != nil {
		return err
	}

	// Build the envelope as an in-memory map, then emit it. Going
	// through a typed intermediate makes the key ordering decisions
	// explicit (we sort keys alphabetically by default to match
	// Apple's PropertyListEditor output and keep diffs stable).
	root := map[string]any{
		"PayloadType":    "Configuration",
		"PayloadVersion": 1,
		"PayloadUUID":    newUUID(),
	}
	if env.DisplayName != "" {
		root["PayloadDisplayName"] = env.DisplayName
	}
	if env.Identifier != "" {
		root["PayloadIdentifier"] = env.Identifier
	} else {
		root["PayloadIdentifier"] = "jc." + newUUID()
	}
	if env.Organization != "" {
		root["PayloadOrganization"] = env.Organization
	}
	if env.RemovalDisallowed {
		root["PayloadRemovalDisallowed"] = true
	}

	content := make([]any, 0, len(payloads))
	for _, p := range payloads {
		dict, err := buildPayloadDict(p)
		if err != nil {
			return fmt.Errorf("building payload %s: %w", p.Schema.Type, err)
		}
		content = append(content, dict)
	}
	root["PayloadContent"] = content

	if err := writePlistValue(w, root, "", "dictionary"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	_, err := io.WriteString(w, plistFooter)
	return err
}

// buildPayloadDict assembles one inner-payload dict, adding the magic 5
// keys and merging the user-supplied schema values on top.
//
// Order of precedence on collision: user values win for non-magic keys;
// the magic 5 (PayloadType, PayloadUUID, PayloadVersion,
// PayloadIdentifier, PayloadDisplayName) are owned by the emitter — a
// user-set PayloadType in values would silently override the schema's
// payloadtype, which is a footgun, so we drop user attempts to set them.
func buildPayloadDict(p PayloadInstance) (map[string]any, error) {
	if p.Schema.Type == "" {
		return nil, fmt.Errorf("payload schema has no PayloadType")
	}
	dict := map[string]any{
		"PayloadType":    p.Schema.Type,
		"PayloadVersion": 1,
		"PayloadUUID":    newUUID(),
		"PayloadIdentifier": fmt.Sprintf("jc.%s.%s",
			strings.ReplaceAll(p.Schema.Type, "com.", ""),
			newUUID()),
	}
	if p.DisplayName != "" {
		dict["PayloadDisplayName"] = p.DisplayName
	} else if p.Schema.Title != "" {
		dict["PayloadDisplayName"] = p.Schema.Title
	}

	// Schema-defined keys layer on top. Reject user attempts to
	// shadow the 5 magic Payload keys the emitter owns; everything
	// else (including legitimate Apple keys with the Payload prefix
	// like PayloadCertificateUUID on com.apple.wifi.managed) is the
	// schema's territory and must pass through. Pre-fix the check was
	// a strings.HasPrefix(k, "Payload") that blocked certificate-based
	// and similar profiles from generating (Bugbot PR #50 review).
	for k, v := range p.Values {
		if reservedPayloadKey(k) {
			return nil, fmt.Errorf("user values may not set the reserved key %q; the emitter owns it", k)
		}
		dict[k] = v
	}
	return dict, nil
}

// reservedPayloadKey returns true for the 5 magic Configuration-Profile
// keys the emitter generates from the schema + envelope metadata. Any
// other Apple-defined key with a "Payload" prefix (PayloadCertificateUUID,
// PayloadCertificateAnchorUUID, etc.) is schema territory and must pass
// through.
func reservedPayloadKey(k string) bool {
	switch k {
	case "PayloadType",
		"PayloadUUID",
		"PayloadVersion",
		"PayloadIdentifier",
		"PayloadDisplayName":
		return true
	}
	return false
}

// writePlistValue emits one value as plist XML, indented to the given
// prefix. typeHint is the Apple schema type when known ("string",
// "boolean", "integer", "real", "data", "date", "array", "dictionary",
// "any"); "" or "any" triggers Go-type inference. The function
// recurses into arrays and dicts.
func writePlistValue(w io.Writer, v any, indent, typeHint string) error {
	// Resolve "any" / empty hints from the Go type. This is the path
	// taken by nested values inside arrays/dicts where the schema
	// only declared the parent's type.
	if typeHint == "" || typeHint == "any" {
		typeHint = inferType(v)
	}
	switch typeHint {
	case "string":
		return writeStringElem(w, indent, v)
	case "integer":
		return writeIntegerElem(w, indent, v)
	case "real":
		return writeRealElem(w, indent, v)
	case "boolean":
		return writeBooleanElem(w, indent, v)
	case "data":
		return writeDataElem(w, indent, v)
	case "date":
		return writeDateElem(w, indent, v)
	case "array":
		return writeArrayElem(w, indent, v)
	case "dictionary":
		return writeDictElem(w, indent, v)
	default:
		return fmt.Errorf("unknown plist type %q for value %v", typeHint, v)
	}
}

// inferType maps a Go value back to its Apple plist type when the
// schema declared "any" (or no explicit type, as inside nested
// arrays/dicts). Plist decoders (notably howett.net/plist used by
// the edit flow) return integers as uint64 and dates as time.Time;
// pre-fix (Bugbot PR #54 re-review) those landed in the default
// branch and EmitMobileconfig failed with "unknown plist type" the
// moment an edit merged a nested integer back into the rebuilt
// mobileconfig.
func inferType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return "integer"
	case float32, float64:
		return "real"
	case time.Time:
		return "date"
	case []byte:
		return "data"
	case []any:
		return "array"
	case map[string]any:
		return "dictionary"
	default:
		return ""
	}
}

func writeStringElem(w io.Writer, indent string, v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("string type expected, got %T", v)
	}
	_, err := fmt.Fprintf(w, "%s<string>%s</string>", indent, xmlEscape(s))
	return err
}

func writeIntegerElem(w io.Writer, indent string, v any) error {
	var n int64
	switch x := v.(type) {
	case int:
		n = int64(x)
	case int8:
		n = int64(x)
	case int16:
		n = int64(x)
	case int32:
		n = int64(x)
	case int64:
		n = x
	case uint:
		n = int64(x)
	case uint8:
		n = int64(x)
	case uint16:
		n = int64(x)
	case uint32:
		n = int64(x)
	case uint64:
		// howett.net/plist decodes <integer> as uint64. Apple's
		// plist <integer> type is signed in practice, so a value
		// over int64-max would be malformed anyway — surface the
		// overflow instead of silently truncating.
		if x > 1<<63-1 {
			return fmt.Errorf("uint64 value %d overflows signed integer", x)
		}
		n = int64(x)
	case float64:
		// JSON-decoded numbers land as float64; accept whole values.
		if x != float64(int64(x)) {
			return fmt.Errorf("integer type expected, got non-whole float %v", x)
		}
		n = int64(x)
	default:
		return fmt.Errorf("integer type expected, got %T", v)
	}
	_, err := fmt.Fprintf(w, "%s<integer>%d</integer>", indent, n)
	return err
}

func writeRealElem(w io.Writer, indent string, v any) error {
	var f float64
	switch x := v.(type) {
	case float32:
		f = float64(x)
	case float64:
		f = x
	case int:
		f = float64(x)
	case int64:
		f = float64(x)
	default:
		return fmt.Errorf("real type expected, got %T", v)
	}
	_, err := fmt.Fprintf(w, "%s<real>%g</real>", indent, f)
	return err
}

func writeBooleanElem(w io.Writer, indent string, v any) error {
	b, ok := v.(bool)
	if !ok {
		return fmt.Errorf("boolean type expected, got %T", v)
	}
	tag := "<false/>"
	if b {
		tag = "<true/>"
	}
	_, err := fmt.Fprintf(w, "%s%s", indent, tag)
	return err
}

func writeDataElem(w io.Writer, indent string, v any) error {
	switch x := v.(type) {
	case string:
		// User can supply already-base64-encoded data verbatim; we
		// pass it through (validator will have checked it parses).
		_, err := fmt.Fprintf(w, "%s<data>%s</data>", indent, x)
		return err
	case []byte:
		_, err := fmt.Fprintf(w, "%s<data>%s</data>", indent, base64.StdEncoding.EncodeToString(x))
		return err
	}
	return fmt.Errorf("data type expected (string or []byte), got %T", v)
}

func writeDateElem(w io.Writer, indent string, v any) error {
	switch x := v.(type) {
	case string:
		_, err := fmt.Fprintf(w, "%s<date>%s</date>", indent, x)
		return err
	case time.Time:
		// Plist decoders (howett.net/plist) return <date> as
		// time.Time; an edit-merge carrying a date subkey lands here.
		_, err := fmt.Fprintf(w, "%s<date>%s</date>", indent, x.UTC().Format("2006-01-02T15:04:05Z"))
		return err
	}
	return fmt.Errorf("date type expected (RFC3339 string or time.Time), got %T", v)
}

func writeArrayElem(w io.Writer, indent string, v any) error {
	arr, ok := v.([]any)
	if !ok {
		return fmt.Errorf("array type expected, got %T", v)
	}
	if len(arr) == 0 {
		_, err := fmt.Fprintf(w, "%s<array/>", indent)
		return err
	}
	if _, err := fmt.Fprintf(w, "%s<array>", indent); err != nil {
		return err
	}
	inner := indent + "\t"
	for _, item := range arr {
		if _, err := fmt.Fprint(w, "\n"); err != nil {
			return err
		}
		if err := writePlistValue(w, item, inner, ""); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\n%s</array>", indent)
	return err
}

func writeDictElem(w io.Writer, indent string, v any) error {
	dict, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("dictionary type expected, got %T", v)
	}
	if len(dict) == 0 {
		_, err := fmt.Fprintf(w, "%s<dict/>", indent)
		return err
	}
	keys := make([]string, 0, len(dict))
	for k := range dict {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if _, err := fmt.Fprintf(w, "%s<dict>", indent); err != nil {
		return err
	}
	inner := indent + "\t"
	for _, k := range keys {
		if _, err := fmt.Fprintf(w, "\n%s<key>%s</key>\n", inner, xmlEscape(k)); err != nil {
			return err
		}
		if err := writePlistValue(w, dict[k], inner, ""); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\n%s</dict>", indent)
	return err
}

// xmlEscape handles the five XML special characters. Configuration
// profiles legitimately contain quotes, ampersands, and angle
// brackets (SSIDs, descriptions, regex strings), so this is
// load-bearing — a missed escape produces an invalid mobileconfig.
func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

// newUUID generates an Apple-style upper-case UUID for the magic 5
// keys. Overridable so tests can pin a deterministic UUID and produce
// stable mobileconfig snapshots.
//
// We use random bytes for V4 UUIDs rather than crypto/rand panic on
// entropy starvation (rare; the OS RNG never blocks in practice).
var newUUID = randomUUIDv4

func randomUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Cryptographic randomness failure is unrecoverable for our
		// use case (the resulting profile would be a uniqueness
		// hazard); panic is appropriate.
		panic("apple_mdm: crypto/rand failure: " + err.Error())
	}
	// RFC 4122 v4 variant bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	hexBytes := hex.EncodeToString(b[:])
	return strings.ToUpper(
		hexBytes[0:8] + "-" +
			hexBytes[8:12] + "-" +
			hexBytes[12:16] + "-" +
			hexBytes[16:20] + "-" +
			hexBytes[20:32],
	)
}
