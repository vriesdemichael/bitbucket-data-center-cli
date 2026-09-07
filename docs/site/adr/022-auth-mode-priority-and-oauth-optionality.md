# ADR 022: Auth mode priority and token-first policy

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `022`
- Title: `Auth mode priority and token-first policy`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/022-auth-mode-priority-and-oauth-optionality.yaml`

## Decision

Use a practical auth priority for runtime operations: CLI flags/env and stored credentials with token or basic auth are first-class and required for milestone delivery. OAuth is not supported in this CLI because Bitbucket Server/Data Center OAuth capability is inconsistent across target environments. Provide a first-class helper command that prints the host-specific personal access token (PAT) creation URL so users can quickly provision token-based auth.

## Agent Instructions

Implement and maintain token/basic auth as the default operational path. Do not add OAuth-dependent behavior. Keep onboarding optimized for token creation and login with --token-stdin. Document authentication mode in status output and troubleshooting guidance.

## Rationale

Bitbucket Server/Data Center OAuth setup is not consistently available in local or restricted environments and often requires extra administrative configuration that cannot be standardized for this CLI's deployment targets. Token/basic auth provides reliable behavior for live testing and day-to-day development.

## Rejected Alternatives

- `OAuth-only authentication`: Incompatible with many local/eval setups and increases onboarding friction.
- `Optional OAuth support in CLI`: Adds complexity and unstable flows without dependable cross-environment support.
