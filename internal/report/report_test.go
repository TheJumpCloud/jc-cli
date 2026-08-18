package report

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFamilyNames_Sorted(t *testing.T) {
	got := FamilyNames()
	want := []string{"builder", "custom", "saved", "scheduled", "templates"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FamilyNames() = %v, want %v", got, want)
	}
}

func TestFamilies_EnvelopeKeys(t *testing.T) {
	// Guard the live-verified envelope keys against drift.
	cases := map[string][2]string{ // name -> {listKey, endpoint}
		"templates": {"reportTemplates", "/reports/jumpcloud"},
		"custom":    {"reportViews", "/reports/custom"},
		"builder":   {"customReports", "/reports/custom-reports"},
		"saved":     {"savedReports", "/reports/saved-reports"},
		"scheduled": {"scheduledReports", "/reports/scheduled"},
	}
	for name, want := range cases {
		f, ok := Families[name]
		if !ok {
			t.Errorf("family %q missing", name)
			continue
		}
		if f.ListKey != want[0] || f.ListEndpoint != want[1] {
			t.Errorf("%s = {%s, %s}, want {%s, %s}", name, f.ListKey, f.ListEndpoint, want[0], want[1])
		}
	}
}

func TestUnwrap(t *testing.T) {
	in := json.RawMessage(`{"reportTemplate":{"id":"abc","displayName":"X"}}`)
	out := Unwrap(in, "reportTemplate")
	var m map[string]any
	if json.Unmarshal(out, &m); m["id"] != "abc" {
		t.Errorf("Unwrap = %s", out)
	}
	// Missing key falls back to raw.
	bare := json.RawMessage(`{"id":"abc"}`)
	if got := Unwrap(bare, "reportTemplate"); string(got) != string(bare) {
		t.Errorf("Unwrap fallback = %s", got)
	}
}
