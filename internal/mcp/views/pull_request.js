// The pull request view: a card inline, and its overview in fullscreen or
// opened out beneath the card.
//
// The card is for a glance: where the pull request stands, and what on it
// asks something of someone, in color, with the rest of its state in one
// quiet line. The overview has everything the view carries. Either way, a
// count is Bitbucket's count of the whole, and a view that lists fewer than
// that says so.

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
    el("h1", { class: "pr-title held", title: pr.title }, pr.title),
    byline(pr, avatars),
    el("div", { class: "status" },
      attentionLine(pr, avatars),
      quietLine(pr)),
    el("div", { class: "actions quiet" },
      el("button", {
        type: "button",
        class: "button",
        "aria-expanded": view.expanded ? "true" : "false",
        onclick: () => view.expand(),
      }, icon(view.expanded ? "collapse" : "expand"), view.expanded ? "Hide overview" : "Overview"),
      linkButton("Open in Bitbucket", pr.url, view.bridge, "ghost"),
      el("span", { class: "spacer" }),
      snapshotStamp(payload.generated_at, view.locale, true)));
}

// pullRequestTop is where the pull request lives, its number and its state.
function pullRequestTop(pr, suffix) {
  return el("div", { class: "pr-top" },
    icon("pullRequest", null, "faint"),
    repositoryLabel(pr, suffix),
    el("span", { class: "spacer" }),
    stateBadges(pr));
}

// repositoryLabel is a pull request's repository and number. A long
// repository gives way; the number never does.
function repositoryLabel(pr, suffix) {
  return el("span", { class: "repo-label muted" },
    el("span", { class: "ellipsis", title: repositoryOf(pr) }, repositoryOf(pr)),
    el("span", { class: "nowrap" }, "#" + pr.id + (suffix ? " · " + suffix : "")));
}

// byline is who wants what merged where: the author, and the source branch
// into the target, as Bitbucket draws them. The branches move below the
// author rather than cut the author's name short.
function byline(pr, avatars) {
  const author = pr.author || pr.author_username || "Someone";
  return el("div", { class: "byline" },
    el("span", { class: "byline-author" },
      avatar(pr.author_username, pr.author, avatars, "sm"),
      el("strong", { class: "ellipsis", title: author }, author)),
    branchPair(pr));
}

// branchPair is the source branch into the target. The source gives way
// first: where a pull request goes matters more than what it is called.
function branchPair(pr) {
  return el("span", { class: "branches" },
    branchChip(pr.source_branch, "source"), icon("arrow", "into"), branchChip(pr.target_branch, "target"));
}

// attentionLine is what on the pull request asks something of someone, most
// pressing first, each in its own color: requests for changes, with who made
// them; a conflict; builds that failed or still run; open tasks. A pull
// request that asks nothing has no such line.
function attentionLine(pr, avatars) {
  const items = [];
  const requested = sortReviewers(pr.reviewers || []).filter((reviewer) => voteOf(reviewer) === "changes-requested");
  if (requested.length > 0) {
    const names = requested.map(nameOf);
    items.push(el("span", { class: "attention-item changes-requested", title: names.join(", ") + " requested changes" },
      el("span", { class: "avatar-stack tight" }, requested.slice(0, 2).map((reviewer) => avatar(reviewer.name, reviewer.display_name, avatars, "sm"))),
      el("span", { class: "who" }, namesOf(names, 2) + " requested changes")));
  }
  if (pr.mergeability && pr.mergeability.conflicted) {
    items.push(el("span", { class: "attention-item failed", title: "This pull request has conflicts that need to be resolved before it can be merged." },
      icon("buildFailed"), "Conflict"));
  }
  const builds = buildCountsOf(pr);
  if (builds) {
    for (const state of ["FAILED", "INPROGRESS"]) {
      const count = builds.counts[BUILD_COUNT_FIELDS[state]] || 0;
      if (count > 0) {
        const look = buildStateOf(state);
        items.push(el("span", { class: "attention-item " + look.className, title: look.count(count) }, icon(look.icon), look.count(count)));
      }
    }
  }
  const summary = pr.review_summary || {};
  const tasks = summary.open_tasks !== undefined ? summary.open_tasks : pr.open_task_count;
  if (tasks > 0) items.push(el("span", { class: "attention-item" }, icon("task"), plural(tasks, "open task")));
  if (items.length === 0) return null;
  return el("div", { class: "attention" }, items);
}

