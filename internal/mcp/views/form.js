// The pull request form (#686): what the model drafted, for the person to
// finish and submit. Nothing is created or changed until they do. Submitting
// calls create_pull_request, or update_pull_request for one that exists, and
// the view then shows the pull request.
//
// It is laid out and works as Bitbucket's create page (10.4). The source sits
// over the destination, each branch picked from a list the person searches,
// never typed. The reviewers start as the page fills them in: whoever the
// model named, then the default reviewers and code owners for the branches.
// One the person removes is offered back by the page's quick-add buttons, and
// picking other branches adds their default reviewers and code owners, as
// continuing with other branches does on the page.

const SUGGEST_TOOL = "suggest_form_values";
const SUGGEST_DELAY_MS = 250;
const FORM_DRAFT = "form";
const BRANCH_LIST = "form-branch-list";
const REVIEWER_LIST = "form-reviewer-list";

// The form's icons, drawn as dom.js draws the others: the swap between the
// branches, the plus on a quick-add button, and a repository.
ICON_SHAPES.swap = [["path", "M3.6 9.6a4.6 4.6 0 0 1 7.7-5"], ["path", "M11.8 2v2.9H8.9"], ["path", "M12.4 6.4a4.6 4.6 0 0 1-7.7 5"], ["path", "M4.2 14v-2.9h2.9"]];
ICON_SHAPES.plus = [["path", "M8 3.5v9M3.5 8h9"]];
ICON_SHAPES.repository = [["box", 2, 2, 12, 12, 3], ["cut", "M6.6 5.6L4.4 8l2.2 2.4M9.4 5.6l2.2 2.4-2.2 2.4"]];

function renderPullRequestForm(payload, view) {
  const form = payload.form;
  const show = payload.show || {};
  if (!form || !show.project || !show.repo) return notice("This view has no pull request to fill in.");
  const editing = form.mode === "edit";
  const draft = formDraft(view, form, payload.avatars);
  const repository = show.project + "/" + show.repo;

  if (view.formDone) {
    return el("div", { class: "pr-form" },
      notice("Pull request " + repository + " #" + view.formDone.id + (editing ? " is saved." : " is created.")),
      el("div", { class: "actions" }, linkButton("Open in Bitbucket", view.formDone.url, view.bridge, "primary")));
  }

  const busy = Boolean(draft.busy);
  const pr = editing ? payload.pull_request : null;
  // Once the form is on the page, it makes room for an open reviewer list.
  if (draft.searching) queueMicrotask(fitReviewerList);
  return el("div", { class: "pr-form" + (view.fullscreen ? " page-form" : "") },
    backButton(view),
    el("div", { class: "pr-top" },
      icon("pullRequest", null, "faint"),
      el("span", { class: "repo-label muted" },
        el("span", { class: "ellipsis", title: repository }, repository),
        editing ? el("span", { class: "nowrap" }, "#" + show.id) : null),
      el("span", { class: "spacer" }),
      pr ? stateBadges(pr) : null),
    el("h1", { class: "pr-title" }, editing ? "Edit Pull Request" : "Create pull request"),
    editing ? null : el("p", { class: "form-lead muted" }, "Collaborate on code by choosing teammates to review your changes to a branch."),
    editing
      ? (pr ? el("div", { class: "form-branches" }, branchPair(pr)) : null)
      : branchRows(draft, show, view, busy),
    titleField(draft, busy),
    descriptionField(draft, view, busy),
    editing ? pullRequestReviewers(draft) : reviewersField(draft, show, view, busy),
    editing ? draftCheckbox(draft, busy) : null,
    draft.error ? el("p", { class: "form-error", role: "alert" }, draft.error) : null,
    formActions(payload, view, draft, editing, busy));
}

