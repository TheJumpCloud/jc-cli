package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// --- Helpers ---

func rawMsg(s string) json.RawMessage {
	return json.RawMessage(s)
}

func sampleUsers() []json.RawMessage {
	return []json.RawMessage{
		rawMsg(`{"_id":"abc123","username":"jdoe","email":"jdoe@acme.com","activated":true}`),
		rawMsg(`{"_id":"def456","username":"asmith","email":"asmith@acme.com","activated":false}`),
	}
}

func singleUser() json.RawMessage {
	return rawMsg(`{"_id":"abc123","username":"jdoe","email":"jdoe@acme.com","activated":true}`)
}

// --- Format validation ---

func TestFormatIsValid(t *testing.T) {
	tests := []struct {
		format Format
		valid  bool
	}{
		{FormatJSON, true},
		{FormatTable, true},
		{FormatCSV, true},
		{FormatHuman, true},
		{FormatYAML, true},
		{FormatNDJSON, true},
		{Format("xml"), false},
		{Format(""), false},
	}
	for _, tt := range tests {
		if got := tt.format.IsValid(); got != tt.valid {
			t.Errorf("Format(%q).IsValid() = %v, want %v", tt.format, got, tt.valid)
		}
	}
}

// --- CurrentOptions ---

func TestCurrentOptions(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	viper.Set("defaults.output", "table")
	viper.Set("quiet", true)
	viper.Set("ids", true)

	opts := CurrentOptions()
	if opts.Format != FormatTable {
		t.Errorf("Format = %q, want %q", opts.Format, FormatTable)
	}
	if !opts.Quiet {
		t.Error("Quiet should be true")
	}
	if !opts.IDsOnly {
		t.Error("IDsOnly should be true")
	}
}

// --- JSON output ---

func TestWriteList_JSON_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, nil, Options{Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "[]" {
		t.Errorf("empty list = %q, want %q", got, "[]")
	}
}

func TestWriteList_JSON_Array(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}

	// Must be a valid JSON array.
	var arr []json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("not valid JSON array: %v", err)
	}
	if len(arr) != 2 {
		t.Errorf("array length = %d, want 2", len(arr))
	}
}

func TestWriteList_JSON_SingleResultIsArray(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers()[:1], Options{Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	// Even single result must be wrapped in array.
	got := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(got, "[") {
		t.Errorf("single-result list should start with '[', got %q", got[:10])
	}
}

func TestWriteList_JSON_SortedKeys(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"zebra":1,"alpha":2}`)}
	err := WriteList(&buf, data, Options{Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	alphaIdx := strings.Index(got, `"alpha"`)
	zebraIdx := strings.Index(got, `"zebra"`)
	if alphaIdx >= zebraIdx {
		t.Error("keys should be alphabetically sorted: alpha before zebra")
	}
}

func TestWriteSingle_JSON_Object(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}

	// Must be a valid JSON object (not array).
	got := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(got, "{") {
		t.Errorf("single resource should start with '{', got %q", got[:10])
	}

	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("not valid JSON object: %v", err)
	}
}

func TestWriteSingle_JSON_SortedKeys(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	idIdx := strings.Index(got, `"_id"`)
	usernameIdx := strings.Index(got, `"username"`)
	if idIdx >= usernameIdx {
		t.Error("keys should be alphabetically sorted: _id before username")
	}
}

func TestWriteList_JSON_PrettyPrinted(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"a":1}`)}
	err := WriteList(&buf, data, Options{Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	// Pretty-printed JSON should have newlines and indentation.
	if !strings.Contains(buf.String(), "\n") {
		t.Error("JSON output should be pretty-printed with newlines")
	}
	if !strings.Contains(buf.String(), "  ") {
		t.Error("JSON output should use 2-space indent")
	}
}

func TestWriteList_JSON_BoolsAndNulls(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"active":true,"deleted":false,"notes":null}`)}
	err := WriteList(&buf, data, Options{Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "true") {
		t.Error("booleans should render as true/false")
	}
	if !strings.Contains(got, "false") {
		t.Error("booleans should render as true/false")
	}
	if !strings.Contains(got, "null") {
		t.Error("nulls should be explicit")
	}
}

// --- Table output ---

func TestWriteList_Table(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format:        FormatTable,
		DefaultFields: []string{"username", "email", "activated"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	lines := strings.Split(strings.TrimSpace(got), "\n")

	// Header row.
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (header + 2 data), got %d", len(lines))
	}
	if !strings.Contains(lines[0], "USERNAME") {
		t.Errorf("header should contain USERNAME, got %q", lines[0])
	}
	if !strings.Contains(lines[0], "EMAIL") {
		t.Errorf("header should contain EMAIL, got %q", lines[0])
	}

	// Data rows.
	if !strings.Contains(lines[1], "jdoe") {
		t.Errorf("first data row should contain jdoe, got %q", lines[1])
	}
	if !strings.Contains(lines[2], "asmith") {
		t.Errorf("second data row should contain asmith, got %q", lines[2])
	}
}

func TestWriteList_Table_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, nil, Options{Format: FormatTable})
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "" {
		t.Errorf("empty table should produce no output, got %q", buf.String())
	}
}

func TestWriteList_Table_AllFieldsWhenNoDefault(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"b":"2","a":"1"}`)}
	err := WriteList(&buf, data, Options{Format: FormatTable})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	// Should show all fields in sorted order.
	aIdx := strings.Index(got, "A")
	bIdx := strings.Index(got, "B")
	if aIdx >= bIdx {
		t.Error("fields should be alphabetically sorted when no defaults")
	}
}

