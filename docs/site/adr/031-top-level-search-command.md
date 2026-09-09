---
search:
  boost: 0.3
---

# ADR 031: Top-level search command tree for discovery

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `031`
- Title: `Top-level search command tree for discovery`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/031-top-level-search-command.yaml`

## Decision

Introduce a top-level `search` command group (`bb search ...`) with subcommands for `repos`, `commits`, and `prs`. This acts as an exception to ADR 013 (which banned one-off top-level commands) to explicitly allow `search` as a first-class discovery primitive.

## Agent Instructions

Place global or semi-global discovery functionalities under `bb search <resource>`. Map these commands to existing API listing endpoints utilizing query filters. Preserve pagination (`--limit`, `--start`) and consistent output contracts.

## Rationale

Users need discoverability primitives for automation and triage. While resource-scoped lists (e.g. `bb repo list --name foo`) are technically correct, a top-level `search` command provides better discoverability, aligns with `gh search` UX expectations, and handles cross-project discovery (like dashboard PRs) more naturally than a resource-bound command tree.