// formDraft is what the person has made of the form so far, started from
// what the model drafted, and the people it names, by username.
function formDraft(view, form, avatars) {
  if (!view.drafts.has(FORM_DRAFT)) {
    const draft = {
      from: form.from_ref || "",
      to: form.to_ref || "",
      title: form.title || "",
      description: form.description || "",
      reviewers: (form.reviewers || []).slice(),
      // Whom Bitbucket names for the branches, which the quick-add buttons
      // offer back, and whether it could be asked.
      defaults: (form.default_reviewers || []).slice(),
      owners: (form.code_owners || []).slice(),
      unread: { defaults: Boolean(form.default_reviewers_unread), owners: Boolean(form.code_owners_unread) },
      people: new Map(),
      // The reviewer search: what the person typed, whether its list is
      // open, and the suggestion the arrows are on.
      search: "",
      searching: false,
      found: 0,
      // The branch picker whose list is open, what was typed in its search,
      // and the branch the arrows are on.
      picker: null,
      pickerText: "",
      pickerActive: 0,
      // The latest lookup of reviewers for the branches, and each field's
      // latest request for suggestions: an older answer is dropped.
      lookup: 0,
      lookingUp: false,
      turns: {},
      draft: Boolean(form.draft),
      preview: false,
      // busy is the button whose call is on its way.
      busy: null,
      error: null,
    };
    for (const [name, person] of Object.entries(form.people || {})) {
      rememberPerson(draft, { value: name, label: person && person.display_name, avatar: avatars && avatars[name] });
    }
    view.drafts.set(FORM_DRAFT, draft);
  }
  return view.drafts.get(FORM_DRAFT);
}

// People are known by username, in any case: Bitbucket's usernames do not
// differ by case alone.
function rememberPerson(draft, value) {
  if (!value || typeof value.value !== "string" || !value.value) return;
  const key = value.value.toLowerCase();
  const known = draft.people.get(key) || {};
  draft.people.set(key, {
    name: known.name || value.value,
    display_name: value.label || known.display_name || "",
    avatar: value.avatar || known.avatar || "",
  });
}

function personOf(draft, name) {
  return draft.people.get(String(name).toLowerCase()) || { name: String(name), display_name: "", avatar: "" };
}

function personAvatar(person, size) {
  return avatar(person.name, person.display_name, person.avatar ? { [person.name]: person.avatar } : null, size);
}

function hasReviewer(draft, name) {
  const key = String(name).toLowerCase();
  return draft.reviewers.some((other) => other.toLowerCase() === key);
}

function addReviewerNames(draft, names) {
  for (const name of names) {
    if (name && !hasReviewer(draft, name)) draft.reviewers.push(name);
  }
}

// branchRows are the source over the destination, as the create page lays
// them out: each the repository and a branch picker, joined by a bracket,
// with the swap button beside it. A pull request the form makes stays within
// the one repository, so the repository is shown rather than picked.
function branchRows(draft, show, view, busy) {
  const canPick = canCall(view, SUGGEST_TOOL) && !busy;
  return el("div", { class: "branch-rows" },
    el("button", {
      type: "button",
      class: "button swap-button",
      title: "Swap source and destination",
      "data-draft": "form.swap",
      disabled: !canPick || !draft.from || !draft.to,
      onclick: () => swapBranches(view, show, draft),
    }, icon("swap"), el("span", { class: "visually-hidden" }, "Swap")),
    el("span", { class: "branch-bracket", "aria-hidden": "true" }),
    branchRow(draft, "from", "Source", "Source branch", show, view, canPick),
    branchRow(draft, "to", "Destination", "Destination branch", show, view, canPick));
}

