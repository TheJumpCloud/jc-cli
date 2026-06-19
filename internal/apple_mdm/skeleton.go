package apple_mdm

import (
	"fmt"
	"strings"
)

// EmitValuesSkeleton renders a YAML scaffold the operator edits to
// populate a payload's user-supplied values. The output is annotated
// per-key with type, presence, default, and constraints so the admin
// doesn't have to flip between `jc apple-mdm payloads show` and the
// editor.
//
// YAML (not JSON) so we can ship inline comments — the comment lines
// carry all the per-key affordances that make this useful as an
// authoring surface. The CoerceAndValidate path is type-agnostic
// (map[string]any), so the consumer just yaml.Unmarshal before passing
// the parsed values in.
//
// Required keys are uncommented with a placeholder; optional keys are
// commented out so the operator opts in. The first key on screen is
// always one the admin must set — they shouldn't have to scroll
// hunting for required entries.
func EmitValuesSkeleton(p Payload) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s", p.Type)
	if p.Title != "" {
		fmt.Fprintf(&b, " — %s", p.Title)
	}
	fmt.Fprintln(&b)
	if p.Description != "" {
		// Wrap long descriptions across multiple comment lines so the
		// header doesn't blow past 100 columns in a typical editor.
		for _, line := range wrap(p.Description, 88) {
			fmt.Fprintf(&b, "# %s\n", line)
		}
	}
	fmt.Fprintln(&b, "#")
	fmt.Fprintln(&b, "# Required keys are uncommented below. Uncomment optional keys to set them.")
	fmt.Fprintln(&b, "# Save and exit your editor when done; jc will validate and preview the profile.")
	fmt.Fprintln(&b)

	required, optional, other := groupKeysByPresenceSkeleton(p.Keys)

	if len(required) > 0 {
		fmt.Fprintln(&b, "# ─── Required keys ───")
		fmt.Fprintln(&b)
		for _, k := range required {
			writeKeyEntry(&b, k, false /* commented */)
		}
	} else {
		fmt.Fprintln(&b, "# ─── Required keys ───")
		fmt.Fprintln(&b, "# (this schema has no required keys)")
		fmt.Fprintln(&b)
	}

	if len(optional) > 0 {
		fmt.Fprintln(&b, "# ─── Optional keys ───")
		fmt.Fprintln(&b)
		for _, k := range optional {
			writeKeyEntry(&b, k, true /* commented */)
		}
	}
	if len(other) > 0 {
		fmt.Fprintln(&b, "# ─── Other (deprecated/conditional) keys ───")
		fmt.Fprintln(&b)
		for _, k := range other {
			writeKeyEntry(&b, k, true)
		}
	}

	return b.String()
}

// writeKeyEntry emits one comment block + the YAML entry (commented or
// not) for a single payload key. The structure:
//
//	# KeyName: type (presence, valuetype=..., enum{...}, range [..])
//	#   First line of the description.
//	# KeyName: <example>
//
// Description is single-line so each entry stays compact. Operators
// who want more context can run `jc apple-mdm payloads show <type>`
// for the full reference.
func writeKeyEntry(b *strings.Builder, k Key, commentOut bool) {
	fmt.Fprintf(b, "# %s", k.Name)
	if k.Type != "" {
		fmt.Fprintf(b, ": %s", k.Type)
	}
	pieces := keyAffordances(k)
	if len(pieces) > 0 {
		fmt.Fprintf(b, " (%s)", strings.Join(pieces, ", "))
	}
	fmt.Fprintln(b)
	if d := firstLineSkeleton(k.Content); d != "" {
		fmt.Fprintf(b, "#   %s\n", d)
	}
	prefix := ""
	if commentOut {
		prefix = "# "
	}
	fmt.Fprintf(b, "%s%s: %s\n\n", prefix, k.Name, exampleValue(k))
}

// keyAffordances builds the per-key parenthetical so an admin can see
// type constraints inline. Order: presence (if not default), valuetype,
// enum, range, default. Anything we can't render compactly is omitted.
func keyAffordances(k Key) []string {
	var pieces []string
	if k.Presence != "" && strings.ToLower(k.Presence) != "optional" {
		pieces = append(pieces, k.Presence)
	}
	if k.ValueType != "" {
		pieces = append(pieces, "valuetype="+k.ValueType)
	}
	if len(k.RangeList) > 0 {
		vals := make([]string, 0, len(k.RangeList))
		for _, v := range k.RangeList {
			vals = append(vals, fmt.Sprintf("%v", v))
		}
		pieces = append(pieces, "enum{"+strings.Join(vals, ",")+"}")
	}
	if k.Range != nil {
		pieces = append(pieces, fmt.Sprintf("range [%v..%v]", k.Range.Min, k.Range.Max))
	}
	if k.Default != nil {
		pieces = append(pieces, fmt.Sprintf("default=%v", k.Default))
	}
	return pieces
}

// exampleValue picks a placeholder value the operator can edit in
// place. For booleans we use the default (or false), for strings ""
// or the first rangelist entry. Numerics get 0 unless a range Min is
// set. Arrays and dicts get an empty literal — the operator will
// usually replace these wholesale.
func exampleValue(k Key) string {
	if k.Default != nil {
		// Render the default as a YAML scalar.
		switch d := k.Default.(type) {
		case bool:
			if d {
				return "true"
			}
			return "false"
		case string:
			return yamlString(d)
		default:
			return fmt.Sprintf("%v", d)
		}
	}
	switch k.Type {
	case "boolean":
		return "false"
	case "string":
		if len(k.RangeList) > 0 {
			return yamlString(fmt.Sprintf("%v", k.RangeList[0]))
		}
		return `""`
	case "integer":
		if k.Range != nil && k.Range.Min != nil {
			return fmt.Sprintf("%v", k.Range.Min)
		}
		return "0"
	case "real":
		return "0.0"
	case "data":
		return `""  # base64-encoded`
	case "date":
		return `""  # RFC 3339`
	case "array":
		return "[]"
	case "dictionary":
		return "{}"
	default:
		return `""`
	}
}

// yamlString picks the lightest-weight YAML scalar form. Plain strings
// (no special chars) go unquoted; everything else gets double-quoted
// with the common backslash escapes.
func yamlString(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if r == ':' || r == '#' || r == '\n' || r == '"' || r == '\'' || r == '\\' {
			return fmt.Sprintf(`"%s"`, strings.ReplaceAll(s, `"`, `\"`))
		}
	}
	return s
}

// firstLineSkeleton trims and single-lines a content string.
func firstLineSkeleton(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 140 {
		s = s[:137] + "..."
	}
	return s
}

// groupKeysByPresenceSkeleton mirrors the CLI's grouping so the
// authoring view shows required first.
func groupKeysByPresenceSkeleton(keys []Key) (required, optional, other []Key) {
	for _, k := range keys {
		switch strings.ToLower(k.Presence) {
		case "required":
			required = append(required, k)
		case "optional", "":
			optional = append(optional, k)
		default:
			other = append(other, k)
		}
	}
	return
}

// wrap is a minimal word-wrap that splits on whitespace at column
// boundaries. Used so the header description doesn't blow past
// editor-comfortable column widths.
func wrap(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	current := words[0]
	for _, w := range words[1:] {
		if len(current)+1+len(w) > width {
			lines = append(lines, current)
			current = w
			continue
		}
		current += " " + w
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
