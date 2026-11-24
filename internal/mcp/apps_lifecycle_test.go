package mcp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The MCP Apps lifecycle (spec 2026-01-26) is push, not pull: the View sends
// ui/initialize, then ui/notifications/initialized, and the host pushes the
// tool result. Hosts such as Claude Desktop keep a View hidden until that
// handshake completes, so an app that skips it or fetches its own data on
// load renders nothing there even though it works in a lenient host.

// allAppHTML is every embedded MCP App, keyed by file name.
func allAppHTML() map[string]string {
	return map[string]string{
		"dashboard.html":     dashboardHTML,
		"compliance.html":    complianceHTML,
		"recipe_runner.html": recipeRunnerHTML,
		"user.html":          userHTML,
		"device.html":        deviceHTML,
		"insights.html":      insightsHTML,
	}
}

func TestAppsUseSpecLifecycle(t *testing.T) {
	for name, html := range allAppHTML() {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(html, appCommonMarker) {
				t.Fatalf("does not inject common.js, so it has no MCP Apps handshake")
			}
			for _, want := range []string{"jcApp.ready(", "jcApp.onToolResult(", "jcApp.onToolCancelled("} {
				if !strings.Contains(html, want) {
					t.Errorf("missing %s", want)
				}
			}
			// The pre-spec pattern was a timer that called the app's own
			// tool when no result had arrived yet. Apps must wait for the
			// pushed result instead.
			if strings.Contains(html, "setTimeout(") {
				t.Errorf("schedules work on a timer; apps must not fetch their own data on load")
			}
			for _, old := range []string{`"ui/ready"`, `'ui/ready'`, `"tools/result"`, `'tools/result'`} {
				if strings.Contains(html, old) {
					t.Errorf("uses the pre-spec message %s", old)
				}
			}
		})
	}
	for _, old := range []string{`"ui/ready"`, `"tools/result"`} {
		if strings.Contains(appCommonJS, old) {
			t.Errorf("common.js still speaks the pre-spec message %s", old)
		}
	}
}

// commonJSHarness loads common.js against a fake window and plays the host.
const commonJSHarness = `
const src = require('fs').readFileSync(process.argv[2], 'utf8');
const posted = [], listeners = [];
const parent = { postMessage: (m) => posted.push(JSON.parse(JSON.stringify(m))) };
global.window = { parent, innerWidth: 800, addEventListener: (_t, f) => listeners.push(f) };
global.document = { documentElement: { style: {}, getBoundingClientRect: () => ({ height: 420 }) }, body: {} };
global.ResizeObserver = class { observe() {} };
global.requestAnimationFrame = (f) => f();
new Function(src)();
const fromHost = (data, source = parent) => listeners.forEach((f) => f({ data, source }));
const flush = () => new Promise((r) => setImmediate(r));
const jc = window.jcApp;
(async () => {
  const out = {};
  const init = jc.ready('T', '1');
  const callP = jc.callTool('x_view', { a: 1 });
  await flush();
  out.beforeInit = posted.map((m) => m.method);
  const initReq = posted[0];
  out.initParams = initReq.params;
  fromHost({ jsonrpc: '2.0', id: initReq.id, result: { hostContext: { theme: 'spoofed' } } }, {});
  await flush();
  out.afterSpoof = posted.length;
  fromHost({ jsonrpc: '2.0', method: 'ui/notifications/tool-result', params: { content: [{ type: 'text', text: '{"ok":1}' }] } });
  fromHost({ jsonrpc: '2.0', id: initReq.id, result: { hostContext: { theme: 'dark' } } });
  out.theme = (await init).theme;
  await flush();
  out.afterInit = posted.map((m) => m.method);
  jc.onToolResult((p) => { out.replayed = jc.parseToolResult(p); });
  const call = posted.find((m) => m.method === 'tools/call');
  out.callParams = call.params;
  fromHost({ jsonrpc: '2.0', id: call.id, result: { content: [{ type: 'text', text: '{"r":2}' }] } });
  out.callResult = jc.parseToolResult(await callP);
  fromHost({ jsonrpc: '2.0', id: 99, method: 'ping' });
  fromHost({ jsonrpc: '2.0', id: 100, method: 'ui/resource-teardown', params: { reason: 'x' } });
  fromHost({ jsonrpc: '2.0', id: 101, method: 'bogus' });
  out.answers = posted.filter((m) => m.id >= 99 && !m.method).map((m) => [m.id, m.result ? 'ok' : m.error.code]);
  jc.onToolCancelled((p) => { out.cancelled = p.reason; });
  fromHost({ jsonrpc: '2.0', method: 'ui/notifications/tool-cancelled', params: { reason: 'user' } });
  out.size = posted.find((m) => m.method === 'ui/notifications/size-changed').params;
  console.log(JSON.stringify(out));
})().catch((e) => { console.error(e); process.exit(1); });
`

