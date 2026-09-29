// What the person does in a view (ADR-101): a reply, a comment, a review, a
// pull request. Each goes through the model's own tool, called through the
// host, so the scope, the audit trail and the confirmation of a tool that
// asks apply to it as to the model's call. The view then reads itself again
// to show what changed.

// ACTION_TIMEOUT_MS is how long a view waits for a tool it called for the
// person. A tool that asks waits on the person's answer in the client.
const ACTION_TIMEOUT_MS = 5 * 60 * 1000;

// callForPerson calls one of the model's tools for the person, and refuses
// an answer that says the call failed.
async function callForPerson(view, tool, args) {
  const result = await view.bridge.callTool(tool, args, ACTION_TIMEOUT_MS);
  if (!result || result.isError) throw new Error(textOf(result) || "It did not go through.");
  return result;
}

// pullRequestArgs are the project, repository and id a pull request's tools
// take.
function pullRequestArgs(pr) {
  return { project: pr.repository.project_key, repo: pr.repository.slug, pr_id: String(pr.id) };
}

// Drafts: what the person is writing, kept in the view by where it is, so a
// redraw, a refresh or a resize keeps it and the caret where it was.

function openDraft(view, key, text) {
  if (!view.drafts.has(key)) view.drafts.set(key, { text: text || "", busy: false, error: null });
  view.focusDraft = key;
  render();
}

function closeDraft(view, key) {
  view.drafts.delete(key);
  render();
}

// submitDraft sends what the person wrote, keeps it where sending failed,
// and reads the view again where it went through.
async function submitDraft(view, key, send) {
  const draft = view.drafts.get(key);
  if (!draft || draft.busy) return;
  const text = draft.text.trim();
  if (!text) {
    draft.error = "Write something first.";
    view.focusDraft = key;
    render();
    return;
  }
  draft.busy = true;
  draft.error = null;
  render();
  try {
    await send(text);
    view.drafts.delete(key);
    render();
    refreshView(view, true);
  } catch (error) {
    draft.busy = false;
    draft.error = (error && error.message) || "It did not go through.";
    view.focusDraft = key;
    render();
  }
}

// commentForm is a box to write a comment in, sent with Ctrl+Enter or the
// button, and what went wrong when it was not. It is no <form>: a view's
// frame may be sandboxed without forms, where a form never submits.
function commentForm(view, key, options) {
  const draft = view.drafts.get(key);
  const input = el("textarea", {
    class: "comment-input",
    rows: 3,
    placeholder: options.placeholder,
    "aria-label": options.placeholder,
    "data-draft": key,
    disabled: draft.busy,
    oninput: (event) => { draft.text = event.target.value; },
    onkeydown: (event) => {
      if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
        event.preventDefault();
        submitDraft(view, key, options.send);
      }
    },
  });
  input.value = draft.text;
  return el("div", { class: "comment-form", role: "group", "aria-label": options.placeholder },
  input,
  draft.error ? el("p", { class: "form-error", role: "alert" }, draft.error) : null,
  el("div", { class: "form-actions" },
    el("button", {
      type: "button",
      class: "button primary send-button" + (draft.busy ? " busy" : ""),
      disabled: draft.busy,
      onclick: () => submitDraft(view, key, options.send),
    }, draft.busy ? options.busyLabel : options.submitLabel),
    el("button", { type: "button", class: "button ghost", disabled: draft.busy, onclick: () => closeDraft(view, key) }, "Cancel"),
    el("span", { class: "faint form-hint" }, "Markdown · Ctrl+Enter sends")));
}

// reviewActions approve or request changes, as Bitbucket's pull request
// page does, for someone other than the author while the pull request is
// open. Pressed again, either takes the review back. submit_pr_review asks
// the person to confirm in the client before it runs.
function reviewActions(pr, payload, view) {
  const me = payload.me;
  if (!me || me.author || !pr.repository || pr.state !== "OPEN" || !canCall(view, "submit_pr_review")) return null;
  const approved = me.status === "APPROVED";
  const changesRequested = me.status === "NEEDS_WORK";
  const busy = Boolean(view.reviewing);
  return el("div", { class: "review-actions", role: "group", "aria-label": "Your review" },
    el("button", {
      type: "button",
      class: "button" + (approved ? "" : " primary") + (view.reviewing === (approved ? "unapprove" : "approve") ? " busy" : ""),
      "aria-pressed": approved ? "true" : "false",
      title: approved ? "Take back your approval" : "Approve this pull request",
      disabled: busy,
      onclick: () => submitReview(pr, approved ? "unapprove" : "approve", view),
    }, icon("approved"), approved ? "Approved" : "Approve"),
    el("button", {
      type: "button",
      class: "button" + (view.reviewing === (changesRequested ? "unapprove" : "needs_work") ? " busy" : ""),
      "aria-pressed": changesRequested ? "true" : "false",
      title: changesRequested ? "Take back your request for changes" : "Request changes to this pull request",
      disabled: busy,
      onclick: () => submitReview(pr, changesRequested ? "unapprove" : "needs_work", view),
    }, icon("changesRequested"), changesRequested ? "Changes requested" : "Request changes"),
    view.reviewError ? el("p", { class: "form-error", role: "alert" }, view.reviewError) : null);
}

async function submitReview(pr, action, view) {
  if (view.reviewing) return;
  view.reviewing = action;
  view.reviewError = null;
  render();
  try {
    await callForPerson(view, "submit_pr_review", Object.assign(pullRequestArgs(pr), { action }));
    refreshView(view, true);
  } catch (error) {
    view.reviewError = (error && error.message) || "The review did not go through.";
  } finally {
    view.reviewing = null;
    render();
  }
}

// replyArea replies to a thread, where the server lets a view comment.
function replyArea(thread, pr, view) {
  if (!pr.repository || !canCall(view, "add_pr_comment")) return null;
  const key = "reply-" + thread.id;
  if (!view.drafts.has(key)) {
    return el("div", { class: "reply-row" },
      el("button", { type: "button", class: "button ghost", onclick: () => openDraft(view, key) }, icon("comment"), "Reply"));
  }
  return commentForm(view, key, {
    placeholder: "Reply to " + (thread.author || "this thread"),
    submitLabel: "Reply",
    busyLabel: "Replying…",
    send: (text) => callForPerson(view, "add_pr_comment", Object.assign(pullRequestArgs(pr), { text, parent_id: thread.id })),
  });
}

// newCommentArea comments on the pull request itself.
function newCommentArea(pr, view) {
  if (!pr.repository || !canCall(view, "add_pr_comment")) return null;
  const key = "comment-pr";
  if (!view.drafts.has(key)) {
    return el("div", { class: "new-comment" },
      el("button", { type: "button", class: "button", onclick: () => openDraft(view, key) }, icon("comment"), "Add a comment"));
  }
  return commentForm(view, key, {
    placeholder: "Comment on the pull request",
    submitLabel: "Comment",
    busyLabel: "Commenting…",
    send: (text) => callForPerson(view, "add_pr_comment", Object.assign(pullRequestArgs(pr), { text })),
  });
}
