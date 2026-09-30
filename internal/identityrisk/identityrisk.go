// Package identityrisk is the shared contract for JumpCloud's Identity Risk
// area, used by the CLI and the MCP tools so the two cannot drift.
//
// Everything here was established by probing a live tenant on 2026-09-25 that
// had real detections in it. Where the OpenAPI spec and the server disagree,
// the server wins and the disagreement is written down — there are three of
// them, and one is irreversible.
package identityrisk

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Endpoints. Note the tree is /identityrisk, one word, while the resources
// underneath it are hyphenated.
const (
	Endpoint                = "/identityrisk"
	EventsEndpoint          = Endpoint + "/events"
	IdentitiesEndpoint      = Endpoint + "/identities"
	StatsEndpoint           = Endpoint + "/stats"
	LoginTypesEndpoint      = Endpoint + "/login-types"
	GeolocationsEndpoint    = Endpoint + "/access-geolocations"
	FactorsTimelineEndpoint = Endpoint + "/factors-timeline"
)

// EventEndpoint and friends address one record.
func EventEndpoint(id string) string        { return EventsEndpoint + "/" + id }
func EventResolveEndpoint(id string) string { return EventEndpoint(id) + "/resolve" }
func IdentityEndpoint(id string) string     { return IdentitiesEndpoint + "/" + id }

// IdentityTrendEndpoint compares an identity's detections against the previous
// equivalent period.
func IdentityTrendEndpoint(id string) string { return IdentityEndpoint(id) + "/trend" }

// windowRequired lists the endpoints that reject a call with no time window.
//
// The split is NOT documented and NOT obvious: the two collection reads that
// look most like they would page over a range — /events and /identities/{id} —
// are the two that do not need one, while every aggregate does. Probed
// individually; a call without a window returns
// 400 {"message":"invalid request: start_time and end_time are required"}.
var windowRequired = map[string]bool{
	StatsEndpoint:           true,
	IdentitiesEndpoint:      true,
	LoginTypesEndpoint:      true,
	GeolocationsEndpoint:    true,
	FactorsTimelineEndpoint: true,
}

// RequiresWindow reports whether an endpoint rejects a call that carries no
// start_time and end_time. Callers use it to fail locally with a message
// naming the flags rather than forwarding the server's snake_case field names.
func RequiresWindow(endpoint string) bool { return windowRequired[endpoint] }

// objectIDPattern matches JumpCloud's 24-character hex object ids. Both risk
// events and identities use them, and a malformed one is rejected by the
// server with 400 {"message":"invalid object_id"} rather than a 404 — so the
// distinction between "wrong shape" and "not here" is real and worth keeping.
var objectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// IsObjectID reports whether s is a JumpCloud 24-hex object id.
func IsObjectID(s string) bool { return objectIDPattern.MatchString(s) }

// ErrNotObjectID explains a malformed id before it reaches the API.
func ErrNotObjectID(kind, s string) error {
	return fmt.Errorf("%q is not a JumpCloud object id — a risk %s is addressed by its "+
		"24-character hex objectId, which you take from the listing", s, kind)
}

// --- Enums ----------------------------------------------------------------
//
// The wire values are protobuf-style and long: RISK_RESOLUTION_STATUS_RESOLVED
// rather than "resolved". Making an operator type those would be hostile, so
// every enum here accepts a short form and expands it, and accepts the full
// wire value unchanged for anyone scripting against the API directly.

// resolutionStatuses maps the short form an operator types to the wire value.
//
// RISK_RESOLUTION_STATUS_OPEN is in the spec's enum and is deliberately absent
// here: see ErrAlreadyResolved. RISK_RESOLUTION_STATUS_UNSPECIFIED is absent
// because the server treats it as a missing field.
var resolutionStatuses = map[string]string{
	"resolved":     "RISK_RESOLUTION_STATUS_RESOLVED",
	"dismissed":    "RISK_RESOLUTION_STATUS_DISMISSED",
	"mfa-resolved": "RISK_RESOLUTION_STATUS_MFA_RESOLVED",
}

