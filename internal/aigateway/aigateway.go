// Package aigateway reads the JumpCloud AI Gateway (MCP Gateway)
// configuration: the MCP servers the gateway fronts, the OAuth redirect URIs it
// accepts, and the catalog of preconfigured connectors.
//
// PREVIEW, UNDOCUMENTED API. None of these endpoints is in the published
// OpenAPI spec. They are the ones the Admin Portal's AI Gateway pages call
// (found 2026-11-17 in the portal's public JavaScript), they accept an ordinary
// admin API key, and the Administrator role carries dedicated scopes for them
// (ai.gateway.servers, ai.gateway.servers.readonly). JumpCloud makes no promise
// about their stability, so every response is decoded strictly: a shape this
// package does not recognise is an error, never a silently empty result.
//
// Live contract (probed 2026-11-17, test org):
//
//   - GET /ai-admin/servers?page_size=N&page_token=T →
//     {servers: [...], next_page_token}. The token is "" on the last page.
//     A server is {object_id, name, prefix, target_url, application_object_id,
//     organization_object_id, auth_config, created_at, updated_at}.
//   - GET /ai-admin/servers/{id} → one server; 404 when unknown.
//   - GET /ai-admin/settings/redirect-uris → {redirect_uris: [{object_id, url,
//     ...}], next_page_token}, paged the same way. There is no GET by id.
//   - GET /ai-admin/catalog/resolve?application_object_id=X →
//     {associated: false, reason} or {associated: true, connector_id,
//     catalog_ref, artifact_version, ...}. 422 without the parameter.
//   - GET /ai-admin/catalog/connectors/{id} → the connector definition. There
//     is no list.
//
// SECRETS. auth_config is {oauth: {...}} or {api_token: {...}}. The test org
// returned both empty, but the portal's own parser reads client_secret and
// token_value from it, so a server may carry them. Redact keeps only the keys
// known to be safe and replaces every other value, so a field added later is
// hidden rather than leaked.
//
// Writes (POST/PATCH/DELETE on servers, POST/PUT/DELETE on redirect URIs)
// exist but are not implemented: they were not probed, and a server write
// carries the upstream credential.
package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
)

// Endpoints, relative to the v2 API base.
const (
	ServersEndpoint      = "/ai-admin/servers"
	RedirectURIsEndpoint = "/ai-admin/settings/redirect-uris"
	ResolveEndpoint      = "/ai-admin/catalog/resolve"
	ConnectorsEndpoint   = "/ai-admin/catalog/connectors"
)

// PageSize is the page size requested when listing. The portal uses 100.
const PageSize = 100

// maxPages bounds a listing so a server that keeps returning new tokens
// cannot loop forever. 50 pages of 100 matches the portal's own limit.
const maxPages = 50

// Redacted replaces a value that is not known to be safe to show.
const Redacted = "[redacted]"

// Getter is the part of the v2 API client this package needs.
type Getter interface {
	Get(ctx context.Context, endpoint string) (json.RawMessage, error)
}

// ListServers returns every AI Gateway server, redacted.
func ListServers(ctx context.Context, c Getter) ([]json.RawMessage, error) {
	items, err := listAll(ctx, c, ServersEndpoint, "servers")
	if err != nil {
		return nil, err
	}
	return redactAll(items)
}

// GetServer returns one server by its object id, redacted.
func GetServer(ctx context.Context, c Getter, id string) (json.RawMessage, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("%q is not an AI Gateway server id (24 hex characters)", id)
	}
	raw, err := c.Get(ctx, ServersEndpoint+"/"+id)
	if err != nil {
		return nil, err
	}
	return Redact(raw)
}

// ListRedirectURIs returns the OAuth redirect URIs the gateway accepts.
func ListRedirectURIs(ctx context.Context, c Getter) ([]json.RawMessage, error) {
	return listAll(ctx, c, RedirectURIsEndpoint, "redirect_uris")
}

// Resolve reports whether an application is linked to a catalog connector.
func Resolve(ctx context.Context, c Getter, appID string) (json.RawMessage, error) {
	if !ValidID(appID) {
		return nil, fmt.Errorf("%q is not an application id (24 hex characters)", appID)
	}
	raw, err := c.Get(ctx, ResolveEndpoint+"?"+url.Values{"application_object_id": {appID}}.Encode())
	if err != nil {
		return nil, err
	}
	var probe struct {
		Associated *bool `json:"associated"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Associated == nil {
		return nil, unexpected("catalog resolve", "no associated field")
	}
	return raw, nil
}

// GetConnector returns one catalog connector by id, e.g. "salesforce".
func GetConnector(ctx context.Context, c Getter, id string) (json.RawMessage, error) {
	if !ValidConnectorID(id) {
		return nil, fmt.Errorf("%q is not a connector id (lower-case letters, digits, '-' and '_')", id)
	}
	raw, err := c.Get(ctx, ConnectorsEndpoint+"/"+url.PathEscape(id))
	if err != nil {
		return nil, err
	}
	var probe struct {
		ConnectorID *string `json:"connector_id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.ConnectorID == nil {
		return nil, unexpected("catalog connector", "no connector_id field")
	}
	return raw, nil
}

