package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/klaassen-consulting/jc/internal/api"
)

// Catalog turns the ids in this area's association responses into names.
//
// It exists because /agents/{id}/memberof, /agents/{id}/applications,
// /agentgroups/{id}/members and /agentgroups/{id}/associations all return
// records carrying an id and a type and no name — four of the nine reads in
// the area. Printing them unresolved produces a screen of hex, which is the
// defect that once put eight groups called "" into MCP's user_view; see
// internal/usergroups, which does the same job for users and devices.
//
// The three catalogs involved do not agree on how to spell an id or a name:
//
//	agent_group   /agentgroups    id    name
//	agent         /agents         id    name          (and needs x-org-id)
//	application   /applications   _id   displayLabel
//
// An application's "name" is a slug ("aws-sso") and its "displayName" is the
// connector type ("AWS IAM Identity Center"); the string an operator
// recognises is displayLabel ("AWS"). jc's own `apps list` shows the same
// three fields for the same reason.
//
// Catalogs load lazily and at most once each, so a command that resolves
// nothing makes no extra request, and one that resolves two hundred refs
// still makes exactly one.
type Catalog struct {
	v2   *api.V2Client
	warn func(string)

	loaded map[string]bool
	names  map[string]string
}

// catalogSource describes where one object type's names come from.
type catalogSource struct {
	endpoint  string
	idFields  []string
	nameField []string
}

// catalogSources is keyed by the "type" these responses report.
var catalogSources = map[string]catalogSource{
	"agent_group": {endpoint: GroupsEndpoint, idFields: []string{"id", "_id"}, nameField: []string{"name"}},
	"agent":       {endpoint: AgentsEndpoint, idFields: []string{"id", "_id"}, nameField: []string{"name"}},
	"application": {endpoint: "/applications", idFields: []string{"_id", "id"}, nameField: []string{"displayLabel", "displayName", "name"}},
}

// NewCatalog builds a resolver. warn receives a line whenever a name lookup
// degrades, and may be nil.
func NewCatalog(v2 *api.V2Client, warn func(string)) *Catalog {
	if warn == nil {
		warn = func(string) {}
	}
	return &Catalog{v2: v2, warn: warn, loaded: map[string]bool{}, names: map[string]string{}}
}

// Name returns the display name for one object, or "" when it cannot be
// resolved.
//
// A failure to read a CATALOG degrades to an empty name and a warning rather
// than failing the command: knowing an agent is in three groups is worth
// having when the names cannot be fetched. That is a deliberate asymmetry —
// a missing name is cosmetic, a missing membership is not — and it is the
// same trade internal/usergroups makes.
//
// A catalog record that will not DECODE is a different matter and is
// reported, because a half-read catalog produces names that are missing with
// no indication why.
func (c *Catalog) Name(ctx context.Context, objType, id string) (string, error) {
	src, ok := catalogSources[objType]
	if !ok {
		// An object type this area has not seen before. Not an error: the
		// association still gets reported, just without a name.
		return "", nil
	}
	if !c.loaded[objType] {
		c.loaded[objType] = true
		if err := c.load(ctx, objType, src); err != nil {
			return "", err
		}
	}
	return c.names[objType+"/"+id], nil
}

func (c *Catalog) load(ctx context.Context, objType string, src catalogSource) error {
	// The agents catalog needs x-org-id. The caller installs it for the
	// duration of the command rather than this package threading it through,
	// because sending it to the endpoints that do not require it is harmless
	// — verified against every read in the area.
	result, err := c.v2.ListAll(ctx, src.endpoint, api.V2ListOptions{ResponseKey: responseKeyFor(src.endpoint)})
	if err != nil {
		c.warn(objType + " names from " + src.endpoint + ": " + err.Error())
		return nil
	}
	for i, raw := range result.Data {
		var rec map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rec); err != nil {
			return fmt.Errorf("%s entry %d of %d did not decode: %w — names would otherwise be "+
				"missing with no indication why", src.endpoint, i+1, len(result.Data), err)
		}
		id := firstString(rec, src.idFields)
		if id == "" {
			continue
		}
		if name := firstString(rec, src.nameField); name != "" {
			c.names[objType+"/"+id] = name
		}
	}
	return nil
}

// responseKeyFor names the envelope key for a catalog that wraps its array.
//
// /agents is the only one: {agents, totalSize}. It is safe to read this way
// in a catalog load — ListAll's skip fallback would be unsafe on a tenant
// with more agents than one page holds (see AgentsPagingIsCursorOnly), so
// ListAll is NOT used for the agents LISTING. Here the risk is bounded to
// resolving names, and a duplicate id resolves to the same name; a tenant
// large enough to hit it gets a slow lookup, not a wrong one.
func responseKeyFor(endpoint string) string {
	if endpoint == AgentsEndpoint {
		return "agents"
	}
	return ""
}

func firstString(rec map[string]json.RawMessage, keys []string) string {
	for _, k := range keys {
		raw, ok := rec[k]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && s != "" {
			return s
		}
	}
	return ""
}

// NamedRef is an association with its name filled in, ready to print.
type NamedRef struct {
	ID   string   `json:"id"`
	Type string   `json:"type"`
	Name string   `json:"name"`
	Via  []string `json:"via,omitempty"`
}

// ResolveGraph names a /memberof or /applications result.
//
// Via is reported as names where they resolve, because "AWS, via AWS JKC
// Agent" answers the question an operator actually has — whether the agent
// reaches that application directly or through a group — and a hex id does
// not.
func ResolveGraph(ctx context.Context, cat *Catalog, refs []GraphRef) ([]NamedRef, error) {
	out := make([]NamedRef, 0, len(refs))
	for _, r := range refs {
		name, err := cat.Name(ctx, r.Type, r.ID)
		if err != nil {
			return nil, err
		}
		n := NamedRef{ID: r.ID, Type: r.Type, Name: name}
		for _, viaID := range r.Via {
			// An intermediary in this area is always a group; the type is not
			// repeated on the hop, so it is assumed and falls back to the id.
			viaName, err := cat.Name(ctx, "agent_group", viaID)
			if err != nil {
				return nil, err
			}
			if viaName == "" {
				viaName = viaID
			}
			n.Via = append(n.Via, viaName)
		}
		sort.Strings(n.Via)
		out = append(out, n)
	}
	sortNamed(out)
	return out, nil
}

// ResolveMembers names a /members or /associations result.
func ResolveMembers(ctx context.Context, cat *Catalog, refs []MemberRef) ([]NamedRef, error) {
	out := make([]NamedRef, 0, len(refs))
	for _, r := range refs {
		name, err := cat.Name(ctx, r.Type, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, NamedRef{ID: r.ID, Type: r.Type, Name: name})
	}
	sortNamed(out)
	return out, nil
}

// sortNamed puts named rows first, so a partial resolution still surfaces the
// names it did find, then orders by name and id for a stable output.
func sortNamed(rows []NamedRef) {
	sort.Slice(rows, func(i, j int) bool {
		if (rows[i].Name == "") != (rows[j].Name == "") {
			return rows[i].Name != ""
		}
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].ID < rows[j].ID
	})
}