// resolutionStates maps the short form to the wire value.
//
// RISK_RESOLUTION_STATE_UNSPECIFIED is absent on purpose. The server rejects
// it with "resolution_state is required" — it reads the zero value as an
// absent field, so offering it would be offering a guaranteed 400.
var resolutionStates = map[string]string{
	"safe":   "RISK_RESOLUTION_STATE_SAFE",
	"unsafe": "RISK_RESOLUTION_STATE_UNSAFE",
}

// riskLevels maps the short form to the wire value, for filtering.
var riskLevels = map[string]string{
	"low":      "RISK_LEVEL_LOW",
	"medium":   "RISK_LEVEL_MEDIUM",
	"high":     "RISK_LEVEL_HIGH",
	"critical": "RISK_LEVEL_CRITICAL",
}

func expand(table map[string]string, kind, v string) (string, error) {
	if v == "" {
		return "", fmt.Errorf("%s is required", kind)
	}
	if full, ok := table[strings.ToLower(v)]; ok {
		return full, nil
	}
	// Accept a full wire value unchanged, so a script built against the API
	// does not have to be rewritten to use this CLI.
	for _, full := range table {
		if strings.EqualFold(v, full) {
			return full, nil
		}
	}
	return "", fmt.Errorf("invalid %s %q: expected one of %s", kind, v, strings.Join(shortForms(table), ", "))
}

func shortForms(table map[string]string) []string {
	out := make([]string, 0, len(table))
	for k := range table {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ResolutionStatuses, ResolutionStates and RiskLevels are the short forms an
// operator may type, for help text and MCP tool schemas.
func ResolutionStatuses() []string { return shortForms(resolutionStatuses) }
func ResolutionStates() []string   { return shortForms(resolutionStates) }
func RiskLevels() []string         { return shortForms(riskLevels) }

// ExpandLevel turns "medium" into RISK_LEVEL_MEDIUM for a filter.
func ExpandLevel(v string) (string, error) { return expand(riskLevels, "level", v) }

// --- Resolving ------------------------------------------------------------

// ResolveRequest is the body of PATCH /events/{id}/resolve.
type ResolveRequest struct {
	ResolutionStatus string `json:"resolutionStatus"`
	ResolutionState  string `json:"resolutionState"`
	ResolutionNotes  string `json:"resolutionNotes,omitempty"`
}

// BuildResolve validates and expands a resolution into the request body.
//
// Both status and state are required even though the spec marks neither as
// such: the server rejects a missing or UNSPECIFIED state with
// "resolution_state is required". Failing here names the flags instead.
func BuildResolve(status, state, notes string) (ResolveRequest, error) {
	s, err := expand(resolutionStatuses, "status", status)
	if err != nil {
		return ResolveRequest{}, err
	}
	st, err := expand(resolutionStates, "state", state)
	if err != nil {
		return ResolveRequest{}, err
	}
	return ResolveRequest{ResolutionStatus: s, ResolutionState: st, ResolutionNotes: notes}, nil
}

// IrreversibleWarning is the sentence every surface must show before
// resolving, and the reason this operation is classed destructive rather than
// mutating.
//
// Established the hard way on 2026-09-25: the spec's resolutionStatus enum
// advertises RISK_RESOLUTION_STATUS_OPEN, which reads as "you can reopen a
// detection". You cannot. Once resolved, EVERY further call — to OPEN, to
// DISMISSED, with any state — returns
// 400 {"message":"risk event is already resolved","status":"FAILED_PRECONDITION"}.
// The enum advertises a transition the server refuses.
const IrreversibleWarning = "Resolving a risk detection cannot be undone: the API refuses " +
	"every later change to the same detection, including reopening it."

// ErrAlreadyResolved recognises the server's refusal so a caller can report it
// as the settled state it is rather than as a transient failure worth a retry.
func ErrAlreadyResolved(err error) bool {
	return err != nil && strings.Contains(err.Error(), "already resolved")
}

// --- Envelopes ------------------------------------------------------------
//
// Five endpoints, four envelope shapes, and the timeline is a map rather than
// a list. Each parser below reports a decode failure rather than returning an
// empty result, because absent data is not evidence of a negative: a caller
// that cannot tell "no detections" from "could not read the response" will
// report an org as clean when it is not.

func decodeEnvelope(raw json.RawMessage, v any, what string) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("decoding the %s response: %w", what, err)
	}
	return nil
}

