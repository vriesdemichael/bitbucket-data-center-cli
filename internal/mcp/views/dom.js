// Building the page.
//
// Every element is made with the DOM API, and text that came from Bitbucket
// only ever becomes a text node: nothing on this page parses a string as
// HTML. A title, a comment or a line of a diff is written by someone else,
// and in a view a script that got in could call the server's tools through
// the host.

const SVG_NS = "http://www.w3.org/2000/svg";

// An image a view shows is an avatar bb embedded as data. The view has no
// network, and a source that is anything else is dropped.
const EMBEDDED_IMAGE = /^data:image\/(png|jpeg|gif|webp);base64,[A-Za-z0-9+/]+=*$/;

// Audio or a video a view plays is a file bb embedded as data, and only an
// audio or video element takes it.
const EMBEDDED_MEDIA = /^data:(?:(?:audio|video)\/[a-z0-9.+-]+|application\/ogg);base64,[A-Za-z0-9+/]+=*$/;

function el(tag, props, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (value === undefined || value === null || value === false) continue;
    if (key === "class") {
      node.className = value;
    } else if (key === "text") {
      node.textContent = value;
    } else if (key === "dataset") {
      Object.assign(node.dataset, value);
    } else if (key.startsWith("on")) {
      if (typeof value !== "function") throw new Error("an event handler must be a function");
      node.addEventListener(key.slice(2), value);
    } else if (key === "src") {
      const media = tag === "audio" || tag === "video";
      if (media ? EMBEDDED_MEDIA.test(value) : EMBEDDED_IMAGE.test(value)) node.setAttribute("src", value);
    } else if (key === "href" || key === "srcdoc" || key === "style") {
      // Links open through the host, and nothing styles itself from data.
      throw new Error("el does not set " + key);
    } else {
      node.setAttribute(key, value === true ? "" : String(value));
    }
  }
  append(node, children);
  return node;
}

