// Comments, where Bitbucket has them (ADR-101): in a pull request's activity,
// under its description, and on their lines in its diff. A thread is drawn as
// Bitbucket draws one: its first comment, every reply under it, and Reply
// under each; a resolved thread folds to one line until it is opened.
//
// A count is Bitbucket's count of the whole, however many items the view
// carries, and a view that carries fewer says so.

// INLINE_ACTIVITY is how many items of the activity the overview shows where
// it opens in place; a step shows more.
const INLINE_ACTIVITY = 6;
const ACTIVITY_STEP = 20;

// ACTIVITY_VERBS are Bitbucket's words for what someone did, after their name.
const ACTIVITY_VERBS = {
  OPENED: "opened the pull request",
  DECLINED: "declined the pull request",
  REOPENED: "reopened the pull request",
  RESCOPED: "updated the pull request",
  UPDATED: "updated the reviewers",
};

// REVIEW_MARKS are the reviews an activity names, as Bitbucket marks them.
const REVIEW_MARKS = {
  APPROVED: { label: "Approved", tone: "success" },
  REVIEWED: { label: "Changes requested", tone: "warning" },
  UNAPPROVED: { label: "Unapproved", tone: "" },
};

// activitySection is the pull request's activity: a box to comment on it,
// then what happened, newest first, as Bitbucket's overview lists it.
function activitySection(pr, payload, view) {
  const activity = payload.activity;
  if (!activity) return null;
  const avatars = payload.avatars || {};
  const items = activity.items || [];
  const inline = !view.fullscreen;
  const shown = inline ? items.slice(0, shownCount("activity", INLINE_ACTIVITY, view)) : items;
  return el("section", { class: "activity", id: "activity", "aria-label": "Activity" },
    el("h2", { class: "section-title" }, "Activity"),
    newCommentArea(pr, view),
    items.length === 0
      ? el("p", { class: "faint" }, "Nothing has happened on this pull request yet.")
      : el("ol", { class: "activity-list" }, shown.map((item) => el("li", {}, activityItem(item, pr, avatars, view)))),
    inline ? moreButton("activity", items.length - shown.length, ACTIVITY_STEP, "items", view) : null,
    (activity.total || 0) > items.length
      ? el("p", { class: "faint activity-more" },
        "This view carries the latest " + formatNumber(items.length) + " of " + plural(activity.total, "item") + ". ",
        textLink("See them all in Bitbucket", pr.url, view.bridge))
      : null);
}

// activityItem is one thing someone did: a comment as its thread, a comment
// on a file with the lines it is on, or a line naming what they did.
function activityItem(item, pr, avatars, view) {
  const thread = item.thread;
  if (item.action === "COMMENTED" && thread) {
    if (!thread.anchor || !thread.anchor.path) return commentThread(thread, pr, avatars, view, { place: "activity" });
    return el("div", { class: "activity-entry" },
      activityHead(item, avatars, view, ["commented on a file"]),
      fileComment(thread, pr, avatars, view));
  }
  const review = REVIEW_MARKS[item.action];
  if (review) {
    return activityHead(item, avatars, view, ["marked the pull request as ", badge(review.label, review.tone)]);
  }
  if (item.action === "MERGED") {
    return activityHead(item, avatars, view, [
      "merged ", branchChip(pr.source_branch, "source"), " to ", branchChip(pr.target_branch, "target"),
      item.commit ? [" in commit ", el("span", { class: "mono" }, item.commit)] : null,
    ]);
  }
  const words = [ACTIVITY_VERBS[item.action] || "changed the pull request"];
  if (item.action === "RESCOPED") {
    if (item.added > 0) words.push(" · " + plural(item.added, "commit") + " added");
    if (item.removed > 0) words.push(" · " + plural(item.removed, "commit") + " removed");
  }
  if (item.action === "UPDATED") {
    if ((item.added_reviewers || []).length > 0) words.push(" · added " + item.added_reviewers.join(", "));
    if ((item.removed_reviewers || []).length > 0) words.push(" · removed " + item.removed_reviewers.join(", "));
  }
  return activityHead(item, avatars, view, words);
}

// activityHead is who did what, and when.
function activityHead(item, avatars, view, words) {
  return el("div", { class: "activity-head" },
    avatar(item.username, item.user, avatars, "sm"),
    el("span", { class: "activity-words" },
      el("strong", {}, item.user || "Someone"), " ", words,
      item.date ? [" ", el("span", { class: "faint nowrap" }, relativeTime(item.date, view.locale))] : null));
}

