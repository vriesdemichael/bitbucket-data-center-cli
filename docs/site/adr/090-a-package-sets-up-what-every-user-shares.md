---
search:
  boost: 0.3
---

# ADR 090: A package sets up what every user shares, and bb sets up what is one user's

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `090`
- Title: `A package sets up what every user shares, and bb sets up what is one user's`
- Category: `development`
- Status: `accepted`
- Amends: `40`
- Provenance: `guided-ai`
- Source: `docs/decisions/090-a-package-sets-up-what-every-user-shares.yaml`

## Decision

A package installs what reaches every user of the machine as files it owns: the bash, zsh and fish completion scripts, in the directories each shell reads for everyone. What lives in one user's files -- a PowerShell profile, a .zshrc, ~/.agents/skills -- is written by bb: bb completion install and bb ai skill install, each idempotent, each with a remove that takes out exactly what it wrote. A package that runs as its user after installing, which is Scoop's default, runs those commands and runs the removes before it uninstalls. A package that installs for every user, or can run nothing, names the commands in its install message instead. bb completion install --all-users writes where a shell reads for every user, where the shell has such a place, and refuses with the reason where it does not. The flag is not --global, which on bb ai skill install means every workspace of one user.

## Agent Instructions

Do not make a package write into a user's home or edit a file the user owns; have it run or name the bb command that does. Add a shell or an operating system to bb completion install by asking the shell where it reads, as it asks PowerShell for its profile and zsh for its fpath. Where there is no such place, refuse and say so. What bb writes for a shell stays a loader that runs `bb completion <shell>`, so it follows upgrades, and checks bb is there first, so a shell starts quietly once bb is gone.

## Rationale

A package can own a file in a system directory; it cannot own a line in somebody's profile, and a machine-wide install that edits a profile edits whichever user ran it. Scoop is the exception because it installs per user and runs its hooks as that user. No location reaches every user's agents. Claude Code, Codex and Devin each read a machine-wide skills directory of their own, and Claude Code's is its organization policy directory, whose skills override every user's own copy.

## Rejected Alternatives

- `Install the skill into each agent's machine-wide directory from the .deb and .rpm`: Three agent-specific paths to track, one of them a policy directory. ADR-040 leaves per-agent paths to the skills tooling.
- `Call the all-users flag --global`: bb ai skill install --global already means every workspace of one user.
