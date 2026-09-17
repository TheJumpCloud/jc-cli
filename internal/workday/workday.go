// Package workday is the shared contract for JumpCloud's Workday import
// integration, used by the CLI and the MCP tools so the two cannot drift.
//
// This package is deliberately smaller than its siblings. The tenant it was
// probed against has SIX directory integrations and none of them is Workday,
// so only the empty list and the error shapes could be observed. What is here
// was verified; what could not be is recorded in Unprobed rather than guessed
// at, because a parser written for an envelope nobody has seen is a guess
// wearing a contract's clothes.
package workday

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// Endpoint is the integration list. Note the plural, and note it returns a
// BARE ARRAY rather than an envelope — no {count}, no {results}, no
// {workdays}. That is unusual for this API and is the reason this package
// exists at all for two endpoints.
const Endpoint = "/workdays"

// IntegrationEndpoint addresses one Workday integration.
func IntegrationEndpoint(id string) string { return Endpoint + "/" + id }

// Unprobed records the endpoints this area serves that could not be exercised,
// and why. They are not implemented rather than implemented on a guess.
//
// Each needs a tenant with a real Workday integration. When one exists, probe
// the response shape FIRST — the list endpoint returning a bare array where
// every sibling area returns an envelope is exactly the kind of thing that
// does not generalise.
var Unprobed = map[string]string{
	Endpoint + "/{id}/workers": "response shape unobserved — no Workday integration " +
		"exists on any tenant available. Probe before implementing.",
	Endpoint + "/{id}/import/{job_id}/results": "response shape unobserved, same reason.",
}

// objectIDPattern matches JumpCloud's 24-character hex object ids.
var objectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// IsObjectID reports whether s is a JumpCloud 24-hex object id.
//
// The spec names the path parameter {id} on some Workday endpoints and
// {workday_id} on others. Whether that is a real distinction could NOT be
// established: with no integration configured there is nothing to address. The
// neighbouring Google EMM area advertised the same split and it turned out to
// be fiction, so the working assumption is that these are one id — but that is
// an assumption, and it is written down here as one rather than buried in a
// resolver.
func IsObjectID(s string) bool { return objectIDPattern.MatchString(s) }

// ErrNotObjectID explains a malformed id before it reaches the API.
//
// The API's own message is good ("Bad Request: invalid object id"), so this
// exists only to save the round trip and to name the field, not because the
// server is unhelpful.
func ErrNotObjectID(s string) error {
	return fmt.Errorf("%q is not a JumpCloud object id — Workday integrations are addressed by "+
		"a 24-character hex id, which `jc workday list` reports", s)
}

// ParseList reads the integration list.
//
// VERIFIED: this endpoint returns a bare JSON array. An empty org returns [],
// not an envelope with a zero count.
//
// A record that will not decode is an error, never a skip — the same rule as
// every other area here. There is one extra reason for it in this case: the
// record shape has never been observed with data in it, so the first person to
// run this against a real integration should get a loud, specific failure if
// the shape is not what the caller expects, rather than a silently shorter
// list that looks like a working empty tenant.
func ParseList(raw json.RawMessage) ([]json.RawMessage, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("the Workday integration list is not a JSON array: %w — it was a "+
			"bare array when probed, so a change here means the envelope was added later", err)
	}
	return rows, nil
}
