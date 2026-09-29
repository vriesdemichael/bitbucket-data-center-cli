// The threads view: a pull request's comment threads. Inline, the ones still
// open, for a glance; in fullscreen, or opened out in place, every thread the
// view carries, where it is anchored, each with the lines of the diff that
// lead to its line.
//
// A count is Bitbucket's count of the whole, however many threads the view
// carries, and a view that carries fewer says so. An open task or an
// unresolved comment is never behind a click; resolved ones fold.

// INLINE_THREADS is how many open threads the card lists; a step shows more
// where the host has no fullscreen.
const INLINE_THREADS = 4;
const INLINE_THREADS_PHONE = 3;
const THREAD_STEP = 25;
// REPLIES_SHOWN is how many of a thread's latest replies show; the earlier
// ones fold to their count.
const REPLIES_SHOWN = 3;

function renderThreads(payload, view) {
  const pr = payload.pull_request || {};
  const data = payload.threads || {};
  const summary = data.summary || {};
  const threads = data.threads || [];
  const avatars = payload.avatars || {};
  if (view.fullscreen) return threadsPage(pr, summary, threads, payload, view);

  const open = openThreadsOf(threads);
  const shown = open.slice(0, shownCount("threads", view.phone ? INLINE_THREADS_PHONE : INLINE_THREADS, view));
  // The rest of what is open, counted from Bitbucket's totals: some of it
  // may be more than the view carries.
  const left = Math.max(0, (summary.unresolved || 0) - shown.length);

  return el("div", {},
    backButton(view),
    pullRequestTop(pr, "Comments"),
    el("h1", { class: "pr-title held-2", title: pr.title }, pr.title),
    el("div", { class: "status" }, threadsAttention(summary), threadsQuiet(summary)),
    shown.length === 0
      ? el("p", { class: "faint" }, (summary.total_threads || 0) === 0 ? "No comments." : "Nothing is still open.")
      : el("ul", { class: "pr-list thread-list" }, cardRows(shown, avatars, view)),
    left > 0
      ? el("div", { class: "more-row" }, view.canFullscreen && !view.expanded
        ? el("button", { type: "button", class: "button ghost list-toggle", onclick: () => view.expand() }, "and " + plural(left, "more open comment") + ", in full screen")
        : moreButton("threads", Math.min(left, open.length - shown.length), THREAD_STEP, "open comments", view))
      : null,
    el("div", { class: "actions quiet" },
      el("button", {
        type: "button",
        class: "button",
        "aria-expanded": view.expanded ? "true" : "false",
        onclick: () => view.expand(),
      }, icon(view.expanded ? "collapse" : "expand"), view.expanded ? "Hide the threads" : "All threads"),
      pullRequestTabs(pr, "threads", view),
      linkButton("Open in Bitbucket", pr.url, view.bridge, "ghost"),
      el("span", { class: "spacer" }),
      snapshotStamp(payload, view, true)),
    view.expanded ? el("div", { class: "inline-details" }, threadGroups(pr, summary, threads, avatars, view)) : null);
}

// openThreadsOf are the threads still open, in the order the full view has
// them, so the card lists its start.
function openThreadsOf(threads) {
  return threads.filter((thread) => !thread.resolved).sort(byPlace);
}

// byPlace orders threads by where they are: those on the pull request first,
// then each file's by path, and in a file by line, the oldest first on one.
function byPlace(a, b) {
  const pathA = pathOf(a);
  const pathB = pathOf(b);
  if (pathA !== pathB) return pathA === "" ? -1 : pathB === "" ? 1 : pathA.localeCompare(pathB);
  return lineOf(a) - lineOf(b) || (a.created_date || 0) - (b.created_date || 0);
}

function pathOf(thread) {
  return thread.anchor && thread.anchor.path ? thread.anchor.path : "";
}

// cardRows are the card's open threads under where they are, a heading each
// time the place changes, as the full view groups them.
function cardRows(shown, avatars, view) {
  const rows = [];
  let place = null;
  for (const thread of shown) {
    const path = pathOf(thread);
    if (path !== place) {
      place = path;
      rows.push(el("li", { class: "thread-place" }, path ? pathLabel(path) : "On the pull request"));
    }
    rows.push(el("li", {}, threadRow(thread, avatars, view)));
  }
  return rows;
}

// threadsAttention is what the threads ask of someone: the open tasks, and
// the comments still unresolved.
function threadsAttention(summary) {
  const items = [];
  const tasks = summary.open_tasks || 0;
  const comments = Math.max(0, (summary.unresolved || 0) - tasks);
  if (tasks > 0) items.push(el("span", { class: "attention-item" }, icon("task"), plural(tasks, "open task")));
  if (comments > 0) items.push(el("span", { class: "attention-item" }, icon("comment"), plural(comments, "unresolved comment")));
  return items.length > 0 ? el("div", { class: "attention" }, items) : null;
}

