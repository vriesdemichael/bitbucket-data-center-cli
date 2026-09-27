// The pull request view: a card inline, and its overview in fullscreen or
// opened out beneath the card.

function renderPullRequest(payload, view) {
  const pr = payload.pull_request;
  if (!pr) return notice("This view has no pull request to show.");
  if (view.fullscreen) return pullRequestPage(pr, payload, view);
  return el("div", {},
    pullRequestCard(pr, payload, view),
    view.expanded ? el("div", { class: "inline-details" }, pullRequestOverview(pr, payload, view)) : null);
}

function pullRequestCard(pr, payload, view) {
  const avatars = payload.avatars || {};
  return el("article", { class: "pr-card", "aria-label": "Pull request " + repositoryOf(pr) + " #" + pr.id },
    pullRequestTop(pr),
    el("h1", { class: "pr-title" }, pr.title),
    byline(pr, avatars),
    el("div", { class: "facts" },
      fact("Reviewers", reviewersFact(pr.reviewers || [], avatars)),
      fact("Builds", buildsFact(pr)),
      fact("Activity", activityFact(pr))),
    el("div", { class: "actions" },
      linkButton("Open in Bitbucket", pr.url, view.bridge, "primary"),
      el("button", {
        type: "button",
        class: "button",
        "aria-expanded": view.expanded ? "true" : "false",
        onclick: () => view.expand(),
      }, icon(view.expanded ? "collapse" : "expand"), view.expanded ? "Hide overview" : "Overview")));
}

function pullRequestTop(pr) {
  return el("div", { class: "pr-top" },
    icon("pullRequest", null, "faint"),
    el("span", { class: "ellipsis muted" }, repositoryOf(pr) + " #" + pr.id),
    el("span", { class: "spacer" }),
    stateBadges(pr));
}

// byline is who wants what merged where: the author, and the source branch
// into the target, as Bitbucket draws them.
function byline(pr, avatars) {
  return el("div", { class: "byline" },
    avatar(pr.author_username, pr.author, avatars, "sm"),
    el("strong", {}, pr.author || pr.author_username || "Someone"),
    branchChip(pr.source_branch),
    icon("arrow", "into"),
    branchChip(pr.target_branch));
}

function fact(label, value) {
  return el("div", { class: "fact" }, el("span", { class: "fact-label" }, label), value);
}

function reviewersFact(reviewers, avatars) {
  if (reviewers.length === 0) return el("span", { class: "faint" }, "No reviewers");
  const sorted = sortReviewers(reviewers);
  const approved = reviewers.filter((reviewer) => voteOf(reviewer) === "approved").length;
  const requested = reviewers.filter((reviewer) => voteOf(reviewer) === "changes-requested").length;
  const shown = sorted.slice(0, 6);
  const text = [approved + " approved"];
  if (requested > 0) text.push(requested + " changes requested");
  return el("span", { class: "fact" },
    el("span", { class: "avatar-stack" },
      shown.map((reviewer) => reviewerAvatar(reviewer, avatars)),
      reviewers.length > shown.length ? el("span", { class: "faint" }, "+" + (reviewers.length - shown.length)) : null),
    el("span", { class: "muted" }, text.join(" · ")));
}

function buildsFact(pr) {
  if (!pr.checks) return el("span", { class: "faint" }, "Not reported");
  return buildSummary(countBuilds(pr.checks)) || el("span", { class: "faint" }, "No builds");
}

// activityFact counts what is open on the pull request, as Bitbucket's tasks
// and comments columns do.
function activityFact(pr) {
  const summary = pr.review_summary || {};
  const tasks = summary.open_tasks !== undefined ? summary.open_tasks : pr.open_task_count;
  const comments = pr.comment_count !== undefined ? pr.comment_count : summary.comment_count;
  const parts = [];
  if (tasks !== undefined) {
    parts.push(el("span", { class: "counter" + (tasks > 0 ? "" : " faint") }, icon("task"), plural(tasks, "open task")));
  }
  if (comments !== undefined) {
    parts.push(el("span", { class: "counter" + (comments > 0 ? "" : " faint") }, icon("comment"), plural(comments, "comment")));
  }
  if (pr.auto_merge && pr.auto_merge.enabled) {
    parts.push(el("span", { class: "counter inprogress", title: "It will be merged automatically once all pending merge checks have passed" },
      icon("autoMerge"), "Auto-merge"));
  }
  if (parts.length === 0) return el("span", { class: "faint" }, "Not reported");
  return el("span", { class: "counts" }, parts);
}

