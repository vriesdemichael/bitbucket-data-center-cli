# ADR 012: Git backend abstraction for repository operations

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `012`
- Title: `Git backend abstraction for repository operations`
- Category: `architecture`
- Status: `accepted`
- Amended By: `020`
- Provenance: `guided-ai`
- Source: `docs/decisions/012-git-backend-abstraction-for-repository-operations.yaml`

## Decision

Define a Git backend interface for repository operations and keep the implementation pluggable so the project can support both programmatic Go-native backends and shell-based git backends.

## Agent Instructions

Code against the Git backend interface from workflows. Keep backend-specific behavior isolated behind adapter packages. Add compatibility tests to ensure equivalent behavior across backends where supported.

## Rationale

A pluggable backend avoids lock-in and lets the project balance native integration, feature completeness, and behavior parity with standard git.

## Rejected Alternatives

- `Hard dependency on wrapping the git binary everywhere`: Reduces portability and testability of git behavior.
- `Hard dependency on one Go-native implementation`: Risks missing edge-case compatibility required by workflows.