// ParseEvents reads {riskEvents, totalCount}. totalCount is the server's count
// of matches, which is NOT len(rows) when limit or skip is in play.
func ParseEvents(raw json.RawMessage) ([]json.RawMessage, int, error) {
	var env struct {
		RiskEvents []json.RawMessage `json:"riskEvents"`
		TotalCount int               `json:"totalCount"`
	}
	if err := decodeEnvelope(raw, &env, "risk events"); err != nil {
		return nil, 0, err
	}
	return env.RiskEvents, env.TotalCount, nil
}

// ParseIdentities reads {identities, totalIdentitiesWithRisk, trendsCount}.
// The count field is named for the domain rather than the page, and is the one
// to trust.
func ParseIdentities(raw json.RawMessage) ([]json.RawMessage, int, error) {
	var env struct {
		Identities []json.RawMessage `json:"identities"`
		Total      int               `json:"totalIdentitiesWithRisk"`
	}
	if err := decodeEnvelope(raw, &env, "at-risk identities"); err != nil {
		return nil, 0, err
	}
	return env.Identities, env.Total, nil
}

// ParseLoginTypes reads {loginTypes} — no count field.
func ParseLoginTypes(raw json.RawMessage) ([]json.RawMessage, error) {
	var env struct {
		LoginTypes []json.RawMessage `json:"loginTypes"`
	}
	if err := decodeEnvelope(raw, &env, "login types"); err != nil {
		return nil, err
	}
	return env.LoginTypes, nil
}

// ParseGeolocations reads {items} — the one endpoint in this area that uses
// the generic key.
func ParseGeolocations(raw json.RawMessage) ([]json.RawMessage, error) {
	var env struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := decodeEnvelope(raw, &env, "access geolocations"); err != nil {
		return nil, err
	}
	return env.Items, nil
}

// TimelineSeries is one risk-factor type's buckets over the requested window.
type TimelineSeries struct {
	RiskFactorType string            `json:"riskFactorType"`
	Buckets        []json.RawMessage `json:"buckets"`
}

// ParseTimeline reads {timelineData: {RISK_FACTOR_TYPE_X: [...]}} — a MAP
// keyed by risk-factor type, not a list. It is flattened into a stable,
// name-sorted slice so the output is a table like every other list command
// rather than an object whose keys vary by tenant.
func ParseTimeline(raw json.RawMessage) ([]TimelineSeries, error) {
	var env struct {
		TimelineData map[string][]json.RawMessage `json:"timelineData"`
	}
	if err := decodeEnvelope(raw, &env, "factors timeline"); err != nil {
		return nil, err
	}
	out := make([]TimelineSeries, 0, len(env.TimelineData))
	for k, v := range env.TimelineData {
		out = append(out, TimelineSeries{RiskFactorType: k, Buckets: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RiskFactorType < out[j].RiskFactorType })
	return out, nil
}

// TimelineRequest is the body of POST /factors-timeline.
//
// It is a POST that reads: nothing is created and nothing changes, the body
// exists only because the filter does not fit in a query string. Surfaces
// class it read-only for that reason.
type TimelineRequest struct {
	StartTime       string   `json:"startTime"`
	EndTime         string   `json:"endTime"`
	IntervalType    string   `json:"intervalType,omitempty"`
	IntervalValue   string   `json:"intervalValue,omitempty"`
	RiskFactorTypes []string `json:"riskFactorTypes,omitempty"`
}
