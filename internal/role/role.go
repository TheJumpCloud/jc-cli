// Package role holds the JumpCloud role wire-contract helpers shared by the
// CLI (internal/cmd) and the MCP server (internal/mcp), so the two surfaces
// can never drift on the create-body shape and the read-modify-write strip.
// Verified live against the tenant on 2026-07-24 (KLA-485 roles PR #96):
//
//   - GET /roles wraps the list in {results, totalCount}; the id lives under id.
//   - PUT /roles/{id} is a full-object replace (name and scopes both required),
//     so an update must read-modify-write and strip the server-managed id.
package role

import "strings"

// DefaultFields is the default field subset shown for role list/get output.
var DefaultFields = []string{"id", "name", "description"}

// SplitScopes turns a comma-separated scopes value into a trimmed, empty-free
// slice.
func SplitScopes(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// CreateBody assembles the POST /roles body. name and scopes are required;
// description is optional.
func CreateBody(name string, scopes []string, description string) map[string]any {
	body := map[string]any{"name": name, "scopes": scopes}
	if description != "" {
		body["description"] = description
	}
	return body
}

// StripServerManaged removes server-owned keys from a role object in place
// before it is PUT back as a read-modify-write update. id travels in the URL
// path and PUT rejects/ignores it in the body.
func StripServerManaged(obj map[string]any) {
	delete(obj, "id")
}
