// A pull request's description, drawn from its Markdown as Bitbucket draws
// it: headings, paragraphs, lists and task lists, quotes, fenced and indented
// code, tables, rules, emphasis, strikethrough, code spans and links. As in
// Bitbucket, a single line break is a break.
//
// The parser builds elements with el() and text nodes, and never hands a
// string to the browser to parse, so HTML in a description stays text, as
// everything else a view draws does. A link opens through the host, and only
// when it leads to a web page. An image is named rather than drawn: a view has
// no network.

// MD_MAX_DEPTH is how deeply quotes, lists and emphasis nest before what is
// inside is drawn as text.
const MD_MAX_DEPTH = 8;

// MD_MAX_SCAN is how far an opening mark looks for its close, which keeps a
// description built to be slow, such as thousands of unclosed marks, fast.
const MD_MAX_SCAN = 2000;

const MD_FENCE = /^( {0,3})(`{3,}(?=[^`]*$)|~{3,})(.*)$/;
const MD_HEADING = /^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$/;
const MD_SETEXT = /^ {0,3}(=+|-+)[ \t]*$/;
const MD_RULE = /^ {0,3}(?:(?:\*[ \t]*){3,}|(?:-[ \t]*){3,}|(?:_[ \t]*){3,})$/;
const MD_QUOTE = /^ {0,3}>/;
const MD_ITEM = /^( {0,3})([-+*]|\d{1,9}[.)])(?:([ \t]+)(.*))?$/;
const MD_TABLE_DELIMITER = /^ {0,3}\|?[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$/;
const MD_REFERENCE = /^ {0,3}\[([^\]]+)\]:[ \t]*<?([^\s>]+)>?(?:[ \t]+(?:"([^"]*)"|'([^']*)'|\(([^)]*)\)))?[ \t]*$/;
const MD_PUNCTUATION = /[!"#$%&'()*+,\-./:;<=>?@[\\\]^_`{|}~]/;
const MD_WORD = /[\p{L}\p{N}]/u;

const MD_ENTITIES = {
  amp: "&", lt: "<", gt: ">", quot: "\"", apos: "'", nbsp: " ", copy: "©", reg: "®", trade: "™",
  hellip: "…", mdash: "—", ndash: "–", lsquo: "‘", rsquo: "’", ldquo: "“", rdquo: "”", times: "×", middot: "·", bull: "•",
};

// renderMarkdown draws source. base is the address a link starting with "/"
// is taken against, the Bitbucket the pull request is on.
function renderMarkdown(source, options) {
  const lines = String(source || "")
    .replace(/\r\n?/g, "\n")
    .split("\n")
    .map((line) => line.replace(/^\t+/, (tabs) => "    ".repeat(tabs.length)));
  const context = { bridge: options.bridge, base: options.base, references: markdownReferences(lines) };
  return el("div", { class: "markdown" }, markdownBlocks(lines, context, 0));
}

// markdownReferences takes the link definitions out of the lines, outside
// code, so a link can name one defined further down.
function markdownReferences(lines) {
  const references = new Map();
  let fence = null;
  for (let i = 0; i < lines.length; i++) {
    const opening = MD_FENCE.exec(lines[i]);
    if (fence) {
      if (opening && opening[2][0] === fence[0] && opening[2].length >= fence.length && opening[3].trim() === "") fence = null;
      continue;
    }
    if (opening) {
      fence = opening[2];
      continue;
    }
    const definition = MD_REFERENCE.exec(lines[i]);
    if (definition) {
      const label = normalizeLabel(definition[1]);
      if (!references.has(label)) {
        references.set(label, { url: definition[2], title: definition[3] || definition[4] || definition[5] || "" });
      }
      lines[i] = "";
    }
  }
  return references;
}

function normalizeLabel(label) {
  return label.trim().replace(/\s+/g, " ").toLowerCase();
}

function isBlank(line) {
  return line.trim() === "";
}

function markdownBlocks(lines, context, depth) {
  if (depth > MD_MAX_DEPTH) return [el("p", {}, lines.join("\n"))];
  const blocks = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (isBlank(line)) {
      i++;
      continue;
    }
    let block;
    let match;
    if ((match = MD_FENCE.exec(line))) {
      [block, i] = fencedCode(lines, i, match);
    } else if ((match = MD_HEADING.exec(line))) {
      block = markdownHeading(match[1].length, match[2] || "", context);
      i++;
    } else if (MD_RULE.test(line)) {
      block = el("hr", { class: "md-rule" });
      i++;
    } else if (MD_QUOTE.test(line)) {
      [block, i] = blockQuote(lines, i, context, depth);
    } else if (MD_ITEM.test(line)) {
      [block, i] = markdownList(lines, i, context, depth);
    } else if (/^ {4}/.test(line)) {
      [block, i] = indentedCode(lines, i);
    } else if (startsTable(lines, i)) {
      [block, i] = markdownTable(lines, i, context);
    } else {
      [block, i] = markdownParagraph(lines, i, context);
    }
    blocks.push(block);
  }
  return blocks;
}

