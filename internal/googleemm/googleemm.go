// Package googleemm is the shared contract for JumpCloud's Google EMM
// (Android Enterprise) integration, used by the CLI and the MCP tools so the
// two cannot drift.
//
// Everything here was established by probing a live tenant with a configured
// enterprise, because the OpenAPI spec is wrong about this area in the way
// that matters most: it describes two id systems where there is one.
package googleemm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Endpoints. Note the HYPHEN: /googleemm as one word is a 404.
const (
	Endpoint            = "/google-emm"
	EnterprisesEndpoint = Endpoint + "/enterprises"
)

// EnterpriseSubresource endpoints. All of these take the 24-hex objectId —
// see the note on IsObjectID about the spec's imaginary second id.
func EnterpriseEndpoint(objectID string) string { return EnterprisesEndpoint + "/" + objectID }
func EnterpriseDevicesEndpoint(objectID string) string {
	return EnterpriseEndpoint(objectID) + "/devices"
}
func EnterpriseTokensEndpoint(objectID string) string {
	return EnterpriseEndpoint(objectID) + "/enrollment-tokens"
}
func EnterpriseTokenEndpoint(objectID, tokenID string) string {
	return EnterpriseTokensEndpoint(objectID) + "/" + tokenID
}
func ConnectionStatusEndpoint(objectID string) string {
	return EnterpriseEndpoint(objectID) + "/connection-status"
}

// DeviceEndpoint and friends address one enrolled device. Devices are
// addressed at the top level even though they are only LISTED per enterprise.
func DeviceEndpoint(deviceID string) string { return Endpoint + "/devices/" + deviceID }
func DevicePolicyResultsEndpoint(deviceID string) string {
	return DeviceEndpoint(deviceID) + "/policy_results"
}

// NotImplemented records endpoints this area advertises, or that a reader
// would reasonably expect, and does not serve. Each was probed against a
// tenant with a real enterprise.
var NotImplemented = map[string]string{
	"GET " + EnterprisesEndpoint + "/{objectId}": "HTTP 404 — there is no single-enterprise GET. " +
		"List and filter; `jc google-emm enterprises get` does exactly that.",
	"GET " + Endpoint + "/devices": "HTTP 404 — no org-wide device list. Devices are reachable " +
		"only under an enterprise, which is why every device command needs one.",
}

// objectIDPattern matches JumpCloud's 24-character hex object ids.
var objectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// IsObjectID reports whether s is a JumpCloud 24-hex object id.
//
// This matters more than it looks. The spec describes two path parameters,
// {enterpriseId} and {enterpriseObjectId}, as though the area used two id
// systems. It does not. Every endpoint takes the objectId, including
// connection-status — which the spec calls {enterpriseId} and which returns
// "enterpriseId": "<the objectId>" in its own response body. Google's own id
// (the LC01m7doq1 in "enterprises/LC01m7doq1") returns HTTP 400 everywhere it
// was tried.
//
// Building to the spec would have produced a resolver for an id system that
// does not exist.
func IsObjectID(s string) bool { return objectIDPattern.MatchString(s) }

// Enterprise is one Android Enterprise binding.
type Enterprise struct {
	ObjectID string `json:"objectId"`
	// Name is Google's resource name, "enterprises/<googleId>". It is NOT
	// accepted by any endpoint here; it is display and cross-reference only.
	Name                  string `json:"name"`
	DisplayName           string `json:"displayName"`
	ContactEmail          string `json:"contactEmail"`
	EnterpriseType        string `json:"enterpriseType"`
	DeviceGroupID         string `json:"deviceGroupId"`
	OrganizationObjectID  string `json:"organizationObjectId"`
	AllowDeviceEnrollment bool   `json:"allowDeviceEnrollment"`
	CreatedAt             string `json:"createdAt"`
}

// GoogleID returns the bare Google enterprise id from Name, or "".
func (e Enterprise) GoogleID() string {
	return strings.TrimPrefix(e.Name, "enterprises/")
}

// ─── parsing ───────────────────────────────────────────────────────
//
// Three list endpoints, three different envelopes, in one area:
//
//	enterprises       {count, enterprises}
//	devices           {count, devices}
//	enrollment-tokens {results, totalCount}   <- different key AND count field
//
// Callers should not have to know that, which is the whole point of this
// package.

// Fetcher reads one endpoint. Both surfaces already hold a client.
type Fetcher func(ctx context.Context, endpoint string) (json.RawMessage, error)

func parseList(raw json.RawMessage, arrayKey, countKey, what string) ([]json.RawMessage, int, error) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, 0, fmt.Errorf("%s response is not an object: %w", what, err)
	}
	var rows []json.RawMessage
	if arr, ok := env[arrayKey]; ok {
		if err := json.Unmarshal(arr, &rows); err != nil {
			return nil, 0, fmt.Errorf("%s %q is not an array: %w", what, arrayKey, err)
		}
	}
	total := len(rows)
	if c, ok := env[countKey]; ok {
		var n int
		if err := json.Unmarshal(c, &n); err != nil {
			return nil, 0, fmt.Errorf("%s %q is not a number: %w", what, countKey, err)
		}
		total = n
	}
	return rows, total, nil
}

