// Package agents is the shared contract for JumpCloud's AI Agent Identities
// area, used by the CLI and the MCP tools so the two cannot drift.
//
// Agents are a new kind of identity: an autonomous workload gets its own
// JumpCloud record, its own OAuth client credentials, and membership in agent
// groups that are assigned to applications. The feature went to Public
// Preview on 2026-08-31 and its REST API shipped on 2026-10-13.
//
// Everything here was established by probing a live tenant with real agents
// in it, on 2026-10-14 and again on 2026-10-19. Two spec tags cover the area
// — Agents and Agent Groups — and they behave as two different APIs that
// happen to have shipped on the same day. The differences are not cosmetic:
//
//  1. /agents requires an x-org-id header that the spec marks optional.
//     /agentgroups does not take one.
//  2. /agents pages by cursor. /agentgroups pages by offset. EACH SILENTLY
//     IGNORES THE OTHER'S PARAMETERS, which is how an ignored --limit turns
//     into an unbounded read.
//  3. They accept different filter fields, and report a bad one differently.
//  4. /agentgroups sorts. /agents ignores sort entirely.
//
// Every one of those is a chance for two surfaces to disagree, which is why
// they are written down once, here, rather than rediscovered per caller.
package agents

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// --- Endpoints ------------------------------------------------------------
//
// Note the inconsistency, which is the server's: agents are "/agents" but
// their groups are "/agentgroups", one word, not "/agent-groups". The CLI
// command is `jc agent-groups` because that is how an operator reads it; the
// endpoint is spelled the way the server spells it.

const (
	AgentsEndpoint = "/agents"
	GroupsEndpoint = "/agentgroups"
)

// Agent endpoints.
func AgentEndpoint(id string) string             { return AgentsEndpoint + "/" + id }
func AgentMemberOfEndpoint(id string) string     { return AgentEndpoint(id) + "/memberof" }
func AgentApplicationsEndpoint(id string) string { return AgentEndpoint(id) + "/applications" }

// AgentCredentialsEndpoint addresses an agent's inbound OAuth credentials —
// the client_id/client_secret pairs an agent uses to authenticate TO
// JumpCloud. "Inbound" is the server's word and is worth keeping: it
// distinguishes these from the outbound tokens an agent would use to reach
// something else.
func AgentCredentialsEndpoint(id string) string { return AgentEndpoint(id) + "/inbound-credentials" }

// Agent group endpoints.
func GroupEndpoint(id string) string             { return GroupsEndpoint + "/" + id }
func GroupMembersEndpoint(id string) string      { return GroupEndpoint(id) + "/members" }
func GroupAssociationsEndpoint(id string) string { return GroupEndpoint(id) + "/associations" }

// --- The x-org-id header --------------------------------------------------

// OrgHeader is the header /agents requires and the spec marks `required:
// false`.
const OrgHeader = "x-org-id"

// RequiresOrgID reports whether an endpoint rejects a call carrying no
// x-org-id header with 400 {"message":"x-org-id is required"}.
//
// The split is per-endpoint, not per-tree, and it is not guessable: /agents,
// /agents/{id} and /agents/{id}/inbound-credentials demand the header, while
// /agents/{id}/memberof and /agents/{id}/applications do not — those two are
// served by the same older association subsystem that answers
// /users/{id}/memberof, and it has never wanted one. Nothing under
// /agentgroups wants one either.
//
// Sending the header where it is not required is harmless (verified against
// every read in the area), so callers may simply always send it when they
// have one. This predicate exists for the other direction: when jc has NO
// org id, it can fail here, naming the config it needs, instead of forwarding
// a server error that names a header no flag is called.
func RequiresOrgID(endpoint string) bool {
	// Matched on "/agents" and "/agents/" rather than on "/agent", which is
	// the prefix the two trees actually share. "/agents" alone does not match
	// "/agentgroups" — the sixth character differs — but a predicate written
	// one character shorter would answer true for all nine group operations.
	if endpoint != AgentsEndpoint && !strings.HasPrefix(endpoint, AgentsEndpoint+"/") {
		return false
	}
	if strings.HasSuffix(endpoint, "/memberof") || strings.HasSuffix(endpoint, "/applications") {
		return false
	}
	return true
}

// ErrNoOrgID explains a missing org id in terms of the things an operator can
// actually set, since the server's own message names a header instead.
func ErrNoOrgID() error {
	return fmt.Errorf("the agents API needs your organization id, and this profile has none: " +
		"run `jc auth login` to store it, or set JC_ORG_ID")
}

