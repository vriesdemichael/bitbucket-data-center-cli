// Keeping a view current (ADR-101). A view draws what its result carries,
// then, while it is on screen, asks bb whether that has changed: refresh_view
// reads the view's data again, and answers with it only when its fingerprint
// differs from the one the view holds.

const REFRESH_TOOL = "refresh_view";

// How long a view waits before it asks again: fifteen seconds while something
// is in motion, such as a running build or auto-merge waiting on one; two
// minutes otherwise, twice as long after each answer that nothing changed,
// up to ten. A failed refresh waits as long as a quiet one.
const REFRESH_BUSY_MS = 15 * 1000;
const REFRESH_IDLE_MS = 2 * 60 * 1000;
const REFRESH_MAX_MS = 10 * 60 * 1000;
const REFRESH_TIMEOUT_MS = 60 * 1000;

function newRefreshState() {
  return {
    // onScreen is whether any of the view is in the host's viewport, and
    // visible whether the host's window is.
    onScreen: false,
    visible: document.visibilityState !== "hidden",
    torndown: false,
    busy: false,
    timer: null,
    idleMs: REFRESH_IDLE_MS,
    attemptedAt: 0,
    failure: null,
    // pending is a changed diff the person has not asked to see yet.
    pending: null,
  };
}

// canRefresh is whether the view can ask at all: the host passes a view's
// tool calls to the server, and the data says what it answers.
function canRefresh(view) {
  const payload = view.payload;
  return Boolean(view.hostTools && payload && payload.show && payload.fingerprint && !view.refresh.torndown);
}

// inMotion is a view whose data is bound to change soon.
function inMotion(payload) {
  const running = (pr) => {
    if (!pr) return false;
    if (pr.check_counts) return pr.check_counts.in_progress > 0;
    return (pr.checks || []).some((check) => String(check.state).toUpperCase() === "INPROGRESS");
  };
  switch (payload.kind) {
    case "pull_request":
      return running(payload.pull_request) || Boolean(payload.pull_request && payload.pull_request.auto_merge);
    case "pull_requests":
      return (payload.pull_requests || []).some(running);
    default:
      return false;
  }
}

// settled is a view with nothing left to wait for: a pull request that is
// closed, with no build running. A list is never settled.
function settled(payload) {
  if (payload.kind === "pull_requests") return false;
  const pr = payload.pull_request;
  return Boolean(pr && pr.state && pr.state !== "OPEN") && !inMotion(payload);
}

// scheduleRefresh sets the next time the view asks, or none: off screen,
// behind a hidden window, while a changed diff waits for the person, once
// settled, and after teardown, it does not ask. A view whose data is older
// than its interval, such as one in a conversation opened again, asks as soon
// as it is on screen.
function scheduleRefresh(view) {
  const state = view.refresh;
  clearTimeout(state.timer);
  state.timer = null;
  if (!canRefresh(view) || state.busy || state.pending || !state.onScreen || !state.visible || settled(view.payload)) return;
  const interval = inMotion(view.payload) && !state.failure ? REFRESH_BUSY_MS : state.idleMs;
  const since = Math.max(Date.parse(view.payload.generated_at || "") || 0, state.attemptedAt);
  state.timer = setTimeout(() => refreshView(view, false), Math.max(0, since + interval - Date.now()));
}

// refreshView asks bb for the view's data again. byPerson is a refresh the
// person asked for, which shows a changed diff at once.
async function refreshView(view, byPerson) {
  const state = view.refresh;
  if (!canRefresh(view) || state.busy) return;
  clearTimeout(state.timer);
  state.busy = true;
  state.attemptedAt = Date.now();
  const asked = view.payload;
  if (byPerson) render();
  try {
    const result = await view.bridge.callTool(REFRESH_TOOL,
      Object.assign({}, asked.show, { since: asked.fingerprint }), REFRESH_TIMEOUT_MS);
    // A new result arrived while this one was on its way; it is newer.
    if (view.payload !== asked) return;
    if (!result || result.isError) throw new Error(textOf(result) || "bb could not read it again.");
    const answer = result.structuredContent || {};
    state.failure = null;
    if (answer.changed) {
      const payload = payloadOf(result);
      if (!payload || payload.version !== VIEW_PAYLOAD_VERSION) throw new Error("bb answered with data this page cannot draw.");
      state.idleMs = REFRESH_IDLE_MS;
      if (payload.kind === "diff" && !byPerson) {
        state.pending = { payload, text: textOf(result) };
      } else {
        showRefreshed(view, payload, textOf(result));
      }
    } else {
      if (typeof answer.generated_at === "string") asked.generated_at = answer.generated_at;
      if (!byPerson) state.idleMs = Math.min(state.idleMs * 2, REFRESH_MAX_MS);
    }
  } catch (error) {
    state.failure = (error && error.message) || "bb could not read it again.";
    state.idleMs = Math.min(state.idleMs * 2, REFRESH_MAX_MS);
  } finally {
    state.busy = false;
    render();
    scheduleRefresh(view);
  }
}

// showRefreshed puts changed data in front of the person, keeping what they
// opened. A diff starts over, since its files and lines have moved. The model
// is told, so it answers the person's next message from what they see.
function showRefreshed(view, payload, text) {
  if (payload.kind === "diff") {
    view.diffFiles = null;
    view.openFiles = new Set();
    view.selection = null;
    view.selectionNotice = "";
    view.focusFile = null;
    for (const key of [...view.revealed.keys()]) {
      if (key.startsWith("file-")) view.revealed.delete(key);
    }
  }
  view.payload = payload;
  view.refresh.pending = null;
  if (view.tellsModel && text) view.bridge.updateModelContext(text).catch(() => {});
}

// refreshNotice offers a diff that changed while the person may have been
// reading it, rather than moving its lines under them.
function refreshNotice(view) {
  const pending = view.refresh.pending;
  if (!pending) return null;
  return el("div", { class: "notice refresh-notice", role: "status" },
    el("p", {}, "This pull request has changed since this diff was read."),
    el("button", {
      type: "button",
      class: "button",
      onclick: () => {
        showRefreshed(view, pending.payload, pending.text);
        render();
        scheduleRefresh(view);
      },
    }, icon("refresh"), "Show the new diff"));
}

// refreshButton reads the view again when the person asks.
function refreshButton(view) {
  if (!canRefresh(view)) return null;
  const busy = view.refresh.busy;
  return el("button", {
    type: "button",
    class: "button ghost icon-only refresh-button" + (busy ? " busy" : ""),
    title: busy ? "Reading it again from Bitbucket" : "Read it again from Bitbucket",
    "aria-label": busy ? "Refreshing" : "Refresh",
    disabled: busy,
    onclick: () => refreshView(view, true),
  }, icon("refresh"));
}

// watchVisibility follows whether the view is on screen: in the host's
// viewport, in a window that is not hidden.
function watchVisibility(view) {
  if (typeof IntersectionObserver === "function") {
    new IntersectionObserver((entries) => {
      view.refresh.onScreen = entries[entries.length - 1].isIntersecting;
      scheduleRefresh(view);
    }).observe(document.getElementById("app"));
  } else {
    view.refresh.onScreen = true;
  }
  document.addEventListener("visibilitychange", () => {
    view.refresh.visible = document.visibilityState !== "hidden";
    scheduleRefresh(view);
  });
}
