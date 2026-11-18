package aiready

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckApplied(t *testing.T) {
	if err := CheckApplied(json.RawMessage(`{"aiReady":true}`), true); err != nil {
		t.Errorf("applied true: %v", err)
	}
	if err := CheckApplied(json.RawMessage(`{"aiReady":false}`), false); err != nil {
		t.Errorf("applied false: %v", err)
	}
	// The live behaviour: true requested, 200, value kept at false.
	err := CheckApplied(json.RawMessage(`{"aiReady":false}`), true)
	if err == nil || !strings.Contains(err.Error(), "MCP connection") {
		t.Errorf("silently ignored true must fail naming the MCP connection, got %v", err)
	}
	if err := CheckApplied(json.RawMessage(`{"aiReady":true}`), false); err == nil {
		t.Error("ignored false must fail")
	}
	if err := CheckApplied(json.RawMessage(`{"name":"x"}`), true); err == nil {
		t.Error("missing field must fail")
	}
}

func TestFilter(t *testing.T) {
	apps := []json.RawMessage{
		json.RawMessage(`{"_id":"a","aiReady":true}`),
		json.RawMessage(`{"_id":"b","aiReady":false}`),
		json.RawMessage(`{"_id":"c"}`),
	}
	yes, _ := Filter(apps, true)
	no, _ := Filter(apps, false)
	if len(yes) != 1 || len(no) != 2 {
		t.Errorf("true=%d false=%d, want 1 and 2 (missing counts as false)", len(yes), len(no))
	}
	if out, _ := Filter(nil, true); out == nil {
		t.Error("empty result must be a non-nil slice")
	}
}