func TestCommonJSLifecycle(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		// GitHub's runners ship node; a silent skip there would leave the
		// handshake untested, so CI must fail instead.
		if os.Getenv("CI") != "" {
			t.Fatal("node not on PATH in CI; the MCP Apps lifecycle test needs it")
		}
		t.Skip("node not on PATH; the lifecycle of common.js is checked where node exists (CI has it)")
	}
	dir := t.TempDir()
	js := filepath.Join(dir, "common.js")
	harness := filepath.Join(dir, "harness.js")
	if err := os.WriteFile(js, []byte(appCommonJS), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(harness, []byte(commonJSHarness), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command(node, harness, js).CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, raw)
	}
	var got struct {
		BeforeInit []string           `json:"beforeInit"`
		InitParams map[string]any     `json:"initParams"`
		AfterSpoof int                `json:"afterSpoof"`
		Theme      string             `json:"theme"`
		AfterInit  []string           `json:"afterInit"`
		Replayed   map[string]any     `json:"replayed"`
		CallParams map[string]any     `json:"callParams"`
		CallResult map[string]any     `json:"callResult"`
		Answers    [][]any            `json:"answers"`
		Cancelled  string             `json:"cancelled"`
		Size       map[string]float64 `json:"size"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("harness output: %v\n%s", err, raw)
	}

	// Nothing but ui/initialize may reach the host before the handshake:
	// the tool call made before it is held back.
	if !reflect.DeepEqual(got.BeforeInit, []string{"ui/initialize"}) {
		t.Errorf("before the handshake the app sent %v, want only ui/initialize", got.BeforeInit)
	}
	if got.InitParams["protocolVersion"] != "2026-01-26" {
		t.Errorf("protocolVersion = %v", got.InitParams["protocolVersion"])
	}
	if info, _ := got.InitParams["appInfo"].(map[string]any); info["name"] != "T" || info["version"] != "1" {
		t.Errorf("appInfo = %v", got.InitParams["appInfo"])
	}
	if _, ok := got.InitParams["appCapabilities"]; !ok {
		t.Error("ui/initialize has no appCapabilities (the spec requires it)")
	}
	// A message whose source is not the parent window is ignored.
	if got.AfterSpoof != 1 {
		t.Errorf("a response from a foreign window was acted on (%d messages sent)", got.AfterSpoof)
	}
	if got.Theme != "dark" {
		t.Errorf("host context theme = %q, want the parent's \"dark\"", got.Theme)
	}
	wantOrder := []string{"ui/initialize", "ui/notifications/initialized", "ui/notifications/size-changed", "tools/call"}
	if !reflect.DeepEqual(got.AfterInit, wantOrder) {
		t.Errorf("message order = %v, want %v", got.AfterInit, wantOrder)
	}
	if got.Replayed["ok"] != float64(1) {
		t.Errorf("a tool result pushed before onToolResult was registered was lost: %v", got.Replayed)
	}
	if got.CallParams["name"] != "x_view" {
		t.Errorf("tools/call params = %v", got.CallParams)
	}
	if got.CallResult["r"] != float64(2) {
		t.Errorf("callTool result = %v", got.CallResult)
	}
	wantAnswers := [][]any{{float64(99), "ok"}, {float64(100), "ok"}, {float64(101), float64(-32601)}}
	if !reflect.DeepEqual(got.Answers, wantAnswers) {
		t.Errorf("answers to host requests = %v, want %v", got.Answers, wantAnswers)
	}
	if got.Cancelled != "user" {
		t.Errorf("tool-cancelled reason = %q", got.Cancelled)
	}
	if got.Size["width"] != 800 || got.Size["height"] != 420 {
		t.Errorf("size-changed = %v, want 800x420", got.Size)
	}
}
