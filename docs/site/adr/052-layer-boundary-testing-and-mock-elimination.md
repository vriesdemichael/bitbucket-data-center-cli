# ADR 052: Layer-boundary testing policy and HTTP mock elimination

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `052`
- Title: `Layer-boundary testing policy and HTTP mock elimination`
- Category: `development`
- Status: `accepted`
- Provenance: `human-ai-alignment`
- Source: `docs/decisions/052-layer-boundary-testing-and-mock-elimination.yaml`

## Decision

Define strict testing boundaries per layer: Unit tests must only test fast, deterministic logic (parameter translation, CLI flag mutual exclusion, syntax parsing, output envelopes, error taxonomy mapping, and dry-run prediction state machines). Eliminate the class of hand-rolled httptest.Server mock tests that simulate happy-path Bitbucket API responses merely to echo canned payloads. Live integration tests against the SDK-licensed Bitbucket container stack are the sole source of truth for Bitbucket REST API contracts, Git wire transports (HTTP/SSH), permission checks, auto-merge workflows, and real server behavior.

## Agent Instructions

Do not write mock HTTP servers that pretend to be Bitbucket Server in unit tests for happy paths. Unit tests in internal/cli/cmd/ and internal/services/ should verify flag validation, required arguments, conflicting options, formatting (--json vs human), and domain error mapping. When a feature needs verification against the Bitbucket REST API or Git transport, add a live test in tests/integration/live/. Never pad coverage with mock tests that assert tautological return values from hardcoded mock handlers.

## Rationale

AI-generated test suites frequently introduce mock mirroring: spinning up an in-process HTTP mock, returning canned JSON, and asserting that the function returned the struct matching that JSON. This produces nominal 85%+ coverage while providing zero real assurance of Bitbucket compatibility, and creates immense refactoring drag when internal structures evolve. Enforcing layer boundaries eliminates test bloat, speeds up test execution, and ensures test efforts focus on genuine behavioral guarantees.

## Rejected Alternatives

- `Rely exclusively on mock-heavy unit tests to satisfy coverage metrics`: Mock mirroring tests implementation details rather than real API behavior and introduces high maintenance friction on refactoring without catching real Bitbucket bugs.
- `Move all testing exclusively into live integration tests`: Running all combinatorial flag validation in live tests would dramatically slow down the live test suite (which already takes ~8 minutes) and degrade local developer feedback loops.