function branchRow(draft, field, label, name, show, view, canPick) {
  // A list closes in place, without a redraw, so the handlers ask whether it
  // is open when they run.
  const open = draft.picker === field;
  const value = draft[field];
  return el("div", { class: "branch-row" },
    el("span", { class: "form-label" }, label),
    el("div", { class: "branch-controls" },
      el("span", { class: "repository-box", title: show.project + "/" + show.repo },
        icon("repository", null, "repository-icon"),
        el("span", { class: "ellipsis" }, show.project + " / " + show.repo)),
      el("div", {
        class: "picker" + (open ? " open" : ""),
        "data-picker": field,
        onfocusout: () => whenFocusLeaves('[data-picker="' + field + '"]', () => {
          if (draft.picker === field) closePicker(draft, false);
        }),
      },
        el("button", {
          type: "button",
          class: "picker-button",
          role: "combobox",
          "aria-haspopup": "listbox",
          "aria-expanded": open ? "true" : "false",
          "aria-controls": open ? BRANCH_LIST : null,
          "aria-label": name,
          "data-draft": "form.picker." + field,
          title: value || null,
          disabled: !canPick,
          onclick: () => (draft.picker === field ? closePicker(draft, true) : openPicker(view, show, draft, field)),
          onkeydown: (event) => {
            if (draft.picker !== field && (event.key === "ArrowDown" || event.key === "ArrowUp")) {
              event.preventDefault();
              openPicker(view, show, draft, field);
            }
          },
        },
          icon("branch", null, "faint"),
          el("span", { class: "ellipsis picker-value" + (value ? "" : " faint") }, value || "Select branch"),
          icon("chevronDown", null, "faint")),
        open ? branchPopup(draft, name, show, view) : null)));
}

// branchPopup is an open picker: a search, and the repository's branches
// that match it. Only a branch in the list can be picked.
function branchPopup(draft, name, show, view) {
  const search = el("input", {
    type: "text",
    class: "picker-search",
    role: "combobox",
    "aria-autocomplete": "list",
    "aria-expanded": "true",
    "aria-controls": BRANCH_LIST,
    "aria-label": name,
    placeholder: "Enter a branch name",
    autocomplete: "off",
    spellcheck: "false",
    "data-draft": "form.pickerText",
    oninput: (event) => {
      draft.pickerText = event.target.value;
      suggestLater(view, show, "branch", draft.pickerText);
    },
    onkeydown: (event) => pickerKeys(event, view, show, draft),
  });
  search.value = draft.pickerText;
  const options = branchOptions(draft, show, view);
  markActive(search, options);
  return el("div", {
    class: "picker-popup",
    // A click in the list leaves the caret in the search.
    onmousedown: (event) => {
      if (event.target !== search) event.preventDefault();
    },
  },
    el("label", { class: "picker-search-row" }, icon("branch", null, "faint"), search),
    el("ul", { class: "picker-list", role: "listbox", id: BRANCH_LIST, "aria-label": name }, options));
}

function branchOptions(draft, show, view) {
  const values = view.formSuggestions.branch;
  if (values === undefined) return [pickerNote("Loading…")];
  if (values === null) return [pickerNote("The branches could not be read.")];
  if (values.length === 0) return [pickerNote("No branches found")];
  const current = draft[draft.picker];
  return values.map((value, index) => el("li", {
    id: BRANCH_LIST + "-" + index,
    class: "picker-option" + (index === draft.pickerActive ? " active" : "") + (value.value === current ? " current" : ""),
    role: "option",
    "aria-selected": index === draft.pickerActive ? "true" : "false",
    title: value.value,
    onclick: () => pickBranch(view, show, draft, value.value),
    onmousemove: () => {
      if (draft.pickerActive !== index) {
        draft.pickerActive = index;
        paintFormList(view, "branch");
      }
    },
  }, el("span", { class: "ellipsis" }, value.value)));
}

function pickerNote(text) {
  return el("li", { class: "picker-note", role: "presentation" }, text);
}

// markActive points a search at the option its arrows are on.
function markActive(search, options) {
  const active = options.find((option) => option.classList && option.classList.contains("active"));
  if (active) search.setAttribute("aria-activedescendant", active.id);
  else search.removeAttribute("aria-activedescendant");
}

function pickerKeys(event, view, show, draft) {
  const values = view.formSuggestions.branch || [];
  switch (event.key) {
    case "ArrowDown":
    case "ArrowUp":
      event.preventDefault();
      if (values.length === 0) return;
      draft.pickerActive = Math.max(0, Math.min(values.length - 1, draft.pickerActive + (event.key === "ArrowDown" ? 1 : -1)));
      paintFormList(view, "branch");
      return;
    case "Enter":
      event.preventDefault();
      if (values[draft.pickerActive]) pickBranch(view, show, draft, values[draft.pickerActive].value);
      return;
    case "Escape":
      event.preventDefault();
      closePicker(draft, true);
  }
}

