package apple_mdm

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"
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