function markdownHeading(level, text, context) {
  // The view's own title is its h1, so a description's headings sit below it.
  return el("h" + Math.min(level + 2, 6), { class: "md-h md-h" + level }, markdownInline(text, context, 0, false));
}

function fencedCode(lines, start, opening) {
  const indent = opening[1].length;
  const fence = opening[2];
  const content = [];
  let i = start + 1;
  for (; i < lines.length; i++) {
    const closing = MD_FENCE.exec(lines[i]);
    if (closing && closing[2][0] === fence[0] && closing[2].length >= fence.length && closing[3].trim() === "") {
      i++;
      break;
    }
    content.push(lines[i].replace(new RegExp("^ {0," + indent + "}"), ""));
  }
  const language = opening[3].trim().split(/\s+/)[0];
  return [el("pre", { class: "md-code", "data-language": language || undefined }, el("code", {}, content.join("\n"))), i];
}

function indentedCode(lines, start) {
  const content = [];
  let i = start;
  for (; i < lines.length; i++) {
    if (/^ {4}/.test(lines[i])) content.push(lines[i].slice(4));
    else if (isBlank(lines[i])) content.push("");
    else break;
  }
  while (content.length > 0 && content[content.length - 1] === "") content.pop();
  return [el("pre", { class: "md-code" }, el("code", {}, content.join("\n"))), i];
}

// interrupts reports whether a line starts a block that ends a paragraph.
function interrupts(lines, i) {
  const line = lines[i];
  if (MD_FENCE.test(line) || MD_HEADING.test(line) || MD_RULE.test(line) || MD_QUOTE.test(line) || startsTable(lines, i)) return true;
  const item = MD_ITEM.exec(line);
  // A list interrupts a paragraph when it has content, and an ordered one
  // only when it starts at one.
  return Boolean(item && item[4] && item[4].trim() !== "" && (!/\d/.test(item[2]) || parseInt(item[2], 10) === 1));
}

function markdownParagraph(lines, start, context) {
  const text = [];
  let i = start;
  while (i < lines.length && !isBlank(lines[i])) {
    if (i > start) {
      const underline = MD_SETEXT.exec(lines[i]);
      if (underline) return [markdownHeading(underline[1][0] === "=" ? 1 : 2, text.join("\n"), context), i + 1];
      if (interrupts(lines, i)) break;
    }
    text.push(lines[i].trim());
    i++;
  }
  return [el("p", {}, markdownInline(text.join("\n"), context, 0, false)), i];
}

