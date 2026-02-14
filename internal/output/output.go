// Package output provides a shared output engine for all CLI commands.
// It formats data as JSON (default), table, CSV, or human-readable key-value pairs.
package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

// Format represents a supported output format.
type Format string

const (
	FormatJSON   Format = "json"
	FormatTable  Format = "table"
	FormatCSV    Format = "csv"
	FormatHuman  Format = "human"
	FormatYAML   Format = "yaml"
	FormatNDJSON Format = "ndjson"
)

// ValidFormats is the set of supported format strings.
var ValidFormats = []Format{FormatJSON, FormatTable, FormatCSV, FormatHuman, FormatYAML, FormatNDJSON}

// IsValid returns true if f is a recognized format.
func (f Format) IsValid() bool {
	for _, v := range ValidFormats {
		if f == v {
			return true
		}
	}
	return false
}

// Options controls output behavior.
type Options struct {
	// Format selects the output format.
	Format Format

	// Quiet suppresses all output when true.
	Quiet bool

	// IDsOnly outputs one ID per line when true.
	IDsOnly bool

	// DefaultFields is the ordered set of fields shown by default in table/CSV.
	// If empty, all top-level fields are shown.
	DefaultFields []string

	// Fields is the user-specified set of fields to include (--fields flag).
	// When set, only these fields appear in output. Mutually exclusive with Exclude.
	Fields []string

	// Exclude is the user-specified set of fields to exclude (--exclude flag).
	// When set, all default fields except these appear. Mutually exclusive with Fields.
	Exclude []string

	// All overrides DefaultFields to show all available fields (--all flag).
	All bool
}

// CurrentOptions reads output options from Viper flags and config.
func CurrentOptions() Options {
	return Options{
		Format:  Format(viper.GetString("defaults.output")),
		Quiet:   viper.GetBool("quiet"),
		IDsOnly: viper.GetBool("ids"),
		Fields:  splitCommaFlag(viper.GetString("fields")),
		Exclude: splitCommaFlag(viper.GetString("exclude")),
		All:     viper.GetBool("all"),
	}
}

// splitCommaFlag splits a comma-separated flag value into a slice.
// Returns nil for empty strings.
func splitCommaFlag(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// WriteList formats a list of resources to w.
// data must be a slice of json.RawMessage.
// Returns nil if Quiet is set.
func WriteList(w io.Writer, data []json.RawMessage, opts Options) error {
	if opts.Quiet {
		return nil
	}

	if opts.IDsOnly {
		return writeIDs(w, data)
	}

	// Resolve the effective field set based on --fields, --exclude, --all, and DefaultFields.
	effectiveFields := opts.resolveEffectiveFields(data)

	// Apply field selection to data when explicit field constraints are active.
	if len(opts.Fields) > 0 || len(opts.Exclude) > 0 || opts.All {
		data = filterFields(data, effectiveFields)
	}

	switch opts.Format {
	case FormatTable:
		return writeTable(w, data, effectiveFields)
	case FormatCSV:
		return writeCSV(w, data, effectiveFields)
	case FormatHuman:
		return writeHumanList(w, data, effectiveFields)
	case FormatNDJSON:
		return writeNDJSONList(w, data)
	case FormatYAML:
		return writeYAMLList(w, data)
	default:
		return writeJSONList(w, data)
	}
}

// WriteSingle formats a single resource to w.
// data must be a single json.RawMessage (an object).
func WriteSingle(w io.Writer, data json.RawMessage, opts Options) error {
	if opts.Quiet {
		return nil
	}

	if opts.IDsOnly {
		return writeIDs(w, []json.RawMessage{data})
	}

	// Resolve the effective field set based on --fields, --exclude, --all, and DefaultFields.
	effectiveFields := opts.resolveEffectiveFields([]json.RawMessage{data})

	// Apply field selection to data when explicit field constraints are active.
	if len(opts.Fields) > 0 || len(opts.Exclude) > 0 || opts.All {
		filtered := filterFields([]json.RawMessage{data}, effectiveFields)
		if len(filtered) > 0 {
			data = filtered[0]
		}
	}

	switch opts.Format {
	case FormatTable:
		return writeTable(w, []json.RawMessage{data}, effectiveFields)
	case FormatCSV:
		return writeCSV(w, []json.RawMessage{data}, effectiveFields)
	case FormatHuman:
		return writeHumanSingle(w, data)
	case FormatNDJSON:
		return writeNDJSONSingle(w, data)
	case FormatYAML:
		return writeYAMLSingle(w, data)
	default:
		return writeJSONSingle(w, data)
	}
}

// resolveEffectiveFields determines the active field set based on the combination
// of --fields, --exclude, --all flags and DefaultFields.
// Priority: --fields > --exclude > --all > DefaultFields > all fields.
func (opts Options) resolveEffectiveFields(data []json.RawMessage) []string {
	if len(opts.Fields) > 0 {
		return opts.Fields
	}

	if opts.All {
		// Show all fields from the data, in sorted order.
		if len(data) > 0 {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(data[0], &m); err == nil {
				return sortedKeys(m)
			}
		}
		return nil
	}

	if len(opts.Exclude) > 0 {
		// Start with DefaultFields (or all fields if no defaults), then remove excluded.
		base := opts.DefaultFields
		if len(base) == 0 && len(data) > 0 {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(data[0], &m); err == nil {
				base = sortedKeys(m)
			}
		}
		excludeSet := make(map[string]bool, len(opts.Exclude))
		for _, e := range opts.Exclude {
			excludeSet[e] = true
		}
		var result []string
		for _, f := range base {
			if !excludeSet[f] {
				result = append(result, f)
			}
		}
		return result
	}

	// No field selection flags — use DefaultFields as-is (may be nil).
	return opts.DefaultFields
}

// filterFields filters each JSON object in data to include only the specified fields.
// If fields is nil, data is returned unchanged.
func filterFields(data []json.RawMessage, fields []string) []json.RawMessage {
	if len(fields) == 0 {
		return data
	}

	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}

	result := make([]json.RawMessage, 0, len(data))
	for _, raw := range data {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			result = append(result, raw)
			continue
		}

		filtered := make(map[string]json.RawMessage)
		for k, v := range m {
			if fieldSet[k] {
				filtered[k] = v
			}
		}

		out, err := json.Marshal(filtered)
		if err != nil {
			result = append(result, raw)
			continue
		}
		result = append(result, out)
	}
	return result
}

