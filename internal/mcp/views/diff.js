// The diff view: the files inline, each opening in place, and the whole diff
// with a file tree in fullscreen. Lines can be selected and handed to the
// model, or asked about.

const INLINE_FILES = 8;

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
  if (!view.diffFiles) view.diffFiles = parseDiff(diff.patch);
  const files = view.diffFiles;
  if (view.fullscreen) return diffPage(pr, diff, files, payload, view);

  const additions = files.reduce((sum, file) => sum + file.additions, 0);
  const deletions = files.reduce((sum, file) => sum + file.deletions, 0);
  const shown = view.expanded ? files : files.slice(0, INLINE_FILES);

  return el("div", {},
    el("div", { class: "pr-top" },
      icon("pullRequest", null, "faint"),
      el("span", { class: "ellipsis muted" }, repositoryOf(pr) + " #" + pr.id + " · Diff"),
      el("span", { class: "spacer" }),
      stateBadges(pr)),
    el("h1", { class: "pr-title" }, pr.title),
    el("div", { class: "diff-summary" },
      el("span", {}, plural(files.length, "file")),
      el("span", { class: "additions" }, "+" + additions),
      el("span", { class: "deletions" }, "−" + deletions),
      branchChip(pr.source_branch),
      icon("arrow", "into"),
      branchChip(pr.target_branch)),
    diff.truncated ? notice("This diff is too large to show whole; the files after these are in Bitbucket.") : null,
    files.length === 0 ? el("p", { class: "faint" }, "No changes.") : el("ul", { class: "file-list" },
      shown.map((file, index) => el("li", {},
        fileLink(file, index, view, () => view.toggleFile(index)),
        view.openFiles.has(index) ? el("div", { class: "inline-file" }, diffFile(file, index, pr, view, false)) : null))),
    el("div", { class: "actions" },
      view.canFullscreen
        ? el("button", { type: "button", class: "button primary", onclick: () => view.expand() }, icon("expand"), "Full screen")
        : null,
      !view.canFullscreen && files.length > INLINE_FILES
        ? el("button", { type: "button", class: "button", onclick: () => view.expand() },
          icon(view.expanded ? "collapse" : "expand"), view.expanded ? "Show fewer files" : "Show all " + files.length + " files")
        : null,
      linkButton("Open in Bitbucket", diffURL(pr), view.bridge)),
    view.canFullscreen ? null : selectionBar(pr, view));
}

// fileLink is a file in a list: its path, and for a rename where it came
// from, with the change as Bitbucket's lozenge, or as a dot in the narrow tree.
function fileLink(file, index, view, onclick, compact) {
  const path = filePath(file);
  const slash = path.lastIndexOf("/");
  const renamed = renamedFrom(file);
  const label = compact
    ? el("span", { class: "file-name" },
      el("span", { class: "ellipsis" }, slash >= 0 ? path.slice(slash + 1) : path),
      renamed ? el("span", { class: "ellipsis faint" }, renamed) : slash >= 0 ? el("span", { class: "ellipsis faint" }, path.slice(0, slash)) : null)
    : el("span", { class: "file-name" },
      el("span", { class: "ellipsis" }, path),
      renamed ? el("span", { class: "ellipsis faint" }, renamed) : null);
  return el("button", { type: "button", class: "file-link", title: renamed ? path + " · " + renamed : path, onclick },
    compact ? changeDot(file.status) : changeLozenge(file.status),
    label,
    el("span", { class: "spacer" }),
    file.binary ? el("span", { class: "faint" }, "binary") : changeBar(file, compact));
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
    el("span", { class: "additions" }, "+" + file.additions), " ",
    el("span", { class: "deletions" }, "−" + file.deletions),
    compact ? null : [" ", el("span", { class: "bar", "aria-hidden": "true" }, blocks)]);
}

function diffPage(pr, diff, files, payload, view) {
  const additions = files.reduce((sum, file) => sum + file.additions, 0);
  const deletions = files.reduce((sum, file) => sum + file.deletions, 0);
  const main = el("div", { class: "diff-main", id: "diff-main" },
    selectionBar(pr, view),
    diff.truncated ? el("p", { class: "notice" }, "This diff is too large to show whole; the files after these are in Bitbucket.") : null,
    files.length === 0 ? el("p", { class: "faint" }, "No changes.") : files.map((file, index) => diffFile(file, index, pr, view, true)));

  return el("div", {},
    el("header", { class: "fullscreen-header" },
      el("div", { class: "row-main" },
        el("div", { class: "pr-top" },
          icon("pullRequest", null, "faint"),
          el("span", { class: "ellipsis muted" }, repositoryOf(pr) + " #" + pr.id + " · Diff"),
          el("span", {}, plural(files.length, "file")),
          el("span", { class: "additions" }, "+" + additions),
          el("span", { class: "deletions" }, "−" + deletions)),
        el("h1", { class: "pr-title ellipsis" }, pr.title)),
      linkButton("Open in Bitbucket", diffURL(pr), view.bridge),
      el("button", { type: "button", class: "button", onclick: () => view.expand() }, icon("collapse"), "Exit full screen")),
    el("div", { class: "diff-layout" },
      el("nav", { class: "diff-tree", "aria-label": "Files" },
        el("ul", { class: "file-list" }, files.map((file, index) => el("li", {},
          fileLink(file, index, view, () => {
            const target = document.getElementById("diff-file-" + index);
            if (target) target.scrollIntoView({ block: "start" });
          }, true))))),
      main));
}

function diffFile(file, index, pr, view, withHeader) {
  const path = filePath(file);
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
    el("span", { class: "ellipsis" }, path),
    renamedFrom(file) ? el("span", { class: "ellipsis faint" }, renamedFrom(file)) : null,
    changeLozenge(file.status),
    el("span", { class: "spacer" }),
    file.binary ? el("span", { class: "faint" }, "binary") : changeBar(file)));
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
  file.hunks.forEach((hunk) => {
    body.append(el("tr", { class: "hunk" }, el("td", { colspan: 3 }, hunk.header)));
    for (const line of hunk.lines) {
      const row = el("tr", { class: line.type },
        lineNumberCell(line.oldNo, line, index, view),
        lineNumberCell(line.newNo, line, index, view),
        el("td", { class: "code" }, line.text));
      line.row = row;
      body.append(row);
    }
  });
  section.append(el("table", { class: "diff-table", role: "grid", "aria-label": path }, body));
  return section;
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
