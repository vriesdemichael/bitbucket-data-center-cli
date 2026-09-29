// The pull request form (#686): what the model drafted, for the person to
// finish and submit. Nothing is created or changed until they do. Submitting
// calls create_pull_request, or update_pull_request for one that exists, and
// the view then shows the pull request.

const SUGGEST_TOOL = "suggest_form_values";
const SUGGEST_DELAY_MS = 250;
const FORM_DRAFT = "form";

function renderPullRequestForm(payload, view) {
  const form = payload.form;
  const show = payload.show || {};
  if (!form || !show.project || !show.repo) return notice("This view has no pull request to fill in.");
  const draft = formDraft(view, form);
  const editing = form.mode === "edit";
  const repository = show.project + "/" + show.repo;

  if (view.formDone) {
    return el("div", { class: "pr-form" },
      notice("Pull request " + repository + " #" + view.formDone.id + (editing ? " is saved." : " is created.")),
      el("div", { class: "actions" }, linkButton("Open in Bitbucket", view.formDone.url, view.bridge, "primary")));
  }

  const busy = draft.busy;
  const canSubmit = canCall(view, editing ? "update_pull_request" : "create_pull_request");
  return el("div", { class: "pr-form" + (view.fullscreen ? " page-form" : "") },
    backButton(view),
    el("div", { class: "pr-top" },
      icon("pullRequest", null, "faint"),
      el("span", { class: "repo-label muted" },
        el("span", { class: "ellipsis", title: repository }, repository),
        editing ? el("span", { class: "nowrap" }, "#" + show.id) : null),
      el("span", { class: "spacer" }),
      badge(editing ? "Editing" : "New")),
    el("h1", { class: "pr-title" }, editing ? "Edit pull request" : "Create pull request"),
    editing && payload.pull_request
      ? el("div", { class: "form-branches" }, branchPair(payload.pull_request))
      : el("div", { class: "form-branches" },
        branchField(draft, "from", "From", "The branch with your changes", show, view, busy),
        icon("arrow", "into"),
        branchField(draft, "to", "To", form.default_branch ? "Default: " + form.default_branch : "The branch to merge into", show, view, busy)),
    formField("Title", textInput(draft, "title", "form.title", "Title", busy)),
    descriptionField(draft, view, busy),
    editing ? reviewersReadOnly(form) : reviewersField(draft, show, view, busy),
    el("label", { class: "form-check" },
      checkbox(draft, busy),
      el("span", {}, "Draft"),
      el("span", { class: "faint" }, "A draft cannot be merged until it is marked ready.")),
    draft.error ? el("p", { class: "form-error", role: "alert" }, draft.error) : null,
    el("div", { class: "form-actions" },
      canSubmit
        ? el("button", {
          type: "button",
          class: "button primary" + (busy ? " busy" : ""),
          disabled: busy,
          onclick: () => submitForm(payload, view),
        }, busy ? (editing ? "Saving…" : "Creating…") : (editing ? "Save" : "Create pull request"))
        : el("span", { class: "faint" }, "This client cannot submit it; ask for it to be created in the conversation."),
      el("span", { class: "faint form-hint" }, editing ? "Nothing changes until you save." : "Nothing is created until you submit.")));
}

// formDraft is what the person has made of the form so far, started from
// what the model drafted.
function formDraft(view, form) {
  if (!view.drafts.has(FORM_DRAFT)) {
    view.drafts.set(FORM_DRAFT, {
      from: form.from_ref || "",
      to: form.to_ref || "",
      title: form.title || "",
      description: form.description || "",
      reviewers: (form.reviewers || []).slice(),
      reviewer: "",
      draft: Boolean(form.draft),
      preview: false,
      busy: false,
      error: null,
    });
  }
  return view.drafts.get(FORM_DRAFT);
}

function formField(label, control, hint) {
  return el("label", { class: "form-field" },
    el("span", { class: "form-label" }, label),
    control,
    hint ? el("span", { class: "faint form-hint-line" }, hint) : null);
}

