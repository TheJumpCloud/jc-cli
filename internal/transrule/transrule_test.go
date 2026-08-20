package transrule

import (
	"encoding/json"
	"testing"
)

func TestNormalizeEnums(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"path", "PATH"},
		{"PATH", "PATH"},
		{"expr", "EXPR"},
		{"expression", "EXPR"},
		{" Template ", "GOLANG_TEMPLATE"},
	}
	for _, c := range cases {
		got, err := NormalizeSourceType(c.in)
		if err != nil {
			t.Fatalf("NormalizeSourceType(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("NormalizeSourceType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := NormalizeSourceType("bogus"); err == nil {
		t.Error("expected error for unknown source type")
	}
	if got, _ := NormalizeDirection("Export"); got != "EXPORT" {
		t.Errorf("NormalizeDirection = %q", got)
	}
	if _, err := NormalizeDirection("sideways"); err == nil {
		t.Error("expected error for unknown direction")
	}
	got, err := NormalizeAppliedOn([]string{"create", "UPDATE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "CREATE" || got[1] != "UPDATE" {
		t.Errorf("NormalizeAppliedOn = %v", got)
	}
	if _, err := NormalizeAppliedOn([]string{"delete"}); err == nil {
		t.Error("expected error for unknown appliedOn op")
	}
}

// The PUT endpoint is a full replace, so an update that changes one field must
// still carry the rest of the current rule.
func TestUpdateBody_PreservesUnchangedFields(t *testing.T) {
	cur := Rule{
		ObjectID:    "6a87696eb3b4f20001f18d5d",
		Source:      "department",
		Destination: "department",
		SourceType:  "PATH",
		Direction:   "EXPORT",
		AppliedOn:   []string{"CREATE", "UPDATE"},
	}
	newSource := "costCenter"
	body := UpdateBody(cur, &newSource, nil, nil, nil)

	if body["source"] != "costCenter" {
		t.Errorf("source = %v", body["source"])
	}
	if body["destination"] != "department" {
		t.Errorf("destination not preserved: %v", body["destination"])
	}
	if body["sourceType"] != "PATH" {
		t.Errorf("sourceType not preserved: %v", body["sourceType"])
	}
	ops, _ := body["appliedOn"].([]string)
	if len(ops) != 2 {
		t.Errorf("appliedOn not preserved: %v", body["appliedOn"])
	}
	// direction is preserved server-side and must not be sent.
	if _, ok := body["direction"]; ok {
		t.Error("direction must not be sent on update")
	}
}

func TestParseBulkFile(t *testing.T) {
	raw := []byte(`{"insertTranslationRules":[{"source":"a","destination":"b"}],"deleteTranslationRuleObjectIds":["x","y"]}`)
	body, ops, err := ParseBulkFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ops.Inserts != 1 || ops.Deletes != 2 || ops.Updates != 0 {
		t.Errorf("ops = %+v", ops)
	}
	var check map[string]json.RawMessage
	if err := json.Unmarshal(body, &check); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}

	if _, _, err := ParseBulkFile([]byte(`{}`)); err == nil {
		t.Error("expected error for body with no operations")
	}
	if _, _, err := ParseBulkFile([]byte(`{"insertTranslationRule":[]}`)); err == nil {
		t.Error("expected error for typo'd bulk key")
	}
	if _, _, err := ParseBulkFile([]byte(`{"insertTranslationRules":{}}`)); err == nil {
		t.Error("expected error for non-array bulk value")
	}
}

func TestParsePreviewRules_AcceptsBareAndWrapped(t *testing.T) {
	bare := []byte(`[{"source":"username","destination":"sAMAccountName"}]`)
	got, err := ParsePreviewRules(bare)
	if err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(got, &arr); err != nil || len(arr) != 1 {
		t.Fatalf("bare array not round-tripped: %v %s", err, got)
	}

	// A body straight from the list endpoint must round-trip.
	wrapped := []byte(`{"totalCount":1,"rules":[{"source":"username","destination":"sAMAccountName"}]}`)
	got, err = ParsePreviewRules(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &arr); err != nil || len(arr) != 1 {
		t.Fatalf("{rules} envelope not unwrapped: %v %s", err, got)
	}

	if _, err := ParsePreviewRules([]byte(`{"nope":1}`)); err == nil {
		t.Error("expected error for body with no rules array")
	}
}

func TestPreviewBody_OmitsEmptyAD(t *testing.T) {
	body := PreviewBody(json.RawMessage(`[]`), "5ec7221c4d224c0e309577e7", "")
	if _, ok := body["activeDirectoryId"]; ok {
		t.Error("activeDirectoryId must be omitted when empty")
	}
	body = PreviewBody(json.RawMessage(`[]`), "5ec7221c4d224c0e309577e7", "6646c6915d1558000131637e")
	if body["activeDirectoryId"] != "6646c6915d1558000131637e" {
		t.Errorf("activeDirectoryId = %v", body["activeDirectoryId"])
	}
}

func TestEndpoints(t *testing.T) {
	ad := "6646c6915d1558000131637e"
	if got := Endpoint(ad); got != "/activedirectories/"+ad+"/translation-rules" {
		t.Errorf("Endpoint = %q", got)
	}
	if got := RuleEndpoint(ad, "r1"); got != "/activedirectories/"+ad+"/translation-rules/r1" {
		t.Errorf("RuleEndpoint = %q", got)
	}
	if got := BulkEndpoint(ad); got != "/activedirectories/"+ad+"/translation-rules/bulk" {
		t.Errorf("BulkEndpoint = %q", got)
	}
}
