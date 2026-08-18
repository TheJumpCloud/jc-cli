// Package healthrule holds the JumpCloud health-monitoring rule wire-contract
// helpers shared by the CLI (internal/cmd) and the MCP server (internal/mcp),
// so the two surfaces can never drift on the envelopes, the RULE_STATUS_* enum,
// and the {rule}/{status} request bodies. Health-monitoring rules are the
// definitions that raise the alerts handled by internal/alert.
//
// Verified live against org 5ec71e8e96bfda0611fc6c5b on 2026-08-18 (KLA-485
// healthmonitoring probe):
//
//   - GET  /healthmonitoring/rules            → {count, rules:[…]} (ResponseKey
//     "rules"); a rule's id is objectId and it has a "name".
//   - GET  /healthmonitoring/rules/{id}       → {rule:{…}}.
//   - POST /healthmonitoring/rules            body {rule:{…}} → {rule:{…}}.
//   - PATCH /healthmonitoring/rules/{id}      body {rule:{…}} → {rule:{…}}.
//   - PATCH /healthmonitoring/rules/{id}/status body {status} → {rule:{…}};
//     status is the enum RULE_STATUS_ENABLED / RULE_STATUS_DISABLED.
//   - DELETE /healthmonitoring/rules/{id}     → {} (empty).
//   - GET  /healthmonitoring/rules-stats      → bare {systemInsightsRequiredCount}
//     — NO envelope.
//   - GET  /healthmonitoring/ruletemplates    → {count, templates:[…]}
//     (ResponseKey "templates"); templates are read-only, each carrying a
//     configurations[] UI-form schema.
//   - GET  /healthmonitoring/ruletemplates/{id} → the template object.
//
// The rule body is a large nested object (conditions[].filters{}, eventFilters,
// pollConditions, selectedScopeValues, ~35 fields) authored against a template's
// configuration form. It is not flag-modelable, so create/update take the raw
// {rule} JSON via --rule-file (the same passthrough pattern as windows-mdm
// --settings-file); this package only wraps/unwraps the envelope.
package healthrule

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RulesEndpoint is the V2 collection endpoint for health-monitoring rules.
const RulesEndpoint = "/healthmonitoring/rules"

// TemplatesEndpoint is the V2 collection endpoint for rule templates (read-only).
const TemplatesEndpoint = "/healthmonitoring/ruletemplates"

// StatsEndpoint is the V2 endpoint for the rules-stats singleton.
const StatsEndpoint = "/healthmonitoring/rules-stats"

// DefaultFields is the default field subset shown for rule list/get output.
var DefaultFields = []string{"objectId", "name", "category", "severity", "status", "ruleType"}

// TemplateDefaultFields is the default field subset shown for template output.
var TemplateDefaultFields = []string{"objectId", "name", "category", "type", "description"}

// Statuses maps the friendly CLI/MCP status value to the API enum.
var Statuses = map[string]string{
	"enabled":  "RULE_STATUS_ENABLED",
	"enable":   "RULE_STATUS_ENABLED",
	"disabled": "RULE_STATUS_DISABLED",
	"disable":  "RULE_STATUS_DISABLED",
}

// NormalizeStatus maps a friendly status (enabled/disabled) to the API enum.
func NormalizeStatus(s string) (string, error) {
	if v, ok := Statuses[strings.ToLower(strings.TrimSpace(s))]; ok {
		return v, nil
	}
	return "", fmt.Errorf("invalid status %q: must be enabled or disabled", s)
}

// StatusBody builds the PATCH /healthmonitoring/rules/{id}/status request body.
func StatusBody(apiStatus string) map[string]any {
	return map[string]any{"status": apiStatus}
}

// RuleBody wraps a rule object in the {rule} envelope the create/update
// endpoints expect.
func RuleBody(rule json.RawMessage) map[string]json.RawMessage {
	return map[string]json.RawMessage{"rule": rule}
}

// ParseRuleFile decodes a --rule-file payload into the object sent as the
// {rule} envelope. It accepts either a bare rule object or one already wrapped
// in {"rule": …}, so a body captured from `jc alerts rules get` round-trips.
func ParseRuleFile(raw []byte) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("parsing rule JSON: %w", err)
	}
	if inner, ok := obj["rule"]; ok {
		return inner, nil
	}
	return json.RawMessage(raw), nil
}

// Unwrap returns raw[key] when the response is a single-key envelope (get/
// create/update/status wrap in {rule}; a single template may be {template} or
// bare). Falls back to raw untouched if the key is absent or the body isn't an
// object.
func Unwrap(raw json.RawMessage, key string) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	if inner, ok := obj[key]; ok {
		return inner
	}
	return raw
}
