package alert

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBulkFilterInput_IsEmpty(t *testing.T) {
	if !(BulkFilterInput{}).IsEmpty() {
		t.Error("zero-value filter should be empty")
	}
	if (BulkFilterInput{Title: "x"}).IsEmpty() {
		t.Error("title-only filter should not be empty")
	}
	if (BulkFilterInput{Severity: []string{"high"}}).IsEmpty() {
		t.Error("severity filter should not be empty")
	}
}

func TestBuildFilter_NormalizesAndOmits(t *testing.T) {
	in := BulkFilterInput{
		Category:   []string{"system", "ALERT_CATEGORY_DIRECTORY"},
		Severity:   []string{"high"},
		Status:     []string{"auto-resolved"},
		SourceType: []string{"device"},
		SourceID:   []string{"id1"},
		Title:      "boom",
	}
	f := in.BuildFilter()
	if got := f["category"].([]string); !reflect.DeepEqual(got, []string{"ALERT_CATEGORY_SYSTEM", "ALERT_CATEGORY_DIRECTORY"}) {
		t.Errorf("category = %v", got)
	}
	if got := f["severity"].([]string); !reflect.DeepEqual(got, []string{"ALERT_SEVERITY_HIGH"}) {
		t.Errorf("severity = %v", got)
	}
	if got := f["status"].([]string); !reflect.DeepEqual(got, []string{"ALERT_STATUS_AUTO_RESOLVED"}) {
		t.Errorf("status = %v", got)
	}
	if got := f["sourceType"].([]string); !reflect.DeepEqual(got, []string{"ALERT_SOURCE_TYPE_DEVICE"}) {
		t.Errorf("sourceType = %v", got)
	}
	if f["sourceId"] == nil || f["title"] != "boom" {
		t.Errorf("sourceId/title missing: %v", f)
	}
	// Omitted fields stay absent.
	if _, ok := f["lastOccurredAtAfter"]; ok {
		t.Error("empty occurred-after should be omitted")
	}
}

func TestBulkDeleteBody(t *testing.T) {
	body := BulkDeleteBody(BulkFilterInput{Title: "x"}, []string{"keep1"})
	if body["excludeIds"].([]string)[0] != "keep1" {
		t.Errorf("excludeIds = %v", body["excludeIds"])
	}
	if _, ok := body["filter"]; !ok {
		t.Error("missing filter")
	}
	// No excludeIds → key omitted.
	if _, ok := BulkDeleteBody(BulkFilterInput{Title: "x"}, nil)["excludeIds"]; ok {
		t.Error("empty excludeIds should be omitted")
	}
}

func TestBulkUpdateBody(t *testing.T) {
	body, err := BulkUpdateBody(BulkFilterInput{Status: []string{"open"}}, "resolved", "cleanup", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	uf := body["updateField"].(map[string]any)
	if uf["status"] != "ALERT_STATUS_RESOLVED" {
		t.Errorf("updateField.status = %v", uf["status"])
	}
	if body["remark"] != "cleanup" {
		t.Errorf("remark = %v", body["remark"])
	}
	// Invalid target status is rejected.
	if _, err := BulkUpdateBody(BulkFilterInput{Title: "x"}, "snoozed", "", nil); err == nil {
		t.Error("expected invalid-status error")
	}
}

func TestAffectedCount(t *testing.T) {
	if n := AffectedCount(json.RawMessage(`{"affectedCount":7}`)); n != 7 {
		t.Errorf("AffectedCount = %d", n)
	}
	if n := AffectedCount(json.RawMessage(`{}`)); n != 0 {
		t.Errorf("missing field should be 0, got %d", n)
	}
}
