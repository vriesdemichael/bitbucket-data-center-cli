---
search:
  boost: 0.3
---

# ADR 021: Persistent auth configuration and keyring storage

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `021`
- Title: `Persistent auth configuration and keyring storage`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/021-persistent-auth-configuration-and-keyring-storage.yaml`

## Decision

Implement first-class authentication workflows with persistent host configuration. Store non-secret host/profile metadata in a user config file and store secrets in OS keyring. Environment variables and command flags override stored configuration at runtime.

## Agent Instructions

Prefer auth commands over manual environment setup for day-to-day usage. Keep precedence deterministic: flags -> environment -> stored config -> defaults. Never print secrets in command output or errors. If keyring is unavailable, use explicit fallback handling and communicate reduced security.

## Rationale

This model mirrors proven CLI practices used by tools like gh and improves usability, security, and predictability compared with .env-only workflows.

## Rejected Alternatives

- `Environment variables only`: Fragile across shells/sessions and poor multi-host ergonomics.
- `Plaintext secrets only in config files`: Increases secret exposure risk.
