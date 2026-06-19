package apple_mdm

import (
	"strings"
	"testing"
)

func TestParseValuePairs(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		got, err := ParseValuePairs([]string{"a=1", "b=hello world", "c=true"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if want := map[string]string{"a": "1", "b": "hello world", "c": "true"}; !mapsEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("equals in value preserved", func(t *testing.T) {
		got, err := ParseValuePairs([]string{"url=https://example.com/?q=1"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got["url"] != "https://example.com/?q=1" {
			t.Errorf("got %q", got["url"])
		}
	})
	t.Run("missing equals errors", func(t *testing.T) {
		_, err := ParseValuePairs([]string{"orphan"})
		if err == nil {
			t.Error("expected error")
		}
	})
	t.Run("duplicate key errors", func(t *testing.T) {
		_, err := ParseValuePairs([]string{"a=1", "a=2"})
		if err == nil {
			t.Error("expected error on duplicate key")
		}
	})
}

func TestCoerceAndValidate_Booleans(t *testing.T) {
	p := Payload{Type: "x", Keys: []Key{{Name: "Flag", Type: "boolean"}}}
	tests := []struct {
		raw  any
		want bool
		err  bool
	}{
		{true, true, false},
		{false, false, false},
		{"true", true, false},
		{"FALSE", false, false},
		{"yes", false, true}, // strconv.ParseBool only takes true/false/1/0/T/F
		{42, false, true},
	}
	for _, tc := range tests {
		out, err := CoerceAndValidate(p, map[string]any{"Flag": tc.raw})
		if tc.err {
			if err == nil {
				t.Errorf("raw=%v: expected error", tc.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("raw=%v: unexpected error %v", tc.raw, err)
			continue
		}
		if out["Flag"] != tc.want {
			t.Errorf("raw=%v: got %v, want %v", tc.raw, out["Flag"], tc.want)
		}
	}
}

func TestCoerceAndValidate_Integer(t *testing.T) {
	p := Payload{Type: "x", Keys: []Key{{Name: "N", Type: "integer", Range: &Range{Min: 1, Max: 100}}}}
	got, err := CoerceAndValidate(p, map[string]any{"N": "42"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got["N"] != 42 {
		t.Errorf("got %v, want 42", got["N"])
	}
	// Range violation
	if _, err := CoerceAndValidate(p, map[string]any{"N": "150"}); err == nil {
		t.Error("expected range error for 150")
	}
	if _, err := CoerceAndValidate(p, map[string]any{"N": "0"}); err == nil {
		t.Error("expected range error for 0")
	}
}

func TestCoerceAndValidate_RangeList(t *testing.T) {
	p := Payload{Type: "x", Keys: []Key{{
		Name: "Mode", Type: "string",
		RangeList: []any{"None", "Manual", "Auto"},
	}}}
	if _, err := CoerceAndValidate(p, map[string]any{"Mode": "Manual"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	_, err := CoerceAndValidate(p, map[string]any{"Mode": "Bogus"})
	if err == nil {
		t.Error("expected rangelist error")
	}
	if err != nil && !strings.Contains(err.Error(), "not in allowed list") {
		t.Errorf("error should mention allowed list: %v", err)
	}
}

func TestCoerceAndValidate_RequiredKeyMissing(t *testing.T) {
	p := Payload{Type: "x", Keys: []Key{
		{Name: "SSID_STR", Type: "string", Presence: "required"},
		{Name: "AutoJoin", Type: "boolean", Presence: "optional"},
	}}
	_, err := CoerceAndValidate(p, map[string]any{"AutoJoin": true})
	if err == nil {
		t.Fatal("expected required-key error")
	}
	if !strings.Contains(err.Error(), "required key \"SSID_STR\" missing") {
		t.Errorf("error should name the missing key: %v", err)
	}
}

func TestCoerceAndValidate_UnknownKeyErrors(t *testing.T) {
	p := Payload{Type: "x", Keys: []Key{{Name: "Known", Type: "string"}}}
	_, err := CoerceAndValidate(p, map[string]any{"Typo": "value"})
	if err == nil {
		t.Fatal("expected unknown-key error")
	}
	if !strings.Contains(err.Error(), "unknown key \"Typo\"") {
		t.Errorf("error should name the unknown key: %v", err)
	}
}

func TestMergeValues_CLIWinsOverFile(t *testing.T) {
	file := map[string]any{"a": "from-file", "b": "file-only"}
	cli := map[string]string{"a": "from-cli", "c": "cli-only"}
	got := MergeValues(file, cli)
	if got["a"] != "from-cli" {
		t.Errorf("CLI didn't win: got %v", got["a"])
	}
	if got["b"] != "file-only" || got["c"] != "cli-only" {
		t.Errorf("file/cli-only entries lost: %v", got)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
