# ADR 014: Output contract for human and machine modes

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `014`
- Title: `Output contract for human and machine modes`
- Category: `architecture`
- Status: `superseded`
- Superseded By: `064`
- Provenance: `guided-ai`
- Source: `docs/decisions/014-output-contract-for-human-and-machine-modes.yaml`

## Decision

Provide consistent human-readable output by default and a stable machine mode via --json. JSON responses must use versioned envelopes for forward-compatible parsing.

## Agent Instructions

Keep display formatting separated from domain/workflow logic. Ensure every data-returning command supports --json and stable field naming. Changes to JSON contracts require explicit versioning and migration notes.

## Rationale

The CLI must serve both interactive operators and automation scripts without ambiguity. Stable machine contracts reduce breaking changes and improve integration reliability.

## Rejected Alternatives

- `Human output only`: Not sufficient for automation and CI/local scripting workflows.
- `Unversioned JSON payloads`: Harder to evolve safely over time.