function append(node, children) {
  for (const child of children.flat(Infinity)) {
    if (child === undefined || child === null || child === false) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}

// Icons are drawn from fixed shapes on a 16-unit grid, after the metaphors
// Bitbucket's own icons use: an approval is a filled circle with a check, a
// request for changes a filled circle with a bar, a failed build a circle with
// an exclamation mark, a task a filled checkbox. A "path" or "circle" is a
// stroke, a "disc" or "box" a fill, and a "cut" a stroke in the background
// colour across a fill.
const ICON_SHAPES = {
  approved: [["disc", 8, 8, 7], ["cut", "M4.8 8.2l2.2 2.2 4.2-4.6"]],
  changesRequested: [["disc", 8, 8, 7], ["cut", "M5 11L11 5"]],
  buildSuccessful: [["circle", 8, 8, 6], ["path", "M5.4 8.2l1.8 1.8 3.4-3.8"]],
  buildFailed: [["circle", 8, 8, 6], ["path", "M8 4.8v3.8"], ["path", "M8 11.1v.1"]],
  buildInProgress: [["circle", 8, 8, 6], ["path", "M8 5v3.3l2.1 1.3"]],
  buildCancelled: [["circle", 8, 8, 6], ["path", "M4 12L12 4"]],
  buildUnknown: [["circle", 8, 8, 6], ["path", "M6.4 6.4a1.7 1.7 0 1 1 2.4 1.5c-.5.3-.8.6-.8 1.2"], ["path", "M8 11.2v.1"]],
  task: [["box", 2, 2, 12, 12, 3], ["cut", "M5 8.2l2 2 4-4.4"]],
  comment: [["path", "M3 3.5h10a1 1 0 0 1 1 1V10a1 1 0 0 1-1 1H8l-3.2 2.6V11H3a1 1 0 0 1-1-1V4.5a1 1 0 0 1 1-1z"]],
  branch: [["circle", 5, 3.5, 1.6], ["circle", 5, 12.5, 1.6], ["circle", 11, 5.5, 1.6], ["path", "M5 5.1v5.8M11 7.1c0 2.6-6 2.2-6 3.8"]],
  pullRequest: [["circle", 4.5, 3.5, 1.6], ["circle", 4.5, 12.5, 1.6], ["circle", 11.5, 12.5, 1.6], ["path", "M4.5 5.1v5.8M11.5 10.9V6.5a2 2 0 0 0-2-2H7M8.6 2.9L7 4.5l1.6 1.6"]],
  arrow: [["path", "M3 8h10M9.5 4.5L13 8l-3.5 3.5"]],
  arrowLeft: [["path", "M13 8H3M6.5 4.5L3 8l3.5 3.5"]],
  external: [["path", "M9.5 2.5h4v4M13.5 2.5L7.5 8.5M11.5 9.5v4h-9v-9h4"]],
  expand: [["path", "M9.5 2.5h4v4M6.5 13.5h-4v-4M13.5 2.5l-4.5 4.5M2.5 13.5L7 9"]],
  collapse: [["path", "M13.5 6.5h-4v-4M2.5 9.5h4v4M9.5 6.5L14 2M6.5 9.5L2 14"]],
  chevronDown: [["path", "M3.5 6L8 10.5 12.5 6"]],
  chevronRight: [["path", "M6 3.5L10.5 8 6 12.5"]],
  image: [["path", "M2.5 3.5h11v9h-11z"], ["path", "M2.5 11l3.5-3.5 3 3 2-2 2.5 2.5"], ["circle", 10.5, 6.2, 1.2]],
  folder: [["path", "M2 4.2a1 1 0 0 1 1-1h3l1.5 1.6H13a1 1 0 0 1 1 1V12a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1z"]],
  autoMerge: [["circle", 4.5, 3.5, 1.6], ["circle", 4.5, 12.5, 1.6], ["path", "M4.5 5.1v5.8M4.5 5.2c0 3.5 5 2.8 6.5 5.3"], ["path", "M12.8 1.8l-2 3h2.4l-2 3"]],
  refresh: [["path", "M13 8a5 5 0 1 1-1.5-3.6"], ["path", "M11.8 1.7v2.9H8.9"]],
};

function icon(name, label, extraClass) {
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("viewBox", "0 0 16 16");
  svg.setAttribute("class", "icon" + (extraClass ? " " + extraClass : ""));
  if (label) {
    svg.setAttribute("role", "img");
    svg.setAttribute("aria-label", label);
  } else {
    svg.setAttribute("aria-hidden", "true");
  }
  for (const shape of ICON_SHAPES[name] || []) {
    const [kind, ...values] = shape;
    let part;
    switch (kind) {
      case "circle":
      case "disc":
        part = document.createElementNS(SVG_NS, "circle");
        part.setAttribute("cx", String(values[0]));
        part.setAttribute("cy", String(values[1]));
        part.setAttribute("r", String(values[2]));
        break;
      case "box":
        part = document.createElementNS(SVG_NS, "rect");
        part.setAttribute("x", String(values[0]));
        part.setAttribute("y", String(values[1]));
        part.setAttribute("width", String(values[2]));
        part.setAttribute("height", String(values[3]));
        part.setAttribute("rx", String(values[4]));
        break;
      default:
        part = document.createElementNS(SVG_NS, "path");
        part.setAttribute("d", values[0]);
    }
    part.setAttribute("class", kind === "disc" || kind === "box" ? "fill" : kind === "cut" ? "cut" : "stroke");
    svg.append(part);
  }
  return svg;
}

function badge(text, tone) {
  return el("span", { class: "badge" + (tone ? " " + tone : "") }, text);
}

// avatar draws a person: the avatar bb embedded for them, or their initials
// on a colour of their own.
function avatar(username, displayName, avatars, size) {
  const name = displayName || username || "?";
  const holder = el("span", { class: "avatar" + (size ? " " + size : ""), title: name });
  const source = avatars && username ? avatars[username] : undefined;
  if (source && EMBEDDED_IMAGE.test(source)) {
    holder.append(el("img", { src: source, alt: name, width: 64, height: 64, decoding: "async" }));
  } else {
    const initials = el("span", { class: "initials", role: "img", "aria-label": name }, initialsOf(name));
    initials.style.backgroundColor = colourFor(username || name);
    holder.append(initials);
  }
  return holder;
}

// reviewerAvatar is an avatar with the reviewer's status on it, at its top
// right as Bitbucket badges one.
function reviewerAvatar(reviewer, avatars, size) {
  const holder = avatar(reviewer.name, reviewer.display_name, avatars, size);
  const vote = voteOf(reviewer);
  if (vote === "approved") {
    holder.append(el("span", { class: "vote approved" }, icon("approved", voteLabel(vote))));
  } else if (vote === "changes-requested") {
    holder.append(el("span", { class: "vote changes-requested" }, icon("changesRequested", voteLabel(vote))));
  }
  const name = nameOf(reviewer);
  holder.title = vote === "none" ? name : name + ": " + voteLabel(vote);
  return holder;
}

function nameOf(reviewer) {
  return reviewer.display_name || reviewer.name || "?";
}

// sortReviewers orders reviewers as Bitbucket does: approvals first, then
// requests for changes, then the rest, each by name.
function sortReviewers(reviewers) {
  const order = { approved: 1, "changes-requested": 2, none: 3 };
  return reviewers.slice().sort((a, b) =>
    order[voteOf(a)] - order[voteOf(b)] || String(nameOf(a)).localeCompare(String(nameOf(b))));
}

// reviewerStack is a row of at most limit reviewers' avatars, in Bitbucket's
// order. A reviewer who requested changes is always among them, however many
// approved; the rest are counted, and named on hover.
function reviewerStack(reviewers, avatars, limit, size) {
  if (reviewers.length === 0) return null;
  const sorted = sortReviewers(reviewers);
  const requested = sorted.filter((reviewer) => voteOf(reviewer) === "changes-requested");
  const others = sorted.filter((reviewer) => voteOf(reviewer) !== "changes-requested");
  const kept = new Set([...requested.slice(0, limit), ...others.slice(0, Math.max(0, limit - requested.length))]);
  const rest = sorted.filter((reviewer) => !kept.has(reviewer));
  return el("span", { class: "avatar-stack" },
    sorted.filter((reviewer) => kept.has(reviewer)).map((reviewer) => reviewerAvatar(reviewer, avatars, size)),
    rest.length > 0 ? el("span", { class: "faint more", title: rest.map(nameOf).join(", ") }, "+" + formatNumber(rest.length)) : null);
}

// clampable holds long content to a few lines, with a button to show the rest
// that appears only when there is more to show (see revealClampToggles).
function clampable(key, content, view) {
  const open = view.unclamped.has(key);
  return el("div", { class: "clamp" + (open ? " open" : ""), dataset: { clamp: key } },
    content,
    el("button", { type: "button", class: "button ghost clamp-toggle", hidden: true, onclick: () => view.toggleClamp(key) },
      open ? "Show less" : "Show more"));
}

// FOLD_AFTER is how many items a group that asks nothing of the person, such
// as the passed builds, shows before it folds to its count.
const FOLD_AFTER = 3;

// foldButton is the title of a group that folds: its count, and a chevron
// that opens it, or closes it again.
function foldButton(key, title, view) {
  const open = view.unclamped.has(key);
  return el("button", {
    type: "button",
    class: "group-title fold",
    "aria-expanded": open ? "true" : "false",
    onclick: () => view.toggleClamp(key),
  }, icon(open ? "chevronDown" : "chevronRight", null, "faint"), title);
}

// shownCount is how many items of a list that grows in steps are showing:
// the first few, and a step more each time the person asked.
function shownCount(key, first, view) {
  return first + (view.revealed.get(key) || 0);
}

// moreButton grows a list by a step, and says how many are still not shown.
function moreButton(key, left, step, noun, view) {
  if (left <= 0) return null;
  const next = Math.min(step, left);
  return el("button", { type: "button", class: "button ghost list-toggle", onclick: () => view.reveal(key, step) },
    "Show " + formatNumber(next) + " more " + noun + (left > next ? " (" + formatNumber(left) + " not shown)" : ""));
}

// branchChip is a branch, as source or target of a pull request.
function branchChip(name, role) {
  return el("span", { class: "chip branch" + (role ? " " + role : ""), title: name },
    icon("branch", null, "icon-sm"), el("span", { class: "ellipsis" }, name));
}

// pathLabel is a file's path, drawn so that a long one gives way from its
// directory first and keeps its file name; the directory gives way from its
// start, which keeps the folders nearest the file.
function pathLabel(path) {
  const slash = path.lastIndexOf("/");
  return el("span", { class: "path", title: path },
    slash >= 0 ? [el("span", { class: "dir" }, path.slice(0, slash)), el("span", { class: "slash" }, "/")] : null,
    el("span", { class: "name" }, slash >= 0 ? path.slice(slash + 1) : path));
}

function linkButton(label, url, bridge, extraClass) {
  return el("button", {
    type: "button",
    class: "button" + (extraClass ? " " + extraClass : ""),
    onclick: () => openLink(bridge, url),
    disabled: !isWebURL(url),
  }, icon("external"), label);
}

// textLink is a link in running text, which opens through the host.
function textLink(label, url, bridge) {
  if (!isWebURL(url)) return null;
  return el("button", { type: "button", class: "text-link", onclick: () => openLink(bridge, url) }, label);
}

// openLink asks the host to open a web page, and says so when it will not.
// A host that opens no links is not asked.
function openLink(bridge, url) {
  if (!isWebURL(url)) return;
  if (!view.opensLinks) {
    linkRefused(url);
    return;
  }
  bridge.openLink(url).then((result) => {
    if (result && result.isError) linkRefused(url);
  }, () => linkRefused(url));
}

function isWebURL(value) {
  if (typeof value !== "string") return false;
  try {
    const parsed = new URL(value);
    return parsed.protocol === "https:" || parsed.protocol === "http:";
  } catch {
    return false;
  }
}