// pullRequestPage is the pull request in fullscreen: its overview.
function pullRequestPage(pr, payload, view) {
  return el("div", {},
    el("header", { class: "fullscreen-header" },
      el("div", { class: "row-main" },
        pullRequestTop(pr),
        el("h1", { class: "pr-title ellipsis" }, pr.title)),
      linkButton("Open in Bitbucket", pr.url, view.bridge, "primary"),
      el("button", { type: "button", class: "button", onclick: () => view.expand() }, icon("collapse"), "Exit full screen")),
    pullRequestOverview(pr, payload, view));
}

function pullRequestOverview(pr, payload, view) {
  const avatars = payload.avatars || {};
  const reviewers = sortReviewers(pr.reviewers || []);
  const summary = pr.review_summary || {};
  return el("div", { class: view.fullscreen ? "details" : "details inline" },
    el("section", { class: "details-main" },
      view.fullscreen ? byline(pr, avatars) : null,
      el("h2", { class: "section-title" }, "Description"),
      pr.description
        ? el("pre", { class: "description" }, pr.description)
        : el("p", { class: "faint" }, "No description.")),
    el("aside", { class: "details-side" },
      el("section", { class: "side-section" },
        el("h2", { class: "section-title" }, "Reviewers"),
        reviewers.length === 0
          ? el("p", { class: "faint" }, "No reviewers.")
          : el("ul", { class: "person-list" }, reviewers.map((reviewer) => el("li", {},
            reviewerAvatar(reviewer, avatars),
            el("span", { class: "ellipsis" }, reviewer.display_name || reviewer.name),
            el("span", { class: "spacer" }),
            voteBadge(voteOf(reviewer)))))),
      el("section", { class: "side-section" },
        el("h2", { class: "section-title" }, "Builds"),
        buildList(pr, view)),
      el("section", { class: "side-section" },
        el("h2", { class: "section-title" }, "Details"),
        el("ul", { class: "person-list" },
          detailRow("Created", relativeTime(pr.created_date, view.locale)),
          detailRow("Last updated", relativeTime(pr.updated_date, view.locale)),
          pr.source_commit ? detailRow("Commit", el("span", { class: "mono" }, shortCommit(pr.source_commit))) : null,
          detailRow("Tasks", openAndResolved(summary.open_tasks, summary.resolved_tasks)),
          detailRow("Comments", pr.comment_count !== undefined ? String(pr.comment_count) : summary.comment_count !== undefined ? String(summary.comment_count) : "unknown"),
          pr.auto_merge && pr.auto_merge.enabled ? detailRow("Auto-merge", "on, once all pending merge checks have passed") : null))));
}

function voteBadge(vote) {
  switch (vote) {
    case "approved": return badge("Approved", "success");
    case "changes-requested": return badge("Changes requested", "warning-bold");
    default: return null;
  }
}

function buildList(pr, view) {
  if (!pr.checks) return el("p", { class: "faint" }, "Bitbucket did not report the builds.");
  if (pr.checks.length === 0) return el("p", { class: "faint" }, "No builds on this commit.");
  return el("ul", { class: "check-list" }, pr.checks.map((build) => {
    const look = buildStateOf(build.state);
    return el("li", {},
      el("span", { class: "build-state " + look.className, title: look.title }, icon(look.icon, look.title)),
      el("span", { class: "ellipsis", title: build.key || "" }, build.name || build.key || "Build"),
      el("span", { class: "spacer" }),
      isWebURL(build.url)
        ? el("button", { type: "button", class: "button ghost", "aria-label": "Open the build", title: "Open the build", onclick: () => openLink(view.bridge, build.url) }, icon("external"))
        : null);
  }));
}

function detailRow(label, value) {
  return el("li", {}, el("span", { class: "fact-label" }, label), el("span", { class: "ellipsis" }, value));
}

function openAndResolved(open, resolved) {
  if (open === undefined && resolved === undefined) return el("span", { class: "faint" }, "unknown");
  return (open || 0) + " open · " + (resolved || 0) + " resolved";
}

function notice(text, tone) {
  return el("p", { class: "notice" + (tone ? " " + tone : "") }, text);
}
