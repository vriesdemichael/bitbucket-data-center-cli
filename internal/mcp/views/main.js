// The page: connects to the host, and draws the show tool's result.

// Where the show tool puts what the view draws, in the result's _meta. The
// same key is in views.go; TestViewPageReadsThePayloadKeyTheToolWrites holds
// the two together.
const VIEW_PAYLOAD_KEY = "io.github.vriesdemichael.bb/view";
const VIEW_PAYLOAD_VERSION = 1;
const CONNECT_TIMEOUT_MS = 10000;
// A view whose result is slow to come says so, and one whose result has not
// come says how to go on, rather than hold its place with no reason given: bb
// answers show in seconds, and a client that drew the view but never passed
// the result on would leave it waiting for ever.
const SLOW_RESULT_MS = 15000;
const NO_RESULT_MS = 60000;

const bridge = createBridge({ name: "bb", version: "1" });

const view = {
  bridge,
  payload: null,
  toolInput: null,
  failure: null,
  standalone: false,
  // waited is how long the result has been on the way: "slow", then "none".
  waited: "",
  fullscreen: false,
  // canFullscreen is whether to ask the host for fullscreen: yes unless it
  // said which modes it has without fullscreen among them, or refused once.
  canFullscreen: true,
  expanded: false,
  // refusedLink is a link the host would not open, shown to open by hand, and
  // opensLinks whether the host said it opens links at all.
  refusedLink: null,
  opensLinks: true,
  locale: undefined,
  filter: "all",
  openFiles: new Set(),
  diffFiles: null,
  // diffFile is the file a diff shows beside its tree in fullscreen, and
  // focusPath a file the diff was opened at, by its path, until it is drawn.
  diffFile: null,
  focusPath: null,
  selection: null,
  selectionBar: null,
  selectionPR: null,
  selectionNotice: "",
  // unclamped are the long things and folded groups the person opened.
  unclamped: new Set(),
  // revealed is how many steps more of each growing list the person asked for.
  revealed: new Map(),
  // focusThread is a thread the view scrolls to once it is drawn.
  focusThread: null,
  // narrow is a screen too narrow for the overview's side panel, and phone
  // one where a list's rows wrap.
  narrow: false,
  phone: false,
  // hostTools is whether the host passes the view's tool calls to bb, which
  // keeps a view current, and tellsModel whether it takes context for the
  // model. refresh is how the view keeps current.
  hostTools: false,
  tellsModel: false,
  refresh: newRefreshState(),
  // history is what the view showed before what it shows now, to go back
  // to; opening is the kind on its way, and openFailure why the last one
  // could not be opened.
  history: [],
  opening: null,
  openFailure: null,
  // drafts are what the person is writing in the view, by where: a reply, a
  // comment on a line, a form's fields. They outlive a redraw.
  drafts: new Map(),
  // focusDraft is a draft to put the caret in once the view is drawn.
  focusDraft: null,
  // The pull request form's suggestions by field, the timers that ask for
  // them, and the pull request it made where it cannot be opened here.
  formSuggestions: {},
  suggestTimers: {},
  formDone: null,

  toggleClamp(key) {
    if (view.unclamped.has(key)) view.unclamped.delete(key);
    else view.unclamped.add(key);
    render();
  },

  reveal(key, step) {
    view.revealed.set(key, (view.revealed.get(key) || 0) + step);
    render();
  },

  unreveal(key) {
    view.revealed.delete(key);
    render();
  },

  // expand opens the view out: fullscreen where the host has it, in place
  // where it does not. Again, it goes back. A host that has not said whether
  // it has fullscreen is asked; one that answers with anything else, or not
  // at all, gets the view opened in place, and is not asked again.
  expand() {
    if (view.fullscreen) {
      bridge.requestDisplayMode("inline")
        .then((result) => setDisplayMode(result && result.mode ? result.mode : "inline"))
        .catch(() => setDisplayMode("inline"));
      return;
    }
    if (view.canFullscreen) {
      bridge.requestDisplayMode("fullscreen")
        .then((result) => {
          if (!result || !result.mode || result.mode === "fullscreen") setDisplayMode("fullscreen");
          else fullscreenRefused();
        })
        .catch(fullscreenRefused);
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
    showPayload(view, payload);
    view.history = [];
  }
  render();
  scheduleRefresh(view);
});