// listAll follows next_page_token until it is empty. key names the array in
// the envelope; a response without it is an error.
func listAll(ctx context.Context, c Getter, endpoint, key string) ([]json.RawMessage, error) {
	all := []json.RawMessage{}
	token := ""
	for page := 0; page < maxPages; page++ {
		q := url.Values{"page_size": {fmt.Sprint(PageSize)}}
		if token != "" {
			q.Set("page_token", token)
		}
		raw, err := c.Get(ctx, endpoint+"?"+q.Encode())
		if err != nil {
			return nil, err
		}
		var env map[string]json.RawMessage
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, unexpected(key, "not a JSON object")
		}
		var items []json.RawMessage
		if err := json.Unmarshal(env[key], &items); err != nil || items == nil {
			return nil, unexpected(key, fmt.Sprintf("no %s array", key))
		}
		all = append(all, items...)
		var next string
		if t, ok := env["next_page_token"]; ok {
			if err := json.Unmarshal(t, &next); err != nil {
				return nil, unexpected(key, "next_page_token is not a string")
			}
		}
		if next == "" {
			return all, nil
		}
		if next == token {
			return nil, unexpected(key, "the server repeated a page token")
		}
		token = next
	}
	return nil, fmt.Errorf("listing %s: more than %d pages; refusing to return a partial list", key, maxPages)
}

func unexpected(what, why string) error {
	return fmt.Errorf("unexpected AI Gateway %s response (%s): this is an undocumented preview API and may have changed", what, why)
}

// safeAuthKeys are the auth_config keys shown as-is. Everything else is
// replaced with Redacted.
var safeAuthKeys = map[string]bool{
	"type":               true,
	"client_id":          true,
	"scope":              true,
	"scopes":             true,
	"token_header_name":  true,
	"documentation_url":  true,
	"client_secret_hint": true,
	"token_hint":         true,
	"dcr_supported":      true,
}

// Redact returns a server with its auth_config reduced to safe keys. The rest
// of the server is returned unchanged.
func Redact(raw json.RawMessage) (json.RawMessage, error) {
	var server map[string]any
	if err := json.Unmarshal(raw, &server); err != nil || server == nil {
		return nil, unexpected("server", "not a JSON object")
	}
	if _, ok := server["object_id"]; !ok {
		return nil, unexpected("server", "no object_id field")
	}
	if ac, ok := server["auth_config"]; ok {
		server["auth_config"] = redactValue(ac)
	}
	return json.Marshal(server)
}

// redactValue keeps safe keys, recurses into objects (auth kinds such as
// "oauth" and "api_token" are objects), and replaces anything else.
func redactValue(v any) any {
	obj, ok := v.(map[string]any)
	if !ok {
		if v == nil {
			return nil
		}
		return Redacted
	}
	out := make(map[string]any, len(obj))
	for k, val := range obj {
		switch {
		case isObject(val):
			out[k] = redactValue(val)
		case safeAuthKeys[k]:
			out[k] = val
		default:
			out[k] = Redacted
		}
	}
	return out
}

func isObject(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func redactAll(items []json.RawMessage) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		r, err := Redact(it)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// AuthType names a server's auth kind ("oauth", "api_token"), or "" when
// auth_config is missing or empty.
func AuthType(server json.RawMessage) string {
	var s struct {
		AuthConfig map[string]json.RawMessage `json:"auth_config"`
	}
	if json.Unmarshal(server, &s) != nil || len(s.AuthConfig) == 0 {
		return ""
	}
	kinds := make([]string, 0, len(s.AuthConfig))
	for k := range s.AuthConfig {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds[0]
}

// FilterByApp keeps the servers linked to application appID.
func FilterByApp(servers []json.RawMessage, appID string) []json.RawMessage {
	out := []json.RawMessage{}
	for _, s := range servers {
		var v struct {
			App string `json:"application_object_id"`
		}
		if json.Unmarshal(s, &v) == nil && v.App == appID {
			out = append(out, s)
		}
	}
	return out
}

// FindServer returns the server whose object_id or exact name is ident.
// A name shared by several servers is an error listing their ids.
func FindServer(servers []json.RawMessage, ident string) (json.RawMessage, error) {
	var matches []json.RawMessage
	var ids []string
	for _, s := range servers {
		var v struct {
			ID   string `json:"object_id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(s, &v) != nil {
			continue
		}
		if v.ID == ident {
			return s, nil
		}
		if v.Name == ident {
			matches = append(matches, s)
			ids = append(ids, v.ID)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no AI Gateway server named or with id %q (see `jc ai-gateway servers list`)", ident)
	case 1:
		return matches[0], nil
	}
	return nil, fmt.Errorf("%d AI Gateway servers are named %q; use an id: %v", len(matches), ident, ids)
}

// ValidID reports whether s is a 24-character hex object id.
func ValidID(s string) bool {
	if len(s) != 24 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// ValidConnectorID reports whether s looks like a catalog connector id.
func ValidConnectorID(s string) bool {
	if s == "" || len(s) > 100 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