// OrgMismatch recognises the server's answer to an org id that does not
// belong to the calling key: 404 "selected organization not found".
//
// Worth recognising separately because a 404 from this area otherwise reads
// as "no such agent", sending an operator to look for a missing record when
// the real fault is a stale org_id in their config.
func OrgMismatch(err error) bool {
	return err != nil && strings.Contains(err.Error(), "selected organization not found")
}

// --- Ids ------------------------------------------------------------------

var objectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// IsObjectID reports whether s is a JumpCloud 24-hex object id. Agents and
// agent groups both use them.
func IsObjectID(s string) bool { return objectIDPattern.MatchString(s) }

// ErrNotObjectID explains a malformed id before it reaches the API.
//
// The server does distinguish the two cases correctly here — 400 'invalid
// object id "not-an-id"' for the wrong shape, 404 "Not Found" for a
// well-formed id that is absent — which is better than several other areas
// manage. Checking locally anyway keeps a typo from costing a round trip.
func ErrNotObjectID(kind, s string) error {
	return fmt.Errorf("%q is not a JumpCloud object id — an %s is addressed by its "+
		"24-character hex id, which you take from the listing", s, kind)
}

// --- Pagination -----------------------------------------------------------
//
// The two listings page differently and ignore each other's parameters in
// silence. /agents honours pageSize/pageToken and ignores limit and skip;
// /agentgroups honours limit/skip and ignores pageSize. Neither says so.

// AgentsPagingIsCursorOnly records why /agents cannot go through the generic
// v2 list helper, and is quoted in the commands' help.
//
// api.V2Client.ListAll follows a Link header, and falls back to incrementing
// skip whenever a page comes back full and no Link header is present.
// /agents emits NO Link header (verified in the response headers) and ignores
// skip. On a tenant with more agents than one page holds, that fallback would
// re-request the first page forever, appending the same rows each time: not a
// truncated read but an unbounded duplicating one. So this area follows
// nextPageToken itself.
const AgentsPagingIsCursorOnly = "the agents listing pages by cursor and emits no Link header; " +
	"it ignores limit and skip, so paging follows nextPageToken instead"

// MaxAgentsPageSize is the largest page this area asks for. The server's own
// cap is not documented and a tenant large enough to find it empirically was
// not available; 100 matches what jc asks of other v2 listings.
const MaxAgentsPageSize = 100

// AgentsPageQuery builds the query for one page of /agents.
//
// pageSize of 0 means "the server's default", which is what the first call of
// a full read uses. pageToken is the opaque cursor from the previous page —
// opaque is the contract, even though today's happens to be base64 of
// {"l":..,"s":..,"v":1}; a caller that decoded it would be relying on an
// implementation detail the server is free to change.
func AgentsPageQuery(pageSize int, pageToken string) url.Values {
	v := url.Values{}
	if pageSize > 0 {
		v.Set("pageSize", strconv.Itoa(pageSize))
	}
	if pageToken != "" {
		v.Set("pageToken", pageToken)
	}
	return v
}

// GroupsPageQuery builds the query for one page of /agentgroups.
func GroupsPageQuery(limit, skip int) url.Values {
	v := url.Values{}
	if limit > 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	if skip > 0 {
		v.Set("skip", strconv.Itoa(skip))
	}
	return v
}

// --- Filtering ------------------------------------------------------------
//
// Both listings take jc's usual field:op:value filters, and both validate
// them — but they accept different fields, support only two operators, and
// report a rejection with very different quality.

var agentFilterFields = []string{"email", "name", "state"}

// groupFilterFields are the fields /agentgroups will filter on.
//
// An unsupported field here does NOT produce the clean 400 that /agents gives
// ("unsupported filter field: ownerId"). It produces
// 500 "failed to list groups: failed to list collection objects: ...", an
// internal message that tells an operator nothing about what they typed. That
// asymmetry is the reason filters are validated locally in this area rather
// than simply forwarded.
var groupFilterFields = []string{"name", "type"}

// filterOperators is the whole set, for both listings. No contains, no
// startsWith, no ordering operators: an agent cannot be found by substring.
var filterOperators = []string{"eq", "ne"}

// AgentFilterFields and GroupFilterFields are for help text and MCP schemas.
func AgentFilterFields() []string { return append([]string(nil), agentFilterFields...) }
func GroupFilterFields() []string { return append([]string(nil), groupFilterFields...) }
func FilterOperators() []string   { return append([]string(nil), filterOperators...) }

// ValidateAgentFilter and ValidateGroupFilter check a filter expression
// against what the respective endpoint will actually accept.
func ValidateAgentFilter(expr string) error {
	return validateFilter(expr, agentFilterFields, "agents")
}

