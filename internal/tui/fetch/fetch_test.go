package fetch

import (
	"encoding/json"
	"testing"
)

func TestFlattenAssociation(t *testing.T) {
	// Typical V2 graph response item.
	input := json.RawMessage(`{"to":{"type":"application","id":"app001"},"attributes":null}`)
	result := flattenAssociation(input)

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(result, &obj); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// "to" should be promoted to top level.
	if _, ok := obj["to"]; ok {
		t.Error("'to' should be removed from top level")
	}

	typeVal := string(obj["type"])
	if typeVal != `"application"` {
		t.Errorf("type = %s, want '\"application\"'", typeVal)
	}

	idVal := string(obj["id"])
	if idVal != `"app001"` {
		t.Errorf("id = %s, want '\"app001\"'", idVal)
	}

	// Non-"to" fields should be preserved.
	if _, ok := obj["attributes"]; !ok {
		t.Error("'attributes' should be preserved")
	}
}

func TestFlattenAssociation_NoToField(t *testing.T) {
	input := json.RawMessage(`{"type":"user","id":"u001"}`)
	result := flattenAssociation(input)

	// Should pass through unchanged.
	if string(result) != string(input) {
		t.Errorf("result = %s, want passthrough", string(result))
	}
}

func TestFlattenAssociation_InvalidJSON(t *testing.T) {
	input := json.RawMessage(`not json`)
	result := flattenAssociation(input)
	if string(result) != string(input) {
		t.Errorf("invalid JSON should pass through unchanged")
	}
}
