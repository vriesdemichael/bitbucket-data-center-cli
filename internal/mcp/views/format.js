// Formatting shared by the views. The words are Bitbucket's own, from its UI
// (10.4): a reviewer approves or requests changes, a pull request has builds,
// tasks and comments, and a file in a diff is added, modified or renamed.

function initialsOf(name) {
  const words = String(name).trim().split(/[\s._@-]+/).filter(Boolean);
  if (words.length === 0) return "?";
  const first = words[0][0] || "";
  const last = words.length > 1 ? words[words.length - 1][0] : "";
  return (first + last).toUpperCase();
}

// colourFor gives each person a steady colour of their own for their
// initials, from their username.
function colourFor(key) {
  let hash = 0;
  for (const character of String(key)) {
    hash = (hash * 31 + character.codePointAt(0)) >>> 0;
  }
  return "hsl(" + (hash % 360) + " 52% 42%)";
}

// voteOf is a reviewer's status. Bitbucket's API still calls a request for
// changes NEEDS_WORK; its UI says "Changes requested".
function voteOf(reviewer) {
  if (reviewer.approved || reviewer.status === "APPROVED") return "approved";
  if (reviewer.status === "NEEDS_WORK") return "changes-requested";
  return "none";
}

function voteLabel(vote) {
  switch (vote) {
    case "approved": return "Approved";
    case "changes-requested": return "Changes requested";
    default: return "";
  }
}

function changesRequested(pr) {
  return (pr.reviewers || []).some((reviewer) => voteOf(reviewer) === "changes-requested");
}

// stateBadges is a pull request's state lozenge, as Bitbucket shows it: an
// open draft is a draft, and an open pull request a reviewer asked changes of
// says so.
function stateBadges(pr) {
  switch (pr.state) {
    case "MERGED": return badge("Merged", "success");
    case "DECLINED": return badge("Declined", "danger");
  }
  if (pr.draft) return badge("Draft");
  if (changesRequested(pr)) return badge("Changes requested", "warning-bold");
  return badge("Open", "info");
}

// Bitbucket's build states: the words of its build icon's tooltip, and an
// icon after the one it draws.
const BUILD_STATES = {
  SUCCESSFUL: { className: "successful", icon: "buildSuccessful", title: "Build successful", count: (n) => plural(n, "build") + " passed" },
  FAILED: { className: "failed", icon: "buildFailed", title: "Build failed", count: (n) => plural(n, "build") + " failed" },
  INPROGRESS: { className: "inprogress", icon: "buildInProgress", title: "Build in progress", count: (n) => plural(n, "build") + " in progress" },
  CANCELLED: { className: "cancelled", icon: "buildCancelled", title: "Build canceled", count: (n) => (n === 1 ? "1 build was" : formatNumber(n) + " builds were") + " canceled" },
  UNKNOWN: { className: "unknown", icon: "buildUnknown", title: "Build status unknown", count: (n) => plural(n, "build") + (n === 1 ? " has" : " have") + " unknown state" },
};

// BUILD_ORDER is the order builds are counted and listed in everywhere: what
// needs attention first, the passes last.
const BUILD_ORDER = ["FAILED", "INPROGRESS", "CANCELLED", "UNKNOWN", "SUCCESSFUL"];

// BUILD_COUNT_FIELDS names each state's field in check_counts.
const BUILD_COUNT_FIELDS = { FAILED: "failed", INPROGRESS: "in_progress", CANCELLED: "cancelled", UNKNOWN: "unknown", SUCCESSFUL: "successful" };

function buildStateOf(state) {
  return BUILD_STATES[String(state || "").toUpperCase()] || BUILD_STATES.UNKNOWN;
}

// countBuilds counts listed builds by state, in the shape check_counts has.
function countBuilds(builds) {
  const counts = { successful: 0, failed: 0, in_progress: 0, cancelled: 0, unknown: 0 };
  for (const build of builds || []) {
    const state = String(build.state || "").toUpperCase();
    counts[BUILD_COUNT_FIELDS[state] || "unknown"]++;
  }
  return counts;
}

// buildCountsOf is a pull request's build counts: Bitbucket's totals, which
// hold however many builds the view lists, or, where Bitbucket gave none, the
// listed builds counted, which are partial when the list was cut.
function buildCountsOf(pr) {
  if (pr.check_counts) return { counts: pr.check_counts, partial: false };
  if (pr.checks) return { counts: countBuilds(pr.checks), partial: Boolean(pr.checks_limit_reached) };
  return null;
}

