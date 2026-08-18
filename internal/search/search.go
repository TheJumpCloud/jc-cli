// Package search holds the JumpCloud v1 search wire-contract helpers shared by
// the CLI (internal/cmd) and the MCP server (internal/mcp), so the two surfaces
// can never drift on the request body or the set of searchable resources.
//
// Verified live against org 5ec71e8e96bfda0611fc6c5b on 2026-08-18 (KLA-485
// search probe): each POST /api/search/<resource> takes
//
//	{"searchFilter": {"searchTerm": "...", "fields": [...]},  // both optional
//	 "filter":       ["field:op:value", ...]}                 // optional
//
// and returns {results, totalCount}. An empty body is a match-all query. The
// api.V1Client.Search helper injects skip/limit/sort and paginates.
//
// Four resources are covered: systems, systemusers, commands, commandresults.
// /search/organizations is intentionally NOT covered — it uses a distinct,
// undocumented filter grammar ({filter:{and|or:[…]}} over an index vocabulary
// that rejects the object's own field names) and could not be made to return a
// row on a single-org key; `jc org list` covers the practical need. Tracked as
// a per-op gap.
package search

import "sort"

// Resource describes one searchable JumpCloud v1 resource.
type Resource struct {
	// Name is the CLI subcommand / MCP suffix (e.g. "systems").
	Name string
	// Endpoint is the v1 search path (e.g. "/search/systemusers").
	Endpoint string
	// SearchFields are the default fields a bare search term matches against.
	// Empty means "let the API pick its defaults" (proven to work for systems).
	SearchFields []string
	// DefaultFields is the default output column subset.
	DefaultFields []string
	// Aliases are alternate CLI subcommand names.
	Aliases []string
}

// Resources is the set of covered search resources, keyed by Name.
var Resources = map[string]Resource{
	"systems": {
		Name:          "systems",
		Endpoint:      "/search/systems",
		SearchFields:  []string{"hostname", "displayName", "os", "serialNumber"},
		DefaultFields: []string{"_id", "displayName", "hostname", "os", "active"},
		Aliases:       []string{"devices", "system"},
	},
	"users": {
		Name:          "users",
		Endpoint:      "/search/systemusers",
		SearchFields:  []string{"username", "email", "firstname", "lastname"},
		DefaultFields: []string{"_id", "username", "email", "firstname", "lastname", "activated", "suspended"},
		Aliases:       []string{"systemusers", "user"},
	},
	"commands": {
		Name:          "commands",
		Endpoint:      "/search/commands",
		SearchFields:  []string{"name", "command"},
		DefaultFields: []string{"_id", "name", "commandType", "launchType", "trigger"},
		Aliases:       []string{"command"},
	},
	"command-results": {
		Name:          "command-results",
		Endpoint:      "/search/commandresults",
		SearchFields:  []string{"name"},
		DefaultFields: []string{"_id", "name", "system", "user", "requestTime"},
		Aliases:       []string{"commandresults", "command-result"},
	},
}

// ResourceNames returns the covered resource names in stable sorted order.
func ResourceNames() []string {
	names := make([]string, 0, len(Resources))
	for n := range Resources {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Lookup resolves a resource by its name or any alias.
func Lookup(nameOrAlias string) (Resource, bool) {
	if r, ok := Resources[nameOrAlias]; ok {
		return r, true
	}
	for _, r := range Resources {
		for _, a := range r.Aliases {
			if a == nameOrAlias {
				return r, true
			}
		}
	}
	return Resource{}, false
}

// Body builds the v1 search request body. term and searchFields are folded into
// searchFilter (both optional — an empty term omits searchFilter, yielding a
// match-all query bounded only by filter). filterQueries are V1 filter strings
// (from filter.ToV1Queries). An empty result is a valid match-all body.
func (r Resource) Body(term string, searchFields, filterQueries []string) map[string]any {
	body := map[string]any{}
	if term != "" {
		sf := map[string]any{"searchTerm": term}
		fields := searchFields
		if len(fields) == 0 {
			fields = r.SearchFields
		}
		if len(fields) > 0 {
			sf["fields"] = fields
		}
		body["searchFilter"] = sf
	}
	if len(filterQueries) > 0 {
		body["filter"] = filterQueries
	}
	return body
}
