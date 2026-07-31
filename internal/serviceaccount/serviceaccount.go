// Package serviceaccount holds the JumpCloud service-account wire-contract
// helpers shared by the CLI (internal/cmd) and the MCP server (internal/mcp),
// so the two surfaces can never drift on the parts that are easy to get wrong:
// the auth-type/lifetime enum mapping, the create-body shape, and the
// response-envelope unwrap keys. Verified live against the tenant on
// 2026-07-24 (see the probe in the KLA-485 service-accounts PR #95):
//
//   - GET /service-accounts wraps the list in {results, totalCount} and the id
//     lives under objectId, not id.
//   - single GET and POST /service-accounts wrap the object in {serviceAccount}.
//   - POST /service-accounts/{id}/auth-config wraps in {authConfig}.
//   - Creating an account (or rotating its credential) returns the secret ONCE.
package serviceaccount

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultFields is the default field subset shown for service-account list/get
// output. Secrets (apiKey/clientSecret) live under authConfigList and surface
// only with -a / on create.
var DefaultFields = []string{"objectId", "name", "roleName", "status", "expiresAt"}

// AuthTypes maps the friendly CLI/MCP value to the API enum.
var AuthTypes = map[string]string{"api_key": "API_KEY", "client_secret": "CLIENT_SECRET"}

// Lifetimes is the set the API accepts for an API key / client secret.
var Lifetimes = map[string]bool{"30 Days": true, "60 Days": true, "90 Days": true, "365 Days": true}

// BuildAuthConfig validates the auth-type/lifetime pair and returns the
// authConfig body fragment the API expects.
func BuildAuthConfig(authType, lifetime string) (map[string]any, error) {
	apiType, ok := AuthTypes[strings.ToLower(strings.TrimSpace(authType))]
	if !ok {
		return nil, fmt.Errorf("invalid --auth-type %q: must be api_key or client_secret", authType)
	}
	if !Lifetimes[lifetime] {
		return nil, fmt.Errorf(`invalid --lifetime %q: must be one of "30 Days", "60 Days", "90 Days", "365 Days"`, lifetime)
	}
	cfg := map[string]any{"authType": apiType}
	if apiType == "API_KEY" {
		cfg["apiKeyConfig"] = map[string]any{"lifetime": lifetime}
	} else {
		cfg["clientSecretConfig"] = map[string]any{"lifetime": lifetime}
	}
	return cfg, nil
}

// CreateBody assembles the POST /service-accounts body.
func CreateBody(name, roleID string, authConfig map[string]any) map[string]any {
	return map[string]any{
		"name":       name,
		"roleId":     roleID,
		"authConfig": authConfig,
	}
}

// Unwrap returns raw[key] when the response is a single-key envelope (the live
// API wraps get/create in {serviceAccount:…} and rotate in {authConfig:…}
// despite the spec showing the bare object). Falls back to raw untouched if
// the key is absent or the body isn't an object.
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