// A view the host takes down asks nothing more.
bridge.on("teardown", () => {
  view.refresh.torndown = true;
  scheduleRefresh(view);
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
  if (typeof context.locale === "string") {
    view.locale = context.locale;
    setNumberLocale(context.locale);
  }
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

// fullscreenRefused opens the view out in place, where the host would not go
// fullscreen, and a file picked to open there opens in place instead.
function fullscreenRefused() {
  view.canFullscreen = false;
  view.expanded = true;
  if (view.payload && view.payload.kind === "diff" && view.diffFile !== null) {
    view.openFiles.add(view.diffFile);
  }
  render();
}

// linkRefused shows a link the host would not open, so the person can open it
// by hand, rather than a click that seems to do nothing.
function linkRefused(url) {
  view.refusedLink = url;
  render();
}

function linkNotice() {
  const url = view.refusedLink;
  if (!url) return null;
  const address = el("span", { class: "mono link-address" }, url);
  const copy = el("button", { type: "button", class: "button ghost" }, "Copy");
  copy.addEventListener("click", () => {
    const select = () => {
      const range = document.createRange();
      range.selectNodeContents(address);
      window.getSelection().removeAllRanges();
      window.getSelection().addRange(range);
      copy.textContent = "Selected: press Ctrl+C";
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(url).then(() => { copy.textContent = "Copied"; }, select);
    } else {
      select();
    }
  });
  return el("div", { class: "notice link-notice", role: "status" },
    el("p", {}, view.opensLinks ? "This client did not open the link." : "This client does not open links."),
    el("div", { class: "link-row" },
      address,
      copy,
      el("button", { type: "button", class: "button ghost", onclick: () => { view.refusedLink = null; render(); } }, "Dismiss")));
}

// NARROW_WIDTH is the widest a screen is that has no room for the overview's
// side panel beside the description; view.css switches its layout there too.
const NARROW_WIDTH = 720;
const PHONE_WIDTH = 560;

// SCROLLING are the parts of a view that scroll on their own. A view is drawn
// again whenever something changes, and each keeps its place when it is.
const SCROLLING = ["#diff-main", ".diff-tree", ".details-main", ".details-side", ".page > .details"];

function render() {
  const app = document.getElementById("app");
  const places = SCROLLING.map((selector) => [selector, (document.querySelector(selector) || {}).scrollTop || 0]);
  // What the person is writing in keeps the caret across the redraw.
  const active = document.activeElement;
  const writing = active && active.dataset && active.dataset.draft
    ? { key: active.dataset.draft, start: active.selectionStart, end: active.selectionEnd }
    : null;
  const pagePlace = document.scrollingElement ? document.scrollingElement.scrollTop : 0;
  view.selectionBar = null;
  view.narrow = window.innerWidth <= NARROW_WIDTH;
  view.phone = window.innerWidth <= PHONE_WIDTH;
  app.replaceChildren(...[content(), linkNotice(), openNotice(view)].filter(Boolean));
  revealClampToggles();
  paintSelection();
  for (const [selector, top] of places) {
    const part = document.querySelector(selector);
    if (part && top > 0) part.scrollTop = top;
  }
  if (document.scrollingElement && pagePlace > 0) document.scrollingElement.scrollTop = pagePlace;
  // A draft just opened is scrolled to; one written in keeps its place.
  const opened = view.focusDraft;
  view.focusDraft = null;
  const caret = opened ? { key: opened } : writing;
  if (caret) {
    const input = [...document.querySelectorAll("[data-draft]")].find((node) => node.dataset.draft === caret.key);
    if (input && !input.disabled) {
      input.focus({ preventScroll: !opened });
      if (typeof caret.start === "number") input.setSelectionRange(caret.start, caret.end);
    }
  }
  // A thread the view was asked to show is scrolled to once it is drawn, as
  // Bitbucket scrolls to a comment it links to.
  if (view.focusThread !== null) {
    const target = document.getElementById("thread-" + view.focusThread);
    if (target) {
      target.scrollIntoView({ block: "start" });
      view.focusThread = null;
    }
  }
}

// A screen that turns narrow, or wide, gets the views laid out for it.
window.addEventListener("resize", () => {
  if ((window.innerWidth <= NARROW_WIDTH) !== view.narrow || (window.innerWidth <= PHONE_WIDTH) !== view.phone) render();
});

// revealClampToggles shows a clamped block's button, and fades its last line,
// only where the content runs past the clamp.
function revealClampToggles() {
  for (const holder of document.querySelectorAll(".clamp")) {
    const content = holder.firstElementChild;
    const toggle = holder.querySelector(".clamp-toggle");
    const overflowing = content.scrollHeight > content.clientHeight + 1;
    holder.classList.toggle("overflowing", overflowing);
    if (toggle) toggle.hidden = !holder.classList.contains("open") && !overflowing;
  }
}

function content() {
  if (view.failure) return notice(view.failure);
  if (view.payload) {
    try {
      switch (view.payload.kind) {
        case "pull_request": return renderPullRequest(view.payload, view);
        case "pull_requests": return renderPullRequests(view.payload, view);
        case "diff": return renderDiff(view.payload, view);
        case "pull_request_form": return renderPullRequestForm(view.payload, view);
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
  if (view.waited === "none") {
    return notice("bb's answer has not reached this view, so it has nothing to show. Ask for it in the conversation instead.");
  }
  return skeleton(view.waited === "slow");
}

// skeleton holds the place of the view while its result is on the way, and
// says so once it is slow to come.
function skeleton(slow) {
  return el("div", { "aria-busy": "true", "aria-label": "Loading" },
    el("span", { class: "skeleton short" }),
    el("span", { class: "skeleton title" }),
    el("span", { class: "skeleton" }),
    el("span", { class: "skeleton short" }),
    slow ? el("p", { class: "faint waiting", role: "status" }, "Still waiting for bb's answer.") : null);
}

// waitForResult marks the result slow to come, and then not come, while
// nothing has come; a result that comes after all is drawn as ever.
function waitForResult() {
  for (const [after, waited] of [[SLOW_RESULT_MS, "slow"], [NO_RESULT_MS, "none"]]) {
    setTimeout(() => {
      if (view.payload || view.failure) return;
      view.waited = waited;
      render();
    }, after);
  }
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
  watchVisibility(view);
  waitForResult();
  try {
    const result = await bridge.connect(CONNECT_TIMEOUT_MS);
    // A host that says what it can do, and leaves opening links out, opens
    // none: its links are shown to open by hand without asking. One that
    // leaves out passing tool calls keeps its views as they were drawn.
    const capabilities = result.hostCapabilities;
    if (capabilities && typeof capabilities === "object") {
      view.opensLinks = Boolean(capabilities.openLinks);
      view.hostTools = Boolean(capabilities.serverTools);
      view.tellsModel = Boolean(capabilities.updateModelContext);
    }
    applyHostContext(result.hostContext);
    scheduleRefresh(view);
  } catch (error) {
    view.standalone = true;
    render();
  }
}

start();