// --- JSON formatter ---

func writeJSONList(w io.Writer, data []json.RawMessage) error {
	// Always produce an array, even for empty or single results.
	if len(data) == 0 {
		_, err := fmt.Fprintln(w, "[]")
		return err
	}

	// Re-marshal each item with sorted keys, then produce a pretty array.
	var items []json.RawMessage
	for _, raw := range data {
		sorted, err := sortedJSON(raw)
		if err != nil {
			return err
		}
		items = append(items, sorted)
	}

	out, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

func writeJSONSingle(w io.Writer, data json.RawMessage) error {
	// Single resource: produce an object (not an array).
	sorted, err := sortedJSON(data)
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(json.RawMessage(sorted), "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

// sortedJSON re-marshals a JSON object with alphabetically sorted keys.
func sortedJSON(raw json.RawMessage) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		// Not an object — return as-is (could be a primitive or array).
		return raw, nil
	}

	sorted := make(sortedMap, 0, len(m))
	for k, v := range m {
		sorted = append(sorted, kv{Key: k, Value: v})
	}
	sort.Sort(sorted)

	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, pair := range sorted {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyJSON, _ := json.Marshal(pair.Key)
		buf.Write(keyJSON)
		buf.WriteByte(':')
		buf.Write(pair.Value)
	}
	buf.WriteByte('}')

	return buf.Bytes(), nil
}

type kv struct {
	Key   string
	Value json.RawMessage
}

type sortedMap []kv

func (s sortedMap) Len() int           { return len(s) }
func (s sortedMap) Less(i, j int) bool { return s[i].Key < s[j].Key }
func (s sortedMap) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// --- Table formatter ---

func writeTable(w io.Writer, data []json.RawMessage, defaultFields []string) error {
	if len(data) == 0 {
		return nil
	}

	rows, err := toMaps(data)
	if err != nil {
		return err
	}

	fields := resolveFields(rows[0], defaultFields)
	if len(fields) == 0 {
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	// Header row.
	headers := make([]string, len(fields))
	for i, f := range fields {
		headers[i] = strings.ToUpper(f)
	}
	fmt.Fprintln(tw, strings.Join(headers, "\t"))

	// Data rows.
	for _, row := range rows {
		vals := make([]string, len(fields))
		for i, f := range fields {
			vals[i] = formatValue(row[f])
		}
		fmt.Fprintln(tw, strings.Join(vals, "\t"))
	}

	return tw.Flush()
}

// --- CSV formatter ---

func writeCSV(w io.Writer, data []json.RawMessage, defaultFields []string) error {
	if len(data) == 0 {
		return nil
	}

	rows, err := toMaps(data)
	if err != nil {
		return err
	}

	fields := resolveFields(rows[0], defaultFields)
	if len(fields) == 0 {
		return nil
	}

	cw := csv.NewWriter(w)

	// Header row.
	if err := cw.Write(fields); err != nil {
		return err
	}

	// Data rows.
	for _, row := range rows {
		vals := make([]string, len(fields))
		for i, f := range fields {
			vals[i] = formatValue(row[f])
		}
		if err := cw.Write(vals); err != nil {
			return err
		}
	}

	cw.Flush()
	return cw.Error()
}

// --- Human formatter ---

func writeHumanSingle(w io.Writer, data json.RawMessage) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		// Not an object — just print it.
		_, err := fmt.Fprintln(w, string(data))
		return err
	}

	keys := sortedKeys(m)

	// Find the longest key for alignment.
	maxLen := 0
	for _, k := range keys {
		if len(k) > maxLen {
			maxLen = len(k)
		}
	}

	for _, k := range keys {
		label := k + ":"
		fmt.Fprintf(w, "%-*s  %s\n", maxLen+1, label, formatValue(m[k]))
	}
	return nil
}

