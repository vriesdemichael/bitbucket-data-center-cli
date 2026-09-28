// The pull request list view: the first few inline; all of them, with
// filters, in fullscreen; and, where the host has no fullscreen, opened out
// in place a step at a time. A row carries what Bitbucket's dashboard does:
// the author, the summary, reviewers, builds, open tasks, comments and the
// state.

// INLINE_ROWS is how many rows a list shows inline: fewer on a phone, where
// a row wraps to three lines.
const INLINE_ROWS = 6;
const INLINE_ROWS_PHONE = 4;

// LIST_STEP is how many more rows a list opened out in place shows each time.
const LIST_STEP = 25;

// The filters a list offers, each shown only when it would match something.
const LIST_FILTERS = [
  { id: "all", label: "All", matches: () => true },
  { id: "open", label: "Open", matches: (pr) => pr.state === "OPEN" && !pr.draft },
  { id: "draft", label: "Draft", matches: (pr) => pr.state === "OPEN" && pr.draft },
  { id: "changes-requested", label: "Changes requested", matches: (pr) => pr.state === "OPEN" && changesRequested(pr) },
  { id: "failed-builds", label: "Failed builds", matches: (pr) => pr.check_counts && pr.check_counts.failed > 0 },
  { id: "merged", label: "Merged", matches: (pr) => pr.state === "MERGED" },
  { id: "declined", label: "Declined", matches: (pr) => pr.state === "DECLINED" },
];

function renderPullRequests(payload, view) {
  const prs = payload.pull_requests || [];
  const avatars = payload.avatars || {};
  const open = view.fullscreen || view.expanded;

  const filter = LIST_FILTERS.find((candidate) => candidate.id === view.filter) || LIST_FILTERS[0];
  const matching = open ? prs.filter(filter.matches) : prs;
  const inlineRows = view.phone ? INLINE_ROWS_PHONE : INLINE_ROWS;
  const limit = view.fullscreen ? matching.length : open ? shownCount("rows", inlineRows + LIST_STEP, view) : inlineRows;
  const shown = matching.slice(0, limit);
  const left = matching.length - shown.length;

  const header = el("div", { class: "list-header" },
    icon("pullRequest", null, "faint"),
    el("h1", {}, "Pull requests"),
    el("span", { class: "faint" }, formatNumber(prs.length) + (payload.limit_reached ? "+" : "")),
    el("span", { class: "spacer" }),
    snapshotStamp(payload.generated_at, view.locale),
    open
      ? el("button", { type: "button", class: "button ghost", onclick: () => view.expand() }, icon("collapse"), view.fullscreen ? "Exit full screen" : "Show fewer")
      : null);

  // A list that stopped at its limit counts only what it holds, and says so.
  const filters = open && prs.length > 0
    ? el("div", { class: "filters", role: "toolbar", "aria-label": "Filter" },
      LIST_FILTERS
        .filter((candidate) => candidate.id === "all" || prs.some(candidate.matches))
        .map((candidate) => el("button", {
          type: "button",
          class: "button",
          "aria-pressed": candidate.id === filter.id ? "true" : "false",
          onclick: () => view.setFilter(candidate.id),
        }, candidate.label + " " + formatNumber(prs.filter(candidate.matches).length))),
      payload.limit_reached ? el("span", { class: "faint filters-note" }, "Counted among the first " + formatNumber(prs.length)) : null)
    : null;

  const list = shown.length > 0
    ? el("ul", { class: "pr-list" }, shown.map((pr) => pullRequestRow(pr, avatars, view)))
    : el("p", { class: "faint" }, prs.length === 0 ? "No pull requests." : "None match this filter.");

  let footer = null;
  if (!open && (left > 0 || payload.limit_reached)) {
    footer = el("div", { class: "actions" },
      el("button", { type: "button", class: "button", onclick: () => view.expand() }, icon("expand"), "Show all " + formatNumber(prs.length)));
  } else if (open && left > 0) {
    footer = el("div", { class: "actions" }, moreButton("rows", left, LIST_STEP, "pull requests", view));
  }

  return el("div", { class: view.fullscreen ? "list-page" : "" }, header, filters, list, footer,
    payload.limit_reached && open ? el("p", { class: "faint" }, "There may be more than these in Bitbucket.") : null);
}

// pullRequestRow keeps what identifies a pull request, its repository and
// number, over its branches, which give way first on a narrow row.
function pullRequestRow(pr, avatars, view) {
  return el("li", {},
    el("button", {
      type: "button",
      class: "row-button",
      title: pr.title + " (" + repositoryOf(pr) + " #" + pr.id + "), open in Bitbucket",
      onclick: () => openLink(view.bridge, pr.url),
    },
    avatar(pr.author_username, pr.author, avatars, "lg"),
    el("span", { class: "row-main" },
      el("span", { class: "row-title" }, pr.title),
      // The counts ride on the second line, so the title keeps the width.
      el("span", { class: "row-meta" },
        el("span", { class: "meta-repo" }, el("span", { class: "ellipsis" }, repositoryOf(pr)), el("span", { class: "nowrap" }, "#" + pr.id)),
        el("span", { class: "meta-branches ellipsis" }, pr.source_branch + " → " + pr.target_branch),
        pr.updated_date ? el("span", { class: "nowrap" }, lastUpdated(pr.updated_date, view.locale)) : null,
        el("span", { class: "counts" },
          buildBadge(pr.check_counts),
          pr.open_task_count > 0 ? el("span", { class: "counter", title: plural(pr.open_task_count, "open task") }, icon("task", null, "icon-sm"), formatNumber(pr.open_task_count)) : null,
          pr.comment_count > 0 ? el("span", { class: "counter", title: plural(pr.comment_count, "comment") }, icon("comment", null, "icon-sm"), formatNumber(pr.comment_count)) : null))),
    el("span", { class: "row-side" },
      reviewerStack(pr.reviewers || [], avatars, 3, "sm"),
      stateBadges(pr))));
}
