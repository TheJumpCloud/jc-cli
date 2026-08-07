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
