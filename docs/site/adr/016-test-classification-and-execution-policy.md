---
search:
  boost: 0.3
---

# ADR 016: Test classification and execution policy

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `016`
- Title: `Test classification and execution policy`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/016-test-classification-and-execution-policy.yaml`

## Decision

Maintain unit and live integration suites as the default test layers. Contract tests are intentionally out of scope for now and may be added only after live-test maturity criteria are met and documented.

## Agent Instructions

Tag and organize tests by execution profile (fast unit vs live integration). Keep quick local checks free of live dependencies by default. Proposals for contract tests must include maturity evidence and migration rationale.

## Rationale

This policy optimizes for real behavior correctness first while preserving developer velocity. It prevents premature abstraction around contracts before behavior is fully characterized.

## Rejected Alternatives

- `Introduce contract tests immediately`: Premature while live behavior knowledge is still evolving.
