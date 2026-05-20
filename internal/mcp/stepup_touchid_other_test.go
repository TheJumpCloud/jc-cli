//go:build !darwin

package mcp

import "testing"

func TestTouchIDAvailable_NonDarwinAlwaysFalse(t *testing.T) {
	if touchIDAvailable() {
		t.Errorf("touchIDAvailable() = true on non-darwin, want false")
	}
}

func TestNewTouchIDStepUpIfSupported_NonDarwinReturnsNil(t *testing.T) {
	if got := newTouchIDStepUpIfSupported(); got != nil {
		t.Errorf("newTouchIDStepUpIfSupported() = %T on non-darwin, want nil", got)
	}
}

func TestNewStepUp_AutoFallsBackToTTYOnNonDarwin(t *testing.T) {
	a := newStepUp(true, "key12345678", "auto")
	if _, ok := a.(*ttyStepUp); !ok {
		t.Errorf("newStepUp(true, _, \"auto\") = %T on non-darwin, want *ttyStepUp", a)
	}
}

func TestNewStepUp_TouchIDPrefFallsBackToTTYOnNonDarwin(t *testing.T) {
	// Operator pinned touchid but the platform can't supply it. Falling
	// back to TTY (rather than noop) preserves the "challenge before
	// destructive" guarantee for operators who run in HTTP transport
	// with a controlling terminal.
	a := newStepUp(true, "key12345678", "touchid")
	if _, ok := a.(*ttyStepUp); !ok {
		t.Errorf("newStepUp(true, _, \"touchid\") = %T on non-darwin, want *ttyStepUp", a)
	}
}

func TestStepUpReachesOperatorOnStdio_NonDarwinAlwaysFalse(t *testing.T) {
	// Without a biometric channel, every stdio configuration leaves the
	// operator unreachable. The warning must always fire.
	for _, pref := range []string{"", "auto", "tty", "touchid"} {
		if got := StepUpReachesOperatorOnStdio(pref); got {
			t.Errorf("StepUpReachesOperatorOnStdio(%q) = true on non-darwin, want false", pref)
		}
	}
}
