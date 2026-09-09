---
search:
  boost: 0.3
---

# ADR 013: CLI framework and command tree standard

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `013`
- Title: `CLI framework and command tree standard`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/013-cli-framework-and-command-tree-standard.yaml`

## Decision

Implement a structured command tree with groups auth, repo, pr, issue, and admin, following a gh-style UX for discoverability and consistency.

## Agent Instructions

New commands must be added under one of the standard groups with shared flag semantics, predictable help text, and parity between human and JSON output modes. Avoid one-off top-level commands unless justified by a superseding decision.

## Rationale

A stable command taxonomy reduces cognitive load and supports long-term automation compatibility. It also aligns implementation with the project migration and usability goals.

## Rejected Alternatives

- `Flat command namespace`: Harder discovery and increased risk of naming and behavior inconsistency.
