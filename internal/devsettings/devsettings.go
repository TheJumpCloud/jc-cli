// Package devsettings holds the JumpCloud organization-level device settings
// wire contract shared by the CLI (internal/cmd) and the MCP server
// (internal/mcp), so the two surfaces cannot drift on the endpoints, the
// {settings:[…]} envelope, or the OSFamily/Permission enums.
//
// Verified live against org 5ec71e8e96bfda0611fc6c5b on 2026-08-20 (KLA-485
// device-settings probe):
//
//   - GET /devices/settings/signinwithjumpcloud →
//     {organizationObjectId, settings:[{osFamily, enabled, defaultPermission}]}
//     with one entry per OS family. organizationObjectId comes back as a plain
//     24-hex ID, NOT the base64 the spec's `format: byte` implies.
//   - GET /devices/settings/defaultpasswordsync → bare {enabled} — no envelope
//     and no organizationObjectId.
//   - GET /devices/settings/ssao → HTTP 501 "Unexpected error". The endpoint is
//     not implemented server-side (both with and without the
//     organizationObjectId query param), so the SSAO pair is deliberately NOT
//     exposed as commands; see SSAOEndpoint.
//
// Both PUTs take the same body shape as their GET response. Whether the
// signinwithjumpcloud PUT replaces the whole settings array or merges per
// osFamily was NOT verified live (writing org-wide device sign-in settings on
// a real tenant was out of scope for the probe). Callers must therefore use
// MergeSignInSetting, which sends the complete array with one entry changed:
// that is correct under either semantic, and it matches the full-replace
// behaviour already confirmed on two sibling endpoints in this API
// (samba domains, AD translation rules).
package devsettings

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SignInEndpoint is the Sign In with JumpCloud org settings singleton.
const SignInEndpoint = "/devices/settings/signinwithjumpcloud"

// PasswordSyncEndpoint is the default password sync org setting singleton.
const PasswordSyncEndpoint = "/devices/settings/defaultpasswordsync"

// SSAOEndpoint is the SSAO device settings singleton. It returns HTTP 501 on
// a live tenant — the endpoint exists in the OpenAPI spec but is not
// implemented server-side — so no command or tool exposes it. It is kept here
// so the gap is recorded in one place rather than rediscovered.
const SSAOEndpoint = "/devices/settings/ssao"

// SignInDefaultFields is the default field subset shown for sign-in settings.
var SignInDefaultFields = []string{"osFamily", "enabled", "defaultPermission"}

// OSFamilies maps a friendly OS name to the API enum.
var OSFamilies = map[string]string{
	"windows": "WINDOWS",
	"macos":   "MACOS",
	"mac":     "MACOS",
	"unknown": "UNKNOWN",
}

// Permissions maps a friendly permission name to the API enum.
var Permissions = map[string]string{
	"standard": "STANDARD",
	"admin":    "ADMIN",
}

func normalize(kind, v string, m map[string]string) (string, error) {
	if out, ok := m[strings.ToLower(strings.TrimSpace(v))]; ok {
		return out, nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return "", fmt.Errorf("invalid %s %q", kind, v)
}

// NormalizeOSFamily maps windows/macos (or the raw enum) to the OSFamily enum.
func NormalizeOSFamily(v string) (string, error) {
	return normalize("OS family", v, OSFamilies)
}

// NormalizePermission maps standard/admin (or the raw enum) to the Permission
// enum.
func NormalizePermission(v string) (string, error) {
	return normalize("permission", v, Permissions)
}

// SignInSetting is one per-OS Sign In with JumpCloud entry.
type SignInSetting struct {
	OSFamily          string `json:"osFamily"`
	Enabled           bool   `json:"enabled"`
	DefaultPermission string `json:"defaultPermission"`
}

// SignInSettings is the GET/PUT envelope for Sign In with JumpCloud.
type SignInSettings struct {
	OrganizationObjectID string          `json:"organizationObjectId,omitempty"`
	Settings             []SignInSetting `json:"settings"`
}

// PasswordSync is the bare {enabled} body of the default password sync
// setting — this endpoint has no envelope (verified live).
type PasswordSync struct {
	Enabled bool `json:"enabled"`
}

// ParseSignIn decodes a Sign In with JumpCloud GET response.
func ParseSignIn(raw json.RawMessage) (SignInSettings, error) {
	var s SignInSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		return SignInSettings{}, fmt.Errorf("decoding sign-in settings: %w", err)
	}
	return s, nil
}

// MergeSignInSetting returns the complete settings array with the entry for
// osFamily updated, so the PUT is correct whether the API replaces the whole
// array or merges per entry. A nil pointer leaves that field unchanged; an
// osFamily with no existing entry is appended.
func MergeSignInSetting(cur []SignInSetting, osFamily string, enabled *bool, permission *string) []SignInSetting {
	out := make([]SignInSetting, len(cur))
	copy(out, cur)

	for i := range out {
		if out[i].OSFamily != osFamily {
			continue
		}
		if enabled != nil {
			out[i].Enabled = *enabled
		}
		if permission != nil {
			out[i].DefaultPermission = *permission
		}
		return out
	}

	added := SignInSetting{OSFamily: osFamily, DefaultPermission: "STANDARD"}
	if enabled != nil {
		added.Enabled = *enabled
	}
	if permission != nil {
		added.DefaultPermission = *permission
	}
	return append(out, added)
}

// SignInBody builds the PUT body for Sign In with JumpCloud.
func SignInBody(settings []SignInSetting) map[string]any {
	return map[string]any{"settings": settings}
}

// PasswordSyncBody builds the PUT body for the default password sync setting.
func PasswordSyncBody(enabled bool) map[string]any {
	return map[string]any{"enabled": enabled}
}

// FindSignIn returns the entry for an OS family, or false.
func FindSignIn(settings []SignInSetting, osFamily string) (SignInSetting, bool) {
	for _, s := range settings {
		if s.OSFamily == osFamily {
			return s, true
		}
	}
	return SignInSetting{}, false
}

// Describe renders one setting for plan and confirmation output.
func (s SignInSetting) Describe() string {
	state := "disabled"
	if s.Enabled {
		state = "enabled"
	}
	return fmt.Sprintf("%s: %s, default permission %s", s.OSFamily, state, s.DefaultPermission)
}
