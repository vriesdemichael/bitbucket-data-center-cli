// The diff view: the files inline, and the whole diff with a file tree in
// fullscreen, where a file picked inline opens. In a host without
// fullscreen, a file opens in place and the list grows a step at a time.
// Lines can be selected and handed to the model, asked about, or commented
// on, and the pull request's comments are drawn where they are, as
// Bitbucket's diff draws them.

const INLINE_FILES = 8;

// FILE_STEP is how many more files a list opened out in place shows each time.
const FILE_STEP = 50;

// INLINE_FILE_LINES is how many lines of a file open in place, and
// FILE_LINE_STEP how many more each "Show more" adds. An inline view grows
// with what it shows, so it shows a little at a time.
const INLINE_FILE_LINES = 60;
const FILE_LINE_STEP = 200;

// MAX_LINE_CHARS is how much of one line a view draws. A longer line, such as
// a minified file's, is cut, and says how much more it has.
const MAX_LINE_CHARS = 500;

// OMITTED_NAMED is how many of the files too large to show a notice names.
const OMITTED_NAMED = 3;

// parseDiff reads a unified diff, as git and Bitbucket write one, into files
// of hunks of lines numbered on both sides.
function parseDiff(patch) {
  const files = [];
  let file = null;
  let hunk = null;
  let oldLine = 0;
  let newLine = 0;

  for (const line of String(patch || "").split("\n")) {
    if (line.startsWith("diff --git ")) {
      file = { oldPath: "", newPath: "", status: "modified", hunks: [], additions: 0, deletions: 0, binary: false };
      const paths = /^diff --git (?:a\/|src:\/\/)(.*) (?:b\/|dst:\/\/)(.*)$/.exec(line);
      if (paths) {
        file.oldPath = paths[1];
        file.newPath = paths[2];
      }
      files.push(file);
      hunk = null;
      continue;
    }
    if (!file) continue;

    const header = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$/.exec(line);
    if (header) {
      oldLine = Number(header[1]);
      newLine = Number(header[2]);
      hunk = { header: line, context: header[3].trim(), lines: [] };
      file.hunks.push(hunk);
      continue;
    }

    if (!hunk) {
      if (line.startsWith("new file mode")) file.status = "added";
      else if (line.startsWith("deleted file mode")) file.status = "deleted";
      else if (line.startsWith("rename from ")) { file.oldPath = line.slice(12); file.status = "renamed"; }
      else if (line.startsWith("rename to ")) { file.newPath = line.slice(10); file.status = "renamed"; }
      else if (line.startsWith("--- ")) {
        const path = line.slice(4);
        if (path === "/dev/null") file.status = "added";
        else file.oldPath = stripDiffPrefix(path);
      } else if (line.startsWith("+++ ")) {
        const path = line.slice(4);
        if (path === "/dev/null") file.status = "deleted";
        else file.newPath = stripDiffPrefix(path);
      } else if (line.startsWith("Binary files ") || line.startsWith("GIT binary patch")) {
        file.binary = true;
      }
      continue;
    }

    if (line.startsWith("+")) {
      hunk.lines.push({ type: "add", text: line.slice(1), newNo: newLine++ });
      file.additions++;
    } else if (line.startsWith("-")) {
      hunk.lines.push({ type: "del", text: line.slice(1), oldNo: oldLine++ });
      file.deletions++;
    } else if (line.startsWith(" ")) {
      hunk.lines.push({ type: "context", text: line.slice(1), oldNo: oldLine++, newNo: newLine++ });
    } else if (line.startsWith("\\")) {
      hunk.lines.push({ type: "meta", text: line.slice(1).trim() });
    }
  }

  for (const each of files) {
    if (!each.newPath) each.newPath = each.oldPath;
    if (!each.oldPath) each.oldPath = each.newPath;
  }
  return files;
}

function stripDiffPrefix(path) {
  return path.replace(/^(?:a\/|b\/|src:\/\/|dst:\/\/)/, "");
}

function filePath(file) {
  return file.status === "deleted" ? file.oldPath : file.newPath;
}

