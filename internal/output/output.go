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
)

// Format represents a supported output format.
type Format string

const (
	FormatJSON  Format = "json"
	FormatTable Format = "table"
	FormatCSV   Format = "csv"
	FormatHuman Format = "human"
)

// ValidFormats is the set of supported format strings.
var ValidFormats = []Format{FormatJSON, FormatTable, FormatCSV, FormatHuman}

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
}

// CurrentOptions reads output options from Viper flags and config.
func CurrentOptions() Options {
	return Options{
		Format:  Format(viper.GetString("defaults.output")),
		Quiet:   viper.GetBool("quiet"),
		IDsOnly: viper.GetBool("ids"),
	}
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

	switch opts.Format {
	case FormatTable:
		return writeTable(w, data, opts.DefaultFields)
	case FormatCSV:
		return writeCSV(w, data, opts.DefaultFields)
	case FormatHuman:
		return writeHumanList(w, data, opts.DefaultFields)
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

	switch opts.Format {
	case FormatTable:
		return writeTable(w, []json.RawMessage{data}, opts.DefaultFields)
	case FormatCSV:
		return writeCSV(w, []json.RawMessage{data}, opts.DefaultFields)
	case FormatHuman:
		return writeHumanSingle(w, data)
	default:
		return writeJSONSingle(w, data)
	}
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
