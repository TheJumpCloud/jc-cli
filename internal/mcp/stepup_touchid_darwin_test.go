//go:build darwin && cgo

package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

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

// ─── authorize: the wait, not the prompt ───────────────────────────

// The defect this guards: authorize ignored its context and blocked on the
// OS modal, so an unanswered Touch ID prompt looked to the MCP client like a
// tool that hangs. A verification pass spent a whole session concluding the
// workflow write path was broken — reads worked, plan mode returned
// instantly, and only execute=true hung, which is indistinguishable from a
// broken write endpoint.
//
// The CGO call cannot be driven from a test, so authorizeWith takes the
// prompt as a parameter and these exercise the half that does not touch C —
// which is the half that was wrong.
func TestTouchIDAuthorize_UnansweredPromptReturnsRatherThanHanging(t *testing.T) {
	s := &touchIDStepUp{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	blockedUntilTestEnds := make(chan struct{})
	defer close(blockedUntilTestEnds)

	start := time.Now()
	err := s.authorizeWith(ctx, "workflows_create", "", func(string) error {
		<-blockedUntilTestEnds // a modal nobody answers
		return nil
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("an unanswered prompt returned success")
	}
	if elapsed > time.Second {
		t.Errorf("waited %v for a prompt nobody answered; the context deadline was 50ms", elapsed)
	}
	// The message has to name Touch ID, or the next person debugs a hang
	// instead of walking over to the Mac and approving it.
	for _, want := range []string{"Touch ID", "workflows_create", "not answered"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q, got: %v", want, err)
		}
	}
}

// The prompt must still be waited for when it is answered — a timeout that
// fired on a normal approval would block every destructive call instead.
func TestTouchIDAuthorize_AnsweredPromptIsHonoured(t *testing.T) {
	s := &touchIDStepUp{}
	for name, want := range map[string]error{
		"approved":    nil,
		"denied":      errStepUpDenied,
		"unavailable": errStepUpUnavailable,
	} {
		t.Run(name, func(t *testing.T) {
			got := s.authorizeWith(context.Background(), "users_delete", "ada",
				func(string) error { return want })
			if !errors.Is(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// The reason string is what the operator reads on the modal, so it has to
// say which tool and which target are being approved.
func TestTouchIDAuthorize_ReasonNamesToolAndTarget(t *testing.T) {
	s := &touchIDStepUp{}
	var seen string
	_ = s.authorizeWith(context.Background(), "users_delete", "ada",
		func(reason string) error { seen = reason; return nil })
	if !strings.Contains(seen, "users_delete") || !strings.Contains(seen, "ada") {
		t.Errorf("reason %q should name both the tool and the target", seen)
	}

	_ = s.authorizeWith(context.Background(), "users_delete", "",
		func(reason string) error { seen = reason; return nil })
	if strings.Contains(seen, " on ") {
		t.Errorf("with no target the reason should not dangle an \" on \" clause: %q", seen)
	}
}

// A caller that supplies its own deadline keeps it; the cap exists only for
// callers that supply none.
func TestTouchIDAuthorize_CallerDeadlineWins(t *testing.T) {
	s := &touchIDStepUp{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	release := make(chan struct{})
	defer close(release)

	start := time.Now()
	_ = s.authorizeWith(ctx, "workflows_create", "", func(string) error { <-release; return nil })
	if elapsed := time.Since(start); elapsed > touchIDPromptCap/2 {
		t.Errorf("waited %v — the caller's 30ms deadline was replaced by the cap", elapsed)
	}
}