function blockQuote(lines, start, context, depth) {
  const inner = [];
  let i = start;
  while (i < lines.length) {
    const line = lines[i];
    if (MD_QUOTE.test(line)) {
      inner.push(line.replace(/^ {0,3}> ?/, ""));
      i++;
    } else if (!isBlank(line) && inner.length > 0 && !isBlank(inner[inner.length - 1]) && !interrupts(lines, i)) {
      // A line that carries on the quote's paragraph without its own marker.
      inner.push(line);
      i++;
    } else {
      break;
    }
  }
  return [el("blockquote", { class: "md-quote" }, markdownBlocks(inner, context, depth + 1)), i];
}

function markdownList(lines, start, context, depth) {
  const first = MD_ITEM.exec(lines[start]);
  const ordered = /\d/.test(first[2]);
  const marker = ordered ? first[2].slice(-1) : first[2];
  const items = [];
  let i = start;
  while (i < lines.length) {
    const match = MD_ITEM.exec(lines[i]);
    if (!match || /\d/.test(match[2]) !== ordered || (ordered ? match[2].slice(-1) : match[2]) !== marker) break;
    const spacing = match[3] ? match[3].length : 1;
    // Content indented five or more past the marker is indented code, which
    // starts one space in.
    const contentIndent = match[1].length + match[2].length + (spacing > 4 ? 1 : spacing);
    const content = [spacing > 4 ? " ".repeat(spacing - 1) + (match[4] || "") : match[4] || ""];
    i++;
    let blank = false;
    while (i < lines.length) {
      const line = lines[i];
      if (isBlank(line)) {
        content.push("");
        blank = true;
        i++;
        continue;
      }
      if (line.match(/^ */)[0].length >= contentIndent) {
        content.push(line.slice(contentIndent));
        blank = false;
        i++;
        continue;
      }
      // After a blank line, an unindented line ends the item; before one, a
      // line of text carries on its paragraph.
      if (blank || MD_ITEM.test(line) || interrupts(lines, i)) break;
      content.push(line.trim());
      i++;
    }
    while (content.length > 0 && content[content.length - 1] === "") content.pop();
    items.push({ content, number: ordered ? parseInt(match[2], 10) : null });
  }

  const drawn = items.map((item) => {
    const task = /^\[([ xX])\][ \t]+/.exec(item.content[0] || "");
    if (!task) return el("li", {}, markdownBlocks(item.content, context, depth + 1));
    const rest = [item.content[0].slice(task[0].length), ...item.content.slice(1)];
    return el("li", { class: "md-task" },
      el("input", { type: "checkbox", disabled: true, checked: task[1] !== " ", "aria-label": task[1] !== " " ? "Done" : "Not done" }),
      markdownBlocks(rest, context, depth + 1));
  });
  const start1 = items.length > 0 ? items[0].number : null;
  return [ordered
    ? el("ol", { class: "md-list", start: start1 !== null && start1 !== 1 ? start1 : undefined }, drawn)
    : el("ul", { class: "md-list" }, drawn), i];
}

function startsTable(lines, i) {
  return i + 1 < lines.length && lines[i].includes("|") && MD_TABLE_DELIMITER.test(lines[i + 1]) &&
    tableCells(lines[i]).length === tableCells(lines[i + 1]).length;
}

function tableCells(line) {
  let text = line.trim();
  if (text.startsWith("|")) text = text.slice(1);
  if (text.endsWith("|") && !text.endsWith("\\|")) text = text.slice(0, -1);
  const cells = [];
  let cell = "";
  for (let k = 0; k < text.length; k++) {
    if (text[k] === "\\" && text[k + 1] === "|") {
      cell += "|";
      k++;
    } else if (text[k] === "|") {
      cells.push(cell.trim());
      cell = "";
    } else {
      cell += text[k];
    }
  }
  cells.push(cell.trim());
  return cells;
}