function buildTotal(counts) {
  return BUILD_ORDER.reduce((sum, state) => sum + (counts[BUILD_COUNT_FIELDS[state]] || 0), 0);
}

// buildBadge is a list row's one build icon, as Bitbucket's dashboard draws
// it: the most pressing state with its count, and every count in the tooltip.
function buildBadge(counts) {
  if (!counts || buildTotal(counts) === 0) return null;
  const present = BUILD_ORDER.filter((state) => counts[BUILD_COUNT_FIELDS[state]] > 0);
  const look = buildStateOf(present[0]);
  const words = present.map((state) => buildStateOf(state).count(counts[BUILD_COUNT_FIELDS[state]])).join(", ");
  return el("span", { class: "build-state " + look.className, title: words },
    icon(look.icon, words, "icon-sm"), formatNumber(counts[BUILD_COUNT_FIELDS[present[0]]]));
}

// SNAPSHOT_STALE_MS is how old a view is before it says it may be out of
// date. A view shows what Bitbucket said when it was made, and a stored
// conversation can show it again days later.
const SNAPSHOT_STALE_MS = 60 * 60 * 1000;

// snapshotStamp says when the view's data was read from Bitbucket. A view
// made for a glance says it only once that is long enough ago to matter.
function snapshotStamp(generatedAt, locale, onlyWhenStale) {
  const time = Date.parse(generatedAt || "");
  if (!Number.isFinite(time)) return null;
  if (onlyWhenStale && Date.now() - time <= SNAPSHOT_STALE_MS) return null;
  const when = new Date(time);
  const sameDay = when.toDateString() === new Date().toDateString();
  let label;
  try {
    label = sameDay
      ? when.toLocaleTimeString(locale || undefined, { hour: "2-digit", minute: "2-digit" })
      : when.toLocaleString(locale || undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
  } catch {
    label = when.toISOString();
  }
  const stale = Date.now() - time > SNAPSHOT_STALE_MS;
  return el("span", {
    class: "stamp" + (stale ? " stale" : ""),
    title: "What Bitbucket said at " + when.toISOString() + (stale ? ". It may have changed since." : "."),
  }, "As of " + label + (stale ? " · may be out of date" : ""));
}

// Bitbucket's change types, as the lozenge on a file in a diff.
const CHANGE_TYPES = {
  added: { label: "Added", tone: "success" },
  deleted: { label: "Deleted", tone: "danger" },
  modified: { label: "Modified", tone: "info" },
  renamed: { label: "Renamed", tone: "warning" },
  moved: { label: "Moved", tone: "warning" },
  copied: { label: "Copied", tone: "info" },
};

function changeLozenge(status) {
  const type = CHANGE_TYPES[status] || CHANGE_TYPES.modified;
  return badge(type.label, type.tone);
}

function lastUpdated(milliseconds, locale) {
  return milliseconds ? "last updated " + relativeTime(milliseconds, locale) : "";
}

function relativeTime(milliseconds, locale) {
  if (!milliseconds) return "";
  const seconds = Math.round((milliseconds - Date.now()) / 1000);
  const units = [
    ["year", 31536000], ["month", 2592000], ["week", 604800], ["day", 86400], ["hour", 3600], ["minute", 60],
  ];
  let format;
  try {
    format = new Intl.RelativeTimeFormat(locale || undefined, { numeric: "auto" });
  } catch {
    format = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  }
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) return format.format(Math.round(seconds / size), unit);
  }
  return "a moment ago";
}

let numberFormat = new Intl.NumberFormat();

// setNumberLocale formats numbers as the host's locale does: 1,234 or 1.234.
function setNumberLocale(locale) {
  try {
    numberFormat = new Intl.NumberFormat(locale || undefined);
  } catch {
    numberFormat = new Intl.NumberFormat();
  }
}

function formatNumber(value) {
  return typeof value === "number" && Number.isFinite(value) ? numberFormat.format(value) : String(value);
}

// namesOf names people in a sentence: "Carol", "Carol and Bob", or
// "Carol, Bob and 3 others", naming at most max of them.
function namesOf(names, max) {
  if (names.length <= 1) return names.join("");
  if (names.length <= max) return names.slice(0, -1).join(", ") + " and " + names[names.length - 1];
  const others = names.length - max;
  return names.slice(0, max).join(", ") + " and " + plural(others, "other");
}

function plural(count, one, many) {
  return formatNumber(count) + " " + (count === 1 ? one : many || one + "s");
}

function repositoryOf(pr) {
  return pr.repository ? pr.repository.project_key + "/" + pr.repository.slug : "";
}

function shortCommit(commit) {
  return commit ? String(commit).slice(0, 11) : "";
}
