# ADR 040: Agent skill distribution via static npx packaging and dynamic CLI generation

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `040`
- Title: `Agent skill distribution via static npx packaging and dynamic CLI generation`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/040-agent-skill-distribution-static-npx-and-dynamic-cli.yaml`

## Decision

Distribute the bb agent skill through two complementary channels. First, package a baseline SKILL.md at skills/bb/SKILL.md in this repository so that npx skills add vriesdemichael/bitbucket-data-center-cli works out of the box via the open agent skills ecosystem at skills.sh. Second, expose bb ai skill show to generate a live, version-aware skill tailored to the installed bb binary and authenticated Bitbucket instance(s). A bb ai skill install command writes the live skill to the appropriate agent skills directory (.agents/skills/bb/SKILL.md for project scope, ~/.agents/skills/bb/SKILL.md for global scope via --global).
The base skill template is embedded directly in the bb binary at compile time using Go's //go:embed directive, pointing at the canonical skills/bb/SKILL.md source file. bb ai skill show reads from the embedded bytes, applies version and host substitutions, and writes the result to stdout. No filesystem access and no network connection are required at runtime. The npx-installable file and the embedded template are the same source file, so they are always identical at the moment of a release.
The multi-tenant callout in the generated skill (explaining --host usage when multiple Bitbucket instances are detected) is included only when bb auth server list returns more than one configured context.
The help text for bb ai skill show must explain both distribution channels, including the limitation that the npx-installed file is a snapshot from the repository release and will not reflect capabilities added after that release.

## Agent Instructions

When helping a user install or update the bb skill: - Prefer bb ai skill install for accuracy; it always reflects the current bb binary. - Recommend npx skills add vriesdemichael/bitbucket-data-center-cli for zero-friction first-time
  setup or when bb is not yet installed.
- The standard project-scoped path is .agents/skills/bb/SKILL.md, which is picked up by
  GitHub Copilot, Cursor, Codex, Cline, Amp, and most other agents. Agent-specific paths
  (e.g. .claude/skills/) take precedence if the agent supports them; consult bb ai skill show
  help text for guidance.
- The npx path and the CLI path are not mutually exclusive. Users may install via npx for
  discoverability and then override with bb ai skill show output when they need version parity.
- bb ai skill show and bb ai skill install never make network calls. They are safe to use in
  air-gapped or network-restricted environments.
- The embedded template lives at skills/bb/SKILL.md in the source tree. Edits to that file
  are picked up by the binary on the next build; no separate embedding step is needed.

## Rationale

A static file in the repository enables discovery and installation through the established skills.sh ecosystem without requiring bb to be installed. This is the right entry point for new users. A static file cannot capture dynamic information such as the installed bb version, the configured host URL, or whether the user has multiple Bitbucket instances. bb ai skill show fills this gap, ensuring the skill agents receive is accurate and actionable for their specific environment. The two-channel approach maximises reach while preserving correctness.
Embedding the template via //go:embed means bb ai skill show works in air-gapped environments, CI runners without outbound access, and developer machines where the source tree is not present. It also eliminates any risk of the binary looking for a file that has been moved, renamed, or is not present in a release artifact's working directory. Using the same source file for both the embedded template and the npx-installable file ensures they cannot diverge.

## Rejected Alternatives

- `Static repository file only; no CLI generation`: Becomes stale after every release. Agents operating on a newer bb version may attempt commands or flags that the skill does not describe, or follow removed guidance.
- `CLI generation only; no npx packaging`: Requires bb to be installed before the skill can be obtained. Loses discoverability on skills.sh and breaks the npx zero-friction install flow.
- `Read skills/bb/SKILL.md from the filesystem at runtime instead of embedding`: Fails in release builds where the source tree is absent, and in any environment where the working directory is not the repository root. Embedding is the correct Go idiom for shipping static assets in a binary.
- `bb ai skill install --target flag for per-agent path selection`: Per-agent path management belongs to the npx skills CLI, which already handles this across 40+ agents. Duplicating that logic in bb adds maintenance burden without benefit.