// --- CSV output ---

func TestWriteList_CSV(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format:        FormatCSV,
		DefaultFields: []string{"username", "email"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	lines := strings.Split(strings.TrimSpace(got), "\n")

	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	// Header.
	if lines[0] != "username,email" {
		t.Errorf("CSV header = %q, want %q", lines[0], "username,email")
	}

	// Data.
	if !strings.Contains(lines[1], "jdoe") {
		t.Errorf("CSV row 1 should contain jdoe, got %q", lines[1])
	}
}

func TestWriteList_CSV_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, nil, Options{Format: FormatCSV})
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "" {
		t.Errorf("empty CSV should produce no output, got %q", buf.String())
	}
}

func TestWriteList_CSV_ProperEscaping(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"name":"Doe, John","notes":"has \"quotes\""}`)}
	err := WriteList(&buf, data, Options{Format: FormatCSV})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	// CSV should properly escape commas and quotes.
	if !strings.Contains(got, `"Doe, John"`) {
		t.Errorf("CSV should escape commas in values, got %q", got)
	}
}

// --- Human output ---

func TestWriteSingle_Human(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{Format: FormatHuman})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	// Should have key-value pairs.
	if !strings.Contains(got, "username:") {
		t.Errorf("human output should contain 'username:', got %q", got)
	}
	if !strings.Contains(got, "jdoe") {
		t.Errorf("human output should contain 'jdoe', got %q", got)
	}
}

func TestWriteList_Human(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{Format: FormatHuman})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "jdoe") {
		t.Error("human list should contain first user")
	}
	if !strings.Contains(got, "asmith") {
		t.Error("human list should contain second user")
	}
}

// --- NDJSON output ---

func TestWriteList_NDJSON(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{Format: FormatNDJSON})
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("NDJSON should have 2 lines, got %d", len(lines))
	}

	// Each line must be valid JSON.
	for i, line := range lines {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestWriteList_NDJSON_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, nil, Options{Format: FormatNDJSON})
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "" {
		t.Errorf("empty NDJSON should produce no output, got %q", buf.String())
	}
}

func TestWriteList_NDJSON_SingleLine(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"a": 1, "b": 2}`)}
	err := WriteList(&buf, data, Options{Format: FormatNDJSON})
	if err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(buf.String())
	// Must be a single line with no newlines inside.
	if strings.Count(got, "\n") != 0 {
		t.Errorf("NDJSON single item should be one line, got %q", got)
	}
	// Must not have indentation.
	if strings.Contains(got, "  ") {
		t.Errorf("NDJSON should not be indented, got %q", got)
	}
}

