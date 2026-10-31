// The diagnose page (ADR-101): measures what this client's host does with a view,
// which a server cannot see, and says what that means for bb's views. What needs
// no press runs when the page opens; what a host may need a gesture for, or what
// may open a popup, waits for one press each.
//
// The report does not depend on the route it is testing: it shows here as text to
// select, goes to the model through the view's context, and goes to the server
// where the host passes tool calls. It holds no credential, pull request or person,
// and not Bitbucket's address, which is scrubbed from all of them.

const SETTINGS = /*BB_DIAGNOSE_CONFIG*/{};
const DIAGNOSE_KEY = "io.github.vriesdemichael.bb/diagnose";
const CHECK_TOOL = "diagnose_check";
const CHECK_TIMEOUT_MS = 10000;
const LADDER_TIMEOUT_MS = 30000;
const WAIT_FOR_RESULT_MS = 8000;
const HOST_FOLLOWS_MS = 800;
const MAX_CHANGES = 50;
// The sizes a view's own calls try, in KiB, each until one does not arrive. The
// largest a view carries in practice is under a mebibyte, and it may reach some
// twenty with sixty avatars of the most Bitbucket allows.
const META_LADDER_KIB = [64, 256, 1024, 4096, 16384];
// An address that cannot resolve, so nothing leaves the machine even in a host
// that enforces no policy.
const UNLISTED = "https://bb-diagnose.invalid";
// Reserved for documentation, so a press opens nothing of anyone's.
const LINK_HTTPS = "https://example.com/?bb-diagnose";
const LINK_HTTP = "http://example.com/?bb-diagnose";
const TEST_MESSAGE = "This is a test from bb's diagnose page, to see whether a view can send a message. Please ignore it.";
const ONE_PIXEL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

const bridge = createBridge({ name: "bb-diagnose", version: "1" });

// The report, as far as it has got.
const report = {
  page: { build: SETTINGS.build, server_build: null, older_than_server: null },
  initialize: null,
  frame: null,
  tool_result: { arrived: false },
  meta_size: {},
  resources_read: {},
  tools_call: null,
  data_image: null,
  network: {},
  sandbox_csp: null,
  model_context: null,
  size: null,
  presses: {},
  display_mode_changes: [],
  declared_only: {},
  server: null,
  not_capturable: [
    "Whether the model sees a result's _meta or structuredContent: only the model's own requests show that.",
    "Where a client stores a result on disk.",
  ],
};

const state = { running: true, connected: false, standalone: false, copied: "", busy: null, spacer: null };

// effects says what the answers mean for bb's views, not only what they are.
function effects() {
  const out = [];
  const initialized = report.initialize;
  const capabilities = (initialized && initialized.host_capabilities) || {};
  if (initialized && !capabilities.serverTools) {
    out.push("The host declares no serverTools: views do not stay current, their tabs and actions open Bitbucket instead, and a pull request form cannot submit.");
  } else if (report.tools_call && (report.tools_call.error || report.tools_call.is_error)) {
    out.push("The host declares serverTools, and the check's call failed" + (report.tools_call.error ? " (" + report.tools_call.error + ")" : "") + ". Unless diagnose_check was excluded, views fail where they read themselves again or act.");
  }
  const listed = (initialized && initialized.available_display_modes) || [];
  if (listed.includes("fullscreen") && report.presses.fullscreen && report.presses.fullscreen.mode && report.presses.fullscreen.mode !== "fullscreen") {
    out.push("The host lists fullscreen and refused it: a view's Expand opens in place.");
  }
  if (capabilities.updateModelContext) {
    out.push("The host takes a view's context. Whether the model reads it is not known here, so Add to chat context promises only that it was taken.");
  } else if (initialized) {
    out.push("The host declares no updateModelContext: views offer no Add to chat context, and the model is not told what a person opens in one.");
  }
  if (initialized && !capabilities.message) {
    out.push("The host declares no message: views offer no Ask about this.");
  }
  if (initialized && !capabilities.openLinks) {
    out.push("The host declares no openLinks: a link in a view is shown to copy.");
  }
  if (report.page.older_than_server) {
    out.push("This page is older than the bb that is running: the client kept it. Start a new conversation to read it again.");
  }
  if (report.tool_result.arrived && !report.tool_result.meta) {
    const server = report.server || {};
    if (server.client && server.client.displays_views === false) {
      out.push("This client draws this page and told the server it displays no views, so show shows nothing in it.");
    } else if (server.meta_kib_sent > 0) {
      out.push("A result's _meta of " + server.meta_kib_sent + " KiB did not arrive, and it is where a view's data travels: a view that carries as much cannot draw from show's answer alone here. Ask for diagnose with a smaller meta_kib to see what gets through.");
    } else {
      out.push("A result's _meta did not arrive, and it is where a view's data travels: views cannot draw from show's answer alone here.");
    }
  }
  return out;
}

