// Package aiready handles an SSO application's aiReady flag: the admin opt-in
// that shows the app as "AI Ready" in the User Portal. It is informational
// only and grants no access.
//
// Live contract (probed 2026-11-17, test org, throwaway bookmark apps):
//
//   - Every app in GET /api/applications carries aiReady (bool, false by default).
//   - POST /api/applications with aiReady:true fails with HTTP 400 "AI Ready can
//     only be set after the application is created and has an MCP connection".
//   - PUT /api/applications/{id} (full body, see internal/appupdate) with
//     aiReady:true on an app without an MCP connection fails with HTTP 400
//     "AI Ready requires at least one configured MCP connection"; nothing in
//     that request is applied. aiReady:false is accepted.
//   - A PUT without name and ssoUrl is a silent no-op for every field, aiReady
//     included, which is why the result is checked as well (CheckApplied).
//   - The server filter aiReady:eq:false matched 3 of 30 apps whose aiReady is
//     false (apparently only those where the flag was stored explicitly), so
//     filtering happens client-side.
//   - The MCP connection is an AI Gateway server assigned to the app; no public
//     API creates one (Admin Portal: Access > AI Gateway > Servers).
package aiready

import (
	"encoding/json"
	"fmt"
)

// Field is the JSON field name on an application.
const Field = "aiReady"

type flag struct {
	AIReady *bool `json:"aiReady"`
}

// CheckApplied compares the app returned by the PUT with the requested value.
func CheckApplied(app json.RawMessage, want bool) error {
	var f flag
	if err := json.Unmarshal(app, &f); err != nil {
		return fmt.Errorf("checking aiReady in the updated app: %w", err)
	}
	if f.AIReady == nil {
		return fmt.Errorf("the updated app has no aiReady field, so whether aiReady=%t took effect is unknown", want)
	}
	if *f.AIReady != want {
		if want {
			return fmt.Errorf("JumpCloud accepted the update but kept aiReady=false: an app can be marked AI Ready only when it has an MCP connection (assign an AI Gateway server to it under Access > AI Gateway > Servers first)")
		}
		return fmt.Errorf("JumpCloud accepted the update but kept aiReady=true although false was requested")
	}
	return nil
}

// Filter keeps the apps whose aiReady equals want. An app without the field
// counts as false.
func Filter(apps []json.RawMessage, want bool) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	for i, raw := range apps {
		var f flag
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("app %d: %w", i, err)
		}
		if (f.AIReady != nil && *f.AIReady) == want {
			out = append(out, raw)
		}
	}
	return out, nil
}