function renderDiff(payload, view) {
  const pr = payload.pull_request || {};
  const diff = payload.diff || {};
  if (!view.diffFiles) view.diffFiles = diffFilesOf(diff);
  const files = view.diffFiles;
  const comments = threadPlaces(payload);
  const avatars = payload.avatars || {};
  if (view.fullscreen) return diffPage(pr, files, payload, view, comments);

  const shown = files.slice(0, shownCount("files", INLINE_FILES, view));
  const left = files.length - shown.length;

  return el("div", {},
    backButton(view),
    pullRequestTop(pr, "Diff"),
    el("h1", { class: "pr-title held-2", title: pr.title }, pr.title),
    diffSummary(files, pr, true),
    refreshNotice(view),
    omittedNotice(files, pr, view),
    files.length === 0 ? el("p", { class: "faint" }, "No changes.") : el("ul", { class: "file-list" },
      shown.map((file, index) => el("li", {},
        fileLink(file, index, view, () => openFile(index, view), false, openCommentsOn(comments, file)),
        view.openFiles.has(index) ? el("div", { class: "inline-file" }, diffFile(file, index, pr, view, false, comments, avatars)) : null))),
    left > 0
      ? el("div", { class: "more-row" }, view.canFullscreen
        ? el("button", { type: "button", class: "button ghost list-toggle", onclick: () => view.expand() }, "and " + plural(left, "more file") + ", in full screen")
        : moreButton("files", left, FILE_STEP, "files", view))
      : null,
    el("div", { class: "actions quiet" },
      view.canFullscreen
        ? el("button", { type: "button", class: "button", onclick: () => view.expand() }, icon("expand"), "Full screen")
        : null,
      view.revealed.has("files")
        ? el("button", { type: "button", class: "button", onclick: () => view.unreveal("files") }, icon("collapse"), "Show fewer files")
        : null,
      pullRequestTabs(pr, "diff", view),
      linkButton("Open in Bitbucket", diffURL(pr), view.bridge, "ghost"),
      el("span", { class: "spacer" }),
      snapshotStamp(payload, view, true)),
    view.canFullscreen ? null : selectionBar(pr, view));
}

// openFile shows a file's changes: in fullscreen, at that file, where the
// host has it, and in place where it does not.
function openFile(index, view) {
  if (view.canFullscreen) {
    view.focusFile = index;
    view.expand();
    return;
  }
  view.toggleFile(index);
}

function diffSummary(files, pr, withBranches) {
  const additions = files.reduce((sum, file) => sum + file.additions, 0);
  const deletions = files.reduce((sum, file) => sum + file.deletions, 0);
  return el("div", { class: "diff-summary" },
    el("span", { class: "nowrap" }, plural(files.length, "file")),
    el("span", { class: "additions" }, "+" + formatNumber(additions)),
    el("span", { class: "deletions" }, "−" + formatNumber(deletions)),
    withBranches ? branchPair(pr) : null);
}

// omittedNotice names the files whose changes are too large for a view to
// carry, wherever the view is, so a person sees what is missing without
// finding it in the list. Each links to its diff in Bitbucket.
function omittedNotice(files, pr, view) {
  const omitted = files.filter((file) => file.omitted);
  if (omitted.length === 0) return null;
  const named = omitted.slice(0, OMITTED_NAMED);
  return el("div", { class: "notice omitted", role: "note" },
    el("p", {}, omitted.length === 1
      ? "One file is too large to show here. You can view its diff in Bitbucket:"
      : formatNumber(omitted.length) + " files are too large to show here. You can view their diffs in Bitbucket:"),
    el("ul", { class: "omitted-list" }, named.map((file) => el("li", {},
      el("button", {
        type: "button",
        class: "omitted-link",
        title: "View the diff of " + filePath(file) + " in Bitbucket",
        onclick: () => openLink(view.bridge, fileURL(pr, file)),
      },
      changeDot(file.status),
      pathLabel(filePath(file)),
      el("span", { class: "spacer" }),
      changeBar(file, true),
      icon("external", null, "link-icon"))))),
    omitted.length > named.length
      ? el("p", { class: "faint" }, "And " + plural(omitted.length - named.length, "more file") + ", each marked where it is listed.")
      : null);
}

