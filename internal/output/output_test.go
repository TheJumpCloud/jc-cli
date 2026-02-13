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
		{Format("yaml"), false},
		{Format("ndjson"), false},
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
