---
search:
  boost: 0.3
---

# ADR-038: bb ai subcommand as AI-first tooling namespace

`bb ai` is the top-level group for bb's tooling for AI agents. It holds `bb ai mcp`, which runs the MCP server and lists its tools (ADR-039), and `bb ai skill`, which installs, removes and prints the agent skill (ADR-040).

Put every feature for AI agents under `bb ai`, and nothing of it in `auth`, `admin`, `repo` or another resource group. Within `bb ai`, follow the conventions of ADR-013: grouped nouns, the shared global flags, and the same result as text or as the machine document.

Keeping AI tooling in its own namespace keeps the main tree about Bitbucket, and makes the AI features easy to find in `bb --help` for a person and for a coding agent. It also lets the concerns particular to them, such as host scoping, token restriction and skill versions, change without touching any other command's contract.

## Not chosen

- **Add bb serve as a top-level command for the MCP server**: Pollutes the top-level namespace with infrastructure concerns; not parallel with the skill distribution need.
- **Add MCP server and skill commands under bb admin**: `bb admin` is about the Bitbucket instance, not local tooling.
- **Add MCP server and skill commands under bb auth**: auth manages credentials, not tooling distribution. Conflation would confuse both humans and agents.
