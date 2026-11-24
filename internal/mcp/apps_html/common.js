// MCP App scaffolding — injected into every jc MCP App HTML resource.
//
// Implements the View side of the MCP Apps lifecycle (spec 2026-01-26,
// https://github.com/modelcontextprotocol/ext-apps/blob/main/specification/2026-01-26/apps.mdx):
//
//   View → Host  ui/initialize {appInfo, appCapabilities, protocolVersion}
//   Host → View  McpUiInitializeResult {hostContext, ...}
//   View → Host  ui/notifications/initialized
//   Host → View  ui/notifications/tool-input, then tool-result (or tool-cancelled)
//
// Apps are push, not pull: the host already called the tool and pushes its
// result here. An app must not call its own tool on load. Until 2026-11
// this file spoke a pre-spec dialect (a "ready" notification, no
// initialize request) and apps fetched their own data; Claude Desktop keeps
// such an app hidden because the handshake never completes
// (anthropics/claude-ai-mcp#149).
//
// Apps interact with the host via window.jcApp:
//
//   jcApp.ready(name, version)            start the handshake; returns a
//                                         Promise that resolves with the
//                                         host context once initialized
//   jcApp.onToolResult(cb)                the tool result the host pushes
//                                         (a CallToolResult); a result that
//                                         arrived before cb was registered
//                                         is replayed to it
//   jcApp.onToolCancelled(cb)             the host cancelled the tool call
//   jcApp.callTool(name, args) -> Promise call an MCP tool through the host
//                                         (user actions such as Refresh);
//                                         waits for the handshake first
//   jcApp.parseToolResult(result) -> any  the JSON body of a result's text
//                                         block (throws on isError)
//   jcApp.hostContext                     the host context, once known
(function() {
  "use strict";

  var PROTOCOL_VERSION = "2026-01-26";
  var CALL_TIMEOUT_MS = 30000;

  var rpcId = 0;
  var pending = {};
  var started = false;
  var initResolve, initReject;
  var initialized = new Promise(function(resolve, reject) {
    initResolve = resolve;
    initReject = reject;
  });
  var handlers = { toolResult: null, toolCancelled: null };
  var queued = { toolResult: undefined, toolCancelled: undefined };

  function post(msg) {
    msg.jsonrpc = "2.0";
    window.parent.postMessage(msg, "*");
  }

  function request(method, params) {
    var id = ++rpcId;
    return new Promise(function(resolve, reject) {
      var timer = setTimeout(function() {
        delete pending[id];
        reject(new Error(method + " timed out after " + CALL_TIMEOUT_MS / 1000 + "s"));
      }, CALL_TIMEOUT_MS);
      pending[id] = {
        resolve: function(v) { clearTimeout(timer); resolve(v); },
        reject: function(e) { clearTimeout(timer); reject(e); }
      };
      post({ id: id, method: method, params: params || {} });
    });
  }

  function deliver(kind, params) {
    if (handlers[kind]) handlers[kind](params);
    else queued[kind] = params;
  }

  // Report the content height so the host sizes the iframe instead of
  // leaving it at a default height with an inner scrollbar. Measures with
  // max-content the way the reference SDK does, and only sends on change.
  function watchSize() {
    if (typeof ResizeObserver === "undefined") return;
    var scheduled = false, lastW = 0, lastH = 0;
    function measure() {
      if (!scheduled) return;
      scheduled = false;
      var html = document.documentElement;
      var saved = html.style.height;
      html.style.height = "max-content";
      var h = Math.ceil(html.getBoundingClientRect().height);
      html.style.height = saved;
      var w = Math.ceil(window.innerWidth);
      if (w === lastW && h === lastH) return;
      lastW = w; lastH = h;
      post({ method: "ui/notifications/size-changed", params: { width: w, height: h } });
    }
    // Chrome skips rendering (animation frames and ResizeObserver
    // callbacks) for an off-screen cross-origin iframe, so an app rendered
    // below the fold reports its final height once it scrolls into view.
    // The reference SDK behaves the same way.
    function send() {
      if (scheduled) return;
      scheduled = true;
      requestAnimationFrame(measure);
    }
    send();
    var ro = new ResizeObserver(send);
    ro.observe(document.documentElement);
    ro.observe(document.body);
  }

  window.addEventListener("message", function(event) {
    if (event.source !== window.parent) return;
    var msg = event.data;
    if (!msg || msg.jsonrpc !== "2.0") return;

    // Response to one of our requests.
    if (msg.id !== undefined && msg.method === undefined) {
      var cb = pending[msg.id];
      if (!cb) return;
      delete pending[msg.id];
      if (msg.error) cb.reject(new Error(msg.error.message || "request failed"));
      else cb.resolve(msg.result);
      return;
    }

    // Requests from the host need an answer.
    if (msg.id !== undefined) {
      if (msg.method === "ping" || msg.method === "ui/resource-teardown") {
        post({ id: msg.id, result: {} });
      } else {
        post({ id: msg.id, error: { code: -32601, message: "Method not found: " + msg.method } });
      }
      return;
    }

    switch (msg.method) {
      case "ui/notifications/tool-result":
        deliver("toolResult", msg.params);
        break;
      case "ui/notifications/tool-cancelled":
        deliver("toolCancelled", msg.params || {});
        break;
      case "ui/notifications/host-context-changed":
        if (window.jcApp.hostContext && msg.params) {
          for (var k in msg.params) window.jcApp.hostContext[k] = msg.params[k];
        }
        break;
    }
  });

  window.jcApp = {
    hostContext: null,

    ready: function(name, version) {
      if (started) return initialized;
      started = true;
      request("ui/initialize", {
        appInfo: { name: name || "JumpCloud App", version: version || "1.0.0" },
        appCapabilities: { availableDisplayModes: ["inline"] },
        protocolVersion: PROTOCOL_VERSION
      }).then(function(result) {
        window.jcApp.hostContext = (result && result.hostContext) || {};
        post({ method: "ui/notifications/initialized", params: {} });
        watchSize();
        initResolve(window.jcApp.hostContext);
      }, function(err) {
        initReject(err);
      });
      return initialized;
    },

    onToolResult: function(cb) {
      handlers.toolResult = cb;
      if (queued.toolResult !== undefined) {
        var p = queued.toolResult;
        queued.toolResult = undefined;
        cb(p);
      }
    },

    onToolCancelled: function(cb) {
      handlers.toolCancelled = cb;
      if (queued.toolCancelled !== undefined) {
        var p = queued.toolCancelled;
        queued.toolCancelled = undefined;
        cb(p);
      }
    },

    callTool: function(name, args) {
      return initialized.then(function() {
        return request("tools/call", { name: name, arguments: args || {} });
      });
    },

    parseToolResult: function(result) {
      if (!result || !result.content) return null;
      var tc = null;
      for (var i = 0; i < result.content.length; i++) {
        if (result.content[i].type === "text") { tc = result.content[i]; break; }
      }
      if (!tc) return null;
      if (result.isError) throw new Error(tc.text);
      return JSON.parse(tc.text);
    }
  };
})();
