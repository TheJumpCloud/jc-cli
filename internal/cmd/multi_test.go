package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/viper"
)

// setupMultiProfiles installs a fake profile set into the global viper
// (the same channel config.ProfileNames reads).
func setupMultiProfiles(t *testing.T, profiles map[string]map[string]any) {
	t.Helper()
	prev := viper.Get("profiles")
	m := map[string]any{}
	for name, fields := range profiles {
		inner := map[string]any{"api_key": "x"}
		for k, v := range fields {
			inner[k] = v
		}
		m[name] = inner
	}
	viper.Set("profiles", m)
	t.Cleanup(func() { viper.Set("profiles", prev) })
}

// stubMultiRunner swaps the subprocess runner. handler maps profile →
// (stdout, err); unlisted profiles succeed with "null".
func stubMultiRunner(t *testing.T, handler func(profile string, args []string) (string, error)) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	orig := multiRunInner
	multiRunInner = func(ctx context.Context, profile string, innerArgs []string) ([]byte, []byte, error) {
		calls.Add(1)
		out, err := handler(profile, innerArgs)
		if err != nil {
			return nil, []byte(err.Error()), err
		}
		return []byte(out), nil, nil
	}
	t.Cleanup(func() { multiRunInner = orig })
	return &calls
}

func runMulti(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append([]string{"multi"}, args...))
	err := root.Execute()
	return stdout.String(), err
}

func TestMulti_SelectProfiles(t *testing.T) {
	setupMultiProfiles(t, map[string]map[string]any{
		"prod-a": {}, "prod-b": {}, "staging": {},
	})

	// Glob filter.
	got, err := selectMultiProfiles("", "prod-*")
	if err != nil || len(got) != 2 {
		t.Errorf("glob selection wrong: %v %v", got, err)
	}
	// Explicit CSV with an unknown name → error naming it.
	_, err = selectMultiProfiles("prod-a,nope", "")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown profile should error: %v", err)
	}
	// Both / neither → usage error.
	if _, err := selectMultiProfiles("a", "b*"); err == nil {
		t.Error("both selectors should error")
	}
	if _, err := selectMultiProfiles("", ""); err == nil {
		t.Error("neither selector should error")
	}
	// Non-matching glob names the configured profiles.
	_, err = selectMultiProfiles("", "zzz-*")
	if err == nil || !strings.Contains(err.Error(), "staging") {
		t.Errorf("no-match error should list configured profiles: %v", err)
	}
}

func TestMulti_JSONAggregateAndFailureIsolation(t *testing.T) {
	setupMultiProfiles(t, map[string]map[string]any{
		"prod-a": {}, "prod-b": {}, "prod-c": {},
	})
	calls := stubMultiRunner(t, func(profile string, args []string) (string, error) {
		if profile == "prod-b" {
			return "", errors.New("401 unauthorized")
		}
		return `[{"id":"u1","username":"alice-` + profile + `"}]`, nil
	})

	out, err := runMulti(t, "--filter", "prod-*", "--", "users", "list")
	// One profile failed → non-zero exit AFTER rendering.
	if err == nil || !strings.Contains(err.Error(), "1 of 3 profiles failed") {
		t.Fatalf("expected partial-failure error, got %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 fan-out calls, got %d", calls.Load())
	}

	var results []multiProfileResult
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("aggregate output is not JSON: %v\n%s", err, out)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 result entries, got %d", len(results))
	}
	byProfile := map[string]multiProfileResult{}
	for _, r := range results {
		byProfile[r.Profile] = r
	}
	if byProfile["prod-a"].Status != "ok" || !strings.Contains(string(byProfile["prod-a"].Data), "alice-prod-a") {
		t.Errorf("prod-a result wrong: %+v", byProfile["prod-a"])
	}
	if byProfile["prod-b"].Status != "failed" || !strings.Contains(byProfile["prod-b"].Error, "401") {
		t.Errorf("prod-b failure not isolated: %+v", byProfile["prod-b"])
	}
}

