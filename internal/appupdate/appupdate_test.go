package appupdate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMerge(t *testing.T) {
	cur := json.RawMessage(`{"_id":"x","name":"bookmark","ssoUrl":"https://a","description":"d","displayLabel":"old"}`)
	body, err := Merge(cur, map[string]any{"displayLabel": "new", "aiReady": true})
	if err != nil {
		t.Fatal(err)
	}
	if body["name"] != "bookmark" || body["ssoUrl"] != "https://a" || body["description"] != "d" {
		t.Errorf("current fields lost: %v", body)
	}
	if body["displayLabel"] != "new" || body["aiReady"] != true {
		t.Errorf("changes not overlaid: %v", body)
	}
	for _, bad := range []string{`null`, `[]`, `x`} {
		if _, err := Merge(json.RawMessage(bad), nil); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestUnapplied(t *testing.T) {
	result := json.RawMessage(`{"name":"bookmark","displayLabel":"old","aiReady":false,"n":3,"config":{"a":1}}`)
	changes := map[string]any{"displayLabel": "new", "name": "bookmark", "aiReady": true, "n": 3, "config": map[string]any{"a": 2}}
	missing, err := Unapplied(result, changes, "aiReady")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(missing, ",") != "displayLabel" {
		t.Errorf("missing = %v, want [displayLabel] (aiReady skipped, objects not compared, ints equal floats)", missing)
	}
	if !strings.Contains(ErrUnapplied(missing).Error(), "did not change") {
		t.Error("message")
	}
}
