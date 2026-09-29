// The file viewer (#686): a file the person cannot open in their own editor,
// drawn as what it is: code with line numbers, a picture to see whole or at
// its size, a player, an archive's listing, or what the file is where it
// cannot be shown. A long file comes a window at a time.

// INLINE_FILE_VIEW_LINES is how many lines an inline file view draws; the
// rest is in fullscreen, or a step at a time where the host has none.
const INLINE_FILE_VIEW_LINES = 40;
const FILE_VIEW_STEP = 200;

// LINE_KINDS are the files read as lines; only text is highlighted.
const LINE_KINDS = ["text", "document", "archive"];

function renderFile(payload, view) {
  const file = payload.file;
  const show = payload.show || {};
  if (!file) return notice("This view has no file to show.");
  const repository = [show.project, show.repo].filter(Boolean).join("/");
  const title = el("div", { class: "row-main" },
    backButton(view),
    el("div", { class: "pr-top" },
      icon("folder", null, "faint"),
      el("span", { class: "repo-label muted" }, el("span", { class: "ellipsis", title: repository }, repository)),
      file.at ? el("span", { class: "chip branch", title: file.at }, icon("branch", null, "icon-sm"), el("span", { class: "ellipsis" }, file.at)) : null),
    el("h1", { class: "pr-title file-title held-2", title: file.path }, pathLabel(file.path)),
    el("div", { class: "file-meta faint" }, fileFacts(file).join(" · ")));
  const body = fileBody(file, payload, view);

  if (view.fullscreen) {
    return el("div", { class: "page" },
      el("header", { class: "fullscreen-header" },
        title,
        el("div", { class: "header-actions" },
          linkButton("Open in Bitbucket", file.url, view.bridge, "primary"),
          el("button", { type: "button", class: "button", onclick: () => view.expand() }, icon("collapse"), "Exit full screen"))),
      el("div", { class: "file-main", id: "file-main" }, body));
  }
  return el("div", { class: "file-view" },
    title,
    body,
    el("div", { class: "actions quiet" },
      view.canFullscreen && LINE_KINDS.includes(file.kind)
        ? el("button", { type: "button", class: "button", onclick: () => view.expand() }, icon("expand"), "Full screen")
        : null,
      linkButton("Open in Bitbucket", file.url, view.bridge, "ghost")));
}

// fileFacts are what the file is, in one grey line.
function fileFacts(file) {
  const facts = [];
  if (file.mime_type) facts.push(file.mime_type);
  if (file.size >= 0) facts.push(formatBytes(file.size));
  if (LINE_KINDS.includes(file.kind) && file.total_lines) facts.push(plural(file.total_lines, "line"));
  if (file.kind === "image" && file.width) facts.push(file.width + " × " + file.height + (file.scaled ? ", scaled down" : ""));
  return facts;
}

function formatBytes(bytes) {
  if (bytes < 1024) return plural(bytes, "byte");
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(bytes < 10240 ? 1 : 0) + " KB";
  return (bytes / (1024 * 1024)).toFixed(1) + " MB";
}

function fileBody(file, payload, view) {
  if (LINE_KINDS.includes(file.kind)) return fileLines(file, payload, view);
  if (file.kind === "image" && file.data) {
    const actual = view.unclamped.has("image-actual");
    return el("figure", { class: "file-figure" + (actual ? " actual" : "") },
      el("img", {
        src: file.data,
        alt: file.path,
        title: actual ? "Fit it to the view" : "See it at its size",
        onclick: () => view.toggleClamp("image-actual"),
      }),
      el("figcaption", { class: "faint" }, actual ? "At its size. Click to fit it to the view." : "Fitted to the view. Click to see it at its size."));
  }
  if ((file.kind === "audio" || file.kind === "video") && file.data) {
    return el("div", { class: "file-media" },
      el(file.kind, { controls: true, preload: "metadata", src: file.data, class: "file-player" }),
      el("p", { class: "faint" }, "This client may not play it; it plays in Bitbucket."));
  }
  return el("div", { class: "notice file-description" }, el("p", {}, file.description || "This file cannot be shown here."));
}

// fileLines draws a window of the file's lines, numbered, text highlighted
// where the language is known; inline, its start; the next window on
// request.
function fileLines(file, payload, view) {
  const lines = splitLines(file.lines || "");
  if (lines.length === 0) return el("p", { class: "faint" }, "This file is empty.");
  const cap = view.fullscreen ? lines.length : shownCount("file-lines", INLINE_FILE_VIEW_LINES, view);
  const shown = lines.slice(0, cap);
  const spans = file.kind === "text" ? file.highlight || [] : [];
  const rows = shown.map((text, index) => el("tr", {},
    el("td", { class: "line-number" }, String(file.start_line + index)),
    el("td", { class: "code" }, codeText(text, spans[index]))));
  const left = lines.length - shown.length;
  return el("div", { class: "file-lines" },
    el("table", { class: "diff-table file-table", "aria-label": file.path }, el("tbody", {}, rows)),
    left > 0
      ? el("div", { class: "more-row" }, view.canFullscreen
        ? el("button", { type: "button", class: "button ghost list-toggle", onclick: () => view.expand() }, "and " + plural(left, "more line") + ", in full screen")
        : moreButton("file-lines", left, FILE_VIEW_STEP, "lines", view))
      : null,
    left <= 0 && file.next_line
      ? el("div", { class: "more-row" },
        el("button", {
          type: "button",
          class: "button" + (view.opening === "file" ? " busy" : ""),
          disabled: Boolean(view.opening),
          onclick: () => loadMoreLines(payload, view),
        }, "Show lines " + formatNumber(file.next_line) + " on, of " + formatNumber(file.total_lines)))
      : null);
}

// splitLines is a window's lines, without the empty one after its last line
// ending.
function splitLines(text) {
  const lines = text.split("\n").map((line) => line.replace(/\r$/, ""));
  if (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
  return lines;
}

// loadMoreLines reads the file's next window and draws it after the lines
// the view has.
async function loadMoreLines(payload, view) {
  const file = payload.file;
  if (view.opening || !file.next_line || !payload.show) return;
  view.opening = "file";
  render();
  try {
    const result = await view.bridge.callTool(REFRESH_TOOL, Object.assign({}, payload.show, { start_line: file.next_line }), OPEN_TIMEOUT_MS);
    const next = payloadOf(result);
    if (!result || result.isError || !next || !next.file) throw new Error(textOf(result) || "bb could not read the next lines.");
    const joined = file.lines.endsWith("\n") || file.lines === "" ? file.lines : file.lines + "\n";
    const offset = splitLines(file.lines).length;
    const highlight = (file.highlight || []).slice(0, offset);
    while (highlight.length < offset) highlight.push("");
    file.lines = joined + next.file.lines;
    file.highlight = highlight.concat(next.file.highlight || []);
    file.end_line = next.file.end_line;
    file.next_line = next.file.next_line;
  } catch (error) {
    view.openFailure = (error && error.message) || "bb could not read the next lines.";
  } finally {
    view.opening = null;
    render();
  }
}

// fileButton opens a file of the pull request, as it is on the source branch,
// in this view.
function fileButton(file, pr, view) {
  if (!pr.repository || !pr.source_commit || file.status === "deleted" || !canOpen(view, "file")) return null;
  const path = filePath(file);
  return el("button", {
    type: "button",
    class: "button ghost view-file",
    title: "View " + path + " as this pull request has it",
    disabled: Boolean(view.opening),
    onclick: () => openInView(view, { kind: "file", project: pr.repository.project_key, repo: pr.repository.slug, path, at: pr.source_commit }),
  }, "View file");
}