// fileComment is a comment on a file in the activity: the file, which opens
// its diff, and the lines of the diff the comment is among, with the thread
// under its line, as Bitbucket's overview shows one. A comment on the file
// itself, or on a line the diff no longer has, is drawn without the lines.
function fileComment(thread, pr, avatars, view) {
  const lines = thread.context || [];
  const at = lines.findIndex((line) => line.anchor);
  const body = el("div", { class: "activity-file" }, fileCrumb(thread.anchor.path, pr, view, thread.id));
  if (at < 0) {
    if (thread.anchor.orphaned) body.append(el("p", { class: "thread-note faint" }, "The line this was on is no longer in the diff."));
    body.append(el("div", { class: "activity-file-thread" }, commentThread(thread, pr, avatars, view, { place: "file" })));
    return body;
  }
  const rows = el("tbody", {});
  lines.forEach((line, index) => {
    rows.append(el("tr", { class: line.type + (line.anchor ? " anchor-line" : "") },
      el("td", { class: "line-number" }, line.old ? String(line.old) : ""),
      el("td", { class: "line-number" }, line.new ? String(line.new) : ""),
      el("td", { class: "code" }, line.text, line.more > 0 ? el("span", { class: "cut" }, " … " + plural(line.more, "more character")) : null)));
    if (index === at) {
      rows.append(el("tr", { class: "diff-thread " + line.type },
        el("td", { class: "line-number" }), el("td", { class: "line-number" }),
        el("td", { class: "thread-cell" }, commentThread(thread, pr, avatars, view, { place: "line", label: lineLabelOf(line) }))));
    }
  });
  body.append(el("table", { class: "diff-table thread-context", "aria-label": anchorWords(thread.anchor) }, rows));
  return body;
}

// fileCrumb is a file's path as Bitbucket heads a comment on it, which opens
// the pull request's diff at the file where the view can open it.
function fileCrumb(path, pr, view, threadId) {
  const label = pathLabel(path);
  if (!pr.repository || !canOpen(view, "diff")) return el("div", { class: "file-crumb" }, label);
  return el("button", {
    type: "button",
    class: "file-crumb",
    title: "Open " + path + " in the diff",
    onclick: () => openInView(view, { kind: "diff", project: pr.repository.project_key, repo: pr.repository.slug, id: String(pr.id) },
      false, null, { path, thread: threadId }),
  }, label);
}

// lineLabelOf is how Bitbucket names the line a comment is on: + and its
// number as the file is for an added line, - and its number as the file was
// for a removed one, the number alone for a line on both sides.
function lineLabelOf(line) {
  if (line.type === "add") return "Line +" + (line.new !== undefined ? line.new : line.newNo);
  if (line.type === "del") return "Line -" + (line.old !== undefined ? line.old : line.oldNo);
  return "Line " + (line.new !== undefined ? line.new : line.newNo);
}

// commentThread is one thread. options.place is where it is drawn: "line"
// under the line it is on, named by options.label; "file" at the top of its
// file; "activity" on its own in the overview.
function commentThread(thread, pr, avatars, view, options) {
  const place = options.place;
  const folded = threadFolded(thread, view);
  const replies = thread.replies || [];
  return el("article", {
    class: "comment-thread " + place + (thread.resolved ? " resolved" : "") + (thread.task ? " task" : "") + (folded ? " folded" : ""),
    id: "thread-" + thread.id,
    "aria-label": (thread.task ? "Task" : "Comment") + " by " + (thread.author || "someone") + " " + anchorWords(thread.anchor),
  },
  folded
    ? foldedThread(thread, replies, avatars, view, options)
    : [
      commentBlock(thread, thread, pr, avatars, view, options),
      replies.length > 0
        ? el("div", { class: "comment-replies" }, replies.map((reply) => commentBlock(reply, thread, pr, avatars, view, null)))
        : null,
    ]);
}

// threadFolded is whether a thread shows as one line: a resolved one until
// the person opens it, an open one once they fold it.
function threadFolded(thread, view) {
  const toggled = view.unclamped.has("thread-" + thread.id);
  return thread.resolved ? !toggled : toggled;
}

