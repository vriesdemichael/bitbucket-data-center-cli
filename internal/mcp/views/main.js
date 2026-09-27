// The page: connects to the host, and draws the show tool's result.

// Where the show tool puts what the view draws, in the result's _meta. The
// same key is in views.go; TestViewPageReadsThePayloadKeyTheToolWrites holds
// the two together.
const VIEW_PAYLOAD_KEY = "io.github.vriesdemichael.bb/view";
const VIEW_PAYLOAD_VERSION = 1;
const CONNECT_TIMEOUT_MS = 10000;

const bridge = createBridge({ name: "bb", version: "1" });

const view = {
  bridge,
  payload: null,
  toolInput: null,
  failure: null,
  standalone: false,
  fullscreen: false,
  canFullscreen: false,
  expanded: false,
  locale: undefined,
  filter: "all",
  openFiles: new Set(),
  diffFiles: null,
  selection: null,
  selectionBar: null,
  selectionPR: null,
  selectionNotice: "",

  // expand opens the view out: fullscreen where the host has it, in place
  // where it does not. Again, it goes back.
  expand() {
    if (view.canFullscreen) {
      const mode = view.fullscreen ? "inline" : "fullscreen";
      bridge.requestDisplayMode(mode)
        .then((result) => setDisplayMode(result && result.mode ? result.mode : mode))
        .catch(() => {
          view.expanded = !view.expanded;
          render();
        });
      return;
    }
    view.expanded = !view.expanded;
    render();
  },

  setFilter(id) {
    view.filter = id;
    render();
  },

  toggleFile(index) {
    if (view.openFiles.has(index)) view.openFiles.delete(index);
    else view.openFiles.add(index);
    render();
  },

  selectLine(fileIndex, line, extend) {
    const previous = view.selection;
    const sameFile = previous && previous.file === fileIndex;
    view.selection = extend && sameFile
      ? { file: fileIndex, from: previous.from, to: line }
      : { file: fileIndex, from: line, to: line };
    view.selectionNotice = "";
    paintSelection();
  },

  clearSelection() {
    view.selection = null;
    view.selectionNotice = "";
    paintSelection();
  },

  addSelectionToContext() {
    const selection = currentSelection(view);
    if (!selection) return;
    bridge.updateModelContext(selectionText(view.selectionPR, selection))
      .then(() => { view.selectionNotice = "Added; the model reads it with your next message."; })
      .catch(() => { view.selectionNotice = "This client did not take it."; })
      .finally(() => updateSelectionBar(view));
  },

  askAboutSelection() {
    const selection = currentSelection(view);
    if (!selection) return;
    const text = "Explain these lines, and point out anything that looks wrong.\n\n" + selectionText(view.selectionPR, selection);
    bridge.sendMessage(text)
      .then(() => { view.selectionNotice = "Sent."; })
      .catch(() => { view.selectionNotice = "This client did not take it."; })
      .finally(() => updateSelectionBar(view));
  },
};

// paintSelection marks the selected rows without redrawing the diff, which
// can run to thousands of rows.
function paintSelection() {
  for (const row of document.querySelectorAll("tr.selected")) row.classList.remove("selected");
  const selection = currentSelection(view);
  if (selection) {
    for (const line of selection.lines) {
      if (line.row) line.row.classList.add("selected");
    }
  }
  updateSelectionBar(view);
}

function payloadOf(result) {
  const payload = result && result._meta ? result._meta[VIEW_PAYLOAD_KEY] : undefined;
  if (!payload || typeof payload !== "object") return null;
  return payload;
}

function textOf(result) {
  const parts = (result && result.content) || [];
  return parts.filter((part) => part && part.type === "text").map((part) => part.text).join("\n");
}

bridge.on("ui/notifications/tool-input", (params) => {
  view.toolInput = (params && params.arguments) || null;
  render();
});

