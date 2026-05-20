//go:build !darwin

package mcp

// touchIDAvailable is the platform stub matching the darwin probe. Non-
// darwin builds have no biometric stack to consult, so it's always false.
func touchIDAvailable() bool {
	return false
}

// newTouchIDStepUpIfSupported returns nil on non-darwin platforms. The
// newStepUp factory checks for nil and falls back to ttyStepUp. Touch ID
// support on Linux/Windows is not implemented here; if biometric or
// platform-keyring step-up is desired there, add a sibling
// stepup_<platform>.go that returns a non-nil authenticator.
func newTouchIDStepUpIfSupported() stepUpAuthenticator {
	return nil
}
