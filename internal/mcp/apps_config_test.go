package mcp

import (
	"strings"
	"testing"
)

// config means the SSO configuration in both the CLI and the tools; fields
// carries other top-level app fields. The nested form is refused.
func TestAppsTools_ConfigAndFieldsSemantics(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, readOnlyTripwire(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "apps_create", map[string]any{
		"name": "bookmark", "config": `{"idpCertificate":"x"}`, "fields": `{"description":"d"}`}))
	if !strings.Contains(out, `"plan"`) || !strings.Contains(out, `"idpCertificate"`) || !strings.Contains(out, `"description"`) {
		t.Errorf("create plan: %s", out)
	}
	for _, args := range []map[string]any{
		{"name": "bookmark", "config": `{"config":{"idpCertificate":"x"}}`},
		{"name": "bookmark", "fields": `{"name":"slack"}`},
	} {
		if out := getResultText(t, callTool(t, cs, "apps_create", args)); strings.Contains(out, `"plan"`) {
			t.Errorf("%v must be refused: %s", args, out)
		}
	}
}
