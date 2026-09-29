// Opening something else in a view (ADR-101): a pull request's diff or
// comments from its card, its card from a list, the list in another state.
// The view reads it through refresh_view, keeps what it showed to go back to,
// and tells the model what the person now looks at.

const OPEN_TIMEOUT_MS = 60 * 1000;

// PULL_REQUEST_TABS are a pull request's views, named as Bitbucket's tabs
// and its comments are.
const PULL_REQUEST_TABS = [
  { kind: "pull_request", label: "Overview" },
  { kind: "diff", label: "Diff" },
  { kind: "threads", label: "Comments" },
];

// LIST_STATES and LIST_ROLES are what a list can be asked for again: the
// states list_pull_requests takes, and, for the person's own pull requests,
// their role in them.
const LIST_STATES = [
  { value: "open", label: "Open" },
  { value: "closed", label: "Merged and declined" },
  { value: "all", label: "All states" },
];
const LIST_ROLES = [
  { value: "", label: "Any role" },
  { value: "REVIEWER", label: "Reviewing" },
  { value: "AUTHOR", label: "Authored" },
  { value: "PARTICIPANT", label: "Participating" },
];

function offersOf(view) {
  return (view.payload && view.payload.offers) || { kinds: [], tools: [] };
}

// canOpen is whether this view can open a kind here: the host passes tool
// calls, and the server shows that kind.
function canOpen(view, kind) {
  return view.hostTools && !view.refresh.torndown && (offersOf(view).kinds || []).includes(kind);
}

// canCall is whether this view can call one of the model's tools for the
// person here.
function canCall(view, tool) {
  return view.hostTools && !view.refresh.torndown && (offersOf(view).tools || []).includes(tool);
}

// openInView reads what show would put in front of the person, and shows it
// in this view. replace is a list asked for again, which takes the place of
// the one it was rather than one to go back to.
async function openInView(view, show, replace) {
  if (view.opening) return;
  view.opening = show.kind;
  view.openFailure = null;
  render();
  try {
    const result = await view.bridge.callTool(REFRESH_TOOL, show, OPEN_TIMEOUT_MS);
    if (!result || result.isError) throw new Error(textOf(result) || "bb could not open it.");
    const payload = payloadOf(result);
    if (!payload || payload.version !== VIEW_PAYLOAD_VERSION) throw new Error("bb answered with data this page cannot draw.");
    if (!replace && view.payload) view.history.push(snapshotOf(view));
    const keep = replace ? { filter: view.filter } : null;
    showPayload(view, payload);
    if (keep) view.filter = keep.filter;
    if (view.tellsModel) view.bridge.updateModelContext(textOf(result)).catch(() => {});
  } catch (error) {
    view.openFailure = (error && error.message) || "bb could not open it.";
  } finally {
    view.opening = null;
    render();
    scheduleRefresh(view);
  }
}

function snapshotOf(view) {
  return {
    payload: view.payload,
    filter: view.filter,
    revealed: new Map(view.revealed),
    unclamped: new Set(view.unclamped),
    expanded: view.expanded,
  };
}

// showPayload puts a payload in front of the person as a view of its own:
// what the person opened in the last one does not carry over.
function showPayload(view, payload) {
  view.payload = payload;
  view.failure = null;
  view.diffFiles = null;
  view.openFiles = new Set();
  view.unclamped = new Set();
  view.revealed = new Map();
  view.focusFile = null;
  view.focusThread = null;
  view.selection = null;
  view.selectionNotice = "";
  view.filter = "all";
  view.expanded = false;
  view.drafts = new Map();
  view.reviewing = null;
  view.reviewError = null;
  view.refresh.pending = null;
  view.refresh.failure = null;
  view.refresh.idleMs = REFRESH_IDLE_MS;
  view.refresh.attemptedAt = 0;
}

// goBack shows what the view showed before, as the person left it. Data read
// a while ago is brought up to date as soon as it is back on screen.
function goBack(view) {
  const previous = view.history.pop();
  if (!previous) return;
  showPayload(view, previous.payload);
  view.filter = previous.filter;
  view.revealed = previous.revealed;
  view.unclamped = previous.unclamped;
  view.expanded = previous.expanded;
  render();
  scheduleRefresh(view);
}

// backButton goes back to what the view showed before, when it showed
// something else first.
function backButton(view) {
  const previous = view.history[view.history.length - 1];
  if (!previous) return null;
  return el("button", { type: "button", class: "button ghost back-button", onclick: () => goBack(view) },
    icon("arrowLeft"), "Back to " + kindWords(previous.payload.kind));
}

function kindWords(kind) {
  switch (kind) {
    case "pull_request": return "the pull request";
    case "pull_requests": return "the list";
    case "diff": return "the diff";
    case "threads": return "the comments";
    default: return "the last view";
  }
}

// pullRequestTabs are buttons to the pull request's other views, those the
// server shows, opened in this view.
function pullRequestTabs(pr, current, view) {
  if (!pr || !pr.repository) return [];
  return PULL_REQUEST_TABS
    .filter((tab) => tab.kind !== current && canOpen(view, tab.kind))
    .map((tab) => el("button", {
      type: "button",
      class: "button" + (view.opening === tab.kind ? " busy" : ""),
      disabled: Boolean(view.opening),
      onclick: () => openInView(view, { kind: tab.kind, project: pr.repository.project_key, repo: pr.repository.slug, id: String(pr.id) }),
    }, tab.label));
}

// openPullRequest opens a pull request's card in this view, or in Bitbucket
// where the view cannot.
function openPullRequest(pr, view) {
  if (pr.repository && canOpen(view, "pull_request")) {
    openInView(view, { kind: "pull_request", project: pr.repository.project_key, repo: pr.repository.slug, id: String(pr.id) });
    return;
  }
  openLink(view.bridge, pr.url);
}

// listScope asks for the list again, in another state or, for the person's
// own pull requests, another role.
function listScope(payload, view) {
  const show = payload.show;
  if (!show || !canOpen(view, "pull_requests")) return null;
  const state = String(show.state || "open").toLowerCase();
  const reask = (change) => openInView(view, Object.assign({}, show, change), true);
  const choose = (label, options, current, onchange) => el("label", { class: "list-scope" },
    el("span", { class: "visually-hidden" }, label),
    el("select", { disabled: Boolean(view.opening), onchange: (event) => onchange(event.target.value) },
      options.map((option) => el("option", { value: option.value, selected: option.value === current }, option.label))));
  return el("div", { class: "list-scopes" },
    choose("State", LIST_STATES, state, (value) => reask({ state: value })),
    show.repo ? null : choose("Role", LIST_ROLES, show.role || "", (value) => reask({ role: value })));
}

// openNotice says what could not be opened, over the view.
function openNotice(view) {
  if (!view.openFailure) return null;
  return el("div", { class: "notice danger float-notice", role: "alert" },
    el("p", {}, "This could not be opened: " + view.openFailure),
    el("button", { type: "button", class: "button ghost", onclick: () => { view.openFailure = null; render(); } }, "Dismiss"));
}
