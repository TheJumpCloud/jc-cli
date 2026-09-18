// Package usergroups resolves the groups a user or device belongs to, with
// their names, for the CLI and the MCP tools.
//
// It exists because the /memberof endpoints return graph association objects
// carrying an id and a type and NO name. Reading a name straight off them
// yields "" every time, which is what both MCP view tools once did — user_view
// showed eight groups all called "", device_view one. The names have to be
// joined from the group catalog.
//
// The logic lived in internal/mcp, where its own comment recorded that it was
// "the third bug in this codebase caused by two surfaces doing the same job
// separately". Adding a CLI command that needed the same join would have made
// it the fourth, so it moved here instead.
package usergroups

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/klaassen-consulting/jc/internal/api"
)

// MemberOfPath builds the association endpoint for a user or a device.
func UserMemberOfPath(userID string) string     { return "/users/" + userID + "/memberof" }
func DeviceMemberOfPath(deviceID string) string { return "/systems/" + deviceID + "/memberof" }

// Ref is a group a user or device belongs to.
type Ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Resolve fetches the group ids from a /memberof endpoint and joins them
// against the group catalogs to get names.
//
// A failure to read the CATALOG degrades to id-only refs rather than dropping
// the membership: knowing a user is in eight groups is still worth having when
// the names cannot be fetched, and the caller is warned. That is a deliberate
// asymmetry — a missing name is cosmetic, a missing group is not.
//
// A record that will not DECODE is a different matter and is reported. An
// association jc cannot read is a group the user might be in, and silently
// dropping it understates someone's access — which is the wrong direction for
// a number used in offboarding and access reviews.
func Resolve(ctx context.Context, v2 *api.V2Client, memberOfPath string, warn func(string)) ([]Ref, error) {
	result, err := v2.ListAll(ctx, memberOfPath, api.V2ListOptions{})
	if err != nil {
		warn("groups: " + err.Error())
		return nil, nil
	}

	ids := make([]string, 0, len(result.Data))
	for i, raw := range result.Data {
		var g struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, fmt.Errorf("group association %d of %d did not decode: %w — the record "+
				"shape has changed, and jc would otherwise report fewer groups than the user "+
				"belongs to", i+1, len(result.Data), err)
		}
		if g.ID == "" {
			// Legitimately not a group reference. Distinct from a record that
			// could not be read, which is why the two are no longer one test.
			continue
		}
		ids = append(ids, g.ID)
	}
	if len(ids) == 0 {
		return []Ref{}, nil
	}

	// One catalog fetch and an in-memory join, rather than N lookups.
	nameByID := map[string]string{}
	for _, endpoint := range []string{"/usergroups", "/systemgroups"} {
		all, cerr := v2.ListAll(ctx, endpoint, api.V2ListOptions{})
		if cerr != nil {
			warn("group names from " + endpoint + ": " + cerr.Error())
			continue
		}
		for i, raw := range all.Data {
			var g struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &g); err != nil {
				return nil, fmt.Errorf("%s entry %d of %d did not decode: %w — names would "+
					"otherwise be missing with no indication why", endpoint, i+1, len(all.Data), err)
			}
			if g.Name != "" {
				nameByID[g.ID] = g.Name
			}
		}
	}

	refs := make([]Ref, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, Ref{ID: id, Name: nameByID[id]})
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
	return refs, nil
}
