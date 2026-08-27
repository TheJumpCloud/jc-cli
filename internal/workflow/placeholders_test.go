package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestClassifyPlaceholder_EveryLiveMarker pins the classification of every
// REPLACE_WITH_* marker the 12 shipped templates actually use, captured from
// the live catalog on 2026-08-27.
//
// This is the test that matters for this feature. The markers overlap —
// DEVICE_GROUP_ID ends with GROUP_ID, FAILED_POLICY_DEVICE_GROUP_ID contains
// both POLICY and DEVICE_GROUP — so the rule order in placeholderRules is load
// bearing, and reordering it misclassifies silently rather than failing.
func TestClassifyPlaceholder_EveryLiveMarker(t *testing.T) {
	for marker, want := range map[string]string{
		"REPLACE_WITH_COMMAND_ID":                    KindCommand,
		"REPLACE_WITH_APPLE_MDM_ID":                  KindAppleMDM,
		"REPLACE_WITH_WORKFLOW_ID":                   KindWorkflow,
		"REPLACE_WITH_POLICY_ID":                     KindPolicy,
		"REPLACE_WITH_DEVICE_GROUP_ID":               KindDeviceGroup,
		"REPLACE_WITH_FAILED_POLICY_DEVICE_GROUP_ID": KindDeviceGroup,
		"REPLACE_WITH_GROUP_ID_1":                    KindUserGroup,
		"REPLACE_WITH_GROUP_ID_2":                    KindUserGroup,
		"REPLACE_WITH_GROUP_ID_3":                    KindUserGroup,
		"REPLACE_WITH_NON_MFA_GROUP_ID":              KindUserGroup,
		"REPLACE_WITH_PRE_HIRE_GROUP_ID":             KindUserGroup,
		"REPLACE_WITH_DEPARTMENT_NAME":               KindFreeText,
		"REPLACE_WITH_HEADER_VALUE":                  KindFreeText,
		"REPLACE_WITH_IT_OPS_EMAIL":                  KindFreeText,
		"REPLACE_WITH_PATH":                          KindFreeText,
		"REPLACE_WITH_CONNECTOR":                     KindFreeText,
		"REPLACE_WITH_STATUS_ID":                     KindFreeText,
	} {
		got := ClassifyPlaceholder(marker)
		if got.Kind != want {
			t.Errorf("%s classified as %q, want %q", marker, got.Kind, want)
		}
		if got.Describe == "" {
			t.Errorf("%s has no description; prompts would read blank", marker)
		}
		if got.Resolvable != (want != KindFreeText) {
			t.Errorf("%s Resolvable = %v for kind %q", marker, got.Resolvable, want)
		}
	}
}

// The specific rule must beat the general one. Stated separately from the
// table so a regression names the actual trap.
func TestClassifyPlaceholder_SpecificBeatsGeneral(t *testing.T) {
	if k := ClassifyPlaceholder("REPLACE_WITH_DEVICE_GROUP_ID"); k.Kind != KindDeviceGroup {
		t.Errorf("DEVICE_GROUP_ID must not fall through to the GROUP_ID rule, got %q", k.Kind)
	}
	if k := ClassifyPlaceholder("REPLACE_WITH_FAILED_POLICY_DEVICE_GROUP_ID"); k.Kind != KindDeviceGroup {
		t.Errorf("a marker containing both POLICY and DEVICE_GROUP must resolve to the device group, got %q", k.Kind)
	}
}

func TestNormalizeMarker(t *testing.T) {
	for in, want := range map[string]string{
		"COMMAND_ID":              "REPLACE_WITH_COMMAND_ID",
		"REPLACE_WITH_COMMAND_ID": "REPLACE_WITH_COMMAND_ID",
		"  COMMAND_ID  ":          "REPLACE_WITH_COMMAND_ID",
		"":                        "",
	} {
		if got := NormalizeMarker(in); got != want {
			t.Errorf("NormalizeMarker(%q) = %q, want %q", in, got, want)
		}
	}
}

const fillTemplate = `{
  "schedule": {"on": {"one": {"with": {"source": "jc_events", "type": "association_change",
     "condition": "association.connection.from.object_id in [\"REPLACE_WITH_DEVICE_GROUP_ID\"]"}}}},
  "do": [{"run": {"call": "jc_operation", "with": {
     "operationId": "postApiRuncommand", "version": 1,
     "bodyParams": {"_id": "REPLACE_WITH_COMMAND_ID"}}}}]
}`

