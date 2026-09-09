---
search:
  boost: 0.3
---

# ADR 015: Live test harness and deterministic seeding

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `015`
- Title: `Live test harness and deterministic seeding`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/015-live-test-harness-and-deterministic-seeding.yaml`

## Decision

Build a deterministic live integration harness using the local Bitbucket stack, seeded baseline entities, unique per-test namespaces where feasible, and repeatable cleanup/reset workflows.

## Agent Instructions

Live tests must avoid hidden ordering dependencies. Prefer unique test resources and explicit teardown. When adding new domains, extend seeding/reset logic to keep tests reproducible.

## Rationale

Deterministic setup is necessary for reliable live behavior validation and low maintenance overhead. It reduces flaky failures caused by shared mutable state.

## Rejected Alternatives

- `Shared long-lived mutable test state`: Increases flakiness and debugging cost.
