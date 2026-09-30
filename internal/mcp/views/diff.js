// The diff view: its files inline, and in fullscreen Bitbucket's diff page:
// the file tree beside one file, the one picked in the tree. In a host
// without fullscreen, a file opens in place and the list grows a step at a
// time. Lines can be selected and handed to the model, asked about, or
// commented on, and each comment is drawn where Bitbucket's diff draws it.

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
  focusPathOf(files, view);
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

// focusPathOf turns a file the view was opened at, by its path, into the file
// it shows: in fullscreen, the one beside the tree, and in place, one opened.
function focusPathOf(files, view) {
  if (!view.focusPath) return;
  const index = files.findIndex((file) => filePath(file) === view.focusPath || file.oldPath === view.focusPath);
  view.focusPath = null;
  if (index < 0) return;
  if (view.fullscreen || view.canFullscreen) view.diffFile = index;
  else view.openFiles.add(index);
}

// openFile shows a file's changes: in fullscreen, beside the tree, where the
// host has it, and in place where it does not.
function openFile(index, view) {
  if (view.canFullscreen) {
    view.diffFile = index;
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
// name alone in the tree under its folder, where a renamed file came from,
// whether it is too large to show, its open comments and its counts.
function fileLink(file, index, view, onclick, compact, openComments, current) {
  const path = filePath(file);
  const slash = path.lastIndexOf("/");
  const renamed = renamedFrom(file);
  const second = [file.omitted ? "too large to show here" : "", renamed].filter(Boolean).join(" · ");
  const label = el("span", { class: "file-name" },
    compact ? el("span", { class: "ellipsis" }, slash >= 0 ? path.slice(slash + 1) : path) : pathLabel(path),
    second ? el("span", { class: "ellipsis faint" + (file.omitted ? " omitted-mark" : "") }, second) : null);
  const title = [path, renamed, file.omitted ? "too large to show here" : ""].filter(Boolean).join(" · ");
  return el("button", { type: "button", class: "file-link", title, onclick, "aria-current": current ? "true" : null },
    compact ? changeIcon(file.status) : changeDot(file.status),
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

// changeIcon is a file's change as Bitbucket's tree draws it: a small square
// in the change's colour, marked with what happened to the file.
const CHANGE_ICONS = { added: "fileAdded", deleted: "fileDeleted", renamed: "fileRenamed", moved: "fileRenamed", copied: "fileAdded" };

function changeIcon(status) {
  const type = CHANGE_TYPES[status] || CHANGE_TYPES.modified;
  return icon(CHANGE_ICONS[status] || "fileModified", type.label, "change-icon " + type.tone);
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

// diffPage is the diff in fullscreen, as Bitbucket's diff page is: the tree,
// and beside it one file, the one picked in it; the first in the tree to begin
// with.
function diffPage(pr, files, payload, view, comments) {
  const avatars = payload.avatars || {};
  const order = treeOrder(files);
  if (view.diffFile === null || !files[view.diffFile]) view.diffFile = order.length > 0 ? order[0] : null;
  const current = view.diffFile;
  const main = el("div", { class: "diff-main", id: "diff-main" },
    selectionBar(pr, view),
    refreshNotice(view),
    omittedNotice(files, pr, view),
    current === null ? el("p", { class: "faint" }, "No changes.") : diffFile(files[current], current, pr, view, true, comments, avatars));

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

// treeOf puts the files under their folders, each file by its place in the
// diff.
function treeOf(files) {
  const root = { name: "", dirs: new Map(), files: [] };
  files.forEach((file, index) => {
    const parts = filePath(file).split("/");
    let node = root;
    for (const part of parts.slice(0, -1)) {
      if (!node.dirs.has(part)) node.dirs.set(part, { name: part, dirs: new Map(), files: [] });
      node = node.dirs.get(part);
    }
    node.files.push({ name: parts[parts.length - 1], index });
  });
  return root;
}

// byName orders names as Bitbucket's tree does, and as the person's language
// sorts them.
function byName(a, b) {
  return a.name.localeCompare(b.name) || (a.name < b.name ? -1 : a.name > b.name ? 1 : 0);
}

// treeWalk visits a folder as Bitbucket's tree draws it: its folders first,
// then its files, each by name. A folder that holds nothing but one folder is
// drawn joined to it, as "internal/payments".
function treeWalk(node, depth, visit) {
  for (const dir of [...node.dirs.values()].sort(byName)) {
    let joined = dir;
    let name = dir.name;
    while (joined.files.length === 0 && joined.dirs.size === 1) {
      joined = [...joined.dirs.values()][0];
      name += "/" + joined.name;
    }
    visit({ dir: name, depth });
    treeWalk(joined, depth + 1, visit);
  }
  for (const file of node.files.slice().sort(byName)) visit({ file: file.index, depth });
}

// treeOrder is the files' indexes in the order the tree lists them.
function treeOrder(files) {
  const order = [];
  treeWalk(treeOf(files), 0, (entry) => { if (entry.file !== undefined) order.push(entry.file); });
  return order;
}

// fileTree lists the files under their folders, as Bitbucket's file tree
// does; a file opens beside it.
function fileTree(files, view, comments) {
  const rows = [];
  treeWalk(treeOf(files), 0, (entry) => {
    if (entry.dir !== undefined) {
      rows.push(el("li", { class: "tree-dir", title: entry.dir, dataset: { depth: String(entry.depth) } },
        icon("folder", null, "icon-sm"), el("span", { class: "ellipsis" }, entry.dir)));
      return;
    }
    const file = files[entry.file];
    rows.push(el("li", { class: "tree-file", dataset: { depth: String(entry.depth) } },
      fileLink(file, entry.file, view, () => {
        view.diffFile = entry.file;
        view.selection = null;
        render();
        const main = document.getElementById("diff-main");
        if (main) main.scrollTop = 0;
      }, true, openCommentsOn(comments, file), entry.file === view.diffFile)));
  });
  return el("ul", { class: "file-list tree" }, rows);
}

function diffFile(file, index, pr, view, withHeader, comments, avatars) {
  const path = filePath(file);
  const place = (comments && comments.files.get(path)) || { onFile: [], lines: new Map() };
  const section = el("section", { class: "diff-file", id: "diff-file-" + index });
  if (withHeader) {
    section.append(el("div", { class: "diff-file-head" },
      el("div", { class: "diff-file-header" },
        pathCrumbs(path),
        changeLozenge(file.status),
        renamedFrom(file) ? el("span", { class: "ellipsis faint" }, renamedFrom(file)) : null,
        el("span", { class: "spacer" }),
        file.binary ? el("span", { class: "faint" }, "binary") : changeBar(file)),
      viewFileButton(file, pr, view)));
  }

  // The file's own comments come first, as Bitbucket puts them at its top.
  if (place.onFile.length > 0) {
    section.append(el("div", { class: "file-threads" }, place.onFile.map((thread) => commentThread(thread, pr, avatars, view, { place: "file" }))));
  }

  if (file.omitted || file.binary || file.hunks.length === 0) {
    section.append(file.omitted
      ? el("div", { class: "diff-note" },
        el("p", {}, "This file's changes are too large to show here."),
        linkButton("View its diff in Bitbucket", fileURL(pr, file), view.bridge))
      : el("p", { class: "diff-note" }, file.binary
        ? "Binary file. Open it in Bitbucket to see the change."
        : file.status === "renamed" ? "Renamed without changes." : "No changes to show."));
    // Comments on lines a view cannot draw are listed, with the line each
    // is on, rather than lost.
    const lineThreads = [...place.lines.values()].flat();
    if (lineThreads.length > 0) {
      section.append(el("div", { class: "file-threads" }, lineThreads.map((thread) => commentThread(thread, pr, avatars, view, { place: "file", label: placeLabel(thread.place) }))));
    }
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
        el("td", { class: "code" }, codeText(line.text, line.spans)));
      line.row = row;
      body.append(row);
      drawn++;
      if (line.type === "meta") continue;
      drawnKeys.add(keyOfLine(line));
      for (const thread of place.lines.get(keyOfLine(line)) || []) {
        body.append(threadRow(line, commentThread(thread, pr, avatars, view, { place: "line", label: lineLabelOf(line) })));
      }
      if (view.drafts.has(lineDraftKey(file, line))) {
        body.append(threadRow(line, lineCommentForm(file, line, pr, view), true));
      }
    }
  }
  if (drawn < total) {
    // Comments on lines still folded away are counted where the file goes on.
    const further = [...place.lines.entries()].filter(([lineKey]) => !drawnKeys.has(lineKey))
      .reduce((sum, [, threads]) => sum + threads.length, 0);
    body.append(el("tr", { class: "more" }, el("td", { colspan: 3 },
      moreButton(key, total - drawn, FILE_LINE_STEP, "lines", view),
      further > 0 ? el("span", { class: "faint more-note" }, plural(further, "comment") + " further down") : null)));
  }
  section.append(el("table", { class: "diff-table", role: "grid", "aria-label": path }, body));
  return section;
}

// threadRow is a row under a line that holds a thread, or a comment being
// written on the line: the line's colour carries on beside it, so it reads as
// the line's, as in Bitbucket's diff.
function threadRow(line, content, draft) {
  return el("tr", { class: "diff-thread " + (draft ? "diff-draft " : "") + line.type },
    el("td", { class: "line-number" }),
    el("td", { class: "line-number" }),
    el("td", { class: "thread-cell" }, content));
}

// placeLabel is Bitbucket's name for the line a place is on, for a comment
// listed apart from its line.
function placeLabel(place) {
  const match = /^(old|new):(\d+)$/.exec(place || "");
  if (!match) return "";
  return "Line " + (match[1] === "old" ? "-" : "") + match[2];
}

// pathCrumbs is a file's path as Bitbucket heads a file: its folders, then
// its name in bold.
function pathCrumbs(path) {
  const parts = path.split("/");
  const name = parts.pop();
  return el("span", { class: "path-crumbs", title: path },
    parts.map((part) => [el("span", { class: "crumb" }, part), el("span", { class: "crumb-slash" }, " / ")]),
    el("span", { class: "crumb name" }, name));
}

// viewFileButton opens the file as the pull request has it, in Bitbucket.
function viewFileButton(file, pr, view) {
  const url = sourceURL(pr, file);
  if (!url) return null;
  return el("button", { type: "button", class: "button ghost view-file", title: "View " + filePath(file) + " as the pull request has it, in Bitbucket", onclick: () => openLink(view.bridge, url) },
    icon("external"), "View file");
}

// sourceURL is a file's page in Bitbucket at the pull request's source
// commit; a deleted file has none.
function sourceURL(pr, file) {
  if (file.status === "deleted" || !pr.url) return "";
  const repository = String(pr.url).replace(/\/pull-requests\/.*$/, "");
  const at = pr.source_commit || (pr.source_branch ? "refs/heads/" + pr.source_branch : "");
  return repository + "/browse/" + encodeURI(filePath(file)) + (at ? "?at=" + encodeURIComponent(at) : "");
}

// codeText is a line of code as a view draws it: whole, or its first
// MAX_LINE_CHARS characters and how many more it has, coloured by the spans
// bb highlighted it with where it did.
function codeText(text, spans) {
  let end = text.length;
  if (end > MAX_LINE_CHARS) {
    end = MAX_LINE_CHARS;
    const code = text.charCodeAt(end - 1);
    if (code >= 0xd800 && code <= 0xdbff) end++;
  }
  const shown = spans ? coloured(text.slice(0, end), spans) : text.slice(0, end);
  if (end >= text.length) return shown;
  return [shown, el("span", { class: "cut" }, " … " + plural(text.length - end, "more character"))];
}

// coloured splits a line by its spans, each a class letter and a length in
// the line's UTF-16 units, as bb writes them. A letter it does not know is
// plain text, and whatever the spans do not cover stays plain.
function coloured(text, spans) {
  const parts = [];
  let at = 0;
  for (const span of String(spans).split(" ")) {
    const kind = span.charAt(0);
    const length = Number(span.slice(1));
    if (!/^[a-z]$/.test(kind) || !Number.isInteger(length) || length <= 0) continue;
    const piece = text.slice(at, at + length);
    at += length;
    if (!piece) break;
    parts.push(kind === "t" ? piece : el("span", { class: "hl-" + kind }, piece));
  }
  if (at < text.length) parts.push(text.slice(at));
  return parts;
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
  withSpans(parsed, diff.highlight || {});
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

// withSpans gives each code line of each file in the patch the spans bb
// highlighted it with: a file's spans are its code lines', in the order the
// patch has them, as parseDiff reads them.
function withSpans(files, highlight) {
  files.forEach((file, index) => {
    const spans = highlight[index];
    if (!Array.isArray(spans)) return;
    let at = 0;
    for (const hunk of file.hunks) {
      for (const line of hunk.lines) {
        if (line.type !== "meta") line.spans = spans[at++];
      }
    }
  });
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

// threadPlaces sorts the threads a diff carries by where Bitbucket's diff
// draws them: at the top of a file, or under a line of it, by the side it is
// numbered on. A thread on the pull request, or one Bitbucket's diff no
// longer draws, is in the overview's activity and not here.
function threadPlaces(payload) {
  const index = { files: new Map() };
  for (const thread of (payload.threads && payload.threads.threads) || []) {
    const path = thread.anchor && thread.anchor.path;
    if (!path || !thread.place) continue;
    if (!index.files.has(path)) index.files.set(path, { onFile: [], lines: new Map() });
    const place = index.files.get(path);
    if (thread.place === "file") {
      place.onFile.push(thread);
      continue;
    }
    if (!place.lines.has(thread.place)) place.lines.set(thread.place, []);
    place.lines.get(thread.place).push(thread);
  }
  return index;
}

// keyOfLine is a line's place, as a thread's place names it: a removed line
// by its number as the file was, any other by its number as it is.
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
