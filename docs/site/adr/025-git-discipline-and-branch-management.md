---
search:
  boost: 0.3
---

# ADR 025: Git discipline and branch management

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `025`
- Title: `Git discipline and branch management`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/025-git-discipline-and-branch-management.yaml`

## Decision

Use strict git discipline: no direct commits to main, no merge or squash commits, conventional commits only, and rebase-based integration. History rewriting is permitted on PR branches but never on main.

## Agent Instructions

Never commit directly to main. Work on PR branches from latest origin/main. Do not use merge commits or squash merges; use rebase-based merges. Keep commit messages conventional and meaningful for changelog generation. You may amend/rebase/force-with-lease on PR branches to clean history. Rebase PR branches on latest main before merge.

## Rationale

Linear rebase-only history improves readability, bisectability, and release automation. Conventional commit discipline ensures changelog quality and predictable versioning behavior.