// addressForms are the ways Bitbucket's address gets written: as the server wrote
// them, and as a browser puts it in order, lower case and in punycode.
function addressForms() {
  const forms = (SETTINGS.forms || []).slice();
  try {
    const parsed = new URL(SETTINGS.bitbucket);
    forms.push(parsed.origin, parsed.host, parsed.hostname);
  } catch {
    // An address a browser cannot read writes no form of its own.
  }
  return [...new Set(forms)].filter(Boolean).sort((a, b) => b.length - a.length);
}

// ADDRESS finds the address in any string, in any case, as whole names: a host
// called "git" leaves "digit" alone, and a dot at the end of a sentence does not go
// on with it. The server's scrub of what it keeps reads the same.
const ADDRESS = addressForms().map((form) => new RegExp(
  "(^|[^a-z0-9-])" + form.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&") + "(?=$|[^a-z0-9.-]|\\.($|[^a-z0-9-]))", "gi"));

// scrub takes the address out of every string it is asked to write. Keys and the
// report's shape stay as they are.
function scrub(key, value) {
  if (typeof value !== "string") return value;
  return ADDRESS.reduce((text, matcher) => text.replace(matcher, "$1<bitbucket>"), value);
}

// reportText is the report as it leaves the page, in every direction.
function reportText() {
  return JSON.stringify(Object.assign({ effects: effects() }, report), scrub, 2);
}

function wait(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// outcome is a request's answer, or that it failed and why.
async function outcome(answer, keep) {
  try {
    const result = await answer;
    if (result && result.isError) return { answered: true, is_error: true };
    return Object.assign({ answered: true }, keep ? keep(result) : null);
  } catch (error) {
    return { answered: false, error: (error && error.message) || "no answer" };
  }
}

// The checks that need no press.

function checkInitialize(result) {
  const capabilities = result.hostCapabilities || {};
  const context = result.hostContext || {};
  report.initialize = {
    host_info: result.hostInfo || null,
    protocol_version: result.protocolVersion || null,
    host_capabilities: capabilities,
    host_context_keys: Object.keys(context).sort(),
    available_display_modes: context.availableDisplayModes || null,
    display_mode: context.displayMode || null,
    container_dimensions: context.containerDimensions || null,
    platform: context.platform || null,
  };
  report.declared_only.logging = Boolean(capabilities.logging);
}

function checkFrame() {
  const ancestors = window.location.ancestorOrigins;
  report.frame = { origin: window.location.origin, ancestors: ancestors ? [...ancestors] : null };
}

function noteResult(result) {
  const payload = result && result._meta ? result._meta[DIAGNOSE_KEY] : undefined;
  report.tool_result = {
    arrived: true,
    content: Boolean(result && Array.isArray(result.content) && result.content.length > 0),
    structured_content: Boolean(result && result.structuredContent),
    meta: Boolean(payload),
  };
  // The answer, less the page's own last report, or each diagnose would nest it.
  if (result && result.structuredContent) {
    report.server = Object.assign({}, result.structuredContent);
    delete report.server.page_report;
  }
  // The fresh answer's build against this page's: a host may keep a page, and a
  // fresh answer is this bb's whichever route it took.
  const build = payload ? payload.build : result && result.structuredContent && result.structuredContent.build && result.structuredContent.build.page;
  if (build) {
    report.page.server_build = build;
    report.page.older_than_server = build !== SETTINGS.build;
  }
  if (payload) report.meta_size.model_call_kib = payload.meta_kib;
}

async function checkResources() {
  const read = (uri) => outcome(bridge.readResource(uri, CHECK_TIMEOUT_MS), (result) => {
    const first = (result && result.contents && result.contents[0]) || {};
    return { type: first.mimeType || null, has_text: typeof first.text === "string", has_blob: typeof first.blob === "string" };
  });
  const [page, image, text, pdf, template] = await Promise.all([
    read("ui://bb/diagnose/page.html"),
    read("ui://bb/diagnose/fixture.png"),
    read("ui://bb/diagnose/fixture.txt"),
    read("ui://bb/diagnose/fixture.pdf"),
    read("ui://bb/diagnose/template/any"),
  ]);
  report.resources_read = { page, image, text, pdf, template };
}

async function checkToolCall() {
  report.tools_call = await outcome(bridge.callTool(CHECK_TOOL, {}, CHECK_TIMEOUT_MS), (result) => ({
    reached_server: Boolean(result && result.structuredContent),
  }));
  if (!report.tools_call.answered || report.tools_call.is_error) return;
  // The largest result that reaches a view through its own calls, which a host
  // may treat otherwise than the model's.
  for (const kib of META_LADDER_KIB) {
    const result = await outcome(bridge.callTool(CHECK_TOOL, { meta_kib: kib }, LADDER_TIMEOUT_MS), (answer) => ({
      arrived: Boolean(answer && answer._meta && answer._meta[DIAGNOSE_KEY]),
    }));
    if (!result.arrived) {
      report.meta_size.view_call_failed_at_kib = kib;
      break;
    }
    report.meta_size.view_call_largest_kib = kib;
    render();
  }
}

function drawsImage(url, timeout) {
  return new Promise((resolve) => {
    const image = new Image();
    const timer = setTimeout(() => resolve(false), timeout);
    image.onload = () => { clearTimeout(timer); resolve(image.naturalWidth > 0); };
    image.onerror = () => { clearTimeout(timer); resolve(false); };
    image.src = url;
  });
}

// A policy's violation is an event, and it says what was blocked.
const violations = [];
document.addEventListener("securitypolicyviolation", (event) => {
  violations.push({ blocked: event.blockedURI, directive: event.effectiveDirective });
});

function originOfURL(url) {
  try {
    return new URL(url).origin;
  } catch {
    return null;
  }
}

// violationFor is the directive that stopped a kind of request to this address.
function violationFor(url, kind) {
  const found = violations.find((violation) => originOfURL(violation.blocked) === originOfURL(url) && violation.directive === kind);
  return found ? found.directive : null;
}

async function fetchReaches(url) {
  try {
    await fetch(url, { mode: "no-cors", credentials: "omit", cache: "no-store", signal: AbortSignal.timeout(CHECK_TIMEOUT_MS) });
    return true;
  } catch {
    return false;
  }
}

// checkNetwork asks for a fetch and an image, of nothing anyone wrote: Bitbucket's
// status, and the default avatar of a user that does not exist. Their outcome goes
// into the report, and nothing they answer does.
async function checkNetwork() {
  const base = SETTINGS.bitbucket;
  const image = (url) => drawsImage(url, CHECK_TIMEOUT_MS);
  const targets = {
    bitbucket_fetch: { url: base + "/status", kind: "connect-src", look: fetchReaches },
    bitbucket_image: { url: base + "/users/bb-diagnose/avatar.png?s=16", kind: "img-src", look: image },
    unlisted_fetch: { url: UNLISTED + "/status", kind: "connect-src", look: fetchReaches },
    unlisted_image: { url: UNLISTED + "/image.png", kind: "img-src", look: image },
  };
  const names = Object.keys(targets);
  const reached = await Promise.all(names.map((name) => targets[name].look(targets[name].url)));
  // The violation's event comes a moment after the failure it explains.
  await wait(100);
  names.forEach((name, index) => {
    report.network[name] = { got_through: reached[index], policy_violation: violationFor(targets[name].url, targets[name].kind) };
  });
  report.sandbox_csp = {
    host_says_approved: ((report.initialize && report.initialize.host_capabilities.sandbox) || {}).csp || null,
    got_through: names.filter((name) => report.network[name].got_through),
    blocked_by_policy: names.filter((name) => report.network[name].policy_violation),
  };
}

// watchSize tells the host how tall the page is, which it sizes an inline frame to.
function watchSize() {
  let queued = false;
  const app = document.getElementById("app");
  new ResizeObserver(() => {
    if (queued) return;
    queued = true;
    requestAnimationFrame(() => {
      queued = false;
      bridge.reportSize(Math.ceil(document.documentElement.clientWidth), Math.ceil(app.getBoundingClientRect().height));
    });
  }).observe(app);
}

// checkSize grows the view past its frame and shrinks it again, and looks at
// whether the frame follows what it reports and whether a scrollbar stays.
async function checkSize() {
  if (document.documentElement.dataset.mode === "fullscreen") {
    report.size = { measured: false, why: "the view is in fullscreen" };
    return;
  }
  const app = document.getElementById("app");
  await wait(HOST_FOLLOWS_MS);
  const before = window.innerHeight;
  state.spacer = el("div");
  state.spacer.style.height = Math.max(400, before + 200 - app.getBoundingClientRect().height) + "px";
  render();
  await wait(HOST_FOLLOWS_MS);
  const grown = window.innerHeight;
  state.spacer = null;
  render();
  await wait(HOST_FOLLOWS_MS);
  const shrunk = window.innerHeight;
  report.size = {
    measured: true,
    follows_growth: grown > before,
    follows_shrinking: shrunk < grown,
    scrollbar_px_after: window.innerWidth - document.documentElement.clientWidth,
  };
}

async function checkModelContext() {
  report.model_context = await outcome(bridge.updateModelContext("bb's diagnose page is checking what this client can do with a view."));
}

// share sends the report where it can go: to the model through the view's context,
// and to the server where the host passes tool calls, for the next diagnose.
async function share() {
  const text = reportText();
  await outcome(bridge.updateModelContext("What bb's diagnose page measured in this client:\n\n" + text));
  if (report.tools_call && report.tools_call.answered && !report.tools_call.is_error) {
    await outcome(bridge.callTool(CHECK_TOOL, { report: JSON.parse(text) }, CHECK_TIMEOUT_MS));
  }
}

// shareSoon sends a report that has changed since, once it settles.
let shareTimer = null;
function shareSoon() {
  if (state.running) return;
  clearTimeout(shareTimer);
  shareTimer = setTimeout(share, 300);
}

async function runChecks() {
  checkFrame();
  report.data_image = await drawsImage(ONE_PIXEL, CHECK_TIMEOUT_MS);
  render();
  await Promise.all([checkResources(), checkToolCall(), checkNetwork(), checkModelContext()]);
  render();
  await checkSize();
  state.running = false;
  render();
  await share();
}

// The checks that take one press each.

const PRESSES = {
  link_own_scheme: {
    label: () => "ui/open-link, " + SETTINGS.scheme + " as Bitbucket is",
    run: () => outcome(bridge.openLink(SETTINGS.scheme === "http" ? LINK_HTTP : LINK_HTTPS)),
  },
  link_other_scheme: {
    label: () => "ui/open-link, " + (SETTINGS.scheme === "http" ? "https" : "http"),
    run: () => outcome(bridge.openLink(SETTINGS.scheme === "http" ? LINK_HTTPS : LINK_HTTP)),
  },
  window_open: {
    label: () => "window.open",
    asks: "Did a page open?",
    run: async () => ({ answered: true, returned_a_window: Boolean(window.open(LINK_HTTPS, "_blank")) }),
  },
  clipboard: {
    label: () => "Clipboard write",
    run: async () => {
      try {
        await navigator.clipboard.writeText("bb diagnose");
        return { answered: true };
      } catch (error) {
        return { answered: false, error: (error && error.name) || "no clipboard" };
      }
    },
  },
  fullscreen: {
    label: () => "ui/request-display-mode, fullscreen",
    run: () => outcome(bridge.requestDisplayMode("fullscreen"), (result) => ({ mode: (result && result.mode) || null })),
  },
  inline: {
    label: () => "ui/request-display-mode, inline",
    run: () => outcome(bridge.requestDisplayMode("inline"), (result) => ({ mode: (result && result.mode) || null })),
  },
  message: {
    label: () => "ui/message, a labelled test sentence",
    asks: "Did it arrive in the conversation?",
    run: () => outcome(bridge.sendMessage(TEST_MESSAGE)),
  },
};

async function press(name) {
  const check = PRESSES[name];
  state.busy = name;
  render();
  try {
    report.presses[name] = await check.run();
  } catch (error) {
    report.presses[name] = { answered: false, error: (error && error.message) || "it failed" };
  }
  state.busy = null;
  render();
  shareSoon();
}

// said is the person's own word on what a press did, which nothing else can see.
function said(name, seen) {
  report.presses[name].person_says = seen;
  render();
  shareSoon();
}

// The page.

function words(value) {
  if (value === null || value === undefined) return "not measured";
  return JSON.stringify(value, scrub);
}

function checkRow(label, value) {
  return el("div", { class: "check-row" }, el("dt", {}, label), el("dd", { class: "mono" }, words(value)));
}

function pressRow(name) {
  const check = PRESSES[name];
  const done = report.presses[name];
  return el("div", { class: "check-row press-row" },
    el("button", { type: "button", class: "button", disabled: Boolean(state.busy), "data-press": name, onclick: () => press(name) }, check.label()),
    done ? el("span", { class: "mono" }, words(done)) : null,
    done && check.asks && done.person_says === undefined
      ? [el("span", {}, check.asks),
        el("button", { type: "button", class: "button ghost", "data-said": name + "-yes", onclick: () => said(name, true) }, "Yes"),
        el("button", { type: "button", class: "button ghost", "data-said": name + "-no", onclick: () => said(name, false) }, "No")]
      : null);
}

// plainLinkRow is a link as a page writes one, which only the person can say
// anything about: it may open, be swallowed, or be refused.
function plainLinkRow() {
  const link = document.createElement("a");
  link.setAttribute("href", LINK_HTTPS);
  link.setAttribute("target", "_blank");
  link.setAttribute("rel", "noopener");
  link.textContent = "A plain link";
  link.addEventListener("click", () => {
    report.presses.plain_link = report.presses.plain_link || { pressed: true };
    render();
    shareSoon();
  });
  const done = report.presses.plain_link;
  return el("div", { class: "check-row press-row" }, link,
    done && done.person_says === undefined
      ? [el("span", {}, "Did a page open?"),
        el("button", { type: "button", class: "button ghost", "data-said": "plain_link-yes", onclick: () => said("plain_link", true) }, "Yes"),
        el("button", { type: "button", class: "button ghost", "data-said": "plain_link-no", onclick: () => said("plain_link", false) }, "No")]
      : done ? el("span", { class: "mono" }, words(done)) : null);
}

// copyReport copies the report, or selects it for the person where the clipboard
// is refused: after the page is drawn again, which would take a selection with it.
function copyReport(box) {
  const select = () => {
    state.copied = "Selected: press Ctrl+C.";
    render();
    const drawn = document.querySelector(".report-text");
    drawn.focus();
    drawn.select();
  };
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(box.value).then(() => {
      state.copied = "Copied.";
      render();
    }, select);
  } else {
    select();
  }
}

function render() {
  const app = document.getElementById("app");
  if (state.standalone) {
    app.replaceChildren(el("p", { class: "notice" }, "This page measures what a client that renders MCP Apps can do. Ask for diagnose in one."));
    return;
  }
  const meant = effects();
  const box = el("textarea", { class: "report-text", readonly: true, rows: 12, "aria-label": "The report" });
  box.value = reportText();
  const network = report.network;
  app.replaceChildren(...[el("div", { class: "diagnose" },
    el("h1", {}, "What this client can do with bb's views"),
    state.running ? el("p", { class: "faint", role: "status" }, "Checking…") : null,
    el("section", {},
      el("h2", {}, "What this means"),
      meant.length > 0
        ? el("ul", { class: "effects" }, meant.map((effect) => el("li", {}, effect)))
        : el("p", { class: "faint" }, state.running ? "So far, nothing that limits a view." : "Nothing here limits a view.")),
    el("section", {},
      el("h2", {}, "The report"),
      el("p", { class: "faint" }, "It holds no credential, pull request or person, and not Bitbucket's address, so it can be pasted into an issue."),
      box,
      el("div", { class: "form-actions" },
        el("button", { type: "button", class: "button", "data-copy": "report", onclick: () => copyReport(box) }, "Copy"),
        state.copied ? el("span", { class: "faint", role: "status" }, state.copied) : null)),
    el("section", {},
      el("h2", {}, "One press each"),
      el("p", { class: "faint" }, "A host may need a gesture for these, and some open a popup."),
      Object.keys(PRESSES).map(pressRow),
      plainLinkRow()),
    el("section", {},
      el("h2", {}, "What was measured"),
      el("dl", { class: "checks" },
        checkRow("Host", report.initialize && report.initialize.host_info),
        checkRow("Host capabilities", report.initialize && report.initialize.host_capabilities),
        checkRow("Display modes", report.initialize && report.initialize.available_display_modes),
        checkRow("Container", report.initialize && report.initialize.container_dimensions),
        checkRow("Frame", report.frame),
        checkRow("This page's build", report.page),
        checkRow("The tool result", report.tool_result),
        checkRow("Size of _meta", report.meta_size),
        checkRow("resources/read", report.resources_read),
        checkRow("tools/call", report.tools_call),
        checkRow("data: image draws", report.data_image),
        checkRow("Network", network),
        checkRow("Policy", report.sandbox_csp),
        checkRow("ui/update-model-context", report.model_context),
        checkRow("Size", report.size),
        checkRow("Display mode changes", report.display_mode_changes),
        checkRow("Declared only", report.declared_only))),
    el("section", {},
      el("h2", {}, "What this cannot find out"),
      el("ul", { class: "effects faint" }, report.not_capturable.map((item) => el("li", {}, item))))), state.spacer].filter(Boolean));
}

function applyTheme(context) {
  if (!context || typeof context !== "object") return;
  const root = document.documentElement;
  if (context.theme === "light" || context.theme === "dark") root.dataset.theme = context.theme;
  for (const [name, value] of Object.entries((context.styles && context.styles.variables) || {})) {
    if (name.startsWith("--") && typeof value === "string") root.style.setProperty(name, value);
  }
  if (typeof context.displayMode === "string") {
    root.dataset.mode = context.displayMode === "fullscreen" ? "fullscreen" : "inline";
  }
}

bridge.on("ui/notifications/tool-result", (result) => {
  noteResult(result);
  render();
  shareSoon();
  if (state.waitingForResult) state.waitingForResult();
});

// The host's own fullscreen button shows here, as a host says it changed.
bridge.on("ui/notifications/host-context-changed", (context) => {
  applyTheme(context);
  if (context && (context.displayMode || context.containerDimensions) && report.display_mode_changes.length < MAX_CHANGES) {
    report.display_mode_changes.push({ display_mode: context.displayMode || null, container_dimensions: context.containerDimensions || null });
    render();
    shareSoon();
  }
});

async function start() {
  render();
  try {
    const result = await bridge.connect(CHECK_TIMEOUT_MS);
    checkInitialize(result);
    applyTheme(result.hostContext);
    state.connected = true;
    watchSize();
  } catch {
    state.standalone = true;
    render();
    return;
  }
  render();
  // Give the result a moment: the checks that follow do not depend on it, and a
  // host may never pass it on.
  if (!report.tool_result.arrived) {
    await Promise.race([new Promise((resolve) => { state.waitingForResult = resolve; }), wait(WAIT_FOR_RESULT_MS)]);
  }
  await runChecks();
}

start();
