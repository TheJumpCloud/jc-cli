package mcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"

	"golang.org/x/term"
)

// isExecutingDestructive returns true if the tool argument carries an
// Execute: true field — the codebase's signal that a tool is about to
// mutate state at the JumpCloud API. The signal is structural: every
// destructive tool input embeds an `Execute bool` field, by convention
// (see `destructiveInput`, `userUpdateInput`, `commandRunInput`, etc.).
func isExecutingDestructive(args any) bool {
	v := reflect.ValueOf(args)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	f := v.FieldByName("Execute")
	if !f.IsValid() || f.Kind() != reflect.Bool {
		return false
	}
	return f.Bool()
}

// destructiveTarget returns a short string that identifies the resource
// the destructive operation will hit (e.g. the username for users_*,
// the device hostname for devices_*). Used purely for the human-facing
// step-up prompt — never persisted, never sent to the JumpCloud API.
//
// We try a small set of well-known field names in priority order. If
// none are present, return an empty string and the prompt skips the
// "on <target>" clause.
func destructiveTarget(args any) string {
	v := reflect.ValueOf(args)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	for _, name := range []string{"Identifier", "Member", "Group", "Target", "Username", "DeviceID", "Command"} {
		f := v.FieldByName(name)
		if f.IsValid() && f.Kind() == reflect.String && f.String() != "" {
			return f.String()
		}
	}
	return ""
}

// stepUpAuthenticator gates a destructive MCP operation behind a fresh
// proof of operator presence. Implementations decide what "fresh proof"
// means: a TTY prompt, a Touch ID popup, an out-of-band approval, etc.
//
// The chokepoint in addTypedTool calls authorize() once per destructive
// tool invocation (any tool argument carrying Execute: true). A non-nil
// error blocks the underlying API call.
type stepUpAuthenticator interface {
	authorize(ctx context.Context, toolName, target string) error
}

// noopStepUp permits every operation. Used when the feature is disabled
// (the default) so the chokepoint adds zero overhead.
type noopStepUp struct{}

func (noopStepUp) authorize(context.Context, string, string) error { return nil }

// errStepUpDenied indicates the operator declined the prompt. Distinct
// from infrastructure errors so the audit log can record the difference.
var errStepUpDenied = errors.New("operator denied step-up auth challenge")

// errStepUpUnavailable indicates the authenticator could not present a
// challenge (no TTY in stdio mode, missing keychain key, etc.). The
// caller treats this as a deny — fail closed.
var errStepUpUnavailable = errors.New("step-up auth required but no challenge channel is available")

// ttyStepUp prompts on the controlling terminal for the last 6 chars of
// the configured API key. It's intentionally a weak challenge — anyone
// holding the key answers the same prompt — so its real value is in
// (a) catching an autonomous agent that flipped Execute: true without
// the operator noticing, and (b) forcing an in-the-loop pause on
// destructive ops.
//
// Stronger authenticators (Touch ID, push-approval) plug into the same
// interface and supersede this one when the platform supports them.
type ttyStepUp struct {
	mu        sync.Mutex
	in        io.Reader // defaults to os.Stdin; injectable for tests
	out       io.Writer // defaults to os.Stderr
	stdinFD   int       // for IsTerminal check; -1 to skip the check (tests)
	apiKey    string    // captured at server start so prompt-time changes don't shift the answer
	expectLen int       // last-N chars to ask for (default 6)
}

// newTTYStepUp constructs a TTY-prompt authenticator. apiKey is the
// credential used to derive the challenge answer; if empty, every
// authorize() call returns errStepUpUnavailable.
func newTTYStepUp(apiKey string) *ttyStepUp {
	return &ttyStepUp{
		in:        os.Stdin,
		out:       os.Stderr,
		stdinFD:   int(os.Stdin.Fd()),
		apiKey:    apiKey,
		expectLen: 6,
	}
}

func (t *ttyStepUp) authorize(ctx context.Context, toolName, target string) error {
	// Serialize prompts: concurrent destructive ops would interleave on
	// stdin and produce nonsense. The MCP transport may multiplex tool
	// calls, so this lock matters.
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.apiKey == "" {
		return errStepUpUnavailable
	}
	if len(t.apiKey) < t.expectLen {
		// Defensively fail-closed: a too-short key can't produce a
		// meaningful challenge.
		return errStepUpUnavailable
	}

	// stdinFD -1 skips the terminal check (test injection). In
	// production, refuse if stdin isn't a TTY — the prompt would
	// otherwise block forever or read from a JSON-RPC pipe in stdio
	// transport mode.
	if t.stdinFD >= 0 && !term.IsTerminal(t.stdinFD) {
		return errStepUpUnavailable
	}

	expected := t.apiKey[len(t.apiKey)-t.expectLen:]

	targetClause := ""
	if target != "" {
		targetClause = fmt.Sprintf(" on %s", target)
	}
	fmt.Fprintf(t.out,
		"\nStep-up auth: %s%s\nEnter the last %d characters of the API key to confirm (or 'no' to deny): ",
		toolName, targetClause, t.expectLen)

	// We can't easily call term.ReadPassword here without taking over
	// the controlling terminal — and the value isn't a secret in the
	// usual sense (the operator already knows it). A line read on stdin
	// is enough.
	reader := bufio.NewReader(t.in)
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading step-up response: %w", err)
	}
	line = strings.TrimSpace(line)

	if strings.EqualFold(line, "no") || strings.EqualFold(line, "n") || strings.EqualFold(line, "deny") {
		return errStepUpDenied
	}
	if line != expected {
		return errStepUpDenied
	}
	return nil
}

// newStepUp returns the authenticator a Server should use given the
// requested configuration. Callers that haven't enabled the feature
// get noopStepUp (zero-cost path).
func newStepUp(required bool, apiKey string) stepUpAuthenticator {
	if !required {
		return noopStepUp{}
	}
	return newTTYStepUp(apiKey)
}
