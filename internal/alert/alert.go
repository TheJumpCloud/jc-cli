// Package alert holds the JumpCloud alert wire-contract helpers shared by the
// CLI (internal/cmd) and the MCP server (internal/mcp), so the two surfaces
// can never drift on the envelopes, the ALERT_STATUS_* enum, and the status/
// note request bodies. Verified live against the tenant on 2026-08-07 (see the
// probe in the KLA-485 alerts PR):
//
//   - GET /alerts wraps the array in {alerts} (ResponseKey); id is objectId.
//   - single GET and the status POST wrap the object in {alert}.
//   - GET /alerts/{id}/occurrences → {alertOccurrences, count}.
//   - GET /alerts/{id}/notes → {notes}; POST notes body {alertNote} → {note}.
//     There is NO delete-note endpoint — a note is permanent.
//   - POST /alerts/{id}/status body {status, remark}; status is the enum
//     ALERT_STATUS_OPEN / _ACKNOWLEDGED / _RESOLVED (also _AUTO_RESOLVED /
//     _DELETED, which are server-set and not offered to users).
//   - GET /alerts-stats is a bare {context:{count}, totalCount} — no envelope.
package alert

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultFields is the default field subset shown for alert list/get output.
var DefaultFields = []string{"objectId", "title", "severity", "status", "sourceName", "lastOccurredAt"}

// Statuses maps the friendly CLI/MCP status value to the API enum. Only the
// user-settable transitions are exposed; AUTO_RESOLVED and DELETED are
// server-managed.
var Statuses = map[string]string{
	"open":         "ALERT_STATUS_OPEN",
	"acknowledged": "ALERT_STATUS_ACKNOWLEDGED",
	"ack":          "ALERT_STATUS_ACKNOWLEDGED",
	"resolved":     "ALERT_STATUS_RESOLVED",
}

// NormalizeStatus maps a friendly status (open/acknowledged/ack/resolved) to
// the API enum.
func NormalizeStatus(s string) (string, error) {
	if v, ok := Statuses[strings.ToLower(strings.TrimSpace(s))]; ok {
		return v, nil
	}
	return "", fmt.Errorf("invalid status %q: must be open, acknowledged, or resolved", s)
}

// StatusBody builds the POST /alerts/{id}/status request body.
func StatusBody(apiStatus, remark string) map[string]any {
	body := map[string]any{"status": apiStatus}
	if remark != "" {
		body["remark"] = remark
	}
	return body
}

// NoteBody builds the POST /alerts/{id}/notes request body.
func NoteBody(text string) map[string]any {
	return map[string]any{"alertNote": text}
}

// BulkFilterInput carries the raw filter values from CLI/MCP flags for the
// bulk-delete/bulk-update endpoints. All fields are optional; array fields are
// AND-combined across kinds and OR-combined within a kind by the server.
// Verified live 2026-08-18: POST /alerts/bulk-delete {filter} and
// /alerts/bulk-update {filter, updateField:{status}, remark} both return
// {affectedCount}. A zero-match filter returns affectedCount 0 (no-op).
type BulkFilterInput struct {
	Category       []string // friendly (system) or raw ALERT_CATEGORY_*
	Severity       []string // low/medium/high or raw ALERT_SEVERITY_*
	Status         []string // open/acknowledged/resolved/… or raw ALERT_STATUS_*
	SourceType     []string // device/user/… or raw ALERT_SOURCE_TYPE_*
	SourceID       []string // objectIds, passed through
	Title          string
	OccurredAfter  string
	OccurredBefore string
}

// IsEmpty reports whether no filter criterion was supplied. Callers MUST refuse
// a bulk mutation on an empty filter — it would match every alert.
func (f BulkFilterInput) IsEmpty() bool {
	return len(f.Category) == 0 && len(f.Severity) == 0 && len(f.Status) == 0 &&
		len(f.SourceType) == 0 && len(f.SourceID) == 0 &&
		f.Title == "" && f.OccurredAfter == "" && f.OccurredBefore == ""
}

// normEnum maps a friendly value to its ALERT_* enum, passing through a value
// that already carries the prefix. "system" → ALERT_CATEGORY_SYSTEM;
// "ALERT_CATEGORY_SYSTEM" is returned unchanged; "auto-resolved" → *_AUTO_RESOLVED.
func normEnum(prefix, v string) string {
	u := strings.ToUpper(strings.TrimSpace(v))
	if strings.HasPrefix(u, prefix) {
		return u
	}
	return prefix + strings.ReplaceAll(u, "-", "_")
}

func normEnumSlice(prefix string, vs []string) []string {
	if len(vs) == 0 {
		return nil
	}
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, normEnum(prefix, v))
	}
	return out
}

// BuildFilter normalizes a BulkFilterInput into the wire "filter" object,
// omitting empty fields.
func (f BulkFilterInput) BuildFilter() map[string]any {
	filter := map[string]any{}
	if s := normEnumSlice("ALERT_CATEGORY_", f.Category); s != nil {
		filter["category"] = s
	}
	if s := normEnumSlice("ALERT_SEVERITY_", f.Severity); s != nil {
		filter["severity"] = s
	}
	if s := normEnumSlice("ALERT_STATUS_", f.Status); s != nil {
		filter["status"] = s
	}
	if s := normEnumSlice("ALERT_SOURCE_TYPE_", f.SourceType); s != nil {
		filter["sourceType"] = s
	}
	if len(f.SourceID) > 0 {
		filter["sourceId"] = f.SourceID
	}
	if f.Title != "" {
		filter["title"] = f.Title
	}
	if f.OccurredAfter != "" {
		filter["lastOccurredAtAfter"] = f.OccurredAfter
	}
	if f.OccurredBefore != "" {
		filter["lastOccurredAtBefore"] = f.OccurredBefore
	}
	return filter
}

// BulkDeleteBody builds the POST /alerts/bulk-delete request body.
func BulkDeleteBody(f BulkFilterInput, excludeIDs []string) map[string]any {
	body := map[string]any{"filter": f.BuildFilter()}
	if len(excludeIDs) > 0 {
		body["excludeIds"] = excludeIDs
	}
	return body
}

// BulkUpdateBody builds the POST /alerts/bulk-update request body. updateStatus
// is a friendly settable status (open/acknowledged/resolved), normalized to the
// enum via NormalizeStatus.
func BulkUpdateBody(f BulkFilterInput, updateStatus, remark string, excludeIDs []string) (map[string]any, error) {
	apiStatus, err := NormalizeStatus(updateStatus)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"filter":      f.BuildFilter(),
		"updateField": map[string]any{"status": apiStatus},
	}
	if remark != "" {
		body["remark"] = remark
	}
	if len(excludeIDs) > 0 {
		body["excludeIds"] = excludeIDs
	}
	return body, nil
}

// AffectedCount extracts affectedCount from a bulk-op response.
func AffectedCount(raw json.RawMessage) int {
	var r struct {
		AffectedCount int `json:"affectedCount"`
	}
	_ = json.Unmarshal(raw, &r)
	return r.AffectedCount
}

// Unwrap returns raw[key] when the response is a single-key envelope (the live
// API wraps get/status in {alert} and add-note in {note}). Falls back to raw
// untouched if the key is absent or the body isn't an object.
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
