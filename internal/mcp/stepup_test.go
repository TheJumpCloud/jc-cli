package mcp

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// recordingStepUp captures the (toolName, target) of every call and
// returns the configured response. Used by integration tests to verify
// the chokepoint actually invokes the authenticator on Execute: true
// destructive calls and skips it for everything else.
type recordingStepUp struct {
	calls    atomic.Int64
	lastTool string
	lastArg  string
	deny     bool
}

func (r *recordingStepUp) authorize(_ context.Context, toolName, target string) error {
	r.calls.Add(1)
	r.lastTool = toolName
	r.lastArg = target
	if r.deny {
		return errStepUpDenied
	}
	return nil
}

// stepUpFixture builds a ttyStepUp wired to in-memory I/O and with the
// TTY check skipped, so we can drive the prompt deterministically.
func stepUpFixture(apiKey, response string) (*ttyStepUp, *bytes.Buffer) {
	out := new(bytes.Buffer)
	su := &ttyStepUp{
		in:        strings.NewReader(response),
		out:       out,
		stdinFD:   -1, // skip TTY check in tests
		apiKey:    apiKey,
		expectLen: 6,
	}
	return su, out
}

func TestNoopStepUp_AlwaysAllows(t *testing.T) {
	if err := (noopStepUp{}).authorize(context.Background(), "users_delete", "alice"); err != nil {
		t.Errorf("noopStepUp.authorize returned %v, want nil", err)
	}
}

func TestNewStepUp_DisabledReturnsNoop(t *testing.T) {
	a := newStepUp(false, "anything", "")
	if _, ok := a.(noopStepUp); !ok {
		t.Errorf("newStepUp(false) = %T, want noopStepUp", a)
	}
}

func TestNewStepUp_TTYPrefForcesTTY(t *testing.T) {
	// Explicit "tty" must return ttyStepUp on every platform, even ones
	// that would otherwise pick a stronger authenticator. This is the
	// operator escape hatch when Touch ID is unwanted (CI runner, shared
	// session, headless box with a TTY).
	a := newStepUp(true, "key12345678", "tty")
	if _, ok := a.(*ttyStepUp); !ok {
		t.Errorf("newStepUp(true, _, \"tty\") = %T, want *ttyStepUp", a)
	}
}

func TestTTYStepUp_CorrectFragmentAllows(t *testing.T) {
	// Last 6 chars of "abcdef-12345678" are "345678".
	su, out := stepUpFixture("abcdef-12345678", "345678\n")

	if err := su.authorize(context.Background(), "users_delete", "alice"); err != nil {
		t.Fatalf("authorize() error: %v", err)
	}
	if !strings.Contains(out.String(), "users_delete") {
		t.Errorf("prompt missing tool name: %q", out.String())
	}
	if !strings.Contains(out.String(), "alice") {
		t.Errorf("prompt missing target: %q", out.String())
	}
}

func TestTTYStepUp_WrongFragmentDenies(t *testing.T) {
	su, _ := stepUpFixture("abcdef-12345678", "wrong1\n")

	err := su.authorize(context.Background(), "users_delete", "alice")
	if !errors.Is(err, errStepUpDenied) {
		t.Errorf("authorize() = %v, want errStepUpDenied", err)
	}
}

func TestTTYStepUp_ExplicitNoDenies(t *testing.T) {
	for _, response := range []string{"no\n", "n\n", "NO\n", "deny\n", "Deny\n"} {
		su, _ := stepUpFixture("abcdef-12345678", response)
		err := su.authorize(context.Background(), "users_delete", "alice")
		if !errors.Is(err, errStepUpDenied) {
			t.Errorf("for response %q: got %v, want errStepUpDenied", strings.TrimSpace(response), err)
		}
	}
}

func TestTTYStepUp_EmptyKeyUnavailable(t *testing.T) {
	su, _ := stepUpFixture("", "anything\n")
	err := su.authorize(context.Background(), "users_delete", "alice")
	if !errors.Is(err, errStepUpUnavailable) {
		t.Errorf("authorize() = %v, want errStepUpUnavailable", err)
	}
}

func TestTTYStepUp_TooShortKeyUnavailable(t *testing.T) {
	// 5 chars < expectLen=6
	su, _ := stepUpFixture("short", "short\n")
	err := su.authorize(context.Background(), "users_delete", "alice")
	if !errors.Is(err, errStepUpUnavailable) {
		t.Errorf("authorize() = %v, want errStepUpUnavailable", err)
	}
}