function openPicker(view, show, draft, field) {
  draft.picker = field;
  draft.pickerText = "";
  draft.pickerActive = 0;
  draft.searching = false;
  view.formSuggestions.branch = undefined;
  view.focusDraft = "form.pickerText";
  render();
  suggestLater(view, show, "branch", "", 0);
}

// closePicker takes an open list away where it is, rather than drawing the
// form again: a click that closed it lands on what it was aimed at.
function closePicker(draft, refocus) {
  const field = draft.picker;
  if (!field) return;
  draft.picker = null;
  draft.pickerText = "";
  const holder = document.querySelector('[data-picker="' + field + '"]');
  if (!holder) return;
  holder.classList.remove("open");
  const popup = holder.querySelector(".picker-popup");
  if (popup) popup.remove();
  const button = holder.querySelector(".picker-button");
  if (button) {
    button.setAttribute("aria-expanded", "false");
    button.removeAttribute("aria-controls");
    if (refocus) button.focus();
  }
}

// whenFocusLeaves closes a list whose focus moved out of it, as a Tab moves
// it, once the focus has landed: so that where it lands is not drawn again
// under it, and a list only just drawn, which takes the focus, stays open.
function whenFocusLeaves(selector, close) {
  setTimeout(() => {
    const holder = document.querySelector(selector);
    if (!holder || !holder.contains(document.activeElement)) close();
  }, 0);
}

function pickBranch(view, show, draft, branch) {
  const field = draft.picker;
  if (!field) return;
  const changed = draft[field] !== branch;
  draft[field] = branch;
  draft.picker = null;
  draft.pickerText = "";
  view.focusDraft = "form.picker." + field;
  if (changed) lookUpReviewers(view, show, draft);
  else render();
}

function swapBranches(view, show, draft) {
  const from = draft.from;
  draft.from = draft.to;
  draft.to = from;
  view.focusDraft = "form.swap";
  lookUpReviewers(view, show, draft);
}

// lookUpReviewers asks whom Bitbucket names for the branches now picked, and
// adds them to the reviewers, as the create page does when the person
// continues with other branches; the quick-add buttons offer them from then
// on. A newer pick overtakes an answer still on its way, and nothing is sent
// until the answer is in.
async function lookUpReviewers(view, show, draft) {
  const turn = ++draft.lookup;
  draft.defaults = [];
  draft.owners = [];
  draft.unread = { defaults: false, owners: false };
  draft.error = null;
  if (!draft.from || !draft.to || draft.from === draft.to || !canCall(view, SUGGEST_TOOL)) {
    draft.lookingUp = false;
    render();
    return;
  }
  draft.lookingUp = true;
  render();
  const ask = async (field) => {
    try {
      const result = await view.bridge.callTool(SUGGEST_TOOL, { project: show.project, repo: show.repo, field, from_ref: draft.from, to_ref: draft.to }, REFRESH_TIMEOUT_MS);
      if (!result || result.isError) return null;
      return (result.structuredContent && result.structuredContent.values) || [];
    } catch {
      return null;
    }
  };
  const [defaults, owners] = await Promise.all([ask("default_reviewers"), ask("code_owners")]);
  if (turn !== draft.lookup || view.drafts.get(FORM_DRAFT) !== draft) return;
  for (const value of (defaults || []).concat(owners || [])) rememberPerson(draft, value);
  draft.defaults = (defaults || []).map((value) => value.value);
  draft.owners = (owners || []).map((value) => value.value);
  draft.unread = { defaults: defaults === null, owners: owners === null };
  addReviewerNames(draft, draft.defaults.concat(draft.owners));
  draft.lookingUp = false;
  render();
}

function titleField(draft, busy) {
  const input = el("input", {
    type: "text",
    id: "form-title",
    "aria-required": "true",
    "data-draft": "form.title",
    disabled: busy,
    oninput: (event) => { draft.title = event.target.value; },
  });
  input.value = draft.title;
  return el("div", { class: "form-field" },
    el("label", { class: "form-label", for: "form-title" }, "Title", el("span", { class: "required", "aria-hidden": "true" }, "*")),
    input);
}

