// Package accessreq holds what is known about looking up JumpCloud access
// requests (temporary elevated device privileges), shared by the CLI and the
// MCP tools.
//
// THERE IS NO LIST. The spec has no GET /accessrequests, and the server
// answers it, without all three of requesterId, applicationId and state, with
// 400 "error validating GetAccessRequestByRequesterIdApplicationIdState"
// (probed 2026-11-19). With all three it answers 200 {"results": [...]}. So the
// one read besides get-by-id is a lookup: does this user have a request for
// this device in this state?
//
// UNVERIFIED, AND SAY SO. The server validates none of the three: ids that
// match nothing and a state of "nonsense" all answer 200 with an empty result.
// The probe tenant had no access requests, so it is not yet known which state
// values the API uses, nor that applicationId takes the device id (the create
// body calls it resourceId). Every lookup therefore resolves the user and the
// device first, so at least those are real, and an empty result is reported
// with that caveat. Verifying it needs a request created against a test VM.
package accessreq

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Endpoint is the access-requests collection.
const Endpoint = "/accessrequests"

// CandidateStates are state values seen in JumpCloud documentation and in
// this CLI's own earlier help text. None has been observed on a live request.
var CandidateStates = []string{"pending", "granted", "revoked", "expired", "denied"}

// Unverified is the caveat shown with a lookup that found nothing.
const Unverified = "the access-requests API accepts any state and any ids without checking them, " +
	"and the state values have not been verified against a live request — an empty result " +
	"may mean a state value the API does not use (candidates: " +
	"pending, granted, revoked, expired, denied)"

// NoList explains why there is no list.
const NoList = "JumpCloud has no way to list access requests: GET /accessrequests answers 400 " +
	"unless it is given a requester, a device and a state. Use `jc access-requests find " +
	"--user <user> --device <device> --state <state>`, or `get <access-id>`"

// FindPath builds the lookup for one user, device and state. The ids must
// already be resolved; the server would accept anything.
func FindPath(userID, deviceID, state string) (string, error) {
	state = strings.TrimSpace(state)
	if userID == "" || deviceID == "" || state == "" {
		return "", fmt.Errorf("a lookup needs a user, a device and a state")
	}
	v := url.Values{}
	v.Set("requesterId", userID)
	v.Set("applicationId", deviceID)
	v.Set("state", state)
	return Endpoint + "?" + v.Encode(), nil
}

// ParseFind reads the {"results": [...]} lookup envelope strictly.
func ParseFind(raw json.RawMessage) ([]json.RawMessage, error) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("the access-requests lookup response is not an object: %w", err)
	}
	r, ok := env["results"]
	if !ok {
		return nil, fmt.Errorf("the access-requests lookup response has no results field")
	}
	var rows []json.RawMessage
	if string(r) == "null" {
		return rows, nil
	}
	if err := json.Unmarshal(r, &rows); err != nil {
		return nil, fmt.Errorf("the access-requests lookup results are not an array: %w", err)
	}
	return rows, nil
}
