// Package gcep reads the Google Chrome Enterprise Premium (device trust)
// integration: GET /gcep/configuration and GET
// /gcep/systems/{id}/device-state on the v2 API. On the probe tenant
// (2026-11-19) both answered 404 because the integration is not configured,
// so the response shapes are unobserved and returned as-is.
//
// The writes (PUT and DELETE /gcep/configuration) are not exposed: DELETE
// disconnects the org from Google, and the PUT body is undocumented and
// cannot be probed without a Google tenant to connect.
package gcep

import "fmt"

const ConfigurationPath = "/gcep/configuration"

// DeviceStatePath is one device's trust state from the partner sync mirror.
func DeviceStatePath(systemID string) string { return "/gcep/systems/" + systemID + "/device-state" }

// NotConfigured explains a 404 on the configuration.
func NotConfigured() error {
	return fmt.Errorf("no GCEP configuration: the Google Chrome Enterprise Premium integration " +
		"is not configured on this organization")
}

// NoDeviceState explains a 404 on a device's state.
func NoDeviceState(device string) error {
	return fmt.Errorf("no GCEP device state for %s: the integration is not configured, or it holds "+
		"no state for this device", device)
}
