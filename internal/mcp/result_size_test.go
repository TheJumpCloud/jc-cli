package mcp

import (
	"strings"
	"testing"
)

// An oversized response does not fail in isolation. Observed 2026-08-28: a
// 174KB result poisoned the MCP session and every later call carrying a real
// payload timed out, while jc_ping kept answering — which masked the cause and
// made one bad response look like several unrelated broken tools.
//
// So the guard refuses the call instead of returning it. A refused call is
// strictly better than one that succeeds and breaks everything after it.
func TestResultSizeGuard_RefusesOversizedResults(t *testing.T) {
	small := textResult("fine")
	if small.IsError {
		t.Fatal("an ordinary result must not be refused")
	}

	huge := textResult(strings.Repeat("x", maxResultBytes+1))
	if !huge.IsError {
		t.Fatal("a result over the limit must be refused")
	}
	msg := getResultText(t, huge)
	if !strings.Contains(msg, "result too large") {
		t.Errorf("the refusal should say what happened: %q", msg)
	}
	// It must tell the caller how to proceed, not just that it failed.
	if !strings.Contains(msg, "Narrow the request") {
		t.Errorf("the refusal should say how to recover: %q", msg)
	}
	// And why refusing beats returning, or someone will "fix" it by raising
	// the cap.
	if !strings.Contains(msg, "break every later call") {
		t.Errorf("the refusal should say why it is refused rather than truncated: %q", msg)
	}
}

func TestResultSizeGuard_AppliesToJSONResults(t *testing.T) {
	ok, err := jsonResult(map[string]any{"a": "b"})
	if err != nil || ok.IsError {
		t.Fatalf("a small JSON result must pass: err=%v", err)
	}

	// jsonResult must be guarded too: most listings go through it, and the
	// response that caused the incident was one of them.
	big, err := jsonResult(map[string]any{"blob": strings.Repeat("y", maxResultBytes)})
	if err != nil {
		t.Fatalf("an oversized result should be refused, not error out: %v", err)
	}
	if !big.IsError {
		t.Error("an oversized JSON result must be refused")
	}
}

// The regression the incident actually calls for: a large response must not
// break the small one that follows it. Before the guard, the second call would
// time out even though it was tiny.
func TestResultSizeGuard_LargeResponseDoesNotPoisonTheNext(t *testing.T) {
	overrideV2ClientForTest(t, startWorkflowV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	// A broad listing — the shape that caused the incident.
	first := getResultText(t, callTool(t, cs, "workflows_event_types", map[string]any{}))
	if first == "" {
		t.Fatal("the broad listing returned nothing")
	}

	// A small call immediately afterwards, on the same session, must work.
	second := getResultText(t, callTool(t, cs, "workflows_simulate", map[string]any{
		"dsl": mustDecode(t, `{"schedule":{"on":{"one":{"with":{"source":"external"}}}},
		  "do":[{"a":{"call":"jc_operation","with":{"operationId":"getApiSystemusers","version":1}}}]}`),
	}))
	if !strings.Contains(second, "would-call") {
		t.Errorf("a small call after a large one must still work, got: %s", second)
	}

	// And a third, to show the session is not merely limping.
	third := getResultText(t, callTool(t, cs, "workflows_event_types", map[string]any{
		"service": "access_management",
	}))
	if !strings.Contains(third, "access_management") {
		t.Errorf("the session should still be healthy, got: %s", third)
	}
}