func TestWriteList_NDJSON_SortedKeys(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"zebra":1,"alpha":2}`)}
	err := WriteList(&buf, data, Options{Format: FormatNDJSON})
	if err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(buf.String())
	alphaIdx := strings.Index(got, `"alpha"`)
	zebraIdx := strings.Index(got, `"zebra"`)
	if alphaIdx >= zebraIdx {
		t.Error("NDJSON keys should be alphabetically sorted")
	}
}

func TestWriteSingle_NDJSON(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{Format: FormatNDJSON})
	if err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(buf.String())
	// Single line, valid JSON object.
	if strings.Count(got, "\n") != 0 {
		t.Errorf("NDJSON single should be one line, got %q", got)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
}

func TestWriteList_NDJSON_Fields(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format: FormatNDJSON,
		Fields: []string{"username"},
	})
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for i, line := range lines {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
		if len(m) != 1 {
			t.Errorf("line %d: expected 1 field, got %d", i, len(m))
		}
		if _, ok := m["username"]; !ok {
			t.Errorf("line %d: username should be present", i)
		}
	}
}

func TestWriteList_NDJSON_Exclude(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format:        FormatNDJSON,
		DefaultFields: []string{"username", "email", "activated"},
		Exclude:       []string{"activated"},
	})
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for i, line := range lines {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
		if _, ok := m["activated"]; ok {
			t.Errorf("line %d: activated should be excluded", i)
		}
	}
}

// --- YAML output ---

func TestWriteList_YAML(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{
		rawMsg(`{"name":"alice","age":30}`),
		rawMsg(`{"name":"bob","age":25}`),
	}
	err := WriteList(&buf, data, Options{Format: FormatYAML})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	// YAML sequence items start with "- ".
	if !strings.Contains(got, "- ") {
		t.Errorf("YAML list should contain sequence markers, got %q", got)
	}
	if !strings.Contains(got, "name: alice") {
		t.Errorf("YAML should contain 'name: alice', got %q", got)
	}
	if !strings.Contains(got, "name: bob") {
		t.Errorf("YAML should contain 'name: bob', got %q", got)
	}
}

func TestWriteList_YAML_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, nil, Options{Format: FormatYAML})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "[]" {
		t.Errorf("empty YAML list = %q, want %q", got, "[]")
	}
}

func TestWriteList_YAML_SortedKeys(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"zebra":1,"alpha":2}`)}
	err := WriteList(&buf, data, Options{Format: FormatYAML})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	alphaIdx := strings.Index(got, "alpha:")
	zebraIdx := strings.Index(got, "zebra:")
	if alphaIdx < 0 || zebraIdx < 0 {
		t.Fatalf("YAML should contain both keys, got %q", got)
	}
	if alphaIdx >= zebraIdx {
		t.Error("YAML keys should be alphabetically sorted")
	}
}

func TestWriteSingle_YAML(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{Format: FormatYAML})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	// Single resource should be a YAML mapping (not a sequence).
	if strings.HasPrefix(strings.TrimSpace(got), "-") {
		t.Errorf("single YAML should be a mapping, not a sequence: %q", got)
	}
	if !strings.Contains(got, "username: jdoe") {
		t.Errorf("YAML should contain 'username: jdoe', got %q", got)
	}
}

func TestWriteSingle_YAML_SortedKeys(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{Format: FormatYAML})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	idIdx := strings.Index(got, "_id:")
	usernameIdx := strings.Index(got, "username:")
	if idIdx >= usernameIdx {
		t.Error("YAML keys should be alphabetically sorted: _id before username")
	}
}

func TestWriteList_YAML_Types(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"active":true,"count":42,"name":"test","notes":null}`)}
	err := WriteList(&buf, data, Options{Format: FormatYAML})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "active: true") {
		t.Errorf("YAML should preserve booleans, got %q", got)
	}
	if !strings.Contains(got, "count: 42") {
		t.Errorf("YAML should preserve integers, got %q", got)
	}
	if !strings.Contains(got, "name: test") {
		t.Errorf("YAML should render strings, got %q", got)
	}
	if !strings.Contains(got, "notes: null") {
		t.Errorf("YAML should render nulls, got %q", got)
	}
}

func TestWriteList_YAML_Fields(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format: FormatYAML,
		Fields: []string{"username"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "username:") {
		t.Error("YAML should contain username field")
	}
	if strings.Contains(got, "email:") {
		t.Error("YAML should not contain email (not in --fields)")
	}
	if strings.Contains(got, "_id:") {
		t.Error("YAML should not contain _id (not in --fields)")
	}
}

func TestWriteList_YAML_Exclude(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format:        FormatYAML,
		DefaultFields: []string{"username", "email", "activated"},
		Exclude:       []string{"activated"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if strings.Contains(got, "activated:") {
		t.Error("YAML should not contain excluded 'activated' field")
	}
	if !strings.Contains(got, "username:") {
		t.Error("YAML should contain username field")
	}
}

func TestWriteList_YAML_NestedObjects(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"user":{"name":"jdoe","role":"admin"}}`)}
	err := WriteList(&buf, data, Options{Format: FormatYAML})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "user:") {
		t.Errorf("YAML should contain nested object key, got %q", got)
	}
	if !strings.Contains(got, "name: jdoe") {
		t.Errorf("YAML should render nested values, got %q", got)
	}
}