function textInput(draft, field, key, label, busy, attributes) {
  const input = el("input", Object.assign({
    type: "text",
    "aria-label": label,
    "data-draft": key,
    disabled: busy,
    oninput: (event) => { draft[field] = event.target.value; },
  }, attributes || {}));
  input.value = draft[field];
  return input;
}

// branchField is a branch, with the repository's branches suggested as the
// person types, where the host passes tool calls.
function branchField(draft, field, label, placeholder, show, view, busy) {
  const input = textInput(draft, field, "form." + field, label, busy, { list: "form-branches", placeholder, autocomplete: "off" });
  input.addEventListener("input", () => suggestLater(view, show, "branch", draft[field]));
  input.addEventListener("focus", () => suggestLater(view, show, "branch", draft[field]));
  return el("label", { class: "form-field branch-field" },
    el("span", { class: "form-label" }, label),
    input,
    field === "from" ? suggestionList("form-branches", "branch", view) : null);
}

function descriptionField(draft, view, busy) {
  const write = el("textarea", {
    rows: 8,
    "aria-label": "Description",
    "data-draft": "form.description",
    disabled: busy,
    oninput: (event) => { draft.description = event.target.value; },
  });
  write.value = draft.description;
  return el("div", { class: "form-field" },
    el("div", { class: "form-label-row" },
      el("span", { class: "form-label" }, "Description"),
      el("span", { class: "spacer" }),
      el("button", {
        type: "button",
        class: "button ghost",
        "aria-pressed": draft.preview ? "true" : "false",
        onclick: () => { draft.preview = !draft.preview; render(); },
      }, draft.preview ? "Write" : "Preview")),
    draft.preview
      ? el("div", { class: "form-preview" }, draft.description.trim()
        ? renderMarkdown(draft.description, { bridge: view.bridge })
        : el("p", { class: "faint" }, "Nothing to preview."))
      : write);
}

// reviewersField picks reviewers: each a chip, the people who can read the
// repository suggested as the person types. Enter or a comma adds one.
function reviewersField(draft, show, view, busy) {
  const add = (value) => {
    const name = String(value || "").trim().replace(/,$/, "");
    if (name && !draft.reviewers.includes(name)) draft.reviewers.push(name);
    draft.reviewer = "";
    view.focusDraft = "form.reviewer";
    render();
  };
  const input = textInput(draft, "reviewer", "form.reviewer", "Add a reviewer", busy, {
    list: "form-reviewers",
    placeholder: "Add a reviewer",
    autocomplete: "off",
  });
  input.addEventListener("input", (event) => {
    // Picking a suggestion replaces the text at once.
    if (event.inputType === "insertReplacementText" || (view.formSuggestions.reviewer || []).some((value) => value.value === draft.reviewer)) {
      add(draft.reviewer);
      return;
    }
    suggestLater(view, show, "reviewer", draft.reviewer);
  });
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter" || event.key === ",") {
      event.preventDefault();
      add(draft.reviewer);
    } else if (event.key === "Backspace" && !draft.reviewer && draft.reviewers.length > 0) {
      draft.reviewers.pop();
      view.focusDraft = "form.reviewer";
      render();
    }
  });
  input.addEventListener("focus", () => suggestLater(view, show, "reviewer", draft.reviewer));
  return el("div", { class: "form-field" },
    el("span", { class: "form-label" }, "Reviewers"),
    el("div", { class: "chips" },
      draft.reviewers.map((name) => el("span", { class: "chip reviewer-chip" }, name,
        el("button", {
          type: "button",
          class: "chip-remove",
          "aria-label": "Remove " + name,
          disabled: busy,
          onclick: () => {
            draft.reviewers = draft.reviewers.filter((other) => other !== name);
            render();
          },
        }, "×"))),
      input),
    suggestionList("form-reviewers", "reviewer", view),
    el("span", { class: "faint form-hint-line" }, "The repository's default reviewers for these branches are filled in; only those listed here are added."));
}

