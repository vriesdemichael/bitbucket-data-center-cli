# AI and LLMs

Two ways an agent works with `bb`: it runs the CLI, or it connects to the MCP
server the CLI ships.

## The MCP server

`bb ai mcp serve` speaks the Model Context Protocol over stdio, so an IDE or
agent framework calls typed tools instead of parsing command output. It
exposes most of its tools to any client that connects. The ones that merge a
pull request, or feed the checks deciding whether a merge is allowed, are
withheld unless the server is started with `--yolo`.

[**MCP Tools**](reference/mcp-tools.md) is the reference: every tool, which of
them write, and which need `--yolo`.

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
| `--yolo` (alias `--allow-writes`) | Also expose the withheld tools |
| `--project`, `--repo` | Confine the server to one project or repository; calls aimed elsewhere are refused |
| `--audit-file` | Append a JSON Lines record per tool call, to a path or to `stderr` |

The strongest limit is not a flag: a read-only personal access token in
`BITBUCKET_TOKEN` makes every write fail at the server regardless of which tools
are exposed. [Enterprise Hardening](advanced/enterprise-hardening.md#5-ai-ide-mcp-server-governance-bb-ai-mcp-serve)
covers scoping, token restriction and mandating an audit trail by policy.

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