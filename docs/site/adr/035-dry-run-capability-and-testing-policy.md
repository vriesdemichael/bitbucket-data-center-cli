# ADR 035: Dry-run capability signaling and test strategy policy

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `035`
- Title: `Dry-run capability signaling and test strategy policy`
- Category: `development`
- Status: `superseded`
- Superseded By: `078`
- Provenance: `human`
- Source: `docs/decisions/035-dry-run-capability-and-testing-policy.yaml`

## Decision

Dry-run implementations must explicitly signal capability level and planning mode in output. Static planning paths are validated with unit tests, while stateful dry-run behavior is validated via live integration tests focused on no-side-effect guarantees, context-before/context-after verification, and realistic multi-step previews.

## Agent Instructions

For dry-run features, always include explicit capability signaling (for example full/partial) and planning mode in output payloads. Add focused unit tests for static preview behavior and focused live tests for stateful behavior and side-effect prevention. Avoid synthetic coverage-only tests; each test should prove a concrete safety or contract guarantee.

## Rationale

Explicit capability signaling prevents false confidence during rollout. Separating unit coverage (static logic) from live validation (stateful behavior) aligns with project testing policies and improves confidence that dry-run does not mutate server state. Requiring before/after state checks for live dry-run tests makes the safety contract concrete rather than implied.

## Rejected Alternatives

- `Omit capability signaling until full parity exists`: Hides maturity gaps and makes operator decisions less safe.
- `Use only unit tests for all dry-run behavior`: Cannot prove no server side effects for stateful/API-backed planning paths.