func ValidateGroupFilter(expr string) error {
	return validateFilter(expr, groupFilterFields, "agent groups")
}

func validateFilter(expr string, fields []string, what string) error {
	// A value may legitimately contain colons, so split off only the field
	// and the operator.
	parts := strings.SplitN(expr, ":", 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid filter %q: expected field:operator:value, e.g. name:eq:my-agent", expr)
	}
	field, op := parts[0], parts[1]
	if !contains(fields, field) {
		return fmt.Errorf("the %s listing cannot filter on %q: it accepts %s",
			what, field, strings.Join(fields, ", "))
	}
	if !contains(filterOperators, op) {
		return fmt.Errorf("the %s listing does not support the %q operator: it accepts %s "+
			"— there is no substring match in this area", what, op, strings.Join(filterOperators, ", "))
	}
	if parts[2] == "" {
		return fmt.Errorf("invalid filter %q: no value after %q", expr, op)
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// AgentsIgnoreSort records a silent no-op, so no surface offers a flag that
// does nothing. /agents accepts sort= and disregards it; /agentgroups honours
// both sort=name and sort=-name (verified: the descending form reverses the
// rows).
const AgentsIgnoreSort = "the agents listing ignores sort; agent groups honour it"

// --- Association targets --------------------------------------------------

// AssociationTargetApplication is the only target an agent group may be
// associated with today.
const AssociationTargetApplication = "application"

// AssociationTargets is the whole enum — one value. Agent groups reach
// applications and nothing else: no policies, no commands, no devices.
var AssociationTargets = []string{AssociationTargetApplication}

// AssociationsQuery builds the query for GET /agentgroups/{id}/associations.
//
// targets is REQUIRED, which the spec does not say: without it the server
// answers 400 "Error: Request validation failed: Parameter (targets) is
// Required". Supplying the default locally rather than forwarding that error
// is the difference between a working command and a puzzle.
func AssociationsQuery(target string) (url.Values, error) {
	if target == "" {
		target = AssociationTargetApplication
	}
	if !contains(AssociationTargets, target) {
		return nil, fmt.Errorf("invalid association target %q: agent groups can be associated with %s only",
			target, strings.Join(AssociationTargets, ", "))
	}
	return url.Values{"targets": []string{target}}, nil
}

// WithQuery appends query parameters to an endpoint.
func WithQuery(endpoint string, v url.Values) string {
	if len(v) == 0 {
		return endpoint
	}
	return endpoint + "?" + v.Encode()
}

// --- Envelopes ------------------------------------------------------------
//
// Five shapes across nine reads. Every parser below reports a decode failure
// rather than returning an empty result: a caller that cannot tell "this
// agent has no credentials" from "the response did not parse" will report an
// agent as having no way in when it has one.

func decode(raw json.RawMessage, v any, what string) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("decoding the %s response: %w", what, err)
	}
	return nil
}

