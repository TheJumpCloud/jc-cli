// Package appupdate builds and checks /api/applications bodies: the PUT of an
// update and the POST of a create.
//
// Create contract (probed 2026-11-17, test org): "name" is the catalog
// connector key ("bookmark", "slack", ...), not a title. Any other value is
// answered 400 "Could not find the service provider". The title users see is
// "displayLabel". The SSO type lives in sso.type and follows from the
// connector: a top-level "ssoType" is ignored (name=bookmark with
// ssoType=saml created a bookmark app).
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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/klaassen-consulting/jc/internal/api"
)

// ErrNameImmutable explains why an update may not change "name". Live: a new
// connector key is answered 400 "must include config field", and any other
// value is accepted with 200 and silently kept unchanged.
var ErrNameImmutable = errors.New("an application's name is its catalog connector key and cannot be changed; to change the title users see, set the display label instead")

// IsUnknownConnector reports whether err is JumpCloud rejecting a create
// because the name is not a catalog connector key.
func IsUnknownConnector(err error) bool {
	var apiErr *api.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest &&
		strings.Contains(apiErr.Message, "Could not find the service provider")
}

// UnknownConnectorHint explains IsUnknownConnector to the caller.
func UnknownConnectorHint(name, labelArg string) string {
	return fmt.Sprintf("the application name must be a catalog connector key such as \"bookmark\", not a title (got %q); put the title in %s, and see the app templates catalog for connector keys", name, labelArg)
}

// Client is the part of the v1 API client an update needs.
type Client interface {
	Get(ctx context.Context, endpoint string) (json.RawMessage, error)
	Update(ctx context.Context, endpoint string, body any) (json.RawMessage, error)
}

// Apply updates application id with changes the only way JumpCloud honours:
// read the app, overlay the changes, PUT the whole body, then check that each
// changed scalar field took effect. It returns the updated app. Fields named
// in skip are not checked (the caller checks them itself, with a more
// specific message).
func Apply(ctx context.Context, c Client, id string, changes map[string]any, skip ...string) (json.RawMessage, error) {
	path := "/applications/" + id
	current, err := c.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	full, err := Merge(current, changes)
	if err != nil {
		return nil, err
	}
	result, err := c.Update(ctx, path, full)
	if err != nil {
		return nil, err
	}
	missing, err := Unapplied(result, changes, skip...)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		return nil, ErrUnapplied(missing)
	}
	return result, nil
}

// Merge overlays changes on the current app (as returned by GET) and returns
// the full body to PUT.
//
// The SSO configuration ("config") is merged one level deep: keys in
// changes["config"] replace the same keys in the current config, and every
// other key is kept. Replacing the object wholesale would silently drop, say,
// the SAML settings a caller did not mention.
func Merge(current json.RawMessage, changes map[string]any) (map[string]any, error) {
	var body map[string]any
	if err := json.Unmarshal(current, &body); err != nil {
		return nil, fmt.Errorf("reading the current application: %w", err)
	}
	if body == nil {
		return nil, fmt.Errorf("reading the current application: empty response")
	}
	for k, v := range changes {
		if k == ConfigField {
			if merged, ok := mergeObject(body[k], v); ok {
				body[k] = merged
				continue
			}
		}
		body[k] = v
	}
	return body, nil
}

// ConfigField is the application's SSO configuration object.
const ConfigField = "config"

// mergeObject overlays the keys of change on cur when both are JSON objects.
func mergeObject(cur, change any) (map[string]any, bool) {
	c, ok1 := cur.(map[string]any)
	n, ok2 := change.(map[string]any)
	if !ok1 || !ok2 {
		return nil, false
	}
	out := make(map[string]any, len(c)+len(n))
	for k, v := range c {
		out[k] = v
	}
	for k, v := range n {
		out[k] = v
	}
	return out, true
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
