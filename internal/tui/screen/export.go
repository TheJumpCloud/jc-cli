package screen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/klaassen-consulting/jc/internal/output"
)

// exportListToClipboard renders rows as JSON and copies to clipboard.
func exportListToClipboard(rows []json.RawMessage, fields []string) (string, error) {
	var buf bytes.Buffer
	opts := output.Options{
		Format:        output.FormatJSON,
		DefaultFields: fields,
		All:           true,
		IsPiped:       true,
	}
	if err := output.WriteList(&buf, rows, opts); err != nil {
		return "", fmt.Errorf("format error: %w", err)
	}
	if err := clipboardWriteFunc(buf.String()); err != nil {
		return "", fmt.Errorf("clipboard error: %w", err)
	}
	return fmt.Sprintf("Copied %d items as JSON", len(rows)), nil
}

// exportListToFile writes rows to a file in the given format.
func exportListToFile(rows []json.RawMessage, fields []string, format output.Format, resourceKey, ext string) (string, error) {
	path := exportFilePath(resourceKey, ext)

	opts := output.Options{
		Format:        format,
		DefaultFields: fields,
		All:           true,
		IsPiped:       true,
	}

	var buf bytes.Buffer
	if err := output.WriteList(&buf, rows, opts); err != nil {
		return "", fmt.Errorf("format error: %w", err)
	}

	if err := writeFileAtomic(path, buf.Bytes()); err != nil {
		return "", err
	}

	return fmt.Sprintf("Exported %d items to %s", len(rows), path), nil
}

// exportListToFileAt is a testable variant that writes to a specific path.
func exportListToFileAt(rows []json.RawMessage, fields []string, format output.Format, path string) (string, error) {
	opts := output.Options{
		Format:        format,
		DefaultFields: fields,
		All:           true,
		IsPiped:       true,
	}

	var buf bytes.Buffer
	if err := output.WriteList(&buf, rows, opts); err != nil {
		return "", fmt.Errorf("format error: %w", err)
	}

	if err := writeFileAtomic(path, buf.Bytes()); err != nil {
		return "", err
	}

	return fmt.Sprintf("Exported %d items to %s", len(rows), path), nil
}

// exportSingleToClipboard renders a single object as JSON and copies to clipboard.
func exportSingleToClipboard(data json.RawMessage) (string, error) {
	var buf bytes.Buffer
	opts := output.Options{
		Format:  output.FormatJSON,
		All:     true,
		IsPiped: true,
	}
	if err := output.WriteSingle(&buf, data, opts); err != nil {
		return "", fmt.Errorf("format error: %w", err)
	}
	if err := clipboardWriteFunc(buf.String()); err != nil {
		return "", fmt.Errorf("clipboard error: %w", err)
	}
	return "Copied resource as JSON", nil
}

// exportSingleToFile writes a single object as JSON to a file.
func exportSingleToFile(data json.RawMessage, resourceKey, ext string) (string, error) {
	path := exportFilePath(resourceKey, ext)

	var buf bytes.Buffer
	opts := output.Options{
		Format:  output.FormatJSON,
		All:     true,
		IsPiped: true,
	}
	if err := output.WriteSingle(&buf, data, opts); err != nil {
		return "", fmt.Errorf("format error: %w", err)
	}

	if err := writeFileAtomic(path, buf.Bytes()); err != nil {
		return "", err
	}

	return fmt.Sprintf("Exported to %s", path), nil
}

// exportSingleToFileAt is a testable variant that writes to a specific path.
func exportSingleToFileAt(data json.RawMessage, path string) (string, error) {
	var buf bytes.Buffer
	opts := output.Options{
		Format:  output.FormatJSON,
		All:     true,
		IsPiped: true,
	}
	if err := output.WriteSingle(&buf, data, opts); err != nil {
		return "", fmt.Errorf("format error: %w", err)
	}

	if err := writeFileAtomic(path, buf.Bytes()); err != nil {
		return "", err
	}

	return fmt.Sprintf("Exported to %s", path), nil
}

// exportFilePath builds a path like ~/Downloads/jc-{key}-{timestamp}.{ext}.
func exportFilePath(resourceKey, ext string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	ts := time.Now().Format("20060102-150405")
	name := fmt.Sprintf("jc-%s-%s.%s", resourceKey, ts, ext)
	return filepath.Join(home, "Downloads", name)
}

// writeFileAtomic writes data to path using a temp file + rename for safety.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".jc-export-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
