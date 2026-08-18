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

func TestWritableFamilies(t *testing.T) {
	for _, n := range []string{"custom", "builder", "scheduled"} {
		if !Families[n].Writable {
			t.Errorf("%s should be writable", n)
		}
	}
	for _, n := range []string{"templates", "saved"} {
		if Families[n].Writable {
			t.Errorf("%s must not be writable", n)
		}
	}
}

func TestLooksLikeID(t *testing.T) {
	if !LooksLikeID("6a84b421d97b965cd89c01b4") {
		t.Error("24-hex should be an id")
	}
	if !LooksLikeID("7761ca72-a7b5-40a1-b242-5d7d01ca6821") {
		t.Error("UUID (scheduled id) should be an id")
	}
	if LooksLikeID("Android Applications") {
		t.Error("a name is not an id")
	}
}

func TestWrapBodyAndParse(t *testing.T) {
	f := Families["custom"]
	// WrapBody uses the family's GetKey.
	b := f.WrapBody(json.RawMessage(`{"displayName":"X"}`))
	if _, ok := b["reportView"]; !ok {
		t.Fatalf("WrapBody key = %v", b)
	}
	// ParseReportFile accepts bare and wrapped.
	bare, _ := f.ParseReportFile([]byte(`{"displayName":"bare"}`))
	var m map[string]any
	if json.Unmarshal(bare, &m); m["displayName"] != "bare" {
		t.Errorf("bare parse = %s", bare)
	}
	wrapped, _ := f.ParseReportFile([]byte(`{"reportView":{"displayName":"wrapped"}}`))
	m = nil
	if json.Unmarshal(wrapped, &m); m["displayName"] != "wrapped" {
		t.Errorf("wrapped not unwrapped = %s", wrapped)
	}
}

func TestExportBody(t *testing.T) {
	b := ExportBody("rpt", "", true, json.RawMessage(`{"fields":{}}`))
	if b["exportType"] != "csv" {
		t.Errorf("default exportType = %v", b["exportType"])
	}
	if b["reportName"] != "rpt" || b["notifyByEmail"] != true {
		t.Errorf("export body = %v", b)
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