// quietLine is the rest of where the pull request stands, in one grey line:
// how many approved, how many of its builds passed, the comments still
// unresolved and whether it merges itself.
function quietLine(pr) {
  const parts = [];
  const reviewers = pr.reviewers || [];
  const approved = reviewers.filter((reviewer) => voteOf(reviewer) === "approved").length;
  parts.push(reviewers.length === 0 ? "No reviewers" : formatNumber(approved) + " of " + formatNumber(reviewers.length) + " approved");
  const builds = buildCountsOf(pr);
  if (builds) {
    const total = buildTotal(builds.counts);
    const passed = builds.counts.successful || 0;
    if (total === 0) parts.push("No builds");
    else if (passed === total && !builds.partial) parts.push(plural(total, "build") + " passed");
    else parts.push(formatNumber(passed) + " of " + (builds.partial ? "the first " : "") + plural(total, "build") + " passed");
  }
  const summary = pr.review_summary || {};
  const unresolved = pr.comment_count === undefined ? commentsOf(summary).unresolved : undefined;
  if (unresolved > 0) parts.push(plural(unresolved, "unresolved comment"));
  if (pr.comment_count > 0) parts.push(plural(pr.comment_count, "comment"));
  const line = el("div", { class: "quiet-line" }, parts.join(" · "));
  if (pr.auto_merge && pr.auto_merge.enabled) {
    line.append(" · ", el("span", { title: "It will be merged automatically once all pending merge checks have passed" }, "Auto-merge on"));
  }
  return line;
}

// pullRequestPage is the pull request in fullscreen: its overview, with the
// description and the side panel scrolling each on their own. The header
// reads as Bitbucket's does: where it lives, the title, and under it the
// state with who wants what merged where.
function pullRequestPage(pr, payload, view) {
  return el("div", { class: "page" },
    el("header", { class: "fullscreen-header" },
      el("div", { class: "row-main" },
        el("div", { class: "pr-top" }, icon("pullRequest", null, "faint"), repositoryLabel(pr)),
        el("h1", { class: "pr-title" }, pr.title),
        el("div", { class: "header-meta" }, stateBadges(pr), byline(pr, payload.avatars || {}))),
      el("div", { class: "header-actions" },
        snapshotStamp(payload.generated_at, view.locale),
        linkButton("Open in Bitbucket", pr.url, view.bridge, "primary"),
        el("button", { type: "button", class: "button", onclick: () => view.expand() }, icon("collapse"), "Exit full screen"))),
    pullRequestOverview(pr, payload, view));
}

function pullRequestOverview(pr, payload, view) {
  const avatars = payload.avatars || {};
  return el("div", { class: view.fullscreen ? "details" : "details inline" },
    el("section", { class: "details-main" },
      el("h2", { class: "section-title" }, "Description"),
      descriptionOf(pr, view)),
    el("aside", { class: "details-side" },
      reviewersSection(pr.reviewers || [], avatars, view),
      buildsSection(pr, view),
      detailsSection(pr, view)));
}

// descriptionOf is the description, drawn from its Markdown. It is whole in
// fullscreen, where it scrolls beside the reviewers and builds, and held to a
// few lines where it opens in place or a narrow screen puts them below it.
function descriptionOf(pr, view) {
  if (!pr.description || !String(pr.description).trim()) return el("p", { class: "faint" }, "No description.");
  const body = renderMarkdown(pr.description, { bridge: view.bridge, base: pr.url });
  return view.fullscreen && !view.narrow ? body : clampable("description", body, view);
}

// The reviewers, in groups: who requested changes, who approved, and who is
// still reviewing, which asks nothing of the person and folds once it runs
// long. Every group names everyone in it.
const REVIEWER_GROUPS = [
  { vote: "changes-requested", title: "Changes requested" },
  { vote: "approved", title: "Approved" },
  { vote: "none", title: "Reviewing", folds: true },
];

function reviewersSection(reviewers, avatars, view) {
  const sorted = sortReviewers(reviewers);
  const section = el("section", { class: "side-section" },
    el("h2", { class: "section-title" }, "Reviewers", sorted.length > 0 ? el("span", { class: "count" }, formatNumber(sorted.length)) : null));
  if (sorted.length === 0) {
    section.append(el("p", { class: "faint" }, "No reviewers."));
    return section;
  }
  for (const group of REVIEWER_GROUPS) {
    const members = sorted.filter((reviewer) => voteOf(reviewer) === group.vote);
    if (members.length === 0) continue;
    const key = "reviewers-" + group.vote;
    const folds = group.folds && members.length > FOLD_AFTER;
    const title = group.title + " · " + formatNumber(members.length);
    section.append(el("div", { class: "group" },
      folds ? foldButton(key, title, view) : el("h3", { class: "group-title" }, title),
      folds && !view.unclamped.has(key) ? null : el("ul", { class: "person-list compact" }, members.map((reviewer) => el("li", {},
        reviewerAvatar(reviewer, avatars, "sm"),
        el("span", { class: "ellipsis", title: nameOf(reviewer) }, nameOf(reviewer)))))));
  }
  return section;
}