// fileLink is a file in a list: its change as a colored dot, its path, or its
// name alone in the tree under its directory, where a renamed file came from,
// whether it is too large to show, its open comments and its counts.
function fileLink(file, index, view, onclick, compact, openComments) {
  const path = filePath(file);
  const slash = path.lastIndexOf("/");
  const renamed = renamedFrom(file);
  const second = [file.omitted ? "too large to show here" : "", renamed].filter(Boolean).join(" · ");
  const label = el("span", { class: "file-name" },
    compact ? el("span", { class: "ellipsis" }, slash >= 0 ? path.slice(slash + 1) : path) : pathLabel(path),
    second ? el("span", { class: "ellipsis faint" + (file.omitted ? " omitted-mark" : "") }, second) : null);
  const title = [path, renamed, file.omitted ? "too large to show here" : ""].filter(Boolean).join(" · ");
  return el("button", { type: "button", class: "file-link", title, onclick },
    changeDot(file.status),
    label,
    el("span", { class: "spacer" }),
    openComments > 0
      ? el("span", { class: "counter", title: plural(openComments, "open comment") }, icon("comment", null, "icon-sm"), formatNumber(openComments))
      : null,
    file.binary ? el("span", { class: "faint" }, "binary") : changeBar(file, true));
}

// renamedFrom is Bitbucket's line for a renamed file, or nothing.
function renamedFrom(file) {
  return file.status === "renamed" && file.oldPath !== file.newPath ? "Renamed from '" + file.oldPath + "'" : "";
}

function changeDot(status) {
  const type = CHANGE_TYPES[status] || CHANGE_TYPES.modified;
  return el("span", { class: "change-dot " + type.tone, title: type.label, role: "img", "aria-label": type.label });
}

// changeBar draws a file's added and removed lines as counts and five blocks,
// as Bitbucket's file tree does; the narrow tree gets the counts alone.
function changeBar(file, compact) {
  const total = file.additions + file.deletions;
  const blocks = [];
  const added = total === 0 ? 0 : Math.round((file.additions / total) * 5);
  for (let i = 0; i < 5; i++) {
    blocks.push(el("i", { class: total === 0 ? "" : i < added ? "add" : "del" }));
  }
  return el("span", { class: "nowrap" },
    el("span", { class: "additions" }, "+" + formatNumber(file.additions)), " ",
    el("span", { class: "deletions" }, "−" + formatNumber(file.deletions)),
    compact ? null : [" ", el("span", { class: "bar", "aria-hidden": "true" }, blocks)]);
}

function diffPage(pr, files, payload, view, comments) {
  const avatars = payload.avatars || {};
  const main = el("div", { class: "diff-main", id: "diff-main" },
    selectionBar(pr, view),
    refreshNotice(view),
    pullRequestComments(comments, pr, avatars, view),
    omittedNotice(files, pr, view),
    files.length === 0 ? el("p", { class: "faint" }, "No changes.") : files.map((file, index) => diffFile(file, index, pr, view, true, comments, avatars)));

  return el("div", { class: "page" },
    el("header", { class: "fullscreen-header" },
      el("div", { class: "row-main" },
        backButton(view),
        el("div", { class: "pr-top" }, icon("pullRequest", null, "faint"), repositoryLabel(pr, "Diff")),
        el("h1", { class: "pr-title held-2", title: pr.title }, pr.title),
        el("div", { class: "header-meta" }, stateBadges(pr), diffSummary(files, pr, true))),
      el("div", { class: "header-actions" },
        snapshotStamp(payload, view),
        reviewActions(pr, payload, view),
        pullRequestTabs(pr, "diff", view),
        linkButton("Open in Bitbucket", diffURL(pr), view.bridge),
        el("button", { type: "button", class: "button", onclick: () => view.expand() }, icon("collapse"), "Exit full screen"))),
    el("div", { class: "diff-layout" },
      el("nav", { class: "diff-tree", "aria-label": "Files" }, fileTree(files, view, comments)),
      main));
}

// fileTree lists the files under their directories, in the diff's order, as
// Bitbucket's file tree does: a directory heads the files in it.
function fileTree(files, view, comments) {
  const rows = [];
  let directory = null;
  files.forEach((file, index) => {
    const path = filePath(file);
    const slash = path.lastIndexOf("/");
    const here = slash >= 0 ? path.slice(0, slash) : "";
    if (here !== directory) {
      directory = here;
      if (here) rows.push(el("li", { class: "tree-dir", title: here }, icon("folder", null, "icon-sm"), el("span", { class: "ellipsis" }, here)));
    }
    rows.push(el("li", {}, fileLink(file, index, view, () => {
      const target = document.getElementById("diff-file-" + index);
      if (target) target.scrollIntoView({ block: "start" });
    }, true, openCommentsOn(comments, file))));
  });
  return el("ul", { class: "file-list tree" }, rows);
}

