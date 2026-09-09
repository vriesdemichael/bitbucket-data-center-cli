---
search:
  boost: 0.3
---

# ADR 019: Configuration and secret handling policy

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `019`
- Title: `Configuration and secret handling policy`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/019-configuration-and-secret-handling-policy.yaml`

## Decision

Use typed configuration with strict validation and environment-backed secret inputs. Secrets must never be logged or returned in plain text and must be redacted in diagnostics.

## Agent Instructions

Validate configuration at startup and fail fast with actionable, non-secret error messages. Keep secret values out of logs, panic output, and CLI JSON payloads. Prefer environment variables or secure stores over checked-in configuration files.

## Rationale

Strong config validation and secret hygiene reduce operational incidents and data exposure risk. This policy aligns local-first development with production-grade safety expectations.

## Rejected Alternatives

- `Best-effort validation and permissive startup`: Delays failures and increases debugging complexity.
- `Allow plaintext secret echo for troubleshooting convenience`: Unacceptable security risk.
