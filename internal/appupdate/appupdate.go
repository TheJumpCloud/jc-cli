// Package appupdate builds and checks PUT /api/applications/{id} bodies.
//
// Live contract (probed 2026-11-17, test org, throwaway bookmark app):
//
//   - A PUT body without both name and ssoUrl is answered with 200 and the
//     unchanged app: nothing is applied, and no error is reported. This is what
//     `jc apps update` used to send (only the changed fields), so --name and
//     --config silently did nothing.
//   - With name and ssoUrl present the PUT merges: fields not in the body keep
//     their values. Sending the whole app as returned by GET, with the changes
//     overlaid, was verified to apply.
//
// So an update reads the app, overlays the changes, PUTs the result, and then
// checks the returned app for each changed scalar field.
package appupdate

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// Merge overlays changes on the current app (as returned by GET) and returns
// the full body to PUT.
func Merge(current json.RawMessage, changes map[string]any) (map[string]any, error) {
	var body map[string]any
	if err := json.Unmarshal(current, &body); err != nil {
		return nil, fmt.Errorf("reading the current application: %w", err)
	}
	if body == nil {
		return nil, fmt.Errorf("reading the current application: empty response")
	}
	for k, v := range changes {
		body[k] = v
	}
	return body, nil
}

// Unapplied lists the changed top-level scalar fields (strings, numbers,
// booleans) whose value in the returned app differs from the requested one.
// Objects and arrays are skipped: the server may normalise them. skip names
// fields the caller checks itself.
func Unapplied(result json.RawMessage, changes map[string]any, skip ...string) ([]string, error) {
	var got map[string]any
	if err := json.Unmarshal(result, &got); err != nil {
		return nil, fmt.Errorf("checking the updated application: %w", err)
	}
	skipped := map[string]bool{}
	for _, s := range skip {
		skipped[s] = true
	}
	var missing []string
	for k, want := range changes {
		if skipped[k] || !isScalar(want) {
			continue
		}
		// Round-trip the requested value so numbers compare as float64.
		b, _ := json.Marshal(want)
		var norm any
		_ = json.Unmarshal(b, &norm)
		if !reflect.DeepEqual(got[k], norm) {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

func isScalar(v any) bool {
	switch v.(type) {
	case string, bool, float64, float32, int, int64, json.Number:
		return true
	}
	return false
}

// ErrUnapplied reports fields JumpCloud answered 200 for but did not change.
func ErrUnapplied(fields []string) error {
	return fmt.Errorf("JumpCloud accepted the update but did not change: %v", fields)
}