// foldedThread is a thread on one line, as Bitbucket folds a resolved one:
// who wrote it, when it was last answered, that it is resolved, and where.
function foldedThread(thread, replies, avatars, view, options) {
  const last = replies.length > 0 ? replies[replies.length - 1].date : thread.created_date;
  return el("button", {
    type: "button",
    class: "comment-folded",
    "aria-expanded": "false",
    title: "Show this thread",
    onclick: () => view.toggleClamp("thread-" + thread.id),
  },
  avatar(thread.author_username, thread.author, avatars, "md"),
  el("strong", { class: "comment-author ellipsis" }, thread.author || thread.author_username || "Someone"),
  last ? el("span", { class: "faint nowrap" }, (replies.length > 0 ? "Last reply " : "") + relativeTime(last, view.locale)) : null,
  thread.resolved ? badge(thread.task ? "Done" : "Resolved", "success") : null,
  el("span", { class: "spacer" }),
  options.label ? el("span", { class: "comment-line faint nowrap" }, options.label) : null,
  icon("chevronRight", null, "faint"));
}

// commentBlock is one comment: who wrote it and when, its text drawn from its
// Markdown, a task's checkbox, and Reply. options is the thread's, for its
// first comment, and null for a reply.
function commentBlock(comment, thread, pr, avatars, view, options) {
  const root = Boolean(options);
  const author = comment.author || comment.author_username || "Someone";
  const when = root ? comment.created_date : comment.date;
  const text = comment.text
    ? renderMarkdown(comment.text, { bridge: view.bridge, base: pr.url })
    : el("p", { class: "faint" }, "(no text)");
  return el("div", { class: "comment" + (root ? " root" : " reply") },
    avatar(comment.author_username, comment.author, avatars, "md"),
    el("div", { class: "comment-content" },
      el("div", { class: "comment-head" },
        el("strong", { class: "comment-author ellipsis", title: author }, author),
        when ? el("span", { class: "faint nowrap" }, relativeTime(when, view.locale)) : null,
        root && isWebURL(thread.url)
          ? el("button", { type: "button", class: "button ghost icon-button", title: "Open this thread in Bitbucket", onclick: () => openLink(view.bridge, thread.url) }, icon("external", "Open in Bitbucket"))
          : null,
        root && thread.pending ? badge("Pending") : null,
        root && thread.resolved && !thread.task ? badge("Resolved", "success") : null,
        root ? el("span", { class: "spacer" }) : null,
        root && options.label ? el("span", { class: "comment-line faint nowrap" }, options.label) : null,
        root && options.place !== "activity"
          ? el("button", {
            type: "button",
            class: "button ghost icon-button",
            title: thread.resolved ? "Fold this thread" : "Fold this thread to one line",
            "aria-expanded": "true",
            onclick: () => view.toggleClamp("thread-" + thread.id),
          }, icon("chevronDown", "Fold"))
          : null),
      el("div", { class: "comment-body" + (root && thread.task ? " task-box" : "") },
        root && thread.task
          ? el("span", { class: "task-check" + (thread.resolved ? " done" : ""), role: "img", "aria-label": thread.resolved ? "Task done" : "Open task" },
            thread.resolved ? icon("task") : null)
          : null,
        el("div", { class: "comment-text" }, text,
          comment.text_cut
            ? el("p", { class: "faint" }, "This comment is longer than a view carries. ", textLink("Read the rest in Bitbucket", thread.url, view.bridge))
            : null)),
      replyAction(comment, thread, pr, view)));
}

// replyAction is Reply under a comment, and the box it opens there, where the
// server lets a view comment. The reply answers that comment, as Bitbucket's
// Reply does.
function replyAction(comment, thread, pr, view) {
  if (!pr.repository || !canCall(view, "add_pr_comment")) return null;
  const key = "reply-" + comment.id;
  if (!view.drafts.has(key)) {
    return el("div", { class: "comment-actions" },
      el("button", { type: "button", class: "text-action", onclick: () => openDraft(view, key) }, "Reply"));
  }
  return commentForm(view, key, {
    placeholder: "Reply to " + (comment.author || "this comment"),
    submitLabel: "Reply",
    busyLabel: "Replying…",
    send: (text) => callForPerson(view, "add_pr_comment", Object.assign(pullRequestArgs(pr), { text, parent_id: comment.id })),
  });
}

// anchorWords is where a thread is, in words: on the pull request, a file, or
// a line of one.
function anchorWords(anchor) {
  if (!anchor || !anchor.path) return "on the pull request";
  return "on " + anchor.path + (anchor.line ? ":" + anchor.line : "");
}

// firstOpenTask is the thread of the newest task still open, for the card's
// count of them to open.
function firstOpenTask(payload) {
  const items = (payload.activity && payload.activity.items) || [];
  const item = items.find((entry) => entry.thread && entry.thread.task && !entry.thread.resolved);
  return item ? item.thread : null;
}
