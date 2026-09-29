# AI and LLMs

Two ways an agent works with `bb`: it runs the CLI, or it connects to the MCP
server the CLI ships.

## The MCP server

`bb ai mcp serve` speaks the Model Context Protocol over stdio, so an IDE or
agent framework calls typed tools instead of parsing command output. Every
tool is exposed. The ones that change whether or when a pull request merges,
and creating a tag, ask you to confirm each call in the client before they run.
A client that cannot show that confirmation cannot use them.

Pull requests, their diffs and open threads, files and commits are also
resources you can attach in the client, and prompts such as "review a pull
request" bring them along. The client completes their project, repository, pull
request, path and ref as you type. bb reads them with its own credentials, so
the client never contacts Bitbucket.

In a client that renders MCP Apps, such as Claude Desktop, VS Code or Goose, the
agent answers with the pull request itself, a list of pull requests or a diff,
right in the chat: [views in your agent](#views-in-your-agent).

[**MCP Reference**](reference/mcp-tools.md) lists every tool, which of them
write and which ask, and every resource and prompt.

```bash
bb ai mcp serve
```

Wire it into a client by giving it that command and a token in the client's own
environment block, rather than on the command line:

```json
{
  "mcpServers": {
    "bitbucket": {
      "command": "bb",
      "args": ["ai", "mcp", "serve", "--project", "PLAT"],
      "env": { "BITBUCKET_TOKEN": "${BB_MCP_TOKEN}" }
    }
  }
}
```

Three flags decide what the server can reach:

| Flag | Effect |
|---|---|
| `--read-only` | Expose only the tools that read, for a client you do not trust to make changes |
| `--project`, `--repo` | Confine the server to one project or repository; calls aimed elsewhere are refused |
| `--audit-file` | Append a JSON Lines record per tool call, to a path or to `stderr` |

The strongest limit is not a flag: a read-only personal access token in
`BITBUCKET_TOKEN` makes every write fail at the server regardless of which tools
are exposed. [Enterprise Hardening](advanced/enterprise-hardening.md#5-ai-ide-mcp-server-governance-bb-ai-mcp-serve)
covers scoping, token restriction and mandating an audit trail by policy.

### Views in your agent

Ask about a pull request, and in a client that renders MCP Apps the answer comes
with the pull request itself, laid out as Bitbucket lays it out. There is
nothing to turn on: the model calls the `show` tool once it has found what you
asked about. A terminal client, such as Claude Code, shows the plain answer, and
`--exclude show` turns views off.

![A pull request card: its title, author and branches, who requested changes, a failed and a running build, an open task, and how many approvals and builds passed](assets/views/card-light.webp#only-light)
![A pull request card: its title, author and branches, who requested changes, a failed and a running build, an open task, and how many approvals and builds passed](assets/views/card-dark.webp#only-dark)

The card puts what asks something of someone on one line: who requested
changes, a conflict, failed and running builds, required builds that have not
run, open tasks. The counts under it
are Bitbucket's, however many reviewers and builds there are. **Overview** opens
the rest, fullscreen where the client has it and opened out in place where it
does not: the description as Bitbucket formats it, the reviewers by their
decision, the builds the target branch requires before it merges, every build
by its state, and the comments and tasks. **Diff** and
**Comments** open those in the same view, and **Back** returns to the card.
**Approve** and **Request changes**, in the overview and over the diff, review
the pull request once you confirm it in your client.

![A pull request's overview: its description beside Approve and Request changes, the reviewers grouped by decision, and its builds grouped by state](assets/views/overview-light.webp#only-light)
![A pull request's overview: its description beside Approve and Request changes, the reviewers grouped by decision, and its builds grouped by state](assets/views/overview-dark.webp#only-dark)

A list flags only what needs attention in each pull request: a request for
changes, a draft, failed builds. A row opens its card in the same view.
Fullscreen adds Bitbucket's dashboard columns and filters, and asks for the
pull requests in another state or role.

![A list of four pull requests, two of them with changes requested and a failed build, one a draft](assets/views/list-light.webp#only-light)
![A list of four pull requests, two of them with changes requested and a failed build, one a draft](assets/views/list-dark.webp#only-dark)

A diff comes file by file, with Bitbucket's change lozenges and the files in
their directories, the code highlighted, and each comment on the line it was
written on. Select lines
to add them to the chat, to ask about them, or to comment on them. A file too
large to carry is named, with a link to its diff in Bitbucket.

![A pull request's diff: the changed files by directory, each with its open comments counted, beside the highlighted changes, a comment on the line it was written on, and Approve and Request changes over it](assets/views/diff-light.webp#only-light)
![A pull request's diff: the changed files by directory, each with its open comments counted, beside the highlighted changes, a comment on the line it was written on, and Approve and Request changes over it](assets/views/diff-dark.webp#only-dark)

The comment threads come where they are: those on the pull request first, then
each file's, the open ones out and the resolved folded to their count. A thread
on a changed line comes with the lines of the diff that lead to it, and a long
discussion folds its earlier replies. The card lists what is still open in
the same order, under a heading for each place. Reply to a thread, or comment
on the pull request, from the view itself.

Ask for a pull request to be opened, and the agent can hand you its draft as a
form: the branches, the title and description it wrote, the reviewers it
suggests. Edit what you like, with branches and reviewers completed as you
type, and create it; nothing is created until you do. An existing pull
request's title, description and draft state are edited the same way.

![A pull request form: the source and target branches, the title and the description the agent drafted, a reviewer field, the draft box, and Create pull request](assets/views/form-light.webp#only-light)
![A pull request form: the source and target branches, the title and the description the agent drafted, a reviewer field, the draft box, and Create pull request](assets/views/form-dark.webp#only-dark)

A file you have no copy of, such as another repository's, is shown as what it
is: highlighted code with line numbers, a picture fitted to the view or at its size, audio
and video that play, an archive's listing. A long file comes a window at a
time. In a diff, **View file** opens a changed file as the pull request has it.

![A Go file shown as highlighted code with line numbers](assets/views/file-light.webp#only-light)
![A Go file shown as highlighted code with line numbers](assets/views/file-dark.webp#only-dark)

![A pull request's comment threads: tasks and comments on the pull request and on a file, each with Reply, under Add a comment](assets/views/threads-light.webp#only-light)
![A pull request's comment threads: tasks and comments on the pull request and on a file, each with Reply, under Add a comment](assets/views/threads-dark.webp#only-dark)

- A view carries what it draws. It renders again when you come back to the
  conversation, even without bb running, and it says when it was read.
- While a view is on screen, it keeps itself current: a build that finishes or
  a new comment appears in place, and the model is told. It asks bb every 15
  seconds while a build runs, less often the longer nothing changes, and not at
  all off screen. A changed diff is offered rather than swapped in while you
  read it. `--exclude refresh_view` keeps views as they were drawn.
- A count is Bitbucket's count of the whole, and a view that lists fewer says
  so. A failed build or a request for changes is never behind a click.
- Code is highlighted as far as bb gets before a deadline, and the rest drawn
  plain. Of a Svelte, ERB, PHTML, Go HTML or Jinja template only the markup is
  highlighted: the lexers for the code inside cannot be stopped midway, and a
  crafted file keeps one busy for seconds. `--highlight-templates` highlights
  that code too, and no view waits for it.
- What you click opens in Bitbucket, through the client. A client that will not
  open a link shows you its address to copy.
- bb reads everything a view shows, avatars included, with its own credentials:
  the client never contacts Bitbucket.

### What a tool returns

A tool answers as if the client had nothing but MCP: no file system, no shell
and no `bb`. What a model needs comes back as text or as an image, converted on
the server, and never as a command to run, a path to open or a link to follow
([ADR-094](adr/094-mcp-tool-results-are-what-a-model-can-use-without-files-or-a-shell.md)).
`get_file_content` shows what that means for a file:

| The file | What comes back |
|---|---|
| Text | A window of numbered lines; `start_line` and `line_count` choose it, and each answer gives the `start_line` of the next |
| Word, PowerPoint, Excel | The text extracted from it, in the same windows: paragraphs and table rows, slides in order with their notes, sheets row by row |
| Zip, jar, tar, tar.gz, tar.bz2 | A listing of its entries, in the same windows |
| PNG, JPEG, GIF, WebP, BMP, TIFF | The image, or a TIFF's first page: as a PNG or JPEG when clients do not take its format, turned upright when its metadata says it was stored turned, and scaled down when it is larger than clients take, with a note saying which |
| Audio, video | The file itself beside a description when it is small, and the description alone when not |
| PDF, anything else | A description of its type and size, with its page in Bitbucket for a person to open |

## Driving the CLI directly

An agent that runs commands should pass `--json` and read the envelope rather
than the human output, and consult `--describe` for a command's schema before
guessing at flags. [Machine Mode and Diagnostics](advanced/machine-mode-diagnostics.md)
has the envelope, the error kinds and the exit codes.

## Installing the skill

The skill is a `SKILL.md` that teaches a shell-driving agent the command
surface: target resolution, the flags that matter, and the shapes commands
return. `bb` carries it embedded, so installing it needs no network and no
checkout of this repository:

```bash
bb ai skill install
```

That writes `.agents/skills/bb/SKILL.md`, which most agents read, and
`.claude/skills/bb/SKILL.md`, which Claude Code reads instead, alongside the
project. `--global` writes both under your home directory instead, for every
project of yours. `bb ai skill remove` deletes the files it wrote.

Scoop installs the skill with `--global` when it installs `bb`, and removes it
with `bb`. Homebrew and the `.deb` and `.rpm` packages only remind you to run
the command. They install for every user of the machine, and no one place
reaches every user's agents: the agents that read skills machine-wide each read
a directory of their own.

Where an agent reads a directory of its own, print the skill and redirect it
there; Cline, for one, reads `~/.cline/skills`:

```bash
mkdir -p ~/.cline/skills/bb
bb ai skill show > ~/.cline/skills/bb/SKILL.md
```

**Re-run `bb ai skill install` after upgrading `bb`.** Both subcommands print the
copy compiled into the binary you just ran, so the skill and the command surface
cannot disagree, and `bb doctor` reports an installed copy that an earlier `bb`
wrote. The same skill is published through the open agent skills
ecosystem for machines where `bb` is not installed yet, but that copy is a
snapshot taken at release time and can describe a different version
([ADR-040](adr/040-agent-skill-distribution-static-npx-and-dynamic-cli.md)):

```bash
npx skills add vriesdemichael/bitbucket-data-center-cli
```

## llms.txt

[`llms.txt`](llms.txt) is a setup guide written for an agent to work through in
order: install, authenticate, then enable either the skill or the MCP server. It
ends with the machine output contract and links onward. It is not a summary of
the product or a substitute for the command reference.

Point an agent at it when the task is getting `bb` working. Once `bb` runs, the
agent's sources are the skill or the MCP tool catalogue for what to call,
`--help` and `--describe` for the exact surface, and these pages for behaviour.

- Published `llms.txt`: [llms.txt](llms.txt)
- Versioned docs home: [Home](index.md)
- Installation and Quickstart: [Installation and Quickstart](installation-and-quickstart.md)
- Command Reference: [All Commands](reference/commands/index.md)
- Machine-readable schemas: [JSON Schemas](reference/schemas.md)
- AI skill for agents: [SKILL.md on GitHub](https://github.com/vriesdemichael/bitbucket-data-center-cli/blob/main/skills/bb/SKILL.md)

## Which source answers which question

| Question | Source |
|---|---|
| How do I get `bb` working at all? | `llms.txt` |
| What can I call, and how do I drive it? | The skill, or `bb ai mcp tools` |
| What exactly does this command take? | `bb <command> --help`, and [All Commands](reference/commands/index.md) |
| What shape does it return? | `bb <command> --describe`, and [JSON Schemas](reference/schemas.md) |
| Why does it behave that way? | [Advanced Topics](advanced/index.md) and the [ADRs](adr/index.md) |
| It failed and I need to know why | [Troubleshooting](troubleshooting.md), and [Machine Mode and Diagnostics](advanced/machine-mode-diagnostics.md) |