package component

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTable_MoveCursor(t *testing.T) {
	tbl := &Table{
		Columns: []string{"name"},
		Rows: []json.RawMessage{
			json.RawMessage(`{"name":"a"}`),
			json.RawMessage(`{"name":"b"}`),
			json.RawMessage(`{"name":"c"}`),
		},
		Width:  80,
		Height: 10,
	}

	tbl.MoveCursor(1)
	if tbl.Cursor != 1 {
		t.Errorf("Cursor = %d, want 1", tbl.Cursor)
	}

	tbl.MoveCursor(1)
	if tbl.Cursor != 2 {
		t.Errorf("Cursor = %d, want 2", tbl.Cursor)
	}

	// Should clamp at end.
	tbl.MoveCursor(1)
	if tbl.Cursor != 2 {
		t.Errorf("Cursor = %d, want 2 (clamped)", tbl.Cursor)
	}
}

func TestTable_MoveCursorNegative(t *testing.T) {
	tbl := &Table{
		Columns: []string{"name"},
		Rows: []json.RawMessage{
			json.RawMessage(`{"name":"a"}`),
			json.RawMessage(`{"name":"b"}`),
		},
		Width:  80,
		Height: 10,
	}

	// Should clamp at 0.
	tbl.MoveCursor(-1)
	if tbl.Cursor != 0 {
		t.Errorf("Cursor = %d, want 0", tbl.Cursor)
	}
}

func TestTable_GoToTopAndBottom(t *testing.T) {
	tbl := &Table{
		Columns: []string{"name"},
		Rows:    make([]json.RawMessage, 10),
		Width:   80,
		Height:  5,
	}

	tbl.GoToBottom()
	if tbl.Cursor != 9 {
		t.Errorf("Cursor = %d, want 9", tbl.Cursor)
	}

	tbl.GoToTop()
	if tbl.Cursor != 0 {
		t.Errorf("Cursor = %d, want 0", tbl.Cursor)
	}
	if tbl.Offset != 0 {
		t.Errorf("Offset = %d, want 0", tbl.Offset)
	}
}

func TestTable_ScrollOffset(t *testing.T) {
	tbl := &Table{
		Columns: []string{"name"},
		Rows:    make([]json.RawMessage, 20),
		Width:   80,
		Height:  5,
	}

	// Move cursor past visible area.
	for i := 0; i < 7; i++ {
		tbl.MoveCursor(1)
	}

	if tbl.Offset == 0 {
		t.Error("Offset should have scrolled")
	}
	if tbl.Cursor != 7 {
		t.Errorf("Cursor = %d, want 7", tbl.Cursor)
	}
}

func TestTable_SelectedRow(t *testing.T) {
	row := json.RawMessage(`{"name":"alice"}`)
	tbl := &Table{
		Columns: []string{"name"},
		Rows:    []json.RawMessage{row},
		Width:   80,
	}

	selected := tbl.SelectedRow()
	if string(selected) != string(row) {
		t.Errorf("SelectedRow = %s, want %s", selected, row)
	}
}

func TestTable_SelectedRowEmpty(t *testing.T) {
	tbl := &Table{Columns: []string{"name"}, Width: 80}
	if tbl.SelectedRow() != nil {
		t.Error("SelectedRow on empty table should return nil")
	}
}

func TestTable_View(t *testing.T) {
	tbl := &Table{
		Columns: []string{"name", "email"},
		Rows: []json.RawMessage{
			json.RawMessage(`{"name":"alice","email":"alice@example.com"}`),
			json.RawMessage(`{"name":"bob","email":"bob@example.com"}`),
		},
		Width:  80,
		Height: 10,
	}

	view := tbl.View()
	if !strings.Contains(view, "name") {
		t.Error("view should contain 'name' header")
	}
	if !strings.Contains(view, "alice") {
		t.Error("view should contain 'alice'")
	}
	if !strings.Contains(view, "bob") {
		t.Error("view should contain 'bob'")
	}
}

func TestTable_ViewEmpty(t *testing.T) {
	tbl := &Table{Width: 80}
	if tbl.View() != "" {
		t.Error("view with no columns should be empty")
	}
}

func TestTable_ViewZeroWidth(t *testing.T) {
	tbl := &Table{
		Columns: []string{"name"},
		Rows:    []json.RawMessage{json.RawMessage(`{"name":"x"}`)},
	}
	if tbl.View() != "" {
		t.Error("view with zero width should be empty")
	}
}

func TestTable_SortIndicator(t *testing.T) {
	tbl := &Table{
		Columns:   []string{"name"},
		Rows:      []json.RawMessage{json.RawMessage(`{"name":"a"}`)},
		Width:     80,
		Height:    10,
		SortField: "name",
	}

	view := tbl.View()
	if !strings.Contains(view, "^") {
		t.Error("ascending sort should show ^ indicator")
	}

	tbl.SortDesc = true
	view = tbl.View()
	if !strings.Contains(view, "v") {
		t.Error("descending sort should show v indicator")
	}
}

func TestExtractID(t *testing.T) {
	data := json.RawMessage(`{"_id":"abc123","name":"test"}`)
	id := ExtractID(data, "_id")
	if id != "abc123" {
		t.Errorf("ExtractID = %q, want 'abc123'", id)
	}
}

func TestExtractName(t *testing.T) {
	data := json.RawMessage(`{"_id":"abc","username":"jdoe"}`)
	name := ExtractName(data, "username")
	if name != "jdoe" {
		t.Errorf("ExtractName = %q, want 'jdoe'", name)
	}
}

func TestFormatCount(t *testing.T) {
	tests := []struct {
		count, total int
		want         string
	}{
		{5, 10, "5 of 10"},
		{10, 10, "10"},
		{5, 0, "5"},
	}
	for _, tt := range tests {
		got := FormatCount(tt.count, tt.total)
		if got != tt.want {
			t.Errorf("FormatCount(%d, %d) = %q, want %q", tt.count, tt.total, got, tt.want)
		}
	}
}

func TestTruncateToWidth(t *testing.T) {
	tests := []struct {
		input    string
		maxWidth int
		want     string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hell~"},
		{"ab", 2, "ab"},
	}
	for _, tt := range tests {
		got := truncateToWidth(tt.input, tt.maxWidth)
		if got != tt.want {
			t.Errorf("truncateToWidth(%q, %d) = %q, want %q", tt.input, tt.maxWidth, got, tt.want)
		}
	}
}

func TestJsonValueToString(t *testing.T) {
	tests := []struct {
		input json.RawMessage
		want  string
	}{
		{json.RawMessage(`"hello"`), "hello"},
		{json.RawMessage(`true`), "true"},
		{json.RawMessage(`false`), "false"},
		{json.RawMessage(`42`), "42"},
		{json.RawMessage(`null`), ""},
		{json.RawMessage(`{"key":"val"}`), `{"key":"val"}`},
		{json.RawMessage(`"line1\nline2\nline3"`), "line1 line2 line3"},
		{json.RawMessage(`"tabs\there\ttoo"`), "tabs here too"},
		{json.RawMessage(`"#!/bin/bash\n\necho hello\n"`), "#!/bin/bash echo hello"},
	}
	for _, tt := range tests {
		got := jsonValueToString(tt.input)
		if got != tt.want {
			t.Errorf("jsonValueToString(%s) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
