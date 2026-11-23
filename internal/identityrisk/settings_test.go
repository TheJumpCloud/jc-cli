package identityrisk

import (
	"encoding/json"
	"testing"
)

func TestParseSettings_BothShapes(t *testing.T) {
	obj := json.RawMessage(`{"features":[{"name":"f","settings":{"b":1,"a":true}}]}`)
	lst := json.RawMessage(`{"features":[{"name":"f","settings":[{"name":"b","value":1},{"name":"a","value":true}]}]}`)
	for _, raw := range []json.RawMessage{obj, lst} {
		rows, err := ParseSettings(raw)
		if err != nil || len(rows) != 2 || rows[0].Name != "a" || string(rows[0].Value) != "true" {
			t.Fatalf("rows=%v err=%v", rows, err)
		}
	}
}

func TestValueHelpers(t *testing.T) {
	if _, err := ParseValue(""); err == nil {
		t.Error("empty accepted")
	}
	if _, err := ParseValue("true false"); err == nil {
		t.Error("two values accepted")
	}
	if JSONType(json.RawMessage(`["x"]`)) != "array" || JSONType(json.RawMessage(`false`)) != "boolean" {
		t.Error("JSONType")
	}
	if !SameValue(json.RawMessage(`{"a":1,"b":2}`), json.RawMessage(`{"b":2, "a":1}`)) {
		t.Error("SameValue")
	}
	if !IsDisabling("enabled", json.RawMessage(`false`)) || IsDisabling("enabled", json.RawMessage(`true`)) {
		t.Error("IsDisabling")
	}
}