// threadsQuiet is the rest, in one grey line: what is resolved, and the
// author's own comments not yet published.
function threadsQuiet(summary) {
  const parts = [];
  const resolved = summary.resolved || 0;
  if (resolved > 0) parts.push(plural(resolved, "resolved comment"));
  if (summary.resolved_tasks > 0) parts.push(plural(summary.resolved_tasks, "task") + " done");
  if (summary.pending > 0) parts.push(plural(summary.pending, "pending comment"));
  return parts.length > 0 ? el("div", { class: "quiet-line" }, parts.join(" · ")) : null;
}

// threadRow is an open thread in the card: the start of what was written,
// who wrote it and on which line, under the heading that names the file. It
// opens the thread in the overview.
function threadRow(thread, avatars, view) {
  const author = thread.author || thread.author_username || "Someone";
  return el("button", {
    type: "button",
    class: "row-button thread-row",
    title: author + " " + anchorWords(thread.anchor) + ": open the thread",
    onclick: () => openThread(thread, view),
  },
  avatar(thread.author_username, thread.author, avatars, "lg"),
  el("span", { class: "row-main" },
    el("span", { class: "row-title thread-excerpt" }, excerptOf(thread.text)),
    el("span", { class: "row-meta" },
      el("span", { class: "thread-author ellipsis" }, author),
      lineLabel(thread.anchor))),
  el("span", { class: "row-side" },
    thread.task ? badge("Task") : null,
    thread.replies && thread.replies.length > 0
      ? el("span", { class: "faint nowrap", title: plural(thread.replies.length, "reply", "replies") }, icon("comment"), " " + formatNumber(thread.replies.length))
      : null));
}

// openThread shows a thread in the overview: in fullscreen, at the thread,
// where the host has it, and opened out in place where it does not.
function openThread(thread, view) {
  view.focusThread = thread.id;
  if (!view.expanded || view.canFullscreen) view.expand();
  else render();
}

// lineLabel is where in its file a thread is; the heading above names the
// file, and a thread on the pull request has none.
function lineLabel(anchor) {
  if (!anchor || !anchor.path) return null;
  return el("span", { class: "thread-anchor nowrap" }, anchor.line ? "line " + formatNumber(anchor.line) : "on the file");
}

function anchorWords(anchor) {
  if (!anchor || !anchor.path) return "on the pull request";
  return "on " + anchor.path + (anchor.line ? ":" + anchor.line : "");
}

// excerptOf is the start of a comment as one plain line: its first line
// with text, without the marks Markdown writes it with.
function excerptOf(text) {
  const line = String(text || "").split("\n").map((part) => part.trim()).find((part) => part && !/^(```|~~~)/.test(part)) || "";
  const plain = line.replace(/^(#{1,6}\s+|>\s*|[-*+]\s+(\[[ xX]\]\s+)?|\d+[.)]\s+)/, "").replace(/[*_`~]+/g, "").replace(/\s+/g, " ");
  return plain || "(no text)";
}

// threadsPage is the threads in fullscreen, under the pull request's header,
// scrolling on their own.
function threadsPage(pr, summary, threads, payload, view) {
  return el("div", { class: "page" },
    el("header", { class: "fullscreen-header" },
      el("div", { class: "row-main" },
        backButton(view),
        el("div", { class: "pr-top" }, icon("pullRequest", null, "faint"), repositoryLabel(pr, "Comments")),
        el("h1", { class: "pr-title held-2", title: pr.title }, pr.title),
        el("div", { class: "header-meta" }, stateBadges(pr), threadsAttention(summary), threadsQuiet(summary))),
      el("div", { class: "header-actions" },
        snapshotStamp(payload, view),
        pullRequestTabs(pr, "threads", view),
        linkButton("Open in Bitbucket", pr.url, view.bridge, "primary"),
        el("button", { type: "button", class: "button", onclick: () => view.expand() }, icon("collapse"), "Exit full screen"))),
    el("div", { class: "threads-main", id: "threads-main" }, threadGroups(pr, summary, threads, payload.avatars || {}, view)));
}

// threadGroups are the threads where they are: those on the pull request
// itself first, then each file's, by path. In a group the open threads come
// first, by line; the resolved ones fold to their count.
function threadGroups(pr, summary, threads, avatars, view) {
  const parts = [newCommentArea(pr, view)];
  const total = summary.total_threads || 0;
  if (threads.length < total) {
    parts.push(el("p", { class: "notice", role: "note" },
      "This view carries " + formatNumber(threads.length) + " of the " + plural(total, "comment thread") + ". ",
      textLink("View them all in Bitbucket", pr.url, view.bridge)));
  }
  if (threads.length === 0) {
    parts.push(el("p", { class: "faint" }, total === 0 ? "No comments." : "None of them is in this view."));
    return parts;
  }
  for (const group of groupThreads(threads)) {
    const open = group.threads.filter((thread) => !thread.resolved);
    const resolved = group.threads.filter((thread) => thread.resolved);
    const key = "resolved-" + group.key;
    parts.push(el("section", { class: "thread-group" },
      el("h2", { class: "thread-group-title" },
        group.path ? pathLabel(group.path) : "On the pull request",
        el("span", { class: "count" }, [
          open.length > 0 ? formatNumber(open.length) + " open" : "",
          resolved.length > 0 ? formatNumber(resolved.length) + " resolved" : "",
        ].filter(Boolean).join(" · "))),
      open.map((thread) => threadArticle(thread, pr, avatars, view)),
      resolved.length > 0
        ? el("div", { class: "group" },
          foldButton(key, plural(resolved.length, "resolved comment"), view),
          view.unclamped.has(key) ? resolved.map((thread) => threadArticle(thread, pr, avatars, view)) : null)
        : null));
  }
  return parts;
}

