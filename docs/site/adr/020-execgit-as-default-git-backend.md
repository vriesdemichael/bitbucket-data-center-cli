# ADR 020: Execgit as default Git backend

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `020`
- Title: `Execgit as default Git backend`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/020-execgit-as-default-git-backend.yaml`

## Decision

Use execgit (wrapping the system git binary) as the default Git backend for repository operations. Keep the Git backend abstraction in place so alternative implementations can be added later, but they are not the default path.

## Agent Instructions

Implement Git workflows against the backend interface but route default behavior through execgit. Focus on robust command execution (cwd/env handling, quoting, timeouts, stdout/stderr capture) and deterministic error classification. Treat any non-exec backend as opt-in and non-default unless a superseding decision changes this.

## Rationale

Execgit provides the highest behavior fidelity with upstream Git and avoids feature/compatibility gaps commonly seen in pure library implementations. This aligns with the project's reliability goals and reduces risk of subtle Git semantic drift.

## Rejected Alternatives

- `Go-native Git library as default backend`: Higher risk of parity gaps and edge-case incompatibilities for enterprise workflows.
- `No backend abstraction`: Reduces flexibility and makes future migration/testing strategies harder.
