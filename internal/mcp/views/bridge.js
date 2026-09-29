// The view's side of MCP Apps (2026-01-26): JSON-RPC 2.0 over postMessage
// with the host that renders it. Written for this page rather than taken
// from the reference SDK, which would put some 400 KB of dependencies into
// every view; the protocol it covers is small and stable.

const APPS_PROTOCOL_VERSION = "2026-01-26";
const REQUEST_TIMEOUT_MS = 30000;

function createBridge(appInfo) {
  let nextID = 1;
  const pending = new Map();
  const listeners = new Map();

  function post(message) {
    // The host's origin is not known inside its sandbox, so "*" is the only
    // target there is. What leaves this way is the view's own requests.
    window.parent.postMessage(message, "*");
  }

  function request(method, params, timeout = REQUEST_TIMEOUT_MS) {
    const id = nextID++;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        pending.delete(id);
        reject(new Error(method + " got no answer"));
      }, timeout);
      pending.set(id, {
        resolve: (value) => { clearTimeout(timer); resolve(value); },
        reject: (error) => { clearTimeout(timer); reject(error); },
      });
      post({ jsonrpc: "2.0", id, method, params });
    });
  }

  function notify(method, params) {
    post({ jsonrpc: "2.0", method, params });
  }

  function respond(id, result) {
    post({ jsonrpc: "2.0", id, result });
  }

  function emit(method, params) {
    for (const listener of listeners.get(method) || []) {
      try {
        listener(params);
      } catch (error) {
        console.error(error);
      }
    }
  }

  window.addEventListener("message", (event) => {
    // Only the host speaks to the view.
    if (event.source !== window.parent) return;
    const message = event.data;
    if (!message || typeof message !== "object" || message.jsonrpc !== "2.0") return;

    if (typeof message.method !== "string") {
      const waiting = pending.get(message.id);
      if (!waiting) return;
      pending.delete(message.id);
      if (message.error) {
        waiting.reject(new Error(message.error.message || "the host refused the request"));
      } else {
        waiting.resolve(message.result);
      }
      return;
    }

    if (message.id !== undefined && message.id !== null) {
      switch (message.method) {
        case "ping":
          respond(message.id, {});
          return;
        case "ui/resource-teardown":
          emit("teardown", message.params);
          respond(message.id, {});
          return;
        default:
          post({ jsonrpc: "2.0", id: message.id, error: { code: -32601, message: "Method not found" } });
          return;
      }
    }

    emit(message.method, message.params);
  });

  return {
    on(method, listener) {
      const list = listeners.get(method) || [];
      list.push(listener);
      listeners.set(method, list);
    },

    async connect(timeout) {
      const result = await request("ui/initialize", {
        appInfo,
        appCapabilities: { availableDisplayModes: ["inline", "fullscreen"] },
        protocolVersion: APPS_PROTOCOL_VERSION,
      }, timeout);
      notify("ui/notifications/initialized", {});
      return result || {};
    },

    reportSize(width, height) {
      notify("ui/notifications/size-changed", { width, height });
    },

    openLink(url) {
      return request("ui/open-link", { url });
    },

    // A call of one of bb's tools, which the host passes to the server.
    callTool(name, args, timeout) {
      return request("tools/call", { name, arguments: args }, timeout);
    },

    requestDisplayMode(mode) {
      return request("ui/request-display-mode", { mode });
    },

    // The context the model reads on its next turn. Each call replaces the
    // last; nothing is sent to the model until the person writes.
    updateModelContext(text) {
      return request("ui/update-model-context", { content: [{ type: "text", text }] });
    },

    // A message in the person's name. Only ever sent from a click.
    sendMessage(text) {
      return request("ui/message", { role: "user", content: [{ type: "text", text }] });
    },
  };
}