function reviewersReadOnly(form) {
  return el("div", { class: "form-field" },
    el("span", { class: "form-label" }, "Reviewers"),
    (form.reviewers || []).length > 0
      ? el("div", { class: "chips" }, form.reviewers.map((name) => el("span", { class: "chip" }, name)))
      : el("span", { class: "faint" }, "None"),
    el("span", { class: "faint form-hint-line" }, "Change reviewers in Bitbucket."));
}

function checkbox(draft, busy) {
  const box = el("input", { type: "checkbox", disabled: busy, onchange: (event) => { draft.draft = event.target.checked; } });
  box.checked = draft.draft;
  return box;
}

// suggestionList is a field's suggestions as the browser offers them.
function suggestionList(id, field, view) {
  return el("datalist", { id }, (view.formSuggestions[field] || []).map((value) => el("option", { value: value.value }, value.label || "")));
}

// suggestLater asks bb for suggestions once the person pauses typing, and
// fills the list in place, without drawing the form again under their
// typing.
function suggestLater(view, show, field, text) {
  if (!canCall(view, SUGGEST_TOOL)) return;
  clearTimeout(view.suggestTimers[field]);
  view.suggestTimers[field] = setTimeout(async () => {
    try {
      const result = await view.bridge.callTool(SUGGEST_TOOL, { project: show.project, repo: show.repo, field, text: text || "" }, REFRESH_TIMEOUT_MS);
      const values = (result && !result.isError && result.structuredContent && result.structuredContent.values) || [];
      view.formSuggestions[field] = values;
      const list = document.getElementById(field === "branch" ? "form-branches" : "form-reviewers");
      if (list) list.replaceChildren(...values.map((value) => el("option", { value: value.value }, value.label || "")));
    } catch {
      // Suggestions are a help; the person can still type the value.
    }
  }, SUGGEST_DELAY_MS);
}

// submitForm creates or saves the pull request through the model's tool,
// and shows it once it is done.
async function submitForm(payload, view) {
  const form = payload.form;
  const show = payload.show;
  const draft = formDraft(view, form);
  if (draft.busy) return;
  const editing = form.mode === "edit";
  const title = draft.title.trim();
  const problem = !title ? "A pull request needs a title." : !editing && !draft.from.trim() ? "Pick the branch with your changes." : "";
  if (problem) {
    draft.error = problem;
    render();
    return;
  }
  if (draft.reviewer.trim()) draft.reviewers.push(draft.reviewer.trim());
  draft.reviewer = "";
  draft.busy = true;
  draft.error = null;
  render();
  try {
    let id = show.id;
    if (editing) {
      const args = { project: show.project, repo: show.repo, pr_id: String(show.id), version: form.version, title, description: draft.description };
      // Only a change to the draft flag is sent: setting it asks the person.
      if (draft.draft !== Boolean(form.draft)) args.draft = draft.draft;
      await callForPerson(view, "update_pull_request", args);
    } else {
      const args = {
        project: show.project,
        repo: show.repo,
        from_ref: draft.from.trim(),
        title,
        description: draft.description,
        reviewers: draft.reviewers.join(","),
        draft: draft.draft,
      };
      if (draft.to.trim()) args.to_ref = draft.to.trim();
      const result = await callForPerson(view, "create_pull_request", args);
      const created = ((result && result.structuredContent) || {}).pull_request || {};
      id = created.id;
    }
    const target = show.project + "/" + show.repo + "#" + id;
    const lead = "The person " + (editing ? "saved pull request " : "created pull request ") + target + " with the form you showed them.";
    view.drafts.delete(FORM_DRAFT);
    if (id && canOpen(view, "pull_request")) {
      await openInView(view, { kind: "pull_request", project: show.project, repo: show.repo, id: String(id) }, true, lead);
      return;
    }
    view.formDone = { id, url: form.repository_url && id ? form.repository_url + "/pull-requests/" + id + "/overview" : "" };
    if (view.tellsModel) view.bridge.updateModelContext(lead).catch(() => {});
    render();
  } catch (error) {
    draft.busy = false;
    draft.error = (error && error.message) || "It did not go through.";
    render();
  }
}