function descriptionField(draft, view, busy) {
  const write = el("textarea", {
    rows: 8,
    id: "form-description",
    "data-draft": "form.description",
    disabled: busy,
    oninput: (event) => { draft.description = event.target.value; },
  });
  write.value = draft.description;
  return el("div", { class: "form-field" },
    el("div", { class: "form-label-row" },
      el("label", { class: "form-label", for: draft.preview ? null : "form-description" }, "Description"),
      el("span", { class: "spacer" }),
      el("button", {
        type: "button",
        class: "button ghost preview-toggle",
        "aria-pressed": draft.preview ? "true" : "false",
        title: draft.preview ? null : "View a preview of this description",
        "data-draft": "form.preview",
        onclick: () => { draft.preview = !draft.preview; render(); },
      }, draft.preview ? "Edit" : "Preview")),
    draft.preview
      ? el("div", { class: "form-preview" }, draft.description.trim()
        ? renderMarkdown(draft.description, { bridge: view.bridge })
        : el("p", { class: "faint" }, "Nothing to preview."))
      : write);
}

// reviewersField picks reviewers as the create page does: each a chip with
// their avatar, found by name in a list of the people who can read the
// repository. Bitbucket's field takes reviewer groups too; the form adds
// people only, as create_pull_request takes them.
function reviewersField(draft, show, view, busy) {
  const canSearch = canCall(view, SUGGEST_TOOL) && !busy;
  const open = draft.searching;
  const search = el("input", {
    type: "text",
    id: "form-reviewer-search",
    class: "reviewer-search",
    role: "combobox",
    "aria-autocomplete": "list",
    "aria-expanded": open ? "true" : "false",
    "aria-controls": open ? REVIEWER_LIST : null,
    "aria-describedby": "form-reviewers-hint",
    placeholder: draft.reviewers.length === 0 ? "Start typing to find users" : "",
    autocomplete: "off",
    spellcheck: "false",
    "data-draft": "form.search",
    disabled: !canSearch,
    oninput: (event) => {
      draft.search = event.target.value;
      openReviewerList(view, show, draft);
    },
    onkeydown: (event) => reviewerKeys(event, view, show, draft),
  });
  search.value = draft.search;
  const options = open ? reviewerOptions(draft, view) : [];
  if (open) markActive(search, options);
  return el("div", {
    class: "form-field reviewers-field",
    onfocusout: () => whenFocusLeaves(".reviewers-field", () => closeReviewerList(draft)),
  },
    el("label", { class: "form-label", for: "form-reviewer-search" }, "Reviewers"),
    el("div", { class: "reviewer-anchor" },
      el("div", {
        class: "reviewer-box" + (canSearch ? "" : " disabled"),
        // A click beside the chips puts the caret in the search.
        onmousedown: (event) => {
          if (event.target === event.currentTarget && canSearch) {
            event.preventDefault();
            search.focus();
          }
        },
      },
        draft.reviewers.map((name) => reviewerChip(draft, name, view, busy)),
        search),
      open
        ? el("ul", {
          class: "picker-list reviewer-list",
          role: "listbox",
          id: REVIEWER_LIST,
          "aria-label": "Users",
          // A click in the list leaves the caret in the search.
          onmousedown: (event) => event.preventDefault(),
        }, options)
        : null),
    el("span", { class: "faint form-hint-line", id: "form-reviewers-hint" }, "Locate individual users who can approve this pull request."),
    reviewerNotes(draft),
    quickAddButtons(draft, view, busy));
}

