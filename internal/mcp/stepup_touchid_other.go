//go:build !darwin

package mcp

// newTouchIDStepUpIfSupported returns nil on non-darwin platforms. The
// newStepUp factory checks for nil and falls back to ttyStepUp. Touch ID
// support on Linux/Windows is not implemented here; if biometric or
// platform-keyring step-up is desired there, add a sibling
// stepup_<platform>.go that returns a non-nil authenticator.
func newTouchIDStepUpIfSupported() stepUpAuthenticator {
	return nil
}
