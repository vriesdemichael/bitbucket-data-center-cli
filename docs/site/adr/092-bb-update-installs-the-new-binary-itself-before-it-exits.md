---
search:
  boost: 0.3
---

# ADR 092: bb update installs the new binary itself, before it exits

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `092`
- Title: `bb update installs the new binary itself, before it exits`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/092-bb-update-installs-the-new-binary-itself-before-it-exits.yaml`

## Decision

bb update puts the verified binary in place itself, before it exits, on every operating system, and reports the outcome in its own result or error. It replaces the file the binary's path names, following a symbolic link so that the link keeps starting bb. It writes the new binary into that file's directory, with its permissions, and syncs it to disk first. No backup is kept. On Linux and macOS it renames the new binary over the old one. The rename is atomic, a running bb keeps the file it started from, and a failure before it leaves the installed binary as it was. Windows renames a running executable but will not overwrite or delete one. There bb renames bb.exe aside, to a name no other file has, and the new binary into its place, waiting briefly while another process holds either file; if the second rename fails it renames the old one back, and if that fails too it reports both. Each bb run on Windows deletes what earlier updates left beside it, silently and in the background, and passes over a binary another bb still runs from.

## Agent Instructions

Finish an install inside bb update: never hand a step to another process, script or scheduled task. Keep the new binary in the target's directory, so the rename stays on one file system, and sync it before the rename. Report every failure, a failed rollback included; only the directory sync after the swap is best effort. On Windows retry a rename only while another process holds the file, and only briefly. The startup cleanup deletes only names an update gives, and never prints or waits.

## Rationale

A swap bb does not perform is one it cannot report. A helper that runs after bb exits leaves the command claiming a success it never observed, and whatever blocks the helper turns that claim false without a word. A rename is what each operating system allows on a running binary, and it is how common Go self-updaters replace one.

## Rejected Alternatives

- `A PowerShell helper that swaps the files after bb exits`: Application control such as AppLocker, WDAC or Constrained Language Mode blocks it while bb reports success, and nothing reads the outcome it records.
- `Keep the replaced binary as a backup`: The swap either completes or leaves the old binary in place, so there is nothing for a backup to restore; going back to an earlier release is a reinstall.
- `Swap through a copy of bb started as a helper`: It is still a second process, whose outcome bb has exited before it can report.