func TestMulti_InnerArgsCarryOrgPerProfile(t *testing.T) {
	setupMultiProfiles(t, map[string]map[string]any{"a": {}})
	var sawArgs []string
	stubMultiRunner(t, func(profile string, args []string) (string, error) {
		sawArgs = append([]string{}, args...)
		return "[]", nil
	})
	_, err := runMulti(t, "--profiles", "a", "--", "users", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The stub receives the inner args WITHOUT --org (the real runner
	// appends it) — assert the inner command came through unchanged.
	if strings.Join(sawArgs, " ") != "users list" {
		t.Errorf("inner args mutated: %v", sawArgs)
	}
}

func TestMulti_DestructiveGate(t *testing.T) {
	setupMultiProfiles(t, map[string]map[string]any{"a": {}, "b": {}})
	calls := stubMultiRunner(t, func(string, []string) (string, error) { return "{}", nil })

	// Destructive inner command without the gate → refused, zero calls.
	_, err := runMulti(t, "--profiles", "a,b", "--", "users", "delete", "bot")
	if err == nil || !strings.Contains(err.Error(), "--allow-destructive") {
		t.Fatalf("expected destructive refusal, got %v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("refusal must happen before any fan-out, got %d calls", calls.Load())
	}

	// With the gate → runs.
	_, err = runMulti(t, "--profiles", "a,b", "--allow-destructive", "--", "users", "delete", "bot", "--force")
	if err != nil {
		t.Fatalf("gated destructive run failed: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls, got %d", calls.Load())
	}

	// Unknown inner command classifies as destructive (worst-case).
	_, err = runMulti(t, "--profiles", "a", "--", "definitely-not-a-command")
	if err == nil || !strings.Contains(err.Error(), "--allow-destructive") {
		t.Errorf("unknown inner command should hit the worst-case gate: %v", err)
	}
}

func TestMulti_ReadOnlyProfileSkippedForWrites(t *testing.T) {
	setupMultiProfiles(t, map[string]map[string]any{
		"rw": {}, "ro": {"auth_profile_role": "read_only"},
	})
	calls := stubMultiRunner(t, func(profile string, _ []string) (string, error) {
		return `{"id":"` + profile + `"}`, nil
	})

	// Mutating command: ro is skipped, reported, and doesn't fail the run.
	out, err := runMulti(t, "--filter", "*", "--", "users", "create", "--username", "x")
	if err != nil {
		t.Fatalf("skip must not count as failure: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("read_only profile should be skipped, got %d calls", calls.Load())
	}
	var results []multiProfileResult
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatal(err)
	}
	byProfile := map[string]multiProfileResult{}
	for _, r := range results {
		byProfile[r.Profile] = r
	}
	if byProfile["ro"].Status != "skipped" || !strings.Contains(byProfile["ro"].Error, "read_only") {
		t.Errorf("ro skip not reported: %+v", byProfile["ro"])
	}

	// Read-only command: both run.
	calls.Store(0)
	if _, err := runMulti(t, "--filter", "*", "--", "users", "list"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("read-only command should run on both, got %d", calls.Load())
	}
}

func TestMulti_IDsPrefixed(t *testing.T) {
	setupMultiProfiles(t, map[string]map[string]any{"a": {}, "b": {}})
	stubMultiRunner(t, func(profile string, _ []string) (string, error) {
		return "id-1\nid-2\n", nil
	})
	out, err := runMulti(t, "--filter", "*", "--", "users", "list", "--ids")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a/id-1", "a/id-2", "b/id-1", "b/id-2"} {
		if !strings.Contains(out, want) {
			t.Errorf("ids output missing %q:\n%s", want, out)
		}
	}
}

func TestMulti_TableSections(t *testing.T) {
	setupMultiProfiles(t, map[string]map[string]any{"a": {}})
	stubMultiRunner(t, func(string, []string) (string, error) {
		return "USERNAME  EMAIL\nalice     a@x.co\n", nil
	})
	out, err := runMulti(t, "--profiles", "a", "--", "users", "list", "-t")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "── a ──") || !strings.Contains(out, "alice") {
		t.Errorf("table sections wrong:\n%s", out)
	}
}

func TestMulti_GuardRails(t *testing.T) {
	setupMultiProfiles(t, map[string]map[string]any{"a": {}})
	stubMultiRunner(t, func(string, []string) (string, error) { return "{}", nil })

	// Inner --org is refused.
	_, err := runMulti(t, "--profiles", "a", "--", "users", "list", "--org", "b")
	if err == nil || !strings.Contains(err.Error(), "--org") {
		t.Errorf("inner --org should be refused: %v", err)
	}
	// multi-in-multi is refused.
	_, err = runMulti(t, "--profiles", "a", "--", "multi", "--profiles", "a", "--", "users", "list")
	if err == nil || !strings.Contains(err.Error(), "cannot fan out") {
		t.Errorf("multi recursion should be refused: %v", err)
	}
}
