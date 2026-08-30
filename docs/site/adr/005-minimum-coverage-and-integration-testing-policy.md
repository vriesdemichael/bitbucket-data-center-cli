# ADR 005: Minimum coverage and integration testing policy

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `005`
- Title: `Minimum coverage and integration testing policy`
- Category: `development`
- Status: `superseded`
- Superseded By: `065`
- Provenance: `guided-ai`
- Source: `docs/decisions/005-minimum-coverage-and-integration-testing-policy.yaml`

## Decision

Maintain at least 85% global combined line coverage (unit + live) across the maintained source scope used by quality reporting (cmd/ + internal/ excluding generated code paths), and at least 85% combined patch coverage against an up-to-date origin/main baseline for coverable patch sets with at least 30 coverable lines in the maintained source scope. For smaller coverable patch sets (<30 coverable lines), allow up to 2 uncovered changed lines. Require integration tests for all behavior that interacts with Bitbucket APIs, authentication, permissions, pagination, or server-managed state. Contract tests are deferred until live tests reach maturity.

## Agent Instructions

Keep unit tests focused on fast local correctness and parsing/normalization behavior. Add live tests for endpoint semantics and side effects. Before evaluating patch coverage, fetch and compare against the latest origin/main. Evaluate patch coverage only for the maintained source scope (cmd/ + internal/, excluding generated paths). If global combined coverage drops below 85%, restore coverage before considering work complete. For coverable patch sets >=30 lines, require combined patch coverage >=85%. For coverable patch sets <30 lines, require uncovered changed lines <=2. If these patch constraints fail, restore coverage before considering work complete. Contract tests can be proposed only when live tests show stable low flake rates and broad endpoint coverage.

## Rationale

A combined global floor protects overall code health while a patch floor blocks low-coverage changes from merging. A small-patch uncovered-line allowance prevents denominator noise on tiny changes while preserving strictness for larger patches. Evaluating patch coverage against updated origin/main keeps the signal aligned with current trunk and avoids stale-base false confidence.

## Rejected Alternatives

- `No explicit coverage threshold`: Lacks a clear quality baseline and encourages uneven test discipline.
- `Unit tests only`: Insufficient for validating real Bitbucket behavior.
