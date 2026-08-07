// Package notification holds the JumpCloud notification-channel wire-contract
// helpers shared by the CLI (internal/cmd) and the MCP server (internal/mcp),
// so the two surfaces can never drift on the parts that are easy to get wrong:
// the {channel} envelope, the CHANNEL_TYPE_* enum, and the update strip list.
// Every rule here was verified live against the tenant on 2026-07-31 (see the
// probe in the KLA-485 notification-channels PR):
//
//   - GET /notifications/channels wraps the list in {channels, count} — the
//     array is under "channels" (ResponseKey) and the id is objectId.
//   - single GET, POST, and PATCH all wrap the object in {channel}.
//   - `type` is an enum: CHANNEL_TYPE_EMAIL / _WEBHOOK / _SLACK.
//   - update is PATCH but is NOT partial — it 400s "channel name is required"
//     on a sparse body, so an update must read-modify-write: fetch the channel,
//     strip the server-managed timestamps/owner, apply changes, PATCH the whole
//     {channel}. The channel's own objectId and config.*.objectId are kept.
package notification

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultFields is the default field subset shown for channel list/get output.
var DefaultFields = []string{"objectId", "name", "type", "enabled", "description"}

// ChannelTypes maps the friendly CLI/MCP value to the API enum.
var ChannelTypes = map[string]string{
	"webhook": "CHANNEL_TYPE_WEBHOOK",
	"email":   "CHANNEL_TYPE_EMAIL",
	"slack":   "CHANNEL_TYPE_SLACK",
}

// NormalizeType maps a friendly type (webhook/email/slack) to the API enum.
func NormalizeType(t string) (string, error) {
	if v, ok := ChannelTypes[strings.ToLower(strings.TrimSpace(t))]; ok {
		return v, nil
	}
	return "", fmt.Errorf("invalid --type %q: must be webhook, email, or slack", t)
}

// serverManagedKeys are dropped from a channel object before it is PATCHed back
// as a read-modify-write update. They are server-owned; echoing them is at best
// ignored. The channel's own objectId is KEPT (the update targets it), as is
// config.*.objectId (the sub-resource id).
var serverManagedKeys = []string{"createdAt", "createdBy", "updatedAt", "updatedBy", "organizationObjectId"}

// StripServerManaged removes server-owned keys from a channel object in place.
func StripServerManaged(obj map[string]any) {
	for _, k := range serverManagedKeys {
		delete(obj, k)
	}
}

// Body wraps a channel object in the {channel:{…}} envelope that POST and PATCH
// both require.
func Body(channel map[string]any) map[string]any {
	return map[string]any{"channel": channel}
}

// Unwrap returns raw.channel when the response is the {channel:…} envelope the
// live API always uses. Falls back to raw untouched if the key is absent or the
// body isn't an object.
func Unwrap(raw json.RawMessage) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	if inner, ok := obj["channel"]; ok {
		return inner
	}
	return raw
}

// BuildWebhookConfig assembles a config.webhook fragment from the common
// webhook flags. Only url is required; the rest are optional.
func BuildWebhookConfig(url, authType, authToken, authUsername, authPassword string, sslVerification bool) map[string]any {
	wh := map[string]any{"url": url, "sslVerification": sslVerification}
	if authType != "" {
		wh["authType"] = authType
	}
	if authToken != "" {
		wh["authToken"] = authToken
	}
	if authUsername != "" {
		wh["authUsername"] = authUsername
	}
	if authPassword != "" {
		wh["authPassword"] = authPassword
	}
	return map[string]any{"webhook": wh}
}
