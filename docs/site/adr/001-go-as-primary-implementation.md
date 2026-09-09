---
search:
  boost: 0.3
---

# ADR 001: Go as primary implementation language

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `001`
- Title: `Go as primary implementation language`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/001-go-as-primary-implementation.yaml`

## Decision

Implement the CLI and service client in Go as the primary runtime target. Do not retain Python runtime code in this repository.

## Agent Instructions

For new implementation work, prefer Go packages and binaries over adding new Python runtime features. If parity with legacy behavior is needed, port behavior into Go and keep tests focused on server behavior.

## Rationale

The project requires simple distribution as a standalone binary for local and CI usage. Go provides static binaries, predictable runtime behavior, and low operational friction.

## Rejected Alternatives

- `Keep Python as primary implementation`: Python requires runtime environment management and packaging complexity for standalone distribution.
