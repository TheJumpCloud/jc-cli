package component

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// Table renders a scrollable, sortable table of JSON data.
type Table struct {
	Columns   []string
	Rows      []json.RawMessage
	Cursor    int
	Offset    int // Scroll offset (first visible row index)
	Width     int
	Height    int // Available height for rows (excluding header)
	SortField string
	SortDesc  bool
}

// MoveCursor moves the selection cursor by delta, clamping to bounds.
func (t *Table) MoveCursor(delta int) {
	t.Cursor += delta
	if t.Cursor < 0 {
		t.Cursor = 0
	}
	if t.Cursor >= len(t.Rows) {
		t.Cursor = len(t.Rows) - 1
	}
	if t.Cursor < 0 {
		t.Cursor = 0
	}

	// Scroll to keep cursor visible.
	if t.Cursor < t.Offset {
		t.Offset = t.Cursor
	}
	if t.Height > 0 && t.Cursor >= t.Offset+t.Height {
		t.Offset = t.Cursor - t.Height + 1
	}
}

// GoToTop moves cursor to the first row.
func (t *Table) GoToTop() {
	t.Cursor = 0
	t.Offset = 0
}

// GoToBottom moves cursor to the last row.
func (t *Table) GoToBottom() {
	t.Cursor = len(t.Rows) - 1
	if t.Cursor < 0 {
		t.Cursor = 0
	}
	if t.Height > 0 && t.Cursor >= t.Height {
		t.Offset = t.Cursor - t.Height + 1
	}
}

// SelectedRow returns the currently selected row data, or nil.
func (t *Table) SelectedRow() json.RawMessage {
	if t.Cursor < 0 || t.Cursor >= len(t.Rows) {
		return nil
	}
	return t.Rows[t.Cursor]
}

// View renders the table.
func (t *Table) View() string {
	if len(t.Columns) == 0 || t.Width <= 0 {
		return ""
	}

	colWidths := t.computeColumnWidths()

	var sb strings.Builder

	// Header row.
	var headerCells []string
	for i, col := range t.Columns {
		cell := col
		if col == t.SortField {
			arrow := " ^"
			if t.SortDesc {
				arrow = " v"
			}
			cell += style.SortIndicator.Render(arrow)
		}
		cell = truncateToWidth(cell, colWidths[i])
		cell = padToWidth(cell, colWidths[i])
		headerCells = append(headerCells, cell)
	}
	header := style.TableHeader.Render(strings.Join(headerCells, "  "))
	sb.WriteString(header)
	sb.WriteString("\n")

	// Data rows.
	visibleRows := t.Height
	if visibleRows <= 0 {
		visibleRows = len(t.Rows)
	}

	end := t.Offset + visibleRows
	if end > len(t.Rows) {
		end = len(t.Rows)
	}

	for i := t.Offset; i < end; i++ {
		row := t.Rows[i]
		fields := extractFields(row)

		var cells []string
		for ci, col := range t.Columns {
			val := fields[col]
			val = truncateToWidth(val, colWidths[ci])
			val = padToWidth(val, colWidths[ci])
			cells = append(cells, val)
		}

		line := strings.Join(cells, "  ")
		if i == t.Cursor {
			line = style.SelectedRow.Width(t.Width).Render(line)
		} else {
			line = style.NormalRow.Render(line)
		}
		sb.WriteString(line)
		if i < end-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// computeColumnWidths distributes available width across columns.
func (t *Table) computeColumnWidths() []int {
	n := len(t.Columns)
	if n == 0 {
		return nil
	}

	// Reserve 2 chars gap between columns.
	available := t.Width - (n-1)*2
	if available < n {
		available = n
	}

	// Start with header widths, then scan data for max.
	widths := make([]int, n)
	for i, col := range t.Columns {
		widths[i] = len(col) + 2 // Extra space for sort indicator
	}

	// Sample up to 100 rows for width calculation.
	sampleSize := len(t.Rows)
	if sampleSize > 100 {
		sampleSize = 100
	}
	for _, row := range t.Rows[:sampleSize] {
		fields := extractFields(row)
		for i, col := range t.Columns {
			val := fields[col]
			if len(val) > widths[i] {
				widths[i] = len(val)
			}
		}
	}

	// Cap each column at available/n*2 to prevent one column from dominating.
	maxCol := available * 2 / n
	if maxCol < 10 {
		maxCol = 10
	}
	for i := range widths {
		if widths[i] > maxCol {
			widths[i] = maxCol
		}
	}

	// Distribute remaining space proportionally.
	total := 0
	for _, w := range widths {
		total += w
	}
	if total > available {
		// Shrink proportionally.
		for i := range widths {
			widths[i] = widths[i] * available / total
			if widths[i] < 4 {
				widths[i] = 4
			}
		}
	}

	return widths
}

// extractFields parses a JSON object into a flat map of string values.
func extractFields(data json.RawMessage) map[string]string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}

	fields := make(map[string]string, len(obj))
	for k, v := range obj {
		fields[k] = jsonValueToString(v)
	}
	return fields
}

// jsonValueToString converts a JSON value to a display string.
func jsonValueToString(v json.RawMessage) string {
	if len(v) == 0 {
		return ""
	}

	// Try string first (most common).
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return s
	}

	// Try bool.
	var b bool
	if err := json.Unmarshal(v, &b); err == nil {
		if b {
			return "true"
		}
		return "false"
	}

	// Try number.
	var n json.Number
	if err := json.Unmarshal(v, &n); err == nil {
		return n.String()
	}

	// Check for null.
	if string(v) == "null" {
		return ""
	}

	// Array or object — show compact form.
	return string(v)
}

func truncateToWidth(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		if maxWidth <= 0 {
			return ""
		}
		return s[:maxWidth]
	}
	runes := []rune(s)
	for len(runes) > 0 && runeWidth(runes) > maxWidth-1 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "~"
}

func runeWidth(runes []rune) int {
	return lipgloss.Width(string(runes))
}

func padToWidth(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// ExtractColumnNames returns sorted field names from a JSON object.
// Useful for deriving table columns when no defaults are defined.
func ExtractColumnNames(data json.RawMessage) []string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	names := make([]string, 0, len(obj))
	for k := range obj {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// ExtractID extracts the ID field value from a JSON row.
func ExtractID(data json.RawMessage, idField string) string {
	fields := extractFields(data)
	if fields == nil {
		return ""
	}
	return fields[idField]
}

// ExtractName extracts the name field value from a JSON row.
func ExtractName(data json.RawMessage, nameField string) string {
	fields := extractFields(data)
	if fields == nil {
		return ""
	}
	return fields[nameField]
}

// FormatCount returns a human-readable count string.
func FormatCount(count, total int) string {
	if total > 0 && total != count {
		return fmt.Sprintf("%d of %d", count, total)
	}
	return fmt.Sprintf("%d", count)
}
