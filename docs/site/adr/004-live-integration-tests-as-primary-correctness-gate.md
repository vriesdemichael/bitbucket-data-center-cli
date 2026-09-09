---
search:
  boost: 0.3
---

# ADR 004: Live integration tests as primary correctness gate

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `004`
- Title: `Live integration tests as primary correctness gate`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/004-live-integration-tests-as-primary-correctness-gate.yaml`

## Decision

Use real Bitbucket Server/Data Center integration tests as the primary source of API behavior truth. Do not introduce contract/fixture-based behavior tests initially.

## Agent Instructions

When implementing any behavior beyond simple parsing or local validation, add or update live tests against the local Bitbucket stack. Treat documentation as advisory and server behavior as authoritative. Contract tests may be introduced later only after the live suite is mature and stable.

## Rationale

Bitbucket API documentation is frequently incomplete or inaccurate in edge cases. Live tests catch undocumented behavior and permission nuances that contract tests alone can miss. Starting live-first reduces false confidence from mocked assumptions.

## Rejected Alternatives

- `Contract tests from day one`: Early contracts tend to encode assumptions before behavior is well understood.
- `Documentation-only implementation without live verification`: Too risky due to known doc gaps and inconsistencies.