// groupThreads puts threads under where they are anchored, in the order
// threadGroups draws them.
function groupThreads(threads) {
  const groups = new Map();
  for (const thread of threads.slice().sort(byPlace)) {
    const path = pathOf(thread);
    if (!groups.has(path)) groups.set(path, { key: path || "pull-request", path, threads: [] });
    groups.get(path).threads.push(thread);
  }
  return [...groups.values()];
}

function lineOf(thread) {
  return thread.anchor && thread.anchor.line ? thread.anchor.line : 0;
}

// threadArticle is one thread: the diff leading to its line, its opening
// comment and its replies, the earlier of many folded.
function threadArticle(thread, pr, avatars, view) {
  const replies = thread.replies || [];
  const key = "replies-" + thread.id;
  const earlier = replies.length - REPLIES_SHOWN;
  const unfolded = earlier <= 0 || view.unclamped.has(key);
  return el("article", {
    class: "thread" + (thread.resolved ? " resolved" : "") + (thread.task ? " task" : ""),
    id: "thread-" + thread.id,
    "aria-label": (thread.task ? "Task" : "Comment") + " by " + (thread.author || "someone") + " " + anchorWords(thread.anchor),
  },
  threadContext(thread),
  commentBlock(thread, thread, pr, avatars, view, true),
  replies.length > 0
    ? el("div", { class: "replies" },
      earlier > 0 ? foldButton(key, plural(earlier, "earlier reply", "earlier replies"), view) : null,
      (unfolded ? replies : replies.slice(earlier)).map((reply) => commentBlock(reply, thread, pr, avatars, view, false)))
    : null,
  replyArea(thread, pr, view));
}

// threadContext is the diff leading to the line a thread is on, the line
// itself marked, or a note where the diff no longer has it.
function threadContext(thread) {
  if (thread.context && thread.context.length > 0) {
    return el("table", { class: "diff-table thread-context", "aria-label": anchorWords(thread.anchor) },
      el("tbody", {}, thread.context.map((line) => el("tr", { class: line.type + (line.anchor ? " anchor-line" : "") },
        el("td", { class: "line-number" }, line.old ? String(line.old) : ""),
        el("td", { class: "line-number" }, line.new ? String(line.new) : ""),
        el("td", { class: "code" }, line.text,
          line.more > 0 ? el("span", { class: "cut" }, " … " + plural(line.more, "more character")) : null)))));
  }
  if (thread.anchor && thread.anchor.orphaned) {
    return el("p", { class: "thread-note faint" }, "The line this was on is no longer in the diff.");
  }
  return null;
}

// commentBlock is one comment: who wrote it, when, what state its thread is
// in, and its text, drawn from its Markdown.
function commentBlock(comment, thread, pr, avatars, view, root) {
  const author = comment.author || comment.author_username || "Someone";
  const when = root ? comment.created_date : comment.date;
  return el("div", { class: "comment" + (root ? " root" : "") },
    el("div", { class: "comment-head" },
      avatar(comment.author_username, comment.author, avatars, "sm"),
      el("strong", { class: "ellipsis", title: author }, author),
      when ? el("span", { class: "faint nowrap" }, relativeTime(when, view.locale)) : null,
      root && thread.task ? badge(thread.resolved ? "Task done" : "Task", thread.resolved ? "success" : "") : null,
      root && thread.resolved && !thread.task ? badge("Resolved", "success") : null,
      root && thread.pending ? badge("Pending") : null,
      el("span", { class: "spacer" }),
      root && isWebURL(thread.url)
        ? el("button", { type: "button", class: "button ghost icon-button", title: "Open this thread in Bitbucket", onclick: () => openLink(view.bridge, thread.url) }, icon("external", "Open in Bitbucket"))
        : null),
    el("div", { class: "comment-body" },
      comment.text ? renderMarkdown(comment.text, { bridge: view.bridge, base: pr.url }) : el("p", { class: "faint" }, "(no text)"),
      comment.text_cut
        ? el("p", { class: "faint" }, "This comment is longer than a view carries. ", textLink("Read the rest in Bitbucket", thread.url, view.bridge))
        : null));
}
