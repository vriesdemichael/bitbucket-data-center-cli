---
search:
  boost: 0.3
---

# ADR 011: Error taxonomy and CLI exit contract

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `011`
- Title: `Error taxonomy and CLI exit contract`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/011-error-taxonomy-and-cli-exit-contract.yaml`

## Decision

Define a stable error taxonomy and map it to deterministic CLI exit codes and structured JSON error payloads. The kinds are authentication, authorization, validation, not_found, conflict, transient, permanent, not_implemented, cancelled and internal; internal is what an unclassified error becomes, so every error has a kind.

## Agent Instructions

Map transport and service errors into canonical categories before returning from workflows. Keep human output and JSON output consistent with the same underlying error classification. Avoid leaking raw upstream errors directly to users.

## Rationale

Deterministic error behavior is required for both scriptability and operator trust. A consistent taxonomy simplifies retries, diagnostics, and support workflows.

## Rejected Alternatives

- `Free-form error strings and ad-hoc exit codes`: Breaks machine consumption and makes behavior unpredictable.
