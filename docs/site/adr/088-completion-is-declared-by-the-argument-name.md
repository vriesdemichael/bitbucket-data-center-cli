---
search:
  boost: 0.3
---

# ADR 088: Shell completion is declared by the argument's name, and resolved by the command's own code

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `088`
- Title: `Shell completion is declared by the argument's name, and resolved by the command's own code`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/088-completion-is-declared-by-the-argument-name.yaml`

## Decision

What a slot accepts is declared by what it is called. A positional argument is what its placeholder in the Use line says -- <pr-id> is a pull request, <branch> is a branch that exists, <name> is one being created and completes nothing -- and a flag is what its name says, in every command that declares it. internal/cli/completion holds the two tables and a short list of exceptions for a name that means something else in one place. A command therefore gets completion by naming its arguments well. Nothing is registered at a definition site: one pass over the finished tree installs every completion, beside the passes that already name a missing argument and classify a dry run. Completion resolves the invocation with the code the command would run. Cobra answers a completion request through a hidden command of its own, so the root's PersistentPreRunE runs for that command and not for the one being completed: no global flags applied, no repository inferred. The resolution is split out of the hook and handed to the completion package as functions rather than copied, because a completion that resolved a different repository would offer `bb pr merge` a number that means another pull request in the repository the merge reaches. A tab press is bounded and silent. One deadline covers the keyring read, the git subprocesses and the request together; whatever has not finished is abandoned. A failure completes nothing and prints nothing, and does not fall back to file names -- the shell listing the working directory for an argument that wanted a branch is worse than an empty answer. A reason the caller can act on goes out as Active Help, which bash and zsh print and the other shells drop.

## Agent Instructions

Name a positional argument after what it accepts, using a placeholder the vocabulary in internal/cli/completion already knows. TestEveryCompletionSlotIsDeclared fails on one it does not, and names it. A value bb cannot list -- a title, a URL, the name of something being created -- is KindFree. Declare it; do not leave it out. Add a source by registering it from its own file. Do not add a completion function to a command, and do not read configuration, credentials or git context inside a source: ask the Environment, which resolves each of them once per press through the functions the command path uses. Never print to stdout from a source. Completion output is a protocol, and a stray line is a candidate.

## Rationale

The surface is too large to wire up by hand: 212 positional slots across 164 commands, and 372 flags that take a value. Anything requiring a call per command would be forgotten by the tenth, and the failure is silent -- an argument that completes nothing is indistinguishable from one whose source is not written yet. Keying on the name works because the tree already reads that way: --repo means a repository selector in all 37 places it is declared. It did not hold for positionals until the placeholders were made unambiguous: <id> stood for nine different resources and <key> for two, so `pr merge <id>` became `pr merge <pr-id>` and the help text says what it accepts as a side effect.

## Rejected Alternatives

- `Declare each slot at its definition site, with an annotation or a call`: A second list beside the Use line, which can disagree with it -- the failure internal/cli/args.go already avoids by building its message from the signature the command declares.
- `Let completion resolve the repository itself, since the hook does not run`: Two resolutions that must agree and no test that they do. The one that matters is the destructive case: a pull request number offered from a repository the command will not act on.
- `Print the reason a completion is empty`: Writes over the line being typed. Active Help is the shell-aware form of the same thing, and the shells that cannot show it ask Cobra to leave it out.