// ParseAgents reads {agents, totalSize, nextPageToken}.
//
// Note totalSize, not the totalCount every other jc area uses. It is the
// server's count of MATCHES — it reflects an active filter and is not the
// page length — and it is honest, unlike the Password Vault count that
// excludes a row the listing includes.
//
// nextPageToken is absent on the last page, which is the loop's terminating
// condition.
func ParseAgents(raw json.RawMessage) (rows []json.RawMessage, totalSize int, nextPageToken string, err error) {
	var env struct {
		Agents        []json.RawMessage `json:"agents"`
		TotalSize     int               `json:"totalSize"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if err := decode(raw, &env, "agents"); err != nil {
		return nil, 0, "", err
	}
	return env.Agents, env.TotalSize, env.NextPageToken, nil
}

// ParseGroups reads a bare array. There is no envelope and so no count: the
// footer for a group listing can only report the rows it has, which is why it
// uses a plain count rather than the "N of M" form.
func ParseGroups(raw json.RawMessage) ([]json.RawMessage, error) {
	var rows []json.RawMessage
	if err := decode(raw, &rows, "agent groups"); err != nil {
		return nil, err
	}
	return rows, nil
}

// ParseCredentials reads {credentials}.
//
// The records carry authType, clientId, status, tokenEndpointAuthMethod and
// three timestamps — and NEVER a secret. That is a property of the endpoint,
// verified against every agent on a live tenant, and it is what makes listing
// credentials an ordinary read that MCP may expose. Creating one is not: see
// SecretIsShowOnce.
func ParseCredentials(raw json.RawMessage) ([]json.RawMessage, error) {
	var env struct {
		Credentials []json.RawMessage `json:"credentials"`
	}
	if err := decode(raw, &env, "agent credentials"); err != nil {
		return nil, err
	}
	return env.Credentials, nil
}

// SecretIsShowOnce records the one fact that shapes credential creation, and
// is quoted wherever a surface has to justify its behaviour.
//
// POST /agents/{id}/inbound-credentials returns a clientSecret exactly once.
// No read returns it afterwards. A surface that discards it has cost the
// operator a credential — it cannot be recovered, only replaced — and a
// surface that prints it carelessly has leaked one. Both failures are easy
// and neither is recoverable, which is why the create path handles it
// explicitly instead of letting generic output decide.
const SecretIsShowOnce = "an agent credential's clientSecret is returned once, by the call that " +
	"creates it, and by nothing afterwards"

// GraphRef is one entry from /agents/{id}/memberof or
// /agents/{id}/applications.
//
// Both return bare arrays in jc's existing graph-association shape
// {compiledAttributes, id, paths, type} — the same shape /users/{id}/memberof
// returns, carrying an id and a type and NO NAME. Reading a name straight off
// one yields "" every time, which is exactly the defect that put eight groups
// called "" into MCP's user_view. Names come from Resolve.
type GraphRef struct {
	ID   string `json:"id"`
	Type string `json:"type"`

	// Via holds the ids of the intermediate objects the association passes
	// through — for an application reached because the agent is in a group,
	// that group. It comes from the paths array, which is the only place the
	// distinction between a direct assignment and an inherited one exists.
	Via []string `json:"via,omitempty"`
}

// Direct reports whether the association is immediate rather than inherited.
func (r GraphRef) Direct() bool { return len(r.Via) == 0 }

// ParseGraph reads a /memberof or /applications response.
func ParseGraph(raw json.RawMessage, what string) ([]GraphRef, error) {
	var rows []struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Paths [][]struct {
			To struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"to"`
		} `json:"paths"`
	}
	if err := decode(raw, &rows, what); err != nil {
		return nil, err
	}

	out := make([]GraphRef, 0, len(rows))
	for i, r := range rows {
		if r.ID == "" {
			// Not a reference to anything. Distinct from a record that could
			// not be read — which decode() has already reported — because
			// conflating the two understates an agent's access, and that is
			// the wrong direction for a number used in a review.
			return nil, fmt.Errorf("%s entry %d of %d carries no id — the record shape has "+
				"changed, and jc would otherwise report less access than the agent has",
				what, i+1, len(rows))
		}
		ref := GraphRef{ID: r.ID, Type: r.Type}
		// The last hop of a path is the target itself; anything before it is
		// an intermediary. A direct assignment has a single-hop path.
		seen := map[string]bool{}
		for _, path := range r.Paths {
			for h, hop := range path {
				if h == len(path)-1 || hop.To.ID == "" || hop.To.ID == r.ID {
					continue
				}
				if !seen[hop.To.ID] {
					seen[hop.To.ID] = true
					ref.Via = append(ref.Via, hop.To.ID)
				}
			}
		}
		sort.Strings(ref.Via)
		out = append(out, ref)
	}
	return out, nil
}

// MemberRef is one entry from /agentgroups/{id}/members or
// /agentgroups/{id}/associations.
//
// Both return bare arrays in jc's existing membership shape
// {attributes, to:{id, type}} — the user-group shape, nested one level
// deeper than GraphRef and with the id under "to". Same absence of a name.
type MemberRef struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// ParseMembers reads a /members or /associations response.
func ParseMembers(raw json.RawMessage, what string) ([]MemberRef, error) {
	var rows []struct {
		To struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"to"`
	}
	if err := decode(raw, &rows, what); err != nil {
		return nil, err
	}
	out := make([]MemberRef, 0, len(rows))
	for i, r := range rows {
		if r.To.ID == "" {
			return nil, fmt.Errorf("%s entry %d of %d carries no id under \"to\" — the record "+
				"shape has changed, and jc would otherwise report fewer members than the group has",
				what, i+1, len(rows))
		}
		out = append(out, MemberRef{ID: r.To.ID, Type: r.To.Type})
	}
	return out, nil
}

// --- States ---------------------------------------------------------------

// AgentStates are the values an agent's state field takes.
//
// "active" was observed live; "suspended" is the state the suspend endpoint
// produces and filters accept (filter=state:eq:suspended is a valid query
// that currently matches nothing on the probe tenant). There is no "deleted"
// state — deletion removes the record.
var AgentStates = []string{"active", "suspended"}
