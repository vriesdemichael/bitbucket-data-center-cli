---
search:
  boost: 0.3
---

# ADR 006: Conventional commits with hook enforcement

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `006`
- Title: `Conventional commits with hook enforcement`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/006-conventional-commits-with-hook-enforcement.yaml`

## Decision

Enforce Conventional Commits via local Git hooks as part of the default development workflow. Use lefthook as the hook runner and a commit message linter for commit-msg validation.

## Agent Instructions

Use Conventional Commit types for all commits (for example feat, fix, docs, refactor, test, chore). Prefer running quality checks through Taskfile tasks that are also wired into hooks. When adding new recurring checks, include them in hook configuration when execution time is appropriate.

## Rationale

Conventional commits make changelog generation and semantic versioning deterministic. Local hook enforcement keeps quality gates active even in local-first workflows without mandatory CI.

## Rejected Alternatives

- `Convention by policy only, no enforcement`: Drifts over time and weakens release automation reliability.
- `Husky-based hook management`: Node-centric and unnecessary for a Go-first project.