// The builds, in groups, what needs attention first. Every group but the
// passes is open however long it is; the passes fold to their count. A group
// counts every build Bitbucket has in its state, and says when the view lists
// fewer than that.
function buildsSection(pr, view) {
  const section = el("section", { class: "side-section" }, el("h2", { class: "section-title" }, "Builds"));
  const builds = buildCountsOf(pr);
  if (!builds) {
    section.append(el("p", { class: "faint" }, "Bitbucket did not report the builds."));
    return section;
  }
  if (buildTotal(builds.counts) === 0) {
    section.append(el("p", { class: "faint" }, "No builds on this commit."));
    return section;
  }
  for (const state of BUILD_ORDER) {
    const count = builds.counts[BUILD_COUNT_FIELDS[state]] || 0;
    if (count === 0) continue;
    const look = buildStateOf(state);
    const listed = (pr.checks || []).filter((build) => buildStateOf(build.state) === look);
    const key = "builds-" + state;
    const folds = state === "SUCCESSFUL" && count > FOLD_AFTER;
    const title = el("span", { class: "build-state " + look.className }, icon(look.icon), look.count(count));
    const unlisted = count - listed.length;
    section.append(el("div", { class: "group" },
      folds ? foldButton(key, title, view) : el("h3", { class: "group-title" }, title),
      folds && !view.unclamped.has(key) ? null : el("ul", { class: "check-list" },
        listed.map((build) => buildRow(build, look, view)),
        unlisted > 0 ? el("li", { class: "unlisted" },
          el("span", { class: "faint" }, listed.length === 0
            ? (count === 1 ? "Not listed in this view. " : "None listed in this view. ")
            : "And " + formatNumber(unlisted) + " more, not listed in this view. "),
          textLink(count === 1 ? "View it in Bitbucket" : "View them in Bitbucket", pr.url, view.bridge)) : null)));
  }
  if (builds.partial) {
    section.append(el("p", { class: "faint" }, "Counted among the first " + formatNumber(pr.checks.length) + "; Bitbucket has more."));
  }
  return section;
}

// buildRow is a build under its state's heading, which already says how it
// went: its name, which opens the build where there is one to open.
function buildRow(build, look, view) {
  const name = build.name || build.key || "Build";
  const title = build.name && build.key ? name + " (" + build.key + ")" : name;
  if (!isWebURL(build.url)) {
    return el("li", { dataset: { state: look.className } }, el("span", { class: "build-name ellipsis", title }, name));
  }
  return el("li", { dataset: { state: look.className } },
    el("button", { type: "button", class: "build-link", title: title + ": open the build", onclick: () => openLink(view.bridge, build.url) },
      el("span", { class: "ellipsis" }, name), icon("external", null, "link-icon")));
}

function detailsSection(pr, view) {
  const summary = pr.review_summary || {};
  return el("section", { class: "side-section" },
    el("h2", { class: "section-title" }, "Details"),
    el("ul", { class: "person-list details-list" },
      detailRow("Created", relativeTime(pr.created_date, view.locale)),
      detailRow("Last updated", relativeTime(pr.updated_date, view.locale)),
      pr.source_commit ? detailRow("Commit", el("span", { class: "mono" }, shortCommit(pr.source_commit))) : null,
      detailRow("Tasks", openAndResolved(summary.open_tasks, summary.resolved_tasks, "open")),
      detailRow("Comments", pr.comment_count !== undefined
        ? formatNumber(pr.comment_count)
        : openAndResolved(commentsOf(summary).unresolved, commentsOf(summary).resolved, "unresolved")),
      pr.auto_merge && pr.auto_merge.enabled ? detailRow("Auto-merge", "On, once all pending merge checks have passed") : null));
}

function detailRow(label, value) {
  return el("li", {}, el("span", { class: "fact-label" }, label), el("span", { class: "detail-value" }, value));
}

// commentsOf is a review summary's comment threads without its tasks, which
// are counted as tasks: Bitbucket's thread counts include them, and a task
// counted twice reads as more to do than there is.
function commentsOf(summary) {
  const less = (all, tasks) => (all === undefined ? undefined : Math.max(0, all - (tasks || 0)));
  return { unresolved: less(summary.unresolved_threads, summary.open_tasks), resolved: less(summary.resolved_threads, summary.resolved_tasks) };
}

function openAndResolved(open, resolved, openWord) {
  if (open === undefined && resolved === undefined) return el("span", { class: "faint" }, "not reported");
  return formatNumber(open || 0) + " " + openWord + " · " + formatNumber(resolved || 0) + " resolved";
}

function notice(text, tone) {
  return el("p", { class: "notice" + (tone ? " " + tone : "") }, text);
}