function diffFile(file, index, pr, view, withHeader, comments, avatars) {
  const path = filePath(file);
  const place = (comments && comments.files.get(path)) || { onFile: [], lines: new Map() };
  const section = el("section", { class: "diff-file", id: "diff-file-" + index, dataset: { collapsed: "false" } });
  if (withHeader) {
    section.append(el("button", {
      type: "button",
      class: "diff-file-header",
      "aria-expanded": "true",
      onclick: (event) => {
        const collapsed = section.dataset.collapsed === "true";
        section.dataset.collapsed = collapsed ? "false" : "true";
        event.currentTarget.setAttribute("aria-expanded", collapsed ? "true" : "false");
      },
    },
    icon("chevronDown"),
    changeLozenge(file.status),
    el("span", { class: "file-name" }, pathLabel(path), renamedFrom(file) ? el("span", { class: "ellipsis faint" }, renamedFrom(file)) : null),
    el("span", { class: "spacer" }),
    file.binary ? el("span", { class: "faint" }, "binary") : changeBar(file)));
  }

  // The file's own comments, and those on lines the diff does not draw,
  // come first, as Bitbucket puts a file's comments at its top.
  const lineKeys = new Set(file.hunks.flatMap((hunk) => hunk.lines).filter((line) => line.type !== "meta").map(keyOfLine));
  const elsewhere = place.onFile.concat(...[...place.lines.entries()].filter(([key]) => !lineKeys.has(key)).map(([, threads]) => threads));
  if (elsewhere.length > 0) {
    section.append(el("div", { class: "file-threads" }, elsewhere.map((thread) => threadArticle(thread, pr, avatars, view))));
  }

  if (file.omitted) {
    section.append(el("div", { class: "diff-note" },
      el("p", {}, "This file's changes are too large to show here."),
      linkButton("View its diff in Bitbucket", fileURL(pr, file), view.bridge)));
    return section;
  }
  if (file.binary) {
    section.append(el("p", { class: "diff-note" }, "Binary file. Open it in Bitbucket to see the change."));
    return section;
  }
  if (file.hunks.length === 0) {
    section.append(el("p", { class: "diff-note" }, file.status === "renamed" ? "Renamed without changes." : "No changes to show."));
    return section;
  }

  const body = el("tbody", {});
  const key = "file-" + index;
  const cap = view.fullscreen ? Infinity : shownCount(key, INLINE_FILE_LINES, view);
  const total = file.hunks.reduce((sum, hunk) => sum + hunk.lines.length, 0);
  const drawnKeys = new Set();
  let drawn = 0;
  for (const hunk of file.hunks) {
    if (drawn >= cap) break;
    body.append(el("tr", { class: "hunk" }, el("td", { colspan: 3 }, hunk.header)));
    for (const line of hunk.lines) {
      if (drawn >= cap) break;
      const row = el("tr", { class: line.type },
        lineNumberCell(line.oldNo, line, index, view),
        lineNumberCell(line.newNo, line, index, view),
        el("td", { class: "code" }, codeText(line.text)));
      line.row = row;
      body.append(row);
      drawn++;
      if (line.type === "meta") continue;
      drawnKeys.add(keyOfLine(line));
      for (const thread of place.lines.get(keyOfLine(line)) || []) {
        body.append(el("tr", { class: "diff-thread" }, el("td", { colspan: 3 }, threadArticle(thread, pr, avatars, view))));
      }
      if (view.drafts.has(lineDraftKey(file, line))) {
        body.append(el("tr", { class: "diff-draft" }, el("td", { colspan: 3 }, lineCommentForm(file, line, pr, view))));
      }
    }
  }
  if (drawn < total) {
    // Comments on lines still folded away are counted where the file goes on.
    const further = [...place.lines.entries()].filter(([lineKey]) => lineKeys.has(lineKey) && !drawnKeys.has(lineKey))
      .reduce((sum, [, threads]) => sum + threads.length, 0);
    body.append(el("tr", { class: "more" }, el("td", { colspan: 3 },
      moreButton(key, total - drawn, FILE_LINE_STEP, "lines", view),
      further > 0 ? el("span", { class: "faint more-note" }, plural(further, "comment") + " further down") : null)));
  }
  section.append(el("table", { class: "diff-table", role: "grid", "aria-label": path }, body));
  return section;
}

