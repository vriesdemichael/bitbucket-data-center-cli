---
search:
  boost: 0.3
---

# ADR 034: Unified dry-run planning engine for server mutating commands

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `034`
- Title: `Unified dry-run planning engine for server mutating commands`
- Category: `architecture`
- Status: `accepted`
- Provenance: `human`
- Source: `docs/decisions/034-unified-dry-run-planning-engine.yaml`

## Decision

Use one planning engine for dry-run behavior across both single-command mutation flows and bulk workflows. Introduce a global --dry-run flag for server-mutating commands, defaulting to stateful planning for server-mutating command handlers, with static planning previews retained only as a compatibility and safety fallback. For bulk workflows, `bulk plan` remains the reviewed preview mechanism and `bulk apply` consumes reviewed plans rather than acting as its own dry-run surface.

## Agent Instructions

Implement dry-run behavior through shared planning abstractions rather than per-command ad-hoc flags. Keep dry-run scoped to server-mutating commands only. Ensure dry-run output explicitly reports planning mode and capability signaling for each operation path.

## Rationale

A single planning model reduces divergence between bulk and single-command mutation previews, improves operator trust, and keeps output behavior consistent across automation and interactive usage. Making stateful planning the primary implementation for server mutations improves preview quality, enables realistic no-side-effect validation against live Bitbucket state, and preserves a narrow static fallback for unsupported or future paths without redefining the main operator contract.

## Rejected Alternatives

- `Keep dry-run command-local and independent from bulk planning`: Creates semantic drift, duplicated logic, and inconsistent output contracts.
- `Static-only dry-run globally`: Misses opportunities to provide stronger preflight confidence where API/state checks exist.
