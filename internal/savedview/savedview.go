// Package savedview holds the JumpCloud saved-view wire-contract helpers
// shared by the CLI (internal/cmd) and the MCP server (internal/mcp), so the
// two surfaces can never drift on the parts that are easy to get wrong. Every
// rule here was verified live against the tenant on 2026-07-31 (see the probe
// in the KLA-485 Saved Views PR):
//
//   - GET /saved-views returns {totalCount, views:[…]} — the array is under
//     "views" (ResponseKey), and there is NO GET /saved-views/{id}, so get and
//     the update read-modify-write both pull the object back out of the list.
//   - POST /saved-views returns the bare SavedView object (no envelope).
//   - PUT /saved-views/{id} takes a request body WRAPPED in {savedView:{…}} and
//     is a full-object replace; the response is the bare object.
//   - `source` is a required free-form discriminator naming which list the view
//     belongs to (devices, users, user_groups, systems, policies, … all
//     accepted live — it is NOT enum-locked server-side).
package savedview

import "fmt"

// DefaultFields is the default field subset shown for saved-view output.
var DefaultFields = []string{"id", "name", "source", "shared", "isDefault"}

// CommonSources are the discriminator values seen in the JumpCloud console.
// They are suggestions for help text and completion only — the API accepts
// other values, so this list is NOT used to reject input.
var CommonSources = []string{"devices", "users", "user_groups", "systems", "policies"}

// serverManagedKeys are dropped from a saved-view object before it is PUT back
// as a read-modify-write update. adminId is server-owned (the creating admin);
// id travels in the URL path. Echoing them is at best ignored; stripping keeps
// the PUT body clean and intent-only.
var serverManagedKeys = []string{"adminId"}

// StripServerManaged removes server-owned keys from a saved-view object in
// place. The object still carries its own id (the SavedView schema includes
// it and the live PUT accepts it), so id is left intact.
func StripServerManaged(obj map[string]any) {
	for _, k := range serverManagedKeys {
		delete(obj, k)
	}
}

// PutBody wraps a saved-view object in the {savedView:{…}} envelope that
// PUT /saved-views/{id} requires. The bare object 400s.
func PutBody(obj map[string]any) map[string]any {
	return map[string]any{"savedView": obj}
}

// CreateBody assembles the POST /saved-views body. name and source are
// required; columns and sort are optional; shared and isDefault default false.
// configuration.filters is left to the caller (advanced) — the common case is
// just columns + sort.
func CreateBody(name, source string, columns []string, sort string, shared, isDefault bool) (map[string]any, error) {
	if name == "" {
		return nil, fmt.Errorf("--name is required")
	}
	if source == "" {
		return nil, fmt.Errorf("--source is required (the list the view belongs to, e.g. devices, users, user_groups, systems, policies)")
	}
	config := map[string]any{}
	if sort != "" {
		config["sort"] = sort
	}
	body := map[string]any{
		"name":          name,
		"source":        source,
		"configuration": config,
		"shared":        shared,
		"isDefault":     isDefault,
	}
	if len(columns) > 0 {
		body["columns"] = columns
	}
	return body, nil
}