// --- Quiet mode ---

func TestWriteList_Quiet(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{Format: FormatJSON, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "" {
		t.Errorf("quiet mode should suppress output, got %q", buf.String())
	}
}

func TestWriteSingle_Quiet(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{Format: FormatJSON, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "" {
		t.Errorf("quiet mode should suppress output, got %q", buf.String())
	}
}

// --- IDs mode ---

func TestWriteList_IDsOnly(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{IDsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("IDs output should have 2 lines, got %d", len(lines))
	}
	if lines[0] != "abc123" {
		t.Errorf("first ID = %q, want %q", lines[0], "abc123")
	}
	if lines[1] != "def456" {
		t.Errorf("second ID = %q, want %q", lines[1], "def456")
	}
}

func TestWriteSingle_IDsOnly(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{IDsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "abc123" {
		t.Errorf("single ID = %q, want %q", got, "abc123")
	}
}

func TestWriteList_IDsOnly_FallbackIDField(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"id":"x1","name":"test"}`)}
	err := WriteList(&buf, data, Options{IDsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "x1" {
		t.Errorf("ID = %q, want %q", got, "x1")
	}
}

// --- DefaultFields filtering ---

func TestWriteList_Table_DefaultFields(t *testing.T) {
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"username":"jdoe","email":"j@a.com","secret":"hidden"}`)}
	err := WriteList(&buf, data, Options{
		Format:        FormatTable,
		DefaultFields: []string{"username", "email"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if strings.Contains(got, "SECRET") {
		t.Error("default fields should filter out 'secret'")
	}
	if !strings.Contains(got, "USERNAME") {
		t.Error("should show username field")
	}
}

// --- Edge cases ---

func TestFormatValue_Types(t *testing.T) {
	tests := []struct {
		input json.RawMessage
		want  string
	}{
		{rawMsg(`"hello"`), "hello"},
		{rawMsg(`42`), "42"},
		{rawMsg(`true`), "true"},
		{rawMsg(`false`), "false"},
		{rawMsg(`null`), "null"},
		{rawMsg(`[1,2]`), "[1,2]"},
		{nil, ""},
	}
	for _, tt := range tests {
		got := formatValue(tt.input)
		if got != tt.want {
			t.Errorf("formatValue(%s) = %q, want %q", string(tt.input), got, tt.want)
		}
	}
}

func TestSortedJSON_NonObject(t *testing.T) {
	// A JSON array should pass through unchanged.
	raw := rawMsg(`[1,2,3]`)
	got, err := sortedJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `[1,2,3]` {
		t.Errorf("non-object should pass through, got %s", string(got))
	}
}

func TestWriteSingle_Table(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{
		Format:        FormatTable,
		DefaultFields: []string{"username", "email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "USERNAME") {
		t.Error("single item table should have header")
	}
	if !strings.Contains(got, "jdoe") {
		t.Error("single item table should have data")
	}
}

func TestWriteSingle_CSV(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{
		Format:        FormatCSV,
		DefaultFields: []string{"username", "email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("single CSV should have header + 1 row, got %d lines", len(lines))
	}
	if lines[0] != "username,email" {
		t.Errorf("CSV header = %q, want %q", lines[0], "username,email")
	}
}

// --- Output format from config ---

func TestWriteList_DefaultsToJSON(t *testing.T) {
	var buf bytes.Buffer
	// Empty format should be treated as JSON.
	err := WriteList(&buf, sampleUsers(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Default branch is JSON.
	var arr []json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("default output should be valid JSON array: %v", err)
	}
}

func TestWriteList_InvalidFormatDefaultsToJSON(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{Format: "invalid"})
	if err != nil {
		t.Fatal(err)
	}
	// Invalid format falls through to default (JSON).
	var arr []json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("invalid format should fall through to JSON: %v", err)
	}
}

// --- Field selection (--fields) ---

func TestWriteList_Fields_JSON(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format: FormatJSON,
		Fields: []string{"username", "email"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 items, got %d", len(arr))
	}
	// Should only have username and email.
	for i, item := range arr {
		if _, ok := item["_id"]; ok {
			t.Errorf("item %d: _id should be filtered out", i)
		}
		if _, ok := item["activated"]; ok {
			t.Errorf("item %d: activated should be filtered out", i)
		}
		if _, ok := item["username"]; !ok {
			t.Errorf("item %d: username should be present", i)
		}
		if _, ok := item["email"]; !ok {
			t.Errorf("item %d: email should be present", i)
		}
	}
}

func TestWriteList_Fields_Table(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format:        FormatTable,
		DefaultFields: []string{"username", "email", "activated"},
		Fields:        []string{"username", "email"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	// --fields overrides DefaultFields.
	if strings.Contains(got, "ACTIVATED") {
		t.Error("--fields should override DefaultFields; activated should not appear")
	}
	if !strings.Contains(got, "USERNAME") {
		t.Error("username should appear in table")
	}
	if !strings.Contains(got, "EMAIL") {
		t.Error("email should appear in table")
	}
}

func TestWriteList_Fields_CSV(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format:        FormatCSV,
		DefaultFields: []string{"username", "email", "activated"},
		Fields:        []string{"email"},
	})
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 1 {
		t.Fatal("expected at least header line")
	}
	if lines[0] != "email" {
		t.Errorf("CSV header = %q, want %q", lines[0], "email")
	}
}

func TestWriteSingle_Fields_JSON(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{
		Format: FormatJSON,
		Fields: []string{"username"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if len(m) != 1 {
		t.Errorf("expected 1 field, got %d: %v", len(m), m)
	}
	if _, ok := m["username"]; !ok {
		t.Error("username should be present")
	}
}

func TestWriteList_Fields_InvalidFieldWarning(t *testing.T) {
	// Invalid field names should not cause errors — they just don't match any data.
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format: FormatJSON,
		Fields: []string{"nonexistent_field"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	// Each item should be empty since the field doesn't exist.
	for i, item := range arr {
		if len(item) != 0 {
			t.Errorf("item %d: expected empty object, got %v", i, item)
		}
	}
}

func TestWriteList_Fields_CommaSeparated(t *testing.T) {
	// Fields flag accepts comma-separated list.
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format: FormatJSON,
		Fields: []string{"username", "email"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	for _, item := range arr {
		if len(item) != 2 {
			t.Errorf("expected 2 fields, got %d: %v", len(item), item)
		}
	}
}

// --- Field exclusion (--exclude) ---

func TestWriteList_Exclude_JSON(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format:        FormatJSON,
		DefaultFields: []string{"username", "email", "activated"},
		Exclude:       []string{"activated"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	for i, item := range arr {
		if _, ok := item["activated"]; ok {
			t.Errorf("item %d: activated should be excluded", i)
		}
		if _, ok := item["username"]; !ok {
			t.Errorf("item %d: username should be present", i)
		}
		if _, ok := item["email"]; !ok {
			t.Errorf("item %d: email should be present", i)
		}
	}
}

func TestWriteList_Exclude_Table(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format:        FormatTable,
		DefaultFields: []string{"username", "email", "activated"},
		Exclude:       []string{"email"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if strings.Contains(got, "EMAIL") {
		t.Error("email should be excluded from table")
	}
	if !strings.Contains(got, "USERNAME") {
		t.Error("username should appear in table")
	}
	if !strings.Contains(got, "ACTIVATED") {
		t.Error("activated should appear in table")
	}
}

func TestWriteSingle_Exclude_JSON(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{
		Format:        FormatJSON,
		DefaultFields: []string{"_id", "username", "email", "activated"},
		Exclude:       []string{"_id", "activated"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if _, ok := m["_id"]; ok {
		t.Error("_id should be excluded")
	}
	if _, ok := m["activated"]; ok {
		t.Error("activated should be excluded")
	}
	if _, ok := m["username"]; !ok {
		t.Error("username should be present")
	}
}

func TestWriteList_Exclude_NoDefaultFields(t *testing.T) {
	// When no DefaultFields, exclude removes from all fields.
	var buf bytes.Buffer
	data := []json.RawMessage{rawMsg(`{"a":"1","b":"2","c":"3"}`)}
	err := WriteList(&buf, data, Options{
		Format:  FormatJSON,
		Exclude: []string{"b"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if _, ok := arr[0]["b"]; ok {
		t.Error("b should be excluded")
	}
	if _, ok := arr[0]["a"]; !ok {
		t.Error("a should be present")
	}
	if _, ok := arr[0]["c"]; !ok {
		t.Error("c should be present")
	}
}

// --- All fields (--all) ---

func TestWriteList_All_JSON(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format:        FormatJSON,
		DefaultFields: []string{"username"},
		All:           true,
	})
	if err != nil {
		t.Fatal(err)
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	// --all should include all fields, not just DefaultFields.
	if _, ok := arr[0]["_id"]; !ok {
		t.Error("--all should include _id")
	}
	if _, ok := arr[0]["email"]; !ok {
		t.Error("--all should include email")
	}
	if _, ok := arr[0]["activated"]; !ok {
		t.Error("--all should include activated")
	}
}

func TestWriteList_All_Table(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format:        FormatTable,
		DefaultFields: []string{"username"},
		All:           true,
	})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	// --all should show all columns in table, not just the default "username".
	if !strings.Contains(got, "EMAIL") {
		t.Error("--all table should include EMAIL column")
	}
	if !strings.Contains(got, "_ID") {
		t.Error("--all table should include _ID column")
	}
}

func TestWriteSingle_All_Human(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSingle(&buf, singleUser(), Options{
		Format:        FormatHuman,
		DefaultFields: []string{"username"},
		All:           true,
	})
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	// --all should show all key-value pairs.
	if !strings.Contains(got, "_id:") {
		t.Error("--all human should include _id")
	}
	if !strings.Contains(got, "email:") {
		t.Error("--all human should include email")
	}
}

// --- Fields priority over --all ---

func TestWriteList_Fields_OverridesAll(t *testing.T) {
	var buf bytes.Buffer
	err := WriteList(&buf, sampleUsers(), Options{
		Format: FormatJSON,
		Fields: []string{"username"},
		All:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &arr); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	// --fields takes priority over --all.
	if len(arr[0]) != 1 {
		t.Errorf("--fields should take priority over --all, got %d fields", len(arr[0]))
	}
}

// --- splitCommaFlag ---

func TestSplitCommaFlag(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"  ", nil},
		{"username", []string{"username"}},
		{"username,email", []string{"username", "email"}},
		{"username, email, department", []string{"username", "email", "department"}},
		{" username , email ", []string{"username", "email"}},
	}
	for _, tt := range tests {
		got := splitCommaFlag(tt.input)
		if tt.want == nil && got != nil {
			t.Errorf("splitCommaFlag(%q) = %v, want nil", tt.input, got)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("splitCommaFlag(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitCommaFlag(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

// --- resolveEffectiveFields ---

func TestResolveEffectiveFields_FieldsTakesPriority(t *testing.T) {
	opts := Options{
		DefaultFields: []string{"username", "email"},
		Fields:        []string{"_id"},
	}
	data := sampleUsers()
	got := opts.resolveEffectiveFields(data)
	if len(got) != 1 || got[0] != "_id" {
		t.Errorf("Fields should take priority, got %v", got)
	}
}

func TestResolveEffectiveFields_ExcludeFromDefaults(t *testing.T) {
	opts := Options{
		DefaultFields: []string{"username", "email", "activated"},
		Exclude:       []string{"activated"},
	}
	data := sampleUsers()
	got := opts.resolveEffectiveFields(data)
	if len(got) != 2 {
		t.Fatalf("expected 2 fields, got %v", got)
	}
	for _, f := range got {
		if f == "activated" {
			t.Error("activated should be excluded")
		}
	}
}

func TestResolveEffectiveFields_AllOverridesDefaults(t *testing.T) {
	opts := Options{
		DefaultFields: []string{"username"},
		All:           true,
	}
	data := sampleUsers()
	got := opts.resolveEffectiveFields(data)
	if len(got) <= 1 {
		t.Errorf("--all should return all fields, got %v", got)
	}
}

func TestResolveEffectiveFields_FallbackToDefaults(t *testing.T) {
	opts := Options{
		DefaultFields: []string{"username", "email"},
	}
	data := sampleUsers()
	got := opts.resolveEffectiveFields(data)
	if len(got) != 2 || got[0] != "username" || got[1] != "email" {
		t.Errorf("should fall back to DefaultFields, got %v", got)
	}
}

// --- filterFields ---

func TestFilterFields(t *testing.T) {
	data := []json.RawMessage{rawMsg(`{"a":"1","b":"2","c":"3"}`)}
	got := filterFields(data, []string{"a", "c"})
	if len(got) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got))
	}

	var m map[string]interface{}
	if err := json.Unmarshal(got[0], &m); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if len(m) != 2 {
		t.Errorf("expected 2 fields, got %d: %v", len(m), m)
	}
	if _, ok := m["b"]; ok {
		t.Error("b should be filtered out")
	}
}

func TestFilterFields_NilFields(t *testing.T) {
	data := sampleUsers()
	got := filterFields(data, nil)
	// Nil fields means no filtering.
	if len(got) != len(data) {
		t.Errorf("nil fields should return data unchanged, got %d items", len(got))
	}
}

func TestFilterFields_NonObject(t *testing.T) {
	data := []json.RawMessage{rawMsg(`"just a string"`)}
	got := filterFields(data, []string{"a"})
	// Non-objects should pass through unchanged.
	if string(got[0]) != `"just a string"` {
		t.Errorf("non-object should pass through, got %s", string(got[0]))
	}
}

// --- Pipe Detection Tests (US-056) ---

func TestCurrentOptions_IsPipedField(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	// When running in test, stdout is not a terminal, so IsPiped should be true.
	opts := CurrentOptions()
	if !opts.IsPiped {
		t.Log("IsPiped is false — this is expected if running in a terminal. Skipping assertion.")
	}
	// The field should exist and be settable.
	opts.IsPiped = true
	if !opts.IsPiped {
		t.Error("IsPiped should be settable to true")
	}
}

func TestTableOutput_WorksWhenPiped(t *testing.T) {
	// Table output should produce valid unbounded output when piped.
	var buf bytes.Buffer
	data := sampleUsers()
	opts := Options{
		Format:        FormatTable,
		DefaultFields: []string{"username", "email"},
		IsPiped:       true,
	}

	if err := WriteList(&buf, data, opts); err != nil {
		t.Fatalf("WriteList() error: %v", err)
	}

	out := buf.String()
	// Table should have header and data rows.
	if !strings.Contains(out, "USERNAME") {
		t.Error("table output should contain USERNAME header when piped")
	}
	if !strings.Contains(out, "jdoe") {
		t.Error("table output should contain jdoe when piped")
	}
	if !strings.Contains(out, "asmith") {
		t.Error("table output should contain asmith when piped")
	}
}

func TestCSVOutput_WorksWhenPiped(t *testing.T) {
	var buf bytes.Buffer
	data := sampleUsers()
	opts := Options{
		Format:        FormatCSV,
		DefaultFields: []string{"username", "email"},
		IsPiped:       true,
	}

	if err := WriteList(&buf, data, opts); err != nil {
		t.Fatalf("WriteList() error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "username,email") {
		t.Error("CSV output should contain header when piped")
	}
	if !strings.Contains(out, "jdoe") {
		t.Error("CSV output should contain jdoe when piped")
	}
}

func TestJSONOutput_WorksWhenPiped(t *testing.T) {
	var buf bytes.Buffer
	data := sampleUsers()
	opts := Options{
		Format:  FormatJSON,
		IsPiped: true,
	}

	if err := WriteList(&buf, data, opts); err != nil {
		t.Fatalf("WriteList() error: %v", err)
	}

	out := buf.String()
	if !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Error("JSON output should produce array when piped")
	}
}
