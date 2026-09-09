---
search:
  boost: 0.3
---

# ADR 032: Refactor CLI root into command packages

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `032`
- Title: `Refactor CLI root into command packages`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/032-cli-root-modularization-into-command-packages.yaml`

## Decision

Refactor the monolithic CLI composition in internal/cli/root.go into focused command packages under internal/cli/cmd/<domain>, keeping root.go responsible only for global flags, shared runtime wiring, and top-level command registration. Preserve command names, help text, flags, output contracts, and exit behavior.

## Agent Instructions

New and migrated command handlers should live in domain-focused packages (for example auth, repo, diff, tag, build, insights, pr, issue, admin) and expose constructors consumed by root wiring. Keep shared output and selector helpers in dedicated shared packages rather than duplicating logic. During migration, maintain backward-compatible UX and update tests to guard help/flag parity and command behavior equivalence.

## Rationale

Splitting root command construction by domain lowers cognitive load, improves reviewability, and reduces cross-feature regression risk while preserving user-facing stability.

## Rejected Alternatives

- `Keep all command construction in one root.go file`: Increases maintenance cost and makes architectural boundaries harder to enforce.