// codeText is a line of code as a view draws it: whole, or its first
// MAX_LINE_CHARS characters and how many more it has.
function codeText(text) {
  if (text.length <= MAX_LINE_CHARS) return text;
  let end = MAX_LINE_CHARS;
  const code = text.charCodeAt(end - 1);
  if (code >= 0xd800 && code <= 0xdbff) end++;
  return [text.slice(0, end), el("span", { class: "cut" }, " … " + plural(text.length - end, "more character"))];
}

// fileURL is a file's diff in Bitbucket, as Bitbucket links one: the pull
// request's diff, with the file's path after the #.
function fileURL(pr, file) {
  return diffURL(pr) + "#" + encodeURI(filePath(file));
}

// diffFilesOf is every file of the diff, with what the patch carries of each.
// The file list comes from the server, whole however large the diff; the
// patch carries the files that fit.
function diffFilesOf(diff) {
  const parsed = parseDiff(diff.patch);
  if (!Array.isArray(diff.files)) return parsed;
  const hunks = new Map(parsed.map((file) => [file.newPath, file.hunks]));
  return diff.files.map((file) => ({
    oldPath: file.old_path || file.path,
    newPath: file.path,
    status: file.status || "modified",
    additions: file.additions || 0,
    deletions: file.deletions || 0,
    binary: Boolean(file.binary),
    omitted: Boolean(file.omitted),
    hunks: hunks.get(file.path) || [],
  }));
}

function lineNumberCell(number, line, fileIndex, view) {
  if (line.type === "meta") return el("td", { class: "line-number" });
  return el("td", {},
    el("button", {
      type: "button",
      class: "line-number",
      "aria-label": "Select line " + (number || ""),
      onclick: (event) => view.selectLine(fileIndex, line, event.shiftKey),
    }, number === undefined ? "" : String(number)));
}

// selectionBar is where selected lines go to the model: into its context for
// the next turn, or as a question now.
function selectionBar(pr, view) {
  const bar = el("div", { class: "selection-bar", id: "selection-bar", role: "region", "aria-label": "Selected lines", hidden: true });
  view.selectionBar = bar;
  view.selectionPR = pr;
  updateSelectionBar(view);
  return bar;
}

function updateSelectionBar(view) {
  const bar = view.selectionBar;
  if (!bar) return;
  const selection = currentSelection(view);
  if (!selection) {
    bar.hidden = true;
    bar.replaceChildren();
    return;
  }
  bar.hidden = false;
  bar.replaceChildren(
    el("span", {}, el("strong", {}, plural(selection.lines.length, "line")), " of ", el("span", { class: "mono" }, selection.path), " · " + selection.range),
    el("span", { class: "spacer" }),
    canCall(view, "add_pr_comment") && view.selectionPR && view.selectionPR.repository
      ? el("button", { type: "button", class: "button", onclick: () => commentOnSelection(view) }, "Comment")
      : null,
    el("button", { type: "button", class: "button", onclick: () => view.addSelectionToContext() }, "Add to chat context"),
    el("button", { type: "button", class: "button primary", onclick: () => view.askAboutSelection() }, icon("comment"), "Ask about this"),
    el("button", { type: "button", class: "button ghost", onclick: () => view.clearSelection() }, "Clear"));
  if (view.selectionNotice) bar.append(el("span", { class: "faint" }, view.selectionNotice));
}

function currentSelection(view) {
  const selected = view.selection;
  if (!selected || !view.diffFiles) return null;
  const file = view.diffFiles[selected.file];
  if (!file) return null;
  const all = file.hunks.flatMap((hunk) => hunk.lines).filter((line) => line.type !== "meta");
  const start = all.indexOf(selected.from);
  const end = all.indexOf(selected.to);
  if (start < 0 || end < 0) return null;
  const lines = all.slice(Math.min(start, end), Math.max(start, end) + 1);
  return { file, path: filePath(file), lines, range: describeRange(lines) };
}

function describeRange(lines) {
  const newNumbers = lines.map((line) => line.newNo).filter((n) => n !== undefined);
  const oldNumbers = lines.map((line) => line.oldNo).filter((n) => n !== undefined);
  const span = (numbers) => numbers.length === 1 ? String(numbers[0]) : numbers[0] + "–" + numbers[numbers.length - 1];
  if (newNumbers.length > 0 && oldNumbers.length === 0) return "new " + span(newNumbers);
  if (oldNumbers.length > 0 && newNumbers.length === 0) return "old " + span(oldNumbers);
  if (newNumbers.length === lines.length) return "lines " + span(newNumbers);
  return "new " + span(newNumbers) + ", old " + span(oldNumbers);
}

