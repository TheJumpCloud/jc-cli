package apple_mdm

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// withDeterministicUUIDs swaps the package-level newUUID for a
// monotonically incrementing string, restored on cleanup. Lets us
// snapshot-test the emitter output without flaky UUIDs.
func withDeterministicUUIDs(t *testing.T) {
	t.Helper()
	prev := newUUID
	var counter atomic.Int64
	newUUID = func() string {
		n := counter.Add(1)
		return formatDeterministicUUID(n)
	}
	t.Cleanup(func() { newUUID = prev })
}

func formatDeterministicUUID(n int64) string {
	// Apple-style upper-hex, varying only the low bytes so each
	// generated UUID is visibly distinct in test snapshots.
	const fmt = "00000000-0000-4000-8000-%012X"
	return strings.Replace(fmt, "%012X",
		// Use simple zero-padded hex; %X width is awkward with
		// strings.Replace, so format inline.
		zeroPadHex12(n), 1)
}

func zeroPadHex12(n int64) string {
	const hex = "0123456789ABCDEF"
	out := []byte("000000000000")
	v := uint64(n)
	for i := 11; i >= 0; i-- {
		out[i] = hex[v&0xf]
		v >>= 4
	}
	return string(out)
}

func TestEmitMobileconfig_HappyPath(t *testing.T) {
	withDeterministicUUIDs(t)

	schema := Payload{
		ID:    "com.example.test",
		Type:  "com.example.test",
		Title: "Test Payload",
		Keys: []Key{
			{Name: "FlagA", Type: "boolean", Presence: "optional"},
			{Name: "Limit", Type: "integer", Presence: "optional"},
			{Name: "Name", Type: "string", Presence: "required"},
		},
	}
	var buf bytes.Buffer
	err := EmitMobileconfig(&buf, EnvelopeOpts{DisplayName: "Test"}, []PayloadInstance{{
		Schema: schema,
		Values: map[string]any{"FlagA": true, "Limit": 42, "Name": "hello"},
	}})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	out := buf.String()

	// Header invariants — required by Apple for any .mobileconfig.
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"`,
		`<plist version="1.0">`,
		`</plist>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output", want)
		}
	}
	// Envelope must declare a Configuration profile.
	if !strings.Contains(out, "<key>PayloadType</key>\n\t<string>Configuration</string>") {
		t.Error("envelope PayloadType missing or wrong")
	}
	// Inner payload type came from schema.
	if !strings.Contains(out, "<string>com.example.test</string>") {
		t.Error("inner PayloadType missing")
	}
	// Each value type rendered correctly.
	if !strings.Contains(out, "<key>FlagA</key>\n\t\t\t<true/>") {
		t.Error("FlagA bool not rendered")
	}
	if !strings.Contains(out, "<key>Limit</key>\n\t\t\t<integer>42</integer>") {
		t.Error("Limit integer not rendered")
	}
	if !strings.Contains(out, "<key>Name</key>\n\t\t\t<string>hello</string>") {
		t.Error("Name string not rendered")
	}
	// Auto-generated identifier prefix.
	if !strings.Contains(out, "<string>jc.00000000-") {
		t.Error("auto-identifier prefix missing")
	}
}

func TestEmitMobileconfig_RejectsReservedKeys(t *testing.T) {
	withDeterministicUUIDs(t)
	schema := Payload{ID: "x", Type: "x", Keys: []Key{{Name: "Other", Type: "string"}}}
	_, err := buildPayloadDict(PayloadInstance{
		Schema: schema,
		Values: map[string]any{"PayloadType": "naughty"},
	})
	if err == nil {
		t.Fatal("expected error when user values set a reserved Payload* key")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Errorf("error should mention reserved-key reason: %v", err)
	}
}

func TestXMLEscape_HandlesSpecialChars(t *testing.T) {
	in := `a&b<c>d"e'f`
	want := `a&amp;b&lt;c&gt;d&quot;e&apos;f`
	if got := xmlEscape(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteIntegerElem_RejectsNonWholeFloat(t *testing.T) {
	var buf bytes.Buffer
	err := writeIntegerElem(&buf, "", 1.5)
	if err == nil {
		t.Error("expected error for non-whole float")
	}
}

func TestEmitMobileconfig_HandlesPlistDecodedIntegerTypes(t *testing.T) {
	// Bugbot PR #54 re-review: howett.net/plist (used by the edit
	// decode path) returns plist <integer> values as uint64. Pre-fix
	// the emitter's writeIntegerElem only handled int/int32/int64
	// and erred on uint64; merging a decoded integer into an
	// edit-rebuilt mobileconfig failed before the operator ever saw
	// preview.
	withDeterministicUUIDs(t)
	tests := []struct {
		label string
		v     any
	}{
		{"int", 42},
		{"int8", int8(42)},
		{"int16", int16(42)},
		{"int32", int32(42)},
		{"int64", int64(42)},
		{"uint", uint(42)},
		{"uint8", uint8(42)},
		{"uint16", uint16(42)},
		{"uint32", uint32(42)},
		{"uint64", uint64(42)},
		// Skip float64 in this matrix: when emission bypasses
		// CoerceAndValidate (as it does for nested values inside a
		// dict), inferType maps float64 to "real" and the value
		// renders as <real>42</real> not <integer>. That's correct
		// behavior — float64 isn't a plist-decode shape for integers
		// anyway (howett.net/plist returns uint64). Coverage of the
		// float64 path lives in TestWriteIntegerElem_RejectsNonWholeFloat
		// which exercises writeIntegerElem directly.
	}
	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			var buf bytes.Buffer
			err := EmitMobileconfig(&buf, EnvelopeOpts{}, []PayloadInstance{{
				Schema: Payload{Type: "x", Title: "Test", Keys: []Key{
					{Name: "Count", Type: "integer"},
				}},
				Values: map[string]any{"Count": tc.v},
			}})
			if err != nil {
				t.Fatalf("emit failed for %s: %v", tc.label, err)
			}
			if !strings.Contains(buf.String(), "<integer>42</integer>") {
				t.Errorf("%s not emitted as <integer>42</integer>:\n%s", tc.label, buf.String())
			}
		})
	}
}