// ParseDevices reads the {count, devices} envelope.
func ParseDevices(raw json.RawMessage) ([]json.RawMessage, int, error) {
	return parseList(raw, "devices", "count", "Google EMM devices")
}

// ParseTokens reads the {results, totalCount} envelope — note both keys
// differ from the other two lists in this area.
func ParseTokens(raw json.RawMessage) ([]json.RawMessage, int, error) {
	return parseList(raw, "results", "totalCount", "Google EMM enrollment tokens")
}

// ParseEnterprises reads the {count, enterprises} envelope into typed records.
//
// A record that will not decode is an error, never a skip. Skipping one turns
// "I could not read this enterprise" into "you have fewer enterprises than you
// do", and on this area that compounds: the device list of an enterprise jc
// failed to see would then be unreachable for a reason nobody could diagnose.
func ParseEnterprises(raw json.RawMessage) ([]Enterprise, error) {
	rows, _, err := parseList(raw, "enterprises", "count", "Google EMM enterprises")
	if err != nil {
		return nil, err
	}
	out := make([]Enterprise, 0, len(rows))
	for i, r := range rows {
		var e Enterprise
		if err := json.Unmarshal(r, &e); err != nil {
			return nil, fmt.Errorf("Google EMM enterprise %d of %d did not decode: %w — the "+
				"record shape has changed and jc would otherwise report fewer enterprises "+
				"than exist", i+1, len(rows), err)
		}
		out = append(out, e)
	}
	return out, nil
}

// ─── resolution ────────────────────────────────────────────────────

// ErrNoMatch means nothing matched. A normal outcome, not a fault.
var ErrNoMatch = errors.New("no match")

// AmbiguousError means an identifier matched more than one enterprise.
type AmbiguousError struct {
	Identifier string
	Candidates []Enterprise
}

func (e *AmbiguousError) Error() string {
	lines := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		lines[i] = "  " + c.DisplayName + " (" + c.ObjectID + ")"
	}
	return fmt.Sprintf("enterprise %q is ambiguous — it matches %d records:\n%s\n"+
		"Pass one of the object ids above instead of the name.",
		e.Identifier, len(e.Candidates), strings.Join(lines, "\n"))
}

// FindEnterprise matches by object id, display name, Google resource name, or
// bare Google id. Returns ErrNoMatch or an *AmbiguousError.
func FindEnterprise(list []Enterprise, identifier string) (Enterprise, error) {
	want := strings.ToLower(identifier)
	if IsObjectID(identifier) {
		for _, e := range list {
			if strings.EqualFold(e.ObjectID, identifier) {
				return e, nil
			}
		}
		return Enterprise{}, ErrNoMatch
	}
	var found []Enterprise
	for _, e := range list {
		if strings.ToLower(e.DisplayName) == want ||
			strings.ToLower(e.Name) == want ||
			strings.ToLower(e.GoogleID()) == want {
			found = append(found, e)
		}
	}
	switch len(found) {
	case 0:
		return Enterprise{}, ErrNoMatch
	case 1:
		return found[0], nil
	default:
		return Enterprise{}, &AmbiguousError{Identifier: identifier, Candidates: found}
	}
}

// ResolveEnterprise turns what an operator typed into a CONFIRMED enterprise.
//
// The confirmation is the point, not a side effect. GET
// /enterprises/{id}/devices returns {"count":0,"devices":[]} for ANY
// well-formed 24-hex id, including one no enterprise has ever had — while its
// two siblings under the same path prefix, enrollment-tokens and
// connection-status, both return 404 for the same id. So on the one endpoint
// where a wrong answer is expensive, "this enterprise has no devices" and
// "there is no such enterprise" are the same response.
//
// Every device call therefore goes through here first. A count is only
// reported for an enterprise that was seen in the list.
func ResolveEnterprise(ctx context.Context, get Fetcher, identifier string) (Enterprise, error) {
	raw, err := get(ctx, EnterprisesEndpoint)
	if err != nil {
		return Enterprise{}, err
	}
	list, err := ParseEnterprises(raw)
	if err != nil {
		return Enterprise{}, err
	}
	e, err := FindEnterprise(list, identifier)
	if errors.Is(err, ErrNoMatch) {
		return Enterprise{}, fmt.Errorf("no Google EMM enterprise %q (%s). Note that Google's "+
			"own id is not accepted by the API — jc matches it here and sends the object id",
			identifier, enterpriseCount(len(list)))
	}
	if err != nil {
		return Enterprise{}, err
	}
	return e, nil
}

func enterpriseCount(n int) string {
	switch n {
	case 0:
		return "none are configured"
	case 1:
		return "1 is configured"
	default:
		return fmt.Sprintf("%d are configured", n)
	}
}
