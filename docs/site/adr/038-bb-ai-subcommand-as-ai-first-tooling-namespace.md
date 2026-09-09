---
search:
  boost: 0.3
---

# ADR 038: bb ai subcommand as AI-first tooling namespace

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `038`
- Title: `bb ai subcommand as AI-first tooling namespace`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/038-bb-ai-subcommand-as-ai-first-tooling-namespace.yaml`

## Decision

Introduce a dedicated top-level bb ai command group that houses all AI-first tooling. The initial subcommands are bb ai mcp (MCP server management) and bb ai skill (agent skill distribution). Each of these is itself a command group with further subcommands.

## Agent Instructions

All AI-agent-oriented features (MCP server, skill generation and installation) must live under bb ai. Do not add AI-specific concerns to existing groups such as auth, admin, or repo. Within bb ai, follow the same command-tree conventions established in ADR 013: grouped nouns, shared global flags, parity between human and JSON output modes.

## Rationale

Isolating AI tooling under a dedicated namespace keeps the main command tree focused on Bitbucket operations while making AI-first capabilities clearly discoverable. It also allows AI-specific concerns — host scoping, token capability restriction, skill versioning — to evolve independently without risk to existing command contracts. The name ai signals intent explicitly to both human users and coding agents reading bb --help.

## Rejected Alternatives

- `Add bb serve as a top-level command for the MCP server`: Pollutes the top-level namespace with infrastructure concerns; not parallel with the skill distribution need.
- `Add MCP server and skill commands under bb admin`: admin is scoped to Bitbucket server administration tasks, not local CLI tooling. Wrong semantic bucket.
- `Add MCP server and skill commands under bb auth`: auth manages credentials, not tooling distribution. Conflation would confuse both humans and agents.
