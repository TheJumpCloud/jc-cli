// Package transrule holds the JumpCloud Active Directory translation-rule
// wire-contract helpers shared by the CLI (internal/cmd) and the MCP server
// (internal/mcp), so the two surfaces can never drift on the endpoints, the
// {rules, totalCount} list envelope, and the enum normalization.
//
// Verified live against org 5ec71e8e96bfda0611fc6c5b on 2026-08-20 (KLA-485
// translation-rules probe):
//
//   - GET  /activedirectories/{ad}/translation-rules → {totalCount, rules:[…]}
//     (ResponseKey "rules"). A rule's id is "objectId" and is plain 24-hex —
//     the spec's `format: byte` is wrong. Rules have no name field.
//   - POST /activedirectories/{ad}/translation-rules → {} (EMPTY — the spec's
//     documented {translation_rule:{…}} response is wrong; re-list to see the
//     created rule).
//   - PUT  /activedirectories/{ad}/translation-rules/{id} → the full updated
//     rule (the spec's documented {object_id} response is wrong). The update
//     body takes source/destination/sourceType/appliedOn only (no direction —
//     direction is preserved from the existing rule).
//   - DELETE /activedirectories/{ad}/translation-rules/{id} → empty body.
//   - POST /activedirectories/{ad}/translation-rules/bulk with any of
//     {insertTranslationRules, updateTranslationRules,
//     deleteTranslationRuleObjectIds} → {insertedTranslationRules,
//     updatedTranslationRules, deleteTranslationRuleObjectIds}.
//   - GET  /activedirectories/{provider}/translation-rules/recommendation →
//     same {totalCount, rules} envelope; the {provider_name} path segment is
//     IGNORED (any value returns the same 43-rule catalog, objectId empty).
//   - POST /activedirectories/translation-rules/preview with
//     {translationRules, userObjectId, activeDirectoryId?} →
//     {sourceUser, destinationUser} where both values are JSON-encoded
//     strings of the user before/after translation.
//
// The server normalizes unknown destination attributes into
// customAttributes.* (e.g. destination "employeeID" is stored as
// "customAttributes.employeeID").
package transrule

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RecommendationEndpoint lists JumpCloud's recommended rule catalog. The
// {provider_name} path segment is ignored by the API (verified live), so a
// stable literal is used.
const RecommendationEndpoint = "/activedirectories/activedirectory/translation-rules/recommendation"

// PreviewEndpoint previews the result of applying rules to a user.
const PreviewEndpoint = "/activedirectories/translation-rules/preview"

// Endpoint is the translation-rules collection for one Active Directory.
func Endpoint(adID string) string {
	return "/activedirectories/" + adID + "/translation-rules"
}

// RuleEndpoint is a single translation rule (update/delete).
func RuleEndpoint(adID, ruleID string) string {
	return Endpoint(adID) + "/" + ruleID
}

// BulkEndpoint is the bulk insert/update/delete endpoint for one AD.
func BulkEndpoint(adID string) string {
	return Endpoint(adID) + "/bulk"
}

// DefaultFields is the default field subset shown for rule list output.
var DefaultFields = []string{"objectId", "source", "destination", "sourceType", "direction", "appliedOn", "editable"}

// SourceTypes maps friendly values to the SourceType enum.
var SourceTypes = map[string]string{
	"path":            "PATH",
	"expr":            "EXPR",
	"expression":      "EXPR",
	"constant":        "CONSTANT",
	"template":        "GOLANG_TEMPLATE",
	"golang_template": "GOLANG_TEMPLATE",
}

// Directions maps friendly values to the Direction enum.
var Directions = map[string]string{
	"export":      "EXPORT",
	"import":      "IMPORT",
	"unspecified": "UNSPECIFIED",
}

// AppliedOnOps maps friendly values to the AppliedOn enum.
var AppliedOnOps = map[string]string{
	"create": "CREATE",
	"update": "UPDATE",
}

func normalize(kind, v string, m map[string]string) (string, error) {
	if out, ok := m[strings.ToLower(strings.TrimSpace(v))]; ok {
		return out, nil
	}
	valid := make([]string, 0, len(m))
	for k := range m {
		valid = append(valid, k)
	}
	return "", fmt.Errorf("invalid %s %q (accepted: PATH-style enum or one of the friendly values)", kind, v)
}

// NormalizeSourceType maps path/expr/constant/template (or the raw enum) to
// the SourceType enum value.
func NormalizeSourceType(v string) (string, error) {
	return normalize("source type", v, SourceTypes)
}

// NormalizeDirection maps export/import (or the raw enum) to the Direction
// enum value.
func NormalizeDirection(v string) (string, error) {
	return normalize("direction", v, Directions)
}

