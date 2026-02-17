package screen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/output"
)

func TestExportListToClipboard(t *testing.T) {
	var clipped string
	origClip := clipboardWriteFunc
	clipboardWriteFunc = func(s string) error { clipped = s; return nil }
	defer func() { clipboardWriteFunc = origClip }()

	rows := []json.RawMessage{
		json.RawMessage(`{"_id":"aaa111bbb222ccc333ddd444","username":"alice"}`),
		json.RawMessage(`{"_id":"eee555fff666aaa777bbb888","username":"bob"}`),
	}
	fields := []string{"_id", "username"}

	flash, err := exportListToClipboard(rows, fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(flash, "2 items") {
		t.Errorf("flash = %q, want to contain '2 items'", flash)
	}
	if !strings.Contains(clipped, "alice") {
		t.Errorf("clipboard should contain 'alice', got %q", clipped)
	}
	if !strings.Contains(clipped, "bob") {
		t.Errorf("clipboard should contain 'bob', got %q", clipped)
	}
}

func TestExportListToFile_JSON(t *testing.T) {
	dir := t.TempDir()

	rows := []json.RawMessage{
		json.RawMessage(`{"_id":"aaa111bbb222ccc333ddd444","username":"alice"}`),
	}
	fields := []string{"_id", "username"}

	path := filepath.Join(dir, "test-export.json")
	// Write directly using the helper's internals to test in a temp dir.
	flash, err := exportListToFileAt(rows, fields, output.FormatJSON, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(flash, "1 items") {
		t.Errorf("flash = %q, want to contain '1 items'", flash)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "alice") {
		t.Errorf("file should contain 'alice', got %q", string(data))
	}
}

func TestExportListToFile_CSV(t *testing.T) {
	dir := t.TempDir()

	rows := []json.RawMessage{
		json.RawMessage(`{"_id":"aaa111bbb222ccc333ddd444","username":"alice"}`),
		json.RawMessage(`{"_id":"eee555fff666aaa777bbb888","username":"bob"}`),
	}
	fields := []string{"_id", "username"}

	path := filepath.Join(dir, "test-export.csv")
	flash, err := exportListToFileAt(rows, fields, output.FormatCSV, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(flash, "2 items") {
		t.Errorf("flash = %q, want to contain '2 items'", flash)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "_id") {
		t.Errorf("CSV should contain header '_id', got %q", content)
	}
	if !strings.Contains(content, "alice") {
		t.Errorf("CSV should contain 'alice', got %q", content)
	}
}

func TestExportSingleToClipboard(t *testing.T) {
	var clipped string
	origClip := clipboardWriteFunc
	clipboardWriteFunc = func(s string) error { clipped = s; return nil }
	defer func() { clipboardWriteFunc = origClip }()

	data := json.RawMessage(`{"_id":"aaa111bbb222ccc333ddd444","username":"alice","email":"alice@example.com"}`)

	flash, err := exportSingleToClipboard(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(flash, "JSON") {
		t.Errorf("flash = %q, want to contain 'JSON'", flash)
	}
	if !strings.Contains(clipped, "alice") {
		t.Errorf("clipboard should contain 'alice', got %q", clipped)
	}
}

func TestExportSingleToFile(t *testing.T) {
	dir := t.TempDir()

	data := json.RawMessage(`{"_id":"aaa111bbb222ccc333ddd444","username":"alice"}`)

	path := filepath.Join(dir, "test-single.json")
	flash, err := exportSingleToFileAt(data, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(flash, path) {
		t.Errorf("flash = %q, want to contain path %q", flash, path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(content), "alice") {
		t.Errorf("file should contain 'alice', got %q", string(content))
	}
}

func TestExportFilePath(t *testing.T) {
	path := exportFilePath("users", "json")
	if !strings.Contains(path, "jc-users-") {
		t.Errorf("path = %q, want to contain 'jc-users-'", path)
	}
	if !strings.HasSuffix(path, ".json") {
		t.Errorf("path = %q, want .json suffix", path)
	}
	if !strings.Contains(path, "Downloads") {
		t.Errorf("path = %q, want to contain 'Downloads'", path)
	}
}