// selectionText is the selection as the model reads it.
function selectionText(pr, selection) {
  const marks = { add: "+", del: "-", context: " " };
  const body = selection.lines.map((line) => marks[line.type] + line.text).join("\n");
  return "From the diff of " + repositoryOf(pr) + "#" + pr.id + " (" + pr.title + "), " + selection.path +
    ", " + selection.range + ":\n```diff\n" + body + "\n```";
}

function diffURL(pr) {
  return pr.url ? String(pr.url).replace(/\/overview$/, "/diff") : "";
}

// threadPlaces sorts the threads a diff carries by where they are: on the pull
// request, on a file, or on a line of a file, by the side it is numbered on.
function threadPlaces(payload) {
  const index = { onPullRequest: [], files: new Map() };
  for (const thread of (payload.threads && payload.threads.threads) || []) {
    const anchor = thread.anchor;
    if (!anchor || !anchor.path) {
      index.onPullRequest.push(thread);
      continue;
    }
    if (!index.files.has(anchor.path)) index.files.set(anchor.path, { onFile: [], lines: new Map() });
    const place = index.files.get(anchor.path);
    if (!anchor.line || anchor.orphaned) {
      place.onFile.push(thread);
      continue;
    }
    // A removed line is numbered as the file was, anything else as it is.
    const key = (anchor.line_type === "REMOVED" ? "old:" : "new:") + anchor.line;
    if (!place.lines.has(key)) place.lines.set(key, []);
    place.lines.get(key).push(thread);
  }
  return index;
}

function keyOfLine(line) {
  return line.type === "del" ? "old:" + line.oldNo : "new:" + line.newNo;
}

// openCommentsOn counts the comments on a file still open.
function openCommentsOn(comments, file) {
  const place = comments && comments.files.get(filePath(file));
  if (!place) return 0;
  const all = place.onFile.concat(...place.lines.values());
  return all.filter((thread) => !thread.resolved).length;
}

// pullRequestComments are the pull request's own comments, over the files,
// folded to their count, and a box to add one.
function pullRequestComments(comments, pr, avatars, view) {
  const threads = comments.onPullRequest.slice().sort(byPlace);
  if (threads.length === 0 && !canCall(view, "add_pr_comment")) return null;
  const key = "pr-comments";
  const open = threads.filter((thread) => !thread.resolved).length;
  const title = threads.length === 0
    ? "Comment on the pull request"
    : plural(threads.length, "comment") + " on the pull request" + (open > 0 ? " · " + formatNumber(open) + " open" : "");
  return el("section", { class: "diff-pr-comments" },
    foldButton(key, title, view),
    view.unclamped.has(key)
      ? el("div", { class: "diff-pr-threads" }, newCommentArea(pr, view), threads.map((thread) => threadArticle(thread, pr, avatars, view)))
      : null);
}

// lineDraftKey is where a comment on a line is written: the line, by its
// file and number.
function lineDraftKey(file, line) {
  return "line|" + filePath(file) + "|" + keyOfLine(line);
}

// commentOnSelection opens a box to comment on the last line selected, as
// Bitbucket anchors a comment on one line.
function commentOnSelection(view) {
  const selection = currentSelection(view);
  if (!selection) return;
  const line = selection.lines[selection.lines.length - 1];
  view.selection = null;
  view.selectionNotice = "";
  openDraft(view, lineDraftKey(selection.file, line));
}

// lineCommentForm comments on one line of the diff through add_pr_comment,
// numbered on the side Bitbucket numbers it on.
function lineCommentForm(file, line, pr, view) {
  const number = line.type === "del" ? line.oldNo : line.newNo;
  const lineType = line.type === "del" ? "REMOVED" : line.type === "add" ? "ADDED" : "CONTEXT";
  return commentForm(view, lineDraftKey(file, line), {
    placeholder: "Comment on line " + number + " of " + filePath(file),
    submitLabel: "Comment",
    busyLabel: "Commenting…",
    send: (text) => callForPerson(view, "add_pr_comment", Object.assign(pullRequestArgs(pr), { text, path: filePath(file), line: number, line_type: lineType })),
  });
}