func TestTTYStepUp_PromptOmitsTargetClauseWhenEmpty(t *testing.T) {
	su, out := stepUpFixture("abcdef-12345678", "345678\n")
	if err := su.authorize(context.Background(), "policies_delete", ""); err != nil {
		t.Fatalf("authorize() error: %v", err)
	}
	if strings.Contains(out.String(), " on ") {
		t.Errorf("prompt should omit ' on ' when target is empty, got: %q", out.String())
	}
}

func TestIsExecutingDestructive(t *testing.T) {
	cases := []struct {
		name string
		args any
		want bool
	}{
		{"destructiveInput-execute-true", destructiveInput{Identifier: "alice", Execute: true}, true},
		{"destructiveInput-execute-false", destructiveInput{Identifier: "alice", Execute: false}, false},
		{"destructiveInput-pointer", &destructiveInput{Identifier: "alice", Execute: true}, true},
		{"non-destructive-listInput", listInput{}, false},
		{"plain-string", "not a struct", false},
		{"nil-pointer", (*destructiveInput)(nil), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isExecutingDestructive(c.args); got != c.want {
				t.Errorf("isExecutingDestructive(%T) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

func TestDestructiveTarget(t *testing.T) {
	cases := []struct {
		name string
		args any
		want string
	}{
		{"identifier-set", destructiveInput{Identifier: "alice", Execute: true}, "alice"},
		{"identifier-empty", destructiveInput{Identifier: "", Execute: true}, ""},
		{"membership-input", membershipInput{Group: "Engineering", Member: "alice", Execute: true}, "alice"},
		{"non-struct", "string", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := destructiveTarget(c.args); got != c.want {
				t.Errorf("destructiveTarget(%T) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

// Integration: confirm the chokepoint in addTypedTool actually consults
// the step-up authenticator on Execute: true and blocks the call when
// the authenticator denies.

func TestChokepoint_DestructiveExecuteCallsStepUp(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	rec := &recordingStepUp{}
	cs := connectToolTestServer(t, Options{stepUp: rec})

	result := callTool(t, cs, "users_delete", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		"execute":    true,
	})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", getResultText(t, result))
	}
	if got := rec.calls.Load(); got != 1 {
		t.Errorf("step-up calls = %d, want 1", got)
	}
	if rec.lastTool != "users_delete" {
		t.Errorf("step-up tool = %q, want users_delete", rec.lastTool)
	}
}

func TestChokepoint_DenialBlocksDestructiveCall(t *testing.T) {
	setupToolTest(t)

	rec := &recordingStepUp{deny: true}
	cs := connectToolTestServer(t, Options{stepUp: rec})

	result := callTool(t, cs, "users_delete", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		"execute":    true,
	})
	if !result.IsError {
		t.Fatal("expected destructive call to be blocked when step-up denies; got success")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "step-up auth required") {
		t.Errorf("error result missing step-up message: %q", text)
	}
	if got := rec.calls.Load(); got != 1 {
		t.Errorf("step-up calls = %d, want 1", got)
	}
}

func TestChokepoint_PlanModeBypassesStepUp(t *testing.T) {
	// users_delete with execute=false (plan mode) is not destructive
	// from the chokepoint's perspective — the step-up gate must NOT fire.
	setupToolTest(t)

	rec := &recordingStepUp{deny: true}
	cs := connectToolTestServer(t, Options{stepUp: rec})

	result := callTool(t, cs, "users_delete", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		// execute deliberately omitted (defaults to false)
	})
	if result.IsError {
		t.Fatalf("plan mode should not error: %s", getResultText(t, result))
	}
	if got := rec.calls.Load(); got != 0 {
		t.Errorf("step-up calls = %d, want 0 (plan mode shouldn't trigger step-up)", got)
	}
}

func TestChokepoint_NonDestructiveBypassesStepUp(t *testing.T) {
	// users_list has no Execute field — chokepoint must treat it as
	// non-destructive and skip step-up entirely.
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	rec := &recordingStepUp{deny: true}
	cs := connectToolTestServer(t, Options{stepUp: rec})

	result := callTool(t, cs, "users_list", map[string]any{})
	if result.IsError {
		t.Fatalf("users_list should not error: %s", getResultText(t, result))
	}
	if got := rec.calls.Load(); got != 0 {
		t.Errorf("step-up calls = %d, want 0 (non-destructive tool)", got)
	}
}
