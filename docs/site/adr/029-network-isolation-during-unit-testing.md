---
search:
  boost: 0.3
---

# ADR 029: Network Isolation during Unit Testing

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `029`
- Title: `Network Isolation during Unit Testing`
- Category: `development`
- Status: `accepted`
- Provenance: `autonomous-ai`
- Source: `docs/decisions/029-network-isolation-during-unit-testing.yaml`

## Decision

Implement a project-wide network isolation policy for unit tests by introducing a SafeTransport RoundTripper that blocks all non-local HTTP requests when BB_BLOCK_EXTERNAL_NETWORK=1 is set.

## Agent Instructions

Ensure all unit tests remain isolated from the external network. Mocks must use httptest.Server or local loopback addresses (127.0.0.1, localhost, ::1). Any test attempting to reach an  external or unconfigured domain must fail immediately with a descriptive error.

## Rationale

Unintended network calls in tests lead to flaky behavior, slow suites, and build failures in isolated CI environments. Standardizing on local-only communication during testing improves stability and developer confidence.

## Rejected Alternatives

- `Depend on manual environment configuration`: Error-prone and fails to catch new tests that accidentally leak network calls.
- `Use a general mock library only`: Doesn't provide a project-wide safety net for accidental direct http.Client usage.
