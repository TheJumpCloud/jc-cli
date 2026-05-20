//go:build darwin

package mcp

import "testing"

func TestNewTouchIDStepUpIfSupported_DarwinReturnsNonNil(t *testing.T) {
	if got := newTouchIDStepUpIfSupported(); got == nil {
		t.Errorf("newTouchIDStepUpIfSupported() = nil on darwin, want non-nil")
	}
}

func TestNewStepUp_AutoSelectsTouchIDOnDarwin(t *testing.T) {
	a := newStepUp(true, "anything", "auto")
	if _, ok := a.(*touchIDStepUp); !ok {
		t.Errorf("newStepUp(true, _, \"auto\") = %T on darwin, want *touchIDStepUp", a)
	}
}

func TestNewStepUp_EmptyPrefSelectsTouchIDOnDarwin(t *testing.T) {
	// Empty pref must follow the same "strongest available" rule as
	// "auto" so a fresh install without explicit config still gets the
	// OS biometric prompt by default.
	a := newStepUp(true, "anything", "")
	if _, ok := a.(*touchIDStepUp); !ok {
		t.Errorf("newStepUp(true, _, \"\") = %T on darwin, want *touchIDStepUp", a)
	}
}

func TestNewStepUp_TouchIDPrefSelectsTouchIDOnDarwin(t *testing.T) {
	a := newStepUp(true, "anything", "touchid")
	if _, ok := a.(*touchIDStepUp); !ok {
		t.Errorf("newStepUp(true, _, \"touchid\") = %T on darwin, want *touchIDStepUp", a)
	}
}
