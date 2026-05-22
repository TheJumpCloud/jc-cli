//go:build darwin

package mcp

import "testing"

// touchIDAvailable() is hardware-dependent (Mac mini, Mac Pro, and CI
// runners lack the biometric stack), so the darwin factory tests branch
// on it rather than assuming Touch ID is present.

func TestNewTouchIDStepUpIfSupported_TracksHardware(t *testing.T) {
	got := newTouchIDStepUpIfSupported()
	if touchIDAvailable() {
		if got == nil {
			t.Errorf("newTouchIDStepUpIfSupported() = nil on darwin with biometrics, want non-nil")
		}
	} else {
		if got != nil {
			t.Errorf("newTouchIDStepUpIfSupported() = %T on darwin without biometrics, want nil so the factory can fall back to TTY", got)
		}
	}
}

func TestNewStepUp_AutoOnDarwin(t *testing.T) {
	a := mustStepUp(t, stepUpConfig{Required: true, APIKey: "key12345678", AuthenticatorPref: "auto"})
	assertPlatformAuthenticator(t, a, "auto")
}

func TestNewStepUp_EmptyPrefOnDarwin(t *testing.T) {
	// Empty pref must follow the same "strongest available" rule as
	// "auto" so a fresh install without explicit config still gets the
	// best channel the host can offer.
	a := mustStepUp(t, stepUpConfig{Required: true, APIKey: "key12345678"})
	assertPlatformAuthenticator(t, a, "")
}

func TestNewStepUp_TouchIDPrefOnDarwin(t *testing.T) {
	// Pinned "touchid" should resolve to *touchIDStepUp when biometrics
	// are usable, and fall back to *ttyStepUp when they aren't, so the
	// chokepoint still has *some* challenge to present.
	a := mustStepUp(t, stepUpConfig{Required: true, APIKey: "key12345678", AuthenticatorPref: "touchid"})
	assertPlatformAuthenticator(t, a, "touchid")
}

func assertPlatformAuthenticator(t *testing.T, a stepUpAuthenticator, pref string) {
	t.Helper()
	if touchIDAvailable() {
		if _, ok := a.(*touchIDStepUp); !ok {
			t.Errorf("with biometrics: newStepUp(_, _, %q) = %T, want *touchIDStepUp", pref, a)
		}
		return
	}
	if _, ok := a.(*ttyStepUp); !ok {
		t.Errorf("without biometrics: newStepUp(_, _, %q) = %T, want *ttyStepUp (factory fallback)", pref, a)
	}
}

func TestStepUpReachesOperatorOnStdio_DarwinTracksHardware(t *testing.T) {
	// On darwin, the answer must match runtime biometric availability,
	// not just the platform tag. Otherwise we'd suppress the "TTY can't
	// reach the operator" warning on a Mac Pro that's going to fail
	// every destructive op closed.
	cases := []string{"", "auto", "touchid"}
	for _, pref := range cases {
		got := StepUpReachesOperatorOnStdio(pref)
		want := touchIDAvailable()
		if got != want {
			t.Errorf("StepUpReachesOperatorOnStdio(%q) = %v on darwin (biometrics=%v), want %v",
				pref, got, want, want)
		}
	}
}

// On darwin, the auto / touchid / empty paths only need the API key
// when biometric hardware is missing (the runtime fallback to TTY).
// Stays in lockstep with newStepUp's resolution logic.
func TestStepUpNeedsAPIKey_DarwinTracksHardware(t *testing.T) {
	for _, pref := range []string{"", "auto", "touchid"} {
		got := StepUpNeedsAPIKey(pref)
		want := !touchIDAvailable()
		if got != want {
			t.Errorf("StepUpNeedsAPIKey(%q) = %v on darwin (biometrics=%v), want %v",
				pref, got, touchIDAvailable(), want)
		}
	}
}

func TestStepUpReachesOperatorOnStdio_TTYPrefAlwaysFalse(t *testing.T) {
	// Explicit "tty" must report unreachable on stdio everywhere — the
	// operator has chosen the channel that depends on a controlling
	// terminal, so the warning must still fire on stdio.
	if got := StepUpReachesOperatorOnStdio("tty"); got {
		t.Errorf("StepUpReachesOperatorOnStdio(\"tty\") = true on darwin, want false")
	}
}
