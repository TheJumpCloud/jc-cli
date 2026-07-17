package bundle

import (
	"bytes"
	"strings"
	"testing"
)

// TestBuiltins_NoUnresolvedPlaceholders is the guard for the review
// finding (2026-07-17): a nested $ODV survived the converter and
// shipped a literal placeholder as a device-facing password regex.
// Any literal "$ODV" in a builtin is a converter bug — fail loudly
// here rather than push it to managed devices.
func TestBuiltins_NoUnresolvedPlaceholders(t *testing.T) {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := builtinFS.ReadFile("builtin/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("$ODV")) {
			t.Errorf("%s contains an unresolved $ODV placeholder — regenerate with the current converter", e.Name())
		}
	}
}
