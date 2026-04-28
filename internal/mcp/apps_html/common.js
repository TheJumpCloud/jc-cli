// MCP App scaffolding — injected into every jc MCP App HTML resource.
// Provides postMessage plumbing between the sandboxed app iframe and the host.
// Apps interact with the host via window.jcApp:
//
//   jcApp.ready(name, version)           — signal readiness to host
//   jcApp.onToolResult(cb)               — handle the initial tool result
//                                          pushed by the host after the UI
//                                          renders
//   jcApp.callTool(name, args) -> Promise — call an MCP tool on the server
//                                           (for e.g. refresh buttons)
//   jcApp.parseToolResult(result) -> any  — pull the JSON body out of a
//                                           tool result's text content block
//                                           (throws on isError)
(function() {
  "use strict";

  var rpcId = 0;
  var pending = {};
  var toolResultHandler = null;

  window.addEventListener("message", function(event) {
    var msg = event.data;
    if (!msg || msg.jsonrpc !== "2.0") return;

    if (msg.id !== undefined && pending[msg.id]) {
      var cb = pending[msg.id];
      delete pending[msg.id];
      if (msg.error) cb.reject(msg.error);
      else cb.resolve(msg.result);
    }

    if (msg.method === "tools/result" && toolResultHandler) {
      toolResultHandler(msg.params);
    }
  });

  window.jcApp = {
    callTool: function(name, args) {
      var id = ++rpcId;
      return new Promise(function(resolve, reject) {
        var timer = setTimeout(function() {
          delete pending[id];
          reject(new Error("Tool call timed out after 30s"));
        }, 30000);
        pending[id] = {
          resolve: function(v) { clearTimeout(timer); resolve(v); },
          reject:  function(e) { clearTimeout(timer); reject(e); }
        };
        window.parent.postMessage({
          jsonrpc: "2.0",
          id: id,
          method: "tools/call",
          params: { name: name, arguments: args || {} }
        }, "*");
      });
    },

    onToolResult: function(cb) {
      toolResultHandler = cb;
    },

    ready: function(name, version) {
      window.parent.postMessage({
        jsonrpc: "2.0",
        method: "ui/ready",
        params: {
          name: name || "JumpCloud App",
          version: version || "1.0.0"
        }
      }, "*");
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