bridge.on("ui/notifications/tool-result", (result) => {
  const payload = payloadOf(result);
  if (result && result.isError) {
    view.failure = textOf(result) || "The view could not be built.";
  } else if (!payload) {
    view.failure = textOf(result) || "There is nothing for this view to show.";
  } else if (payload.version !== VIEW_PAYLOAD_VERSION) {
    view.failure = "This view was made by a different version of bb; ask for it again.";
  } else {
    view.payload = payload;
    view.failure = null;
    view.diffFiles = null;
    view.openFiles = new Set();
    view.selection = null;
  }
  render();
});

bridge.on("ui/notifications/tool-cancelled", (params) => {
  if (!view.payload) {
    view.failure = "Cancelled" + (params && params.reason ? ": " + params.reason : ".");
    render();
  }
});

bridge.on("ui/notifications/host-context-changed", (context) => applyHostContext(context));

function applyHostContext(context) {
  if (!context || typeof context !== "object") return;
  const root = document.documentElement;
  if (context.theme === "light" || context.theme === "dark") {
    root.dataset.theme = context.theme;
  }
  const styles = context.styles || {};
  for (const [name, value] of Object.entries(styles.variables || {})) {
    if (name.startsWith("--") && typeof value === "string") root.style.setProperty(name, value);
  }
  if (styles.css && typeof styles.css.fonts === "string") {
    document.getElementById("host-fonts").textContent = styles.css.fonts;
  }
  if (typeof context.locale === "string") view.locale = context.locale;
  if (Array.isArray(context.availableDisplayModes)) {
    view.canFullscreen = context.availableDisplayModes.includes("fullscreen");
  }
  const insets = context.safeAreaInsets;
  if (insets && typeof insets === "object") {
    const app = document.getElementById("app");
    for (const side of ["top", "right", "bottom", "left"]) {
      if (typeof insets[side] === "number" && insets[side] > 0) {
        app.style.setProperty("padding-" + side, "calc(" + insets[side] + "px + 14px)");
      }
    }
  }
  if (typeof context.displayMode === "string") {
    setDisplayMode(context.displayMode);
  } else {
    render();
  }
}

function setDisplayMode(mode) {
  view.fullscreen = mode === "fullscreen";
  document.documentElement.dataset.mode = view.fullscreen ? "fullscreen" : "inline";
  render();
}

function render() {
  const app = document.getElementById("app");
  view.selectionBar = null;
  app.replaceChildren(content());
  paintSelection();
}

function content() {
  if (view.failure) return notice(view.failure);
  if (view.payload) {
    try {
      switch (view.payload.kind) {
        case "pull_request": return renderPullRequest(view.payload, view);
        case "pull_requests": return renderPullRequests(view.payload, view);
        case "diff": return renderDiff(view.payload, view);
        default: return notice("This version of the page cannot show a " + view.payload.kind + ".");
      }
    } catch (error) {
      console.error(error);
      return notice("This view could not be drawn.", "danger");
    }
  }
  if (view.standalone) {
    return notice("This page draws bb's views inside a client that renders MCP Apps.");
  }
  return skeleton();
}

// skeleton holds the place of the view while its result is on the way.
function skeleton() {
  return el("div", { "aria-busy": "true", "aria-label": "Loading" },
    el("span", { class: "skeleton short" }),
    el("span", { class: "skeleton title" }),
    el("span", { class: "skeleton" }),
    el("span", { class: "skeleton short" }));
}

// The host sizes an inline view to what it reports.
function watchSize() {
  let queued = false;
  const report = () => {
    queued = false;
    const app = document.getElementById("app");
    const box = app.getBoundingClientRect();
    bridge.reportSize(Math.ceil(document.documentElement.clientWidth), Math.ceil(box.height));
  };
  const observer = new ResizeObserver(() => {
    if (!queued) {
      queued = true;
      requestAnimationFrame(report);
    }
  });
  observer.observe(document.getElementById("app"));
}

async function start() {
  render();
  watchSize();
  try {
    const result = await bridge.connect(CONNECT_TIMEOUT_MS);
    applyHostContext(result.hostContext);
  } catch (error) {
    view.standalone = true;
    render();
  }
}

start();
