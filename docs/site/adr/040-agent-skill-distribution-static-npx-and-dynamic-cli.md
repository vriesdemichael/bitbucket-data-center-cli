---
search:
  boost: 0.3
---

# ADR-040: Agent skill distribution through npx and the copy embedded in bb

The bb agent skill has one source, skills/bb/SKILL.md, and reaches agents two ways. `npx skills add vriesdemichael/bitbucket-data-center-cli` installs it from the repository, with no bb needed. bb embeds the same file with //go:embed: `bb ai skill show` prints it stamped with the binary's version, and `bb ai skill install` writes that to .agents/skills/bb/SKILL.md, which most agents read, and to .claude/skills/bb/SKILL.md, which Claude Code reads instead, under the working directory or, with --global, under the home directory. `bb ai skill remove` takes out what install wrote. What they write depends on the binary alone, and none of them makes a network call, so they work air-gapped and without the source tree. The help of `bb ai skill show` explains both channels, and that the npx copy is a snapshot of the repository and does not follow the installed bb.

When helping a user install or update the skill, prefer `bb ai skill install` when bb is present: its copy cannot be older than the installed CLI. Recommend npx for a first install, or where bb is not installed yet. The skill does not replace `bb --help` or `bb ai mcp tools` when accuracy matters. Keep the committed file free of placeholders: npx distributes it verbatim, so anything left unrendered reaches the reader raw. Append at render time instead, as buildSkill does with the version stamp. Do not restate the MCP tool catalogue in the skill; point at `bb ai mcp tools`, which derives the list, and which tools ask the person before they run, from mcp.AllSpecs().

A file in the repository reaches agents through the skills.sh ecosystem before bb is installed, which is where a new user starts. It is a snapshot, though, and an agent working with a newer bb needs the skill that bb was built with. Embedding the same file gives it that, with no file to find at runtime and no way for the two copies to differ at a release.

## Not chosen

- **Static repository file only; no CLI copy**: Becomes stale after every release. Agents operating on a newer bb may attempt commands or flags that the skill does not describe, or follow removed guidance.
- **CLI copy only; no npx packaging**: Requires bb to be installed before the skill can be obtained, and loses discoverability on skills.sh.
- **Read skills/bb/SKILL.md from the filesystem at runtime instead of embedding**: Fails in release builds where the source tree is absent, and in any environment where the working directory is not the repository root.
- **A bb ai skill install --target flag for per-agent path selection**: Per-agent path management belongs to the skills tooling. bb writes the two locations that cover the agents that read a project's skills.
