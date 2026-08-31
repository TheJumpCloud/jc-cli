package mcp

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/klaassen-consulting/jc/internal/api"
)

// Group membership, with names.
//
// The /memberof endpoints return graph association objects: an id and a type,
// and NO name. Reading a name straight off them yields "" every time, which is
// what both view tools were doing — user_view showed eight groups all called
// "", device_view one. The names have to be joined from the group catalog,
// exactly as device_view already does for policy names in the same response,
// which is why the policies looked fine while the groups did not.
//
// One helper, used by both tools: this is the third bug in this codebase caused
// by two surfaces doing the same job separately.

// GroupRef is a group a user or device belongs to.
type GroupRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// resolveGroupNames fetches the group ids from a /memberof endpoint and joins
// them against the group catalogs to get names.
//
// A failure to read the catalog degrades to id-only refs rather than dropping
// the membership: knowing a user is in eight groups is still worth having when
// the names cannot be fetched, and the caller is warned.
func resolveGroupNames(ctx context.Context, v2 *api.V2Client, memberOfPath string,
	warn func(string)) []GroupRef {

	result, err := v2.ListAll(ctx, memberOfPath, api.V2ListOptions{})
	if err != nil {
		warn("groups: " + err.Error())
		return nil
	}

	ids := make([]string, 0, len(result.Data))
	for _, raw := range result.Data {
		var g struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &g); err != nil || g.ID == "" {
			continue
		}
		ids = append(ids, g.ID)
	}
	if len(ids) == 0 {
		return []GroupRef{}
	}

	// One catalog fetch and an in-memory join, rather than N lookups.
	nameByID := map[string]string{}
	for _, endpoint := range []string{"/usergroups", "/systemgroups"} {
		all, err := v2.ListAll(ctx, endpoint, api.V2ListOptions{})
		if err != nil {
			warn("group names from " + endpoint + ": " + err.Error())
			continue
		}
		for _, raw := range all.Data {
			var g struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &g); err != nil {
				continue
			}
			if g.Name != "" {
				nameByID[g.ID] = g.Name
			}
		}
	}

	refs := make([]GroupRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, GroupRef{ID: id, Name: nameByID[id]})
	}
	sort.Slice(refs, func(i, j int) bool {
		// Unnamed groups sort last, so a partial resolution still surfaces
		// the names it did find.
		if (refs[i].Name == "") != (refs[j].Name == "") {
			return refs[i].Name != ""
		}
		if refs[i].Name != refs[j].Name {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].ID < refs[j].ID
	})
	return refs
}
