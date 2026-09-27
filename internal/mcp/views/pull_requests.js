// The pull request list view: the first few inline, all of them with filters
// in fullscreen or when the list is opened out. A row carries what Bitbucket's
// dashboard does: the author, the summary, reviewers, builds, open tasks,
// comments and the state.

const INLINE_ROWS = 6;

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
  const all = view.fullscreen || view.expanded;

  const filter = LIST_FILTERS.find((candidate) => candidate.id === view.filter) || LIST_FILTERS[0];
  const shown = all ? prs.filter(filter.matches) : prs.slice(0, INLINE_ROWS);
  const more = prs.length - shown.length;

  const header = el("div", { class: "list-header" },
    icon("pullRequest", null, "faint"),
    el("h1", {}, "Pull requests"),
    el("span", { class: "faint" }, payload.limit_reached ? prs.length + "+" : String(prs.length)),
    el("span", { class: "spacer" }),
    all
      ? el("button", { type: "button", class: "button ghost", onclick: () => view.expand() }, icon("collapse"), view.fullscreen ? "Exit full screen" : "Show fewer")
      : null);

  const filters = all && prs.length > 0
    ? el("div", { class: "filters", role: "toolbar", "aria-label": "Filter" }, LIST_FILTERS
      .filter((candidate) => candidate.id === "all" || prs.some(candidate.matches))
      .map((candidate) => el("button", {
        type: "button",
        class: "button",
        "aria-pressed": candidate.id === filter.id ? "true" : "false",
        onclick: () => view.setFilter(candidate.id),
      }, candidate.label + " " + prs.filter(candidate.matches).length)))
    : null;

  const list = shown.length > 0
    ? el("ul", { class: "pr-list" }, shown.map((pr) => pullRequestRow(pr, avatars, view)))
    : el("p", { class: "faint" }, prs.length === 0 ? "No pull requests." : "None match this filter.");

  const footer = !all && (more > 0 || payload.limit_reached)
    ? el("div", { class: "actions" },
      el("button", { type: "button", class: "button", onclick: () => view.expand() }, icon("expand"),
        more > 0 ? "Show all " + prs.length : "Show all"))
    : null;

  return el("div", { class: view.fullscreen ? "list-page" : "" }, header, filters, list, footer,
    payload.limit_reached && all ? el("p", { class: "faint" }, "There may be more than these in Bitbucket.") : null);
}

function pullRequestRow(pr, avatars, view) {
  const meta = [repositoryOf(pr) + " #" + pr.id, pr.source_branch + " → " + pr.target_branch];
  if (pr.updated_date) meta.push(lastUpdated(pr.updated_date, view.locale));
  return el("li", {},
    el("button", {
      type: "button",
      class: "row-button",
      title: "Open " + repositoryOf(pr) + " #" + pr.id + " in Bitbucket",
      onclick: () => openLink(view.bridge, pr.url),
    },
    avatar(pr.author_username, pr.author, avatars, "lg"),
    el("span", { class: "row-main" },
      el("span", { class: "row-title ellipsis" }, pr.title),
      // The counts ride on the second line, so the title keeps the width.
      el("span", { class: "row-meta" },
        el("span", { class: "ellipsis" }, meta.join(" · ")),
        buildSummary(pr.check_counts, true),
        pr.open_task_count > 0 ? el("span", { class: "counter", title: plural(pr.open_task_count, "open task") }, icon("task", null, "icon-sm"), String(pr.open_task_count)) : null,
        pr.comment_count > 0 ? el("span", { class: "counter", title: plural(pr.comment_count, "comment") }, icon("comment", null, "icon-sm"), String(pr.comment_count)) : null)),
    el("span", { class: "row-side" },
      reviewerStack(pr.reviewers || [], avatars),
      stateBadges(pr))));
}

function reviewerStack(reviewers, avatars) {
  if (reviewers.length === 0) return null;
  const sorted = sortReviewers(reviewers);
  const shown = sorted.slice(0, 3);
  return el("span", { class: "avatar-stack" },
    shown.map((reviewer) => reviewerAvatar(reviewer, avatars, "sm")),
    reviewers.length > shown.length ? el("span", { class: "faint" }, "+" + (reviewers.length - shown.length)) : null);
}