function markdownTable(lines, start, context) {
  const header = tableCells(lines[start]);
  const aligns = tableCells(lines[start + 1]).map((cell) =>
    cell.startsWith(":") && cell.endsWith(":") ? "center" : cell.endsWith(":") ? "right" : cell.startsWith(":") ? "left" : "");
  const rows = [];
  let i = start + 2;
  while (i < lines.length && !isBlank(lines[i]) && lines[i].includes("|") && !MD_FENCE.test(lines[i]) && !MD_QUOTE.test(lines[i])) {
    rows.push(tableCells(lines[i]));
    i++;
  }
  const cell = (tag, text, column) => el(tag, { class: aligns[column] ? "align-" + aligns[column] : undefined },
    markdownInline(text, context, 0, false));
  // A wide table scrolls sideways inside its own box, never the view.
  return [el("div", { class: "md-table-wrap" }, el("table", { class: "md-table" },
    el("thead", {}, el("tr", {}, header.map((text, column) => cell("th", text, column)))),
    rows.length > 0 ? el("tbody", {}, rows.map((row) => el("tr", {}, header.map((_, column) => cell("td", row[column] || "", column))))) : null)), i];
}

// markdownInline draws a run of text: code spans, links, emphasis, line
// breaks and escapes. Inside a link's text, links are text.
function markdownInline(text, context, depth, inLink) {
  const out = [];
  let buffer = "";
  const flush = () => {
    if (buffer) out.push(buffer);
    buffer = "";
  };
  let i = 0;
  while (i < text.length) {
    const ch = text[i];
    const next = text[i + 1];
    let found = null;
    if (ch === "\\" && next === "\n") {
      found = { node: el("br"), end: i + 2 };
    } else if (ch === "\\" && next !== undefined && MD_PUNCTUATION.test(next)) {
      buffer += next;
      i += 2;
      continue;
    } else if (ch === "\n") {
      found = { node: el("br"), end: i + 1 };
    } else if (ch === "&") {
      const entity = /^&(#\d{1,7}|#[xX][0-9a-fA-F]{1,6}|[a-zA-Z][a-zA-Z0-9]{1,31});/.exec(text.slice(i, i + 40));
      const decoded = entity ? decodeEntity(entity[1]) : null;
      if (decoded !== null) {
        buffer += decoded;
        i += entity[0].length;
        continue;
      }
    } else if (ch === "`") {
      found = codeSpan(text, i);
    } else if (ch === "<") {
      found = inLink ? null : autolink(text, i, context);
    } else if (ch === "!" && next === "[") {
      found = inLink ? null : linkOrImage(text, i + 1, context, depth, true);
    } else if (ch === "[") {
      found = inLink ? null : linkOrImage(text, i, context, depth, false);
    } else if (ch === "*" || ch === "_" || (ch === "~" && next === "~")) {
      found = emphasis(text, i, context, depth, inLink);
    } else if ((ch === "h" || ch === "H") && !inLink && (i === 0 || /[\s(]/.test(text[i - 1])) && startsWebAddress(text, i)) {
      found = bareLink(text, i, context);
    }
    if (found) {
      flush();
      out.push(found.node);
      i = found.end;
      continue;
    }
    buffer += ch;
    i++;
  }
  flush();
  return out;
}

function decodeEntity(name) {
  if (name[0] === "#") {
    const code = name[1] === "x" || name[1] === "X" ? parseInt(name.slice(2), 16) : parseInt(name.slice(1), 10);
    if (!Number.isFinite(code) || code <= 0 || code > 0x10ffff || (code >= 0xd800 && code <= 0xdfff)) return "�";
    return String.fromCodePoint(code);
  }
  return Object.prototype.hasOwnProperty.call(MD_ENTITIES, name) ? MD_ENTITIES[name] : null;
}

// codeSpan is the code between a run of backticks and the next run as long,
// or the backticks themselves when nothing closes them.
function codeSpan(text, start) {
  let run = 0;
  while (text[start + run] === "`") run++;
  const limit = Math.min(text.length, start + run + MD_MAX_SCAN * 4);
  for (let k = start + run; k < limit;) {
    if (text[k] !== "`") {
      k++;
      continue;
    }
    let close = 0;
    while (text[k + close] === "`") close++;
    if (close === run) {
      let code = text.slice(start + run, k).replace(/\n/g, " ");
      if (code.length > 2 && code.startsWith(" ") && code.endsWith(" ") && code.trim() !== "") code = code.slice(1, -1);
      return { node: el("code", { class: "md-code-span" }, code), end: k + close };
    }
    k += close;
  }
  return { node: "`".repeat(run), end: start + run };
}

function autolink(text, start, context) {
  const match = /^<([a-zA-Z][a-zA-Z0-9+.-]{1,31}:[^\s<>]*)>/.exec(text.slice(start, start + 2048));
  if (!match) return null;
  return { node: markdownLink([match[1]], match[1], "", context), end: start + match[0].length };
}

// startsWebAddress reports whether a web address starts at text[i].
function startsWebAddress(text, i) {
  const head = text.slice(i, i + 8).toLowerCase();
  return head.startsWith("https://") || head.startsWith("http://");
}

// bareLink is a web address written out in the text, as GitHub and Bitbucket
// link one, less the punctuation that ends the sentence around it.
function bareLink(text, start, context) {
  let end = start;
  while (end < text.length && end - start < 2048 && !/[\s<]/.test(text[end])) end++;
  while (end > start && /[.,:;!?"'*_~]/.test(text[end - 1])) end--;
  const url = text.slice(start, end);
  if (url.endsWith(")") && (url.match(/\)/g) || []).length > (url.match(/\(/g) || []).length) end--;
  const address = text.slice(start, end);
  if (!webAddress(address, "")) return null;
  return { node: markdownLink([address], address, "", context), end };
}

// linkOrImage reads a link or an image whose text opens at text[open]: an
// inline one, [text](url "title"), or one that names a definition.
function linkOrImage(text, open, context, depth, image) {
  const close = closingBracket(text, open);
  if (close < 0) return null;
  const label = text.slice(open + 1, close);
  let url;
  let title = "";
  let end;
  if (text[close + 1] === "(") {
    const destination = linkDestination(text, close + 2);
    if (!destination) return null;
    ({ url, title, end } = destination);
  } else {
    let name = label;
    end = close + 1;
    if (text[close + 1] === "[") {
      const nameClose = text.indexOf("]", close + 2);
      if (nameClose > 0 && nameClose - close < 1000) {
        name = text.slice(close + 2, nameClose) || label;
        end = nameClose + 1;
      }
    }
    const definition = context.references.get(normalizeLabel(name));
    if (!definition) return null;
    ({ url, title } = definition);
  }
  const node = image
    ? markdownImage(label, url, context)
    : markdownLink(markdownInline(label, context, depth + 1, true), url, title, context);
  return { node, end };
}

function closingBracket(text, open) {
  let level = 0;
  const limit = Math.min(text.length, open + MD_MAX_SCAN);
  for (let k = open; k < limit; k++) {
    const ch = text[k];
    if (ch === "\\") {
      k++;
    } else if (ch === "`") {
      const span = codeSpan(text, k);
      if (typeof span.node !== "string") k = span.end - 1;
    } else if (ch === "[") {
      level++;
    } else if (ch === "]") {
      level--;
      if (level === 0) return k;
    }
  }
  return -1;
}

function linkDestination(text, start) {
  let i = start;
  while (text[i] === " " || text[i] === "\n") i++;
  let url = "";
  if (text[i] === "<") {
    const close = text.indexOf(">", i + 1);
    if (close < 0 || text.slice(i + 1, close).includes("\n")) return null;
    url = text.slice(i + 1, close);
    i = close + 1;
  } else {
    let level = 0;
    const from = i;
    for (; i < text.length && i - from < 4096; i++) {
      const ch = text[i];
      if (ch === "\\" && MD_PUNCTUATION.test(text[i + 1] || "")) {
        i++;
      } else if (/\s/.test(ch)) {
        break;
      } else if (ch === "(") {
        level++;
      } else if (ch === ")") {
        if (level === 0) break;
        level--;
      }
    }
    url = text.slice(from, i).replace(/\\([!"#$%&'()*+,\-./:;<=>?@[\\\]^_`{|}~])/g, "$1");
  }
  while (text[i] === " " || text[i] === "\n") i++;
  let title = "";
  const quote = text[i];
  if (quote === "\"" || quote === "'" || quote === "(") {
    const closer = quote === "(" ? ")" : quote;
    const close = text.indexOf(closer, i + 1);
    if (close < 0) return null;
    title = text.slice(i + 1, close);
    i = close + 1;
    while (text[i] === " " || text[i] === "\n") i++;
  }
  if (text[i] !== ")") return null;
  return { url, title, end: i + 1 };
}

// webAddress is where a link leads, when that is a web page: an absolute
// http or https address, or a path on the Bitbucket the pull request is on.
function webAddress(url, base) {
  const target = String(url || "").trim();
  try {
    const parsed = target.startsWith("/") && !target.startsWith("//") && base ? new URL(target, base) : new URL(target);
    return parsed.protocol === "https:" || parsed.protocol === "http:" ? parsed.href : "";
  } catch {
    return "";
  }
}

function markdownLink(children, url, title, context) {
  const target = webAddress(url, context.base);
  if (!target) return el("span", { class: "md-link-text" }, children);
  return el("button", {
    type: "button",
    class: "md-link",
    title: title ? title + " (" + target + ")" : target,
    onclick: () => openLink(context.bridge, target),
  }, children);
}

function markdownImage(alt, url, context) {
  const target = webAddress(url, context.base);
  const label = [icon("image"), alt || "image"];
  if (!target) return el("span", { class: "md-image", title: "An image this view cannot show" }, label);
  return el("button", { type: "button", class: "md-link md-image", title: "Open the image: " + target, onclick: () => openLink(context.bridge, target) }, label);
}

// emphasis reads emphasis, strong emphasis or strikethrough opening at
// text[start], or nothing when the marks do not open or nothing closes them.
// An underscore inside a word, as in snake_case, is a letter.
function emphasis(text, start, context, depth, inLink) {
  if (depth >= MD_MAX_DEPTH) return null;
  const ch = text[start];
  let run = 0;
  while (text[start + run] === ch) run++;
  if (ch === "~" && run !== 2) return null;
  const before = start > 0 ? text[start - 1] : " ";
  const after = text[start + run] || " ";
  if (/\s/.test(after) || (ch === "_" && MD_WORD.test(before))) return null;
  const size = ch === "~" ? 2 : Math.min(run, 3);
  const open = start + run - size;

  const limit = Math.min(text.length, open + size + MD_MAX_SCAN);
  for (let k = open + size; k < limit;) {
    const c = text[k];
    if (c === "\\") {
      k += 2;
      continue;
    }
    if (c === "`") {
      const span = codeSpan(text, k);
      k = span.end;
      continue;
    }
    if (c !== ch) {
      k++;
      continue;
    }
    let close = 0;
    while (text[k + close] === ch) close++;
    const previous = text[k - 1];
    const following = text[k + close] || " ";
    const canClose = k > open + size && !/\s/.test(previous) && (ch !== "_" || !MD_WORD.test(following));
    // A closing run matches an opening run of its own size, or closes one
    // inside it as well when it is longer: ***both*** and **strong *em***.
    if (canClose && (close === size || close >= 3)) {
      const inner = markdownInline(text.slice(open + size, k), context, depth + 1, inLink);
      const lead = text.slice(start, open);
      let node;
      if (ch === "~") node = el("del", {}, inner);
      else if (size === 3) node = el("strong", {}, el("em", {}, inner));
      else if (size === 2) node = el("strong", {}, inner);
      else node = el("em", {}, inner);
      return { node: lead ? [lead, node] : node, end: k + size };
    }
    k += close;
  }
  return { node: text.slice(start, start + run), end: start + run };
}