// NormalizeAppliedOn maps create/update values (or raw enums) to AppliedOn
// enum values.
func NormalizeAppliedOn(vs []string) ([]string, error) {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		n, err := normalize("applied-on operation", v, AppliedOnOps)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// Rule is the wire shape of a translation rule as returned by the list
// endpoint.
type Rule struct {
	ObjectID    string   `json:"objectId"`
	Source      string   `json:"source"`
	Destination string   `json:"destination"`
	SourceType  string   `json:"sourceType"`
	Direction   string   `json:"direction"`
	AppliedOn   []string `json:"appliedOn"`
	Required    bool     `json:"required"`
	Editable    bool     `json:"editable"`
	Default     bool     `json:"default"`
}

// ListEnvelope is the {totalCount, rules} list response.
type ListEnvelope struct {
	TotalCount int64  `json:"totalCount"`
	Rules      []Rule `json:"rules"`
}

// FindRule returns the rule with the given objectId, or false.
func FindRule(rules []Rule, id string) (Rule, bool) {
	for _, r := range rules {
		if r.ObjectID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// UpdateBody builds the PUT body from the current rule merged with the
// changed fields (nil pointer = keep current). The PUT endpoint accepts
// source/destination/sourceType/appliedOn only; direction is preserved
// server-side (verified live).
func UpdateBody(cur Rule, source, destination, sourceType *string, appliedOn []string) map[string]any {
	body := map[string]any{
		"source":      cur.Source,
		"destination": cur.Destination,
		"sourceType":  cur.SourceType,
		"appliedOn":   cur.AppliedOn,
	}
	if source != nil {
		body["source"] = *source
	}
	if destination != nil {
		body["destination"] = *destination
	}
	if sourceType != nil {
		body["sourceType"] = *sourceType
	}
	if appliedOn != nil {
		body["appliedOn"] = appliedOn
	}
	return body
}

// BulkOps summarizes a parsed bulk request body for plan output.
type BulkOps struct {
	Inserts int
	Updates int
	Deletes int
}

// ParseBulkFile validates a bulk request body: an object with at least one
// non-empty insertTranslationRules / updateTranslationRules /
// deleteTranslationRuleObjectIds array. Unknown keys are rejected to catch
// typos before the API silently ignores them.
func ParseBulkFile(raw []byte) (json.RawMessage, BulkOps, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, BulkOps{}, fmt.Errorf("parsing bulk JSON: %w", err)
	}
	var ops BulkOps
	counts := map[string]*int{
		"insertTranslationRules":         &ops.Inserts,
		"updateTranslationRules":         &ops.Updates,
		"deleteTranslationRuleObjectIds": &ops.Deletes,
	}
	for k, v := range obj {
		dst, ok := counts[k]
		if !ok {
			return nil, BulkOps{}, fmt.Errorf("unknown bulk key %q (accepted: insertTranslationRules, updateTranslationRules, deleteTranslationRuleObjectIds)", k)
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(v, &arr); err != nil {
			return nil, BulkOps{}, fmt.Errorf("bulk key %q must be an array: %w", k, err)
		}
		*dst = len(arr)
	}
	if ops.Inserts+ops.Updates+ops.Deletes == 0 {
		return nil, BulkOps{}, fmt.Errorf("bulk body has no operations: provide at least one of insertTranslationRules, updateTranslationRules, deleteTranslationRuleObjectIds")
	}
	return json.RawMessage(raw), ops, nil
}

// Summary renders the op counts for plan/confirmation output.
func (o BulkOps) Summary() string {
	return fmt.Sprintf("%d insert(s), %d update(s), %d delete(s)", o.Inserts, o.Updates, o.Deletes)
}

// ParsePreviewRules decodes a rules payload for the preview endpoint. It
// accepts a bare array of rule objects or one wrapped in
// {"translationRules": […]} (so a list body round-trips).
func ParsePreviewRules(raw []byte) (json.RawMessage, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		return json.RawMessage(raw), nil
	}
	var obj struct {
		TranslationRules json.RawMessage `json:"translationRules"`
		Rules            json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("parsing rules JSON: %w", err)
	}
	if obj.TranslationRules != nil {
		return obj.TranslationRules, nil
	}
	if obj.Rules != nil {
		return obj.Rules, nil
	}
	return nil, fmt.Errorf("rules JSON must be an array of rule objects, or wrapped in {\"translationRules\": […]} / {\"rules\": […]}")
}

// PreviewBody builds the POST /activedirectories/translation-rules/preview
// request body.
func PreviewBody(rules json.RawMessage, userID, adID string) map[string]any {
	body := map[string]any{
		"translationRules": rules,
		"userObjectId":     userID,
	}
	if adID != "" {
		body["activeDirectoryId"] = adID
	}
	return body
}
