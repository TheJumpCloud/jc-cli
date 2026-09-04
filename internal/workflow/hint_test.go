package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// A live pass wrote a scheduled workflow the obvious way — by analogy with
// the other two triggers — and was told the source did not exist. The message
// was right and the hint named only two of the three trigger types, so an
// author was told the thing they were doing is unsupported while explain,
// health and lint all reported trigger_type "scheduler" and lint passed two
// scheduler templates as clean. A hint that is confidently wrong stops
// correct work, which is worse than no hint.
func TestValidate_UnknownTriggerSourceHintNamesTheScheduledForm(t *testing.T) {
	d, err := ParseDSL(json.RawMessage(
		`{"schedule":{"on":{"one":{"with":{"source":"scheduler"}}}},` +
			`"do":[{"s":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var hint string
	for _, f := range Validate(d).Findings {
		if strings.HasSuffix(f.Path, "with.source") {
			hint = f.Hint
		}
	}
	if hint == "" {
		t.Fatal("no finding on the trigger source")
	}
	// Naming the two envelope sources is not enough: the author needs to know
	// a scheduled workflow does not use this envelope at all.
	for _, want := range []string{"scheduler", "frequency", "omit"} {
		if !strings.Contains(strings.ToLower(hint), want) {
			t.Errorf("hint should mention %q so the author can act on it, got: %s", want, hint)
		}
	}
}

// The form the hint recommends must actually validate, or the hint sends the
// author from one dead end to another.
func TestValidate_FlatScheduleIsAcceptedAsScheduler(t *testing.T) {
	d, err := ParseDSL(json.RawMessage(
		`{"schedule":{"frequency":"weekly","interval":1,"day_of_week":["monday"],` +
			`"time":"09:00","timezone":"Etc/UTC"},` +
			`"do":[{"s":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	res := Validate(d)
	for _, f := range res.Findings {
		if f.Severity == Error {
			t.Errorf("the recommended flat form must validate clean, got: %s — %s", f.Path, f.Message)
		}
	}
	if res.TriggerType != "scheduler" {
		t.Errorf("trigger type = %q, want scheduler", res.TriggerType)
	}
}