func TestFill_ReplacesEverywhere(t *testing.T) {
	d, err := ParseDSL(json.RawMessage(fillTemplate))
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	kinds := d.PlaceholderKinds()
	if len(kinds) != 2 {
		t.Fatalf("expected 2 distinct markers, got %v", kinds)
	}

	out, err := d.Fill(map[string]string{
		"COMMAND_ID":      "6139919b03c9b24d0b8f3ef1",
		"DEVICE_GROUP_ID": "6912793ab839ce0001735a0e",
	})
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "REPLACE_WITH_") {
		t.Errorf("markers survived the fill: %s", s)
	}
	// Markers appear in a task parameter AND in the trigger condition; both
	// must be substituted, which is why the fill is textual.
	if !strings.Contains(s, "6139919b03c9b24d0b8f3ef1") || !strings.Contains(s, "6912793ab839ce0001735a0e") {
		t.Errorf("values missing: %s", s)
	}
	if _, err := ParseDSL(out); err != nil {
		t.Errorf("filled document no longer parses: %v", err)
	}
}

func TestFill_RejectsUnknownMarker(t *testing.T) {
	d, _ := ParseDSL(json.RawMessage(fillTemplate))
	_, err := d.Fill(map[string]string{"NOPE_ID": "x"})
	if err == nil {
		t.Fatal("want an error for a marker this template does not have")
	}
	if !strings.Contains(err.Error(), "NOPE_ID") {
		t.Errorf("error should name the bad key: %v", err)
	}
	// And should say what IS available, so the fix is obvious.
	if !strings.Contains(err.Error(), "REPLACE_WITH_COMMAND_ID") {
		t.Errorf("error should list the real markers: %v", err)
	}
}

func TestFill_RejectsPartialFill(t *testing.T) {
	d, _ := ParseDSL(json.RawMessage(fillTemplate))
	_, err := d.Fill(map[string]string{"COMMAND_ID": "abc"})
	if err == nil {
		t.Fatal("a half-filled workflow must be an error, not a returned document")
	}
	if !strings.Contains(err.Error(), "REPLACE_WITH_DEVICE_GROUP_ID") {
		t.Errorf("error should name what is still missing: %v", err)
	}
}

// Regression: REPLACE_WITH_GROUP_ID is a prefix of REPLACE_WITH_GROUP_ID_1.
// Replacing the short marker first would corrupt the long one into a value
// followed by a stray "_1", so the fill substitutes longest-first.
func TestFill_PrefixMarkersDoNotCorruptEachOther(t *testing.T) {
	doc := `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiV2Usergroups","version":2,
	     "queryParams":{"x":"REPLACE_WITH_GROUP_ID_1","y":"REPLACE_WITH_GROUP_ID_2"}}}}]}`
	d, err := ParseDSL(json.RawMessage(doc))
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	out, err := d.Fill(map[string]string{"GROUP_ID_1": "aaa", "GROUP_ID_2": "bbb"})
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "_1") || strings.Contains(s, "_2") {
		t.Errorf("a prefix marker corrupted its longer sibling: %s", s)
	}
	if !strings.Contains(s, `"aaa"`) || !strings.Contains(s, `"bbb"`) {
		t.Errorf("values missing or mangled: %s", s)
	}
}

// A value containing a quote must not break out of the JSON string it lands in.
func TestFill_EscapesValues(t *testing.T) {
	doc := `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
	  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1,
	     "queryParams":{"q":"REPLACE_WITH_DEPARTMENT_NAME"}}}}]}`
	d, _ := ParseDSL(json.RawMessage(doc))
	out, err := d.Fill(map[string]string{"DEPARTMENT_NAME": `R&D "Core"`})
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if _, err := ParseDSL(out); err != nil {
		t.Fatalf("a quoted value broke the document: %v\n%s", err, out)
	}
}

func TestPlaceholderMarkers_Sorted(t *testing.T) {
	d, _ := ParseDSL(json.RawMessage(fillTemplate))
	got := d.PlaceholderMarkers()
	if len(got) != 2 || got[0] != "REPLACE_WITH_COMMAND_ID" || got[1] != "REPLACE_WITH_DEVICE_GROUP_ID" {
		t.Errorf("markers should be sorted for stable output, got %v", got)
	}
}
