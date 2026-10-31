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
  return { project: pr.repository.project_key, repo: pr.repository.slug, pr_id: pr.id };
}

// Drafts: what the person is writing, kept in the view by where it is, so a
// redraw, a refresh or a resize keeps it and the caret where it was.

function openDraft(view, key, text) {
  const held = view.drafts.get(key);
  if (!held || held.sent) view.drafts.set(key, { text: text || "", busy: false, error: null });
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
    // A comment from a view waits in the person's review, which nothing the
    // view reads shows them, so where it went is said in its place.
    view.drafts.set(key, { text: "", busy: false, error: null, sent: true });
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
  if (draft.sent) return addedToReview(view, key, options.reviewURL);
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

// addedToReview says where a comment sent from the view went: into the
// person's review, which only they see until they publish it (#796).
function addedToReview(view, key, url) {
  return el("p", { class: "added-to-review", role: "status" },
    "Added to your review, which only you see until you publish it with Finish review in Bitbucket. ",
    isWebURL(url) ? el("button", { type: "button", class: "button ghost", onclick: () => openLink(view.bridge, url) }, "Open in Bitbucket") : null,
    el("button", { type: "button", class: "button ghost", onclick: () => closeDraft(view, key) }, "Done"));
}

// reviewActions are Bitbucket's Approve and Request changes, for someone
// other than the author while the pull request is open. Nothing here approves:
// an approval made with the person's token is indistinguishable from theirs,
// so each button prepares the review with prepare_pr_review and shows where
// the draft waits, for the person to publish it in Bitbucket (#796).
function reviewActions(pr, payload, view) {
  const me = payload.me;
  const inBitbucket = callsInBitbucket(view, "prepare_pr_review");
  if (!me || me.author || !pr.repository || pr.state !== "OPEN" || !(inBitbucket || canCall(view, "prepare_pr_review"))) return null;
  const busy = Boolean(view.reviewing);
  const status = me.status === "APPROVED" ? "You approved" : me.status === "NEEDS_WORK" ? "You requested changes" : null;
  const act = (suggested) => (inBitbucket ? () => openLink(view.bridge, pr.url) : () => prepareReview(pr, suggested, view));
  return el("div", { class: "review-actions", role: "group", "aria-label": "Your review" },
    status ? el("span", { class: "faint" }, status) : null,
    el("button", {
      type: "button",
      class: "button primary" + (view.reviewing === "approve" ? " busy" : ""),
      title: inBitbucket ? "Open the pull request in Bitbucket, to approve it there" : "Prepare your review, to publish in Bitbucket as an approval",
      disabled: busy,
      onclick: act("approve"),
    }, icon("approved"), "Approve", inBitbucket ? icon("external", null, "link-icon") : null),
    el("button", {
      type: "button",
      class: "button" + (view.reviewing === "needs_work" ? " busy" : ""),
      title: inBitbucket ? "Open the pull request in Bitbucket, to request changes there" : "Prepare your review, to publish in Bitbucket as needing work",
      disabled: busy,
      onclick: act("needs_work"),
    }, icon("changesRequested"), "Request changes", inBitbucket ? icon("external", null, "link-icon") : null),
    preparedReview(view),
    view.reviewError ? el("p", { class: "form-error", role: "alert" }, view.reviewError) : null);
}

// preparedReview says where the person's review waits, what it holds, and
// what to choose when they publish it, in Bitbucket's words. With no comment
// in it there is no review to finish, and the choice is made on its own.
function preparedReview(view) {
  const review = view.preparedReview;
  if (!review) return null;
  const choose = review.suggested_status === "needs_work" ? "Request changes" : "Approve";
  const said = review.pending_comments > 0
    ? "Your review waits in Bitbucket with " + plural(review.pending_comments, "comment") + " only you see. Publish it there with Finish review, and choose " + choose + ". "
    : "No comments wait in your review. Choose " + choose + " in Bitbucket. ";
  return el("p", { class: "review-prepared", role: "status" },
    said,
    isWebURL(review.url) ? el("button", { type: "button", class: "button ghost", onclick: () => openLink(view.bridge, review.url) }, "Open in Bitbucket") : null);
}

async function prepareReview(pr, suggested, view) {
  if (view.reviewing) return;
  view.reviewing = suggested;
  view.reviewError = null;
  render();
  try {
    const result = await callForPerson(view, "prepare_pr_review", Object.assign(pullRequestArgs(pr), { suggested_status: suggested }));
    view.preparedReview = (result.structuredContent && result.structuredContent.review) || null;
  } catch (error) {
    view.reviewError = (error && error.message) || "The review could not be prepared.";
  } finally {
    view.reviewing = null;
    render();
  }
}

// newCommentArea comments on the pull request itself.
function newCommentArea(pr, view) {
  if (pr.repository && callsInBitbucket(view, "add_pr_comment")) {
    return el("div", { class: "new-comment" }, linkButton("Add a comment", pr.url, view.bridge));
  }
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
    reviewURL: pr.url,
    send: (text) => callForPerson(view, "add_pr_comment", Object.assign(pullRequestArgs(pr), { text })),
  });
}