// reviewerChip is a reviewer as the create page draws one: their avatar,
// their name, and, where they can be removed, a button that removes them.
function reviewerChip(draft, name, view, busy) {
  const person = personOf(draft, name);
  const shown = person.display_name || person.name;
  return el("span", {
    class: "reviewer-chip",
    title: person.display_name ? person.display_name + " (" + person.name + ")" : person.name,
    "data-reviewer": person.name,
  },
    personAvatar(person, "sm"),
    el("span", { class: "ellipsis reviewer-name" }, shown),
    view
      ? el("button", {
        type: "button",
        class: "chip-remove",
        "aria-label": "Remove " + shown,
        disabled: busy,
        onclick: () => {
          draft.reviewers = draft.reviewers.filter((other) => other.toLowerCase() !== name.toLowerCase());
          view.focusDraft = "form.search";
          render();
        },
      }, "×")
      : null);
}

// reviewerMatches are the people found for what the person typed, less
// those already chosen: undefined while the answer is on its way, and null
// when bb could not answer.
function reviewerMatches(draft, view) {
  const values = view.formSuggestions.reviewer;
  if (!Array.isArray(values)) return values;
  return values.filter((value) => value && value.value && !hasReviewer(draft, value.value));
}

function reviewerOptions(draft, view) {
  const values = reviewerMatches(draft, view);
  if (values === undefined) return [pickerNote("Loading…")];
  if (values === null) return [pickerNote("The users could not be read.")];
  if (values.length === 0) return [pickerNote("No matches found")];
  return values.map((value, index) => {
    const person = personOf(draft, value.value);
    return el("li", {
      id: REVIEWER_LIST + "-" + index,
      class: "picker-option person-option" + (index === draft.found ? " active" : ""),
      role: "option",
      "aria-selected": index === draft.found ? "true" : "false",
      onclick: () => addReviewer(view, draft, value.value),
      onmousemove: () => {
        if (draft.found !== index) {
          draft.found = index;
          paintFormList(view, "reviewer");
        }
      },
    },
      personAvatar(person, "sm"),
      el("span", { class: "ellipsis" }, person.display_name || person.name),
      person.display_name ? el("span", { class: "faint ellipsis option-detail" }, person.name) : null);
  });
}

function reviewerKeys(event, view, show, draft) {
  const values = reviewerMatches(draft, view) || [];
  switch (event.key) {
    case "ArrowDown":
    case "ArrowUp":
      event.preventDefault();
      if (!draft.searching) {
        openReviewerList(view, show, draft);
        return;
      }
      if (values.length === 0) return;
      draft.found = Math.max(0, Math.min(values.length - 1, draft.found + (event.key === "ArrowDown" ? 1 : -1)));
      paintFormList(view, "reviewer");
      return;
    case "Enter":
      // Enter adds the person the arrows are on, and never what was typed.
      event.preventDefault();
      if (draft.searching && values[draft.found]) addReviewer(view, draft, values[draft.found].value);
      return;
    case "Escape":
      if (draft.searching) {
        event.preventDefault();
        closeReviewerList(draft);
      }
      return;
    case "Backspace":
      if (!draft.search && draft.reviewers.length > 0) {
        event.preventDefault();
        draft.reviewers.pop();
        view.focusDraft = "form.search";
        render();
      }
  }
}

// openReviewerList opens the list under the search, which already has the
// caret, and asks for the people who match what was typed.
function openReviewerList(view, show, draft) {
  if (!draft.searching) {
    draft.searching = true;
    draft.found = 0;
    view.formSuggestions.reviewer = undefined;
    render();
  }
  suggestLater(view, show, "reviewer", draft.search);
}

// closeReviewerList takes the list away where it is, as closePicker does.
function closeReviewerList(draft) {
  if (!draft.searching) return;
  draft.searching = false;
  const list = document.getElementById(REVIEWER_LIST);
  if (list) list.remove();
  const search = document.getElementById("form-reviewer-search");
  if (search) {
    search.setAttribute("aria-expanded", "false");
    search.removeAttribute("aria-controls");
    search.removeAttribute("aria-activedescendant");
  }
  fitReviewerList();
}

// fitReviewerList makes room below the form for the reviewer list, which
// lies over what follows it, so that an inline view grows to hold the list
// rather than cut it off. The room goes below the buttons, which stay where
// they were.
function fitReviewerList() {
  const form = document.querySelector(".pr-form");
  if (!form) return;
  form.style.paddingBottom = "";
  const list = document.getElementById(REVIEWER_LIST);
  if (!list) return;
  const below = list.getBoundingClientRect().bottom - form.getBoundingClientRect().bottom;
  if (below > 0) form.style.paddingBottom = Math.ceil(below + 16) + "px";
}