func TestEmitMobileconfig_HandlesNestedDictWithPlistDecodedInts(t *testing.T) {
	// The real-world hazard: edit merge keeps a dict whose subkeys
	// are uint64. The dict goes straight to writeDictElem →
	// writePlistValue → inferType → writeIntegerElem. Pre-fix the
	// whole chain failed at the inferType step (no case for uint64).
	withDeterministicUUIDs(t)
	var buf bytes.Buffer
	err := EmitMobileconfig(&buf, EnvelopeOpts{},
		[]PayloadInstance{{
			Schema: Payload{Type: "x", Keys: []Key{{Name: "Cfg", Type: "dictionary"}}},
			Values: map[string]any{
				"Cfg": map[string]any{
					"DiskSleepTimer":    uint64(60),
					"DisplaySleepTimer": uint64(120),
				},
			},
		}})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(buf.String(), "<integer>60</integer>") ||
		!strings.Contains(buf.String(), "<integer>120</integer>") {
		t.Errorf("nested uint64 not emitted:\n%s", buf.String())
	}
}

func TestEmitMobileconfig_HandlesPlistDecodedDate(t *testing.T) {
	// howett.net/plist decodes <date> to time.Time. Pre-fix the date
	// element only accepted string RFC3339 and rejected time.Time.
	withDeterministicUUIDs(t)
	when := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := EmitMobileconfig(&buf, EnvelopeOpts{}, []PayloadInstance{{
		Schema: Payload{Type: "x", Keys: []Key{{Name: "Expires", Type: "date"}}},
		Values: map[string]any{"Expires": when},
	}})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(buf.String(), "<date>2026-06-19T12:00:00Z</date>") {
		t.Errorf("time.Time not formatted as RFC3339 date:\n%s", buf.String())
	}
}

func TestWriteIntegerElem_RejectsOverflowingUint64(t *testing.T) {
	// Defensive guard — Apple's plist <integer> is signed in practice.
	// A uint64 over int64-max would be malformed; surface the
	// overflow rather than silently truncating.
	var buf bytes.Buffer
	err := writeIntegerElem(&buf, "", uint64(1<<63))
	if err == nil {
		t.Error("expected overflow error for uint64 > int64 max")
	}
}

func TestRandomUUIDv4_VariantBits(t *testing.T) {
	for i := 0; i < 10; i++ {
		u := randomUUIDv4()
		// V4 UUID has format xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx where y is 8/9/A/B.
		if u[14] != '4' {
			t.Errorf("version nibble: got %c, want 4 in %s", u[14], u)
		}
		switch u[19] {
		case '8', '9', 'A', 'B':
		default:
			t.Errorf("variant nibble: got %c, want 8/9/A/B in %s", u[19], u)
		}
	}
}
