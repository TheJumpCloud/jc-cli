package mcp

import (
	"strings"
	"testing"
)

var gapWriteTools = []struct {
	tool string
	args map[string]any
}{
	{"insights_report_request", map[string]any{"type": "users-to-user-groups"}},
	{"email_subscription_set", map[string]any{"email_key": "lockout-notice-admin", "subscribed": false}},
}

func TestGapAreaWriteTools_ReadOnlyAndPlan(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, readOnlyTripwire(t).URL)

	ro := connectToolTestServer(t, Options{ReadOnly: true})
	for _, c := range gapWriteTools {
		if out := getResultText(t, callTool(t, ro, c.tool, gapArgs(c.args, true))); !strings.Contains(out, "read-only") {
			t.Errorf("%s not refused in read-only mode: %s", c.tool, out)
		}
	}
	rw := connectToolTestServer(t, Options{})
	for _, c := range gapWriteTools {
		if out := getResultText(t, callTool(t, rw, c.tool, gapArgs(c.args, false))); !strings.Contains(out, `"plan"`) {
			t.Errorf("%s did not plan: %s", c.tool, out)
		}
	}
	if out := getResultText(t, callTool(t, rw, "insights_report_request", map[string]any{"type": "nope"})); strings.Contains(out, `"plan"`) {
		t.Errorf("an unknown report type must be refused before planning: %s", out)
	}
	if out := getResultText(t, callTool(t, ro, "identity_risk_setting_set",
		map[string]any{"name": "enabled", "value": "false", "execute": true})); !strings.Contains(out, "read-only") {
		t.Errorf("identity_risk_setting_set not refused in read-only mode: %s", out)
	}
}

func gapArgs(args map[string]any, execute bool) map[string]any {
	out := map[string]any{"execute": execute}
	for k, v := range args {
		out[k] = v
	}
	return out
}