function addReviewer(view, draft, name) {
  addReviewerNames(draft, [name]);
  draft.search = "";
  draft.searching = false;
  view.focusDraft = "form.search";
  render();
}

// reviewerNotes say what the reviewers are waiting on, or could not be told.
function reviewerNotes(draft) {
  const notes = [];
  if (draft.lookingUp) notes.push(el("span", { class: "faint form-note", role: "status" }, "Finding the default reviewers and code owners for these branches…"));
  if (draft.unread.defaults) notes.push(el("span", { class: "form-note warning", role: "status" }, "Unable to retrieve default reviewers."));
  if (draft.unread.owners) notes.push(el("span", { class: "form-note warning", role: "status" }, "Unable to retrieve code owners."));
  return notes;
}

// quickAddButtons add back the default reviewers and code owners for the
// branches that are not among the reviewers, as the create page's do: each
// shown only while it has someone to add, and saying how many.
function quickAddButtons(draft, view, busy) {
  const missingDefaults = draft.defaults.filter((name) => !hasReviewer(draft, name));
  const missingOwners = draft.owners.filter((name) => !hasReviewer(draft, name));
  if (missingDefaults.length === 0 && missingOwners.length === 0) return null;
  const button = (label, names, noun, key) => el("button", {
    type: "button",
    class: "button quick-add-button",
    title: plural(names.length, noun),
    "data-draft": key,
    disabled: busy,
    onclick: () => {
      addReviewerNames(draft, names);
      view.focusDraft = "form.search";
      render();
    },
  }, icon("plus"), label);
  return el("div", { class: "quick-add" },
    missingDefaults.length > 0 ? button("Default reviewers", missingDefaults, "reviewer", "form.quick.defaults") : null,
    missingOwners.length > 0 ? button("Code owners", missingOwners, "code owner", "form.quick.owners") : null);
}

// pullRequestReviewers are an existing pull request's reviewers, which
// update_pull_request does not change.
function pullRequestReviewers(draft) {
  return el("div", { class: "form-field" },
    el("span", { class: "form-label" }, "Reviewers"),
    draft.reviewers.length > 0
      ? el("div", { class: "reviewer-box read-only" }, draft.reviewers.map((name) => reviewerChip(draft, name, null, true)))
      : el("span", { class: "faint" }, "None"),
    el("span", { class: "faint form-hint-line" }, "Change reviewers in Bitbucket."));
}

function draftCheckbox(draft, busy) {
  const box = el("input", {
    type: "checkbox",
    "data-draft": "form.draft",
    disabled: busy,
    onchange: (event) => { draft.draft = event.target.checked; },
  });
  box.checked = draft.draft;
  return el("label", { class: "form-check" },
    box,
    el("span", {}, "Draft"),
    el("span", { class: "faint" }, "A draft cannot be merged until it is marked ready."));
}

// formActions are the create page's buttons, Create and Create as draft, or
// Save for a pull request that exists, where the host can send them.
function formActions(payload, view, draft, editing, busy) {
  if (!canCall(view, editing ? "update_pull_request" : "create_pull_request")) {
    return el("div", { class: "form-actions" },
      el("span", { class: "faint" }, "This client cannot submit it; ask for it to be created in the conversation."));
  }
  const waiting = busy || (!editing && draft.lookingUp);
  const button = (label, working, kind, primary) => el("button", {
    type: "button",
    class: "button" + (primary ? " primary" : "") + (draft.busy === kind ? " busy" : ""),
    "data-draft": "form.submit." + kind,
    disabled: waiting,
    onclick: () => submitForm(payload, view, kind),
  }, draft.busy === kind ? working : label);
  return el("div", { class: "form-actions" },
    editing
      ? button("Save", "Saving…", "save", true)
      : [button("Create", "Creating…", "create", true), button("Create as draft", "Creating…", "draft", false)],
    el("span", { class: "faint form-hint" }, editing ? "Nothing changes until you save." : "Nothing is created until you submit."));
}