func writeHumanList(w io.Writer, data []json.RawMessage, defaultFields []string) error {
	// For lists in human mode, show a summarized key-value block per item.
	for i, raw := range data {
		if i > 0 {
			fmt.Fprintln(w)
		}
		if err := writeHumanSingle(w, raw); err != nil {
			return err
		}
	}
	return nil
}

// --- NDJSON formatter ---

func writeNDJSONList(w io.Writer, data []json.RawMessage) error {
	for _, raw := range data {
		sorted, err := sortedJSON(raw)
		if err != nil {
			return err
		}
		// Compact JSON — one object per line, no indentation.
		var buf bytes.Buffer
		if err := json.Compact(&buf, sorted); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, buf.String()); err != nil {
			return err
		}
	}
	return nil
}

func writeNDJSONSingle(w io.Writer, data json.RawMessage) error {
	return writeNDJSONList(w, []json.RawMessage{data})
}

// --- YAML formatter ---

// jsonToYAMLNode converts a json.RawMessage to a yaml.Node with sorted keys.
func jsonToYAMLNode(raw json.RawMessage) (*yaml.Node, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}

	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, err
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}, nil
	case '{':
		var m map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &m); err != nil {
			return nil, err
		}
		keys := sortedKeys(m)
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, k := range keys {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
			valNode, err := jsonToYAMLNode(m[k])
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, keyNode, valNode)
		}
		return node, nil
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, err
		}
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range arr {
			child, err := jsonToYAMLNode(item)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, child)
		}
		return node, nil
	default:
		// Number or boolean.
		var v interface{}
		if err := json.Unmarshal(trimmed, &v); err != nil {
			return nil, err
		}
		switch val := v.(type) {
		case bool:
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprintf("%t", val)}, nil
		case float64:
			// Use compact representation for integers.
			if val == float64(int64(val)) {
				return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", int64(val))}, nil
			}
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: fmt.Sprintf("%g", val)}, nil
		default:
			return &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%v", val)}, nil
		}
	}
}

func writeYAMLList(w io.Writer, data []json.RawMessage) error {
	if len(data) == 0 {
		_, err := fmt.Fprintln(w, "[]")
		return err
	}

	seqNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, raw := range data {
		node, err := jsonToYAMLNode(raw)
		if err != nil {
			return err
		}
		seqNode.Content = append(seqNode.Content, node)
	}

	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{seqNode}}
	enc := yaml.NewEncoder(w)
	defer enc.Close()
	return enc.Encode(doc)
}

func writeYAMLSingle(w io.Writer, data json.RawMessage) error {
	node, err := jsonToYAMLNode(data)
	if err != nil {
		return err
	}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{node}}
	enc := yaml.NewEncoder(w)
	defer enc.Close()
	return enc.Encode(doc)
}

// --- IDs formatter ---

func writeIDs(w io.Writer, data []json.RawMessage) error {
	for _, raw := range data {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		// Look for "id", "_id", or "ID".
		for _, key := range []string{"_id", "id", "ID"} {
			if v, ok := m[key]; ok {
				fmt.Fprintln(w, formatValue(v))
				break
			}
		}
	}
	return nil
}

// --- Helpers ---

// toMaps converts a slice of json.RawMessage to a slice of string-keyed maps.
func toMaps(data []json.RawMessage) ([]map[string]json.RawMessage, error) {
	rows := make([]map[string]json.RawMessage, 0, len(data))
	for _, raw := range data {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("cannot parse JSON object: %w", err)
		}
		rows = append(rows, m)
	}
	return rows, nil
}

// resolveFields returns the field names to display.
// If defaultFields is non-empty, it returns only those fields that exist in the row.
// Otherwise, it returns all keys in sorted order.
func resolveFields(row map[string]json.RawMessage, defaultFields []string) []string {
	if len(defaultFields) > 0 {
		var fields []string
		for _, f := range defaultFields {
			if _, ok := row[f]; ok {
				fields = append(fields, f)
			}
		}
		return fields
	}
	return sortedKeys(row)
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// formatValue converts a json.RawMessage value to a display string.
// Strings are unquoted; booleans, numbers, and nulls are rendered literally.
// Objects and arrays are rendered as compact JSON.
func formatValue(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}

	// Handle null explicitly before trying string.
	if string(trimmed) == "null" {
		return "null"
	}

	// Try string first (most common for table/CSV display).
	var s string
	if err := json.Unmarshal(trimmed, &s); err == nil {
		return s
	}

	// For booleans, numbers — use the literal JSON representation.
	return string(trimmed)
}
