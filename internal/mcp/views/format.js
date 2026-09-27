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
  CANCELLED: { className: "cancelled", icon: "buildCancelled", title: "Build canceled", count: (n) => (n === 1 ? "1 build was" : n + " builds were") + " canceled" },
  UNKNOWN: { className: "unknown", icon: "buildUnknown", title: "Build status unknown", count: (n) => plural(n, "build") + (n === 1 ? " has" : " have") + " unknown state" },
};

function buildStateOf(state) {
  return BUILD_STATES[String(state || "").toUpperCase()] || BUILD_STATES.UNKNOWN;
}

// countBuilds counts a card's builds by state, in the shape a list's
// check_counts has.
function countBuilds(builds) {
  const counts = { successful: 0, failed: 0, in_progress: 0, cancelled: 0, unknown: 0 };
  for (const build of builds || []) {
    switch (String(build.state || "").toUpperCase()) {
      case "SUCCESSFUL": counts.successful++; break;
      case "FAILED": counts.failed++; break;
      case "INPROGRESS": counts.in_progress++; break;
      case "CANCELLED": counts.cancelled++; break;
      default: counts.unknown++; break;
    }
  }
  return counts;
}

// buildSummary draws build counts, what failed first: the count and its icon,
// with Bitbucket's words as the tooltip, or those words in full. Nothing to
// draw is null.
function buildSummary(counts, compact) {
  if (!counts) return null;
  const parts = [];
  const add = (count, state) => {
    if (count > 0) {
      const look = buildStateOf(state);
      parts.push(el("span", { class: "build-state " + look.className, title: look.count(count) },
        icon(look.icon, compact ? look.count(count) : undefined), compact ? String(count) : look.count(count)));
    }
  };
  add(counts.failed, "FAILED");
  add(counts.in_progress, "INPROGRESS");
  add(counts.successful, "SUCCESSFUL");
  add(counts.cancelled, "CANCELLED");
  add(counts.unknown, "UNKNOWN");
  if (parts.length === 0) return null;
  return el("span", { class: "counts" }, parts);
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

function plural(count, one, many) {
  return count + " " + (count === 1 ? one : many || one + "s");
}

function repositoryOf(pr) {
  return pr.repository ? pr.repository.project_key + "/" + pr.repository.slug : "";
}

function shortCommit(commit) {
  return commit ? String(commit).slice(0, 11) : "";
}