// paintFormList fills an open list with what it holds now, in place, rather
// than drawing the form again under the person's typing.
function paintFormList(view, kind) {
  const draft = view.drafts.get(FORM_DRAFT);
  const list = document.getElementById(kind === "branch" ? BRANCH_LIST : REVIEWER_LIST);
  if (!draft || !list) return;
  const show = (view.payload && view.payload.show) || {};
  const options = kind === "branch" ? branchOptions(draft, show, view) : reviewerOptions(draft, view);
  list.replaceChildren(...options);
  const search = document.querySelector(kind === "branch" ? ".picker-search" : ".reviewer-search");
  if (search) markActive(search, options);
  if (kind === "reviewer") fitReviewerList();
  // The option the arrows are on stays in sight, by scrolling the list
  // alone: scrolling it into view would move the host's page too.
  const active = list.querySelector(".picker-option.active");
  if (active) {
    const top = active.offsetTop;
    const bottom = top + active.offsetHeight;
    if (top < list.scrollTop) list.scrollTop = top;
    else if (bottom > list.scrollTop + list.clientHeight) list.scrollTop = bottom - list.clientHeight;
  }
}

// suggestLater asks bb for what a field suggests once the person pauses
// typing, and fills its list in place. An answer overtaken by a newer
// request is dropped.
function suggestLater(view, show, field, text, delay) {
  const draft = view.drafts.get(FORM_DRAFT);
  if (!draft || !canCall(view, SUGGEST_TOOL)) return;
  clearTimeout(view.suggestTimers[field]);
  view.suggestTimers[field] = setTimeout(async () => {
    const turn = (draft.turns[field] || 0) + 1;
    draft.turns[field] = turn;
    let values = null;
    try {
      const result = await view.bridge.callTool(SUGGEST_TOOL, { project: show.project, repo: show.repo, field, text: text || "" }, REFRESH_TIMEOUT_MS);
      if (result && !result.isError) values = (result.structuredContent && result.structuredContent.values) || [];
    } catch {
      values = null;
    }
    if (draft.turns[field] !== turn || view.drafts.get(FORM_DRAFT) !== draft) return;
    view.formSuggestions[field] = values;
    if (field === "reviewer") {
      for (const value of values || []) rememberPerson(draft, value);
      draft.found = 0;
    } else {
      draft.pickerActive = 0;
    }
    paintFormList(view, field);
  }, delay === undefined ? SUGGEST_DELAY_MS : delay);
}

// A list closes when the person clicks anywhere else, as the create page's
// do.
document.addEventListener("mousedown", (event) => {
  const draft = view.drafts.get(FORM_DRAFT);
  const target = event.target;
  if (!draft || !(target instanceof Element)) return;
  if (draft.picker && !target.closest("[data-picker]")) closePicker(draft, false);
  if (draft.searching && !target.closest(".reviewers-field")) closeReviewerList(draft);
});

// submitForm creates or saves the pull request through the model's tool,
// and shows it once it is done. kind is the button pressed: create, draft or
// save.
async function submitForm(payload, view, kind) {
  const form = payload.form;
  const show = payload.show;
  const draft = formDraft(view, form, payload.avatars);
  if (draft.busy || draft.lookingUp) return;
  const editing = form.mode === "edit";
  const title = draft.title.trim();
  const problem = !title ? "You must supply a title for this pull request." : !editing && !draft.from ? "Select a source branch." : "";
  if (problem) {
    draft.error = problem;
    view.focusDraft = !title ? "form.title" : "form.picker.from";
    render();
    return;
  }
  draft.busy = kind;
  draft.error = null;
  draft.picker = null;
  draft.searching = false;
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
        from_ref: draft.from,
        title,
        description: draft.description,
        reviewers: draft.reviewers.join(","),
        draft: kind === "draft",
      };
      if (draft.to) args.to_ref = draft.to;
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
    draft.busy = null;
    draft.error = (error && error.message) || "It did not go through.";
    render();
  }
}
