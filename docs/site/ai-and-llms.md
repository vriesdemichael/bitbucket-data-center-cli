# AI and LLMs

Two ways an agent works with `bb`: it runs the CLI, or it connects to the MCP
server the CLI ships.

## The MCP server

`bb ai mcp serve` speaks the Model Context Protocol over stdio, so an IDE or
agent framework calls typed tools instead of parsing command output. It
registers 24 tools; 20 are available to any client that connects, and 4 are
withheld unless the server is started with `--yolo`.

[**MCP Tools**](reference/mcp-tools.md) lists all of them, which are read-only,
and which need `--yolo`. That page is generated from the server's own registry,
so it cannot fall behind the tools that actually exist.

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
| `--yolo` (alias `--allow-writes`) | Also expose the four withheld tools |
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

## llms.txt

`bb` publishes an `llms.txt` document for agents that need a product-level briefing before they start calling anything.

Use it when you want a compact description of what the CLI is for, what it does well, and where the deeper source-of-truth docs live.

- Published `llms.txt`: [llms.txt](llms.txt)
- Versioned docs home: [Home](index.md)
- Installation and Quickstart: [Installation and Quickstart](installation-and-quickstart.md)
- Command Reference: [All Commands](reference/commands/index.md)
- Machine-readable schemas: [JSON Schemas](reference/schemas.md)
- AI core skill for agents: [SKILL.md on GitHub](https://github.com/vriesdemichael/bitbucket-data-center-cli/blob/main/skills/bb/SKILL.md)
- AI bulk governance skill for agents: [Bulk SKILL.md on GitHub](https://github.com/vriesdemichael/bitbucket-data-center-cli/blob/main/skills/bb-bulk/SKILL.md)

## What it covers

- Product premise and operator value
- Core strengths and safety model
- Important workflows for pull requests, search, tags, builds, and automation
- Pointers to the full docs, schemas, skill, and repository

## When to use which source

- Start with `llms.txt` when an agent needs quick orientation.
- Use the command reference when exact flags and arguments matter.
- Use the advanced docs and ADRs when the agent needs behavioral or architectural context.