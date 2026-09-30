---
search:
  boost: 0.3
---

# ADR-027: Atlassian 9.4 docs as API reference source

> Replaced by [ADR-068](068-api-reference-derived-from-the-harness-release.md).

Use Atlassian Bitbucket Data Center 9.4 REST documentation and its published OpenAPI artifact as the single external source of API reference for endpoint discovery, payload shape, and status-code expectations. Behavioral correctness is still determined by live server validation tests.

Use the version-pinned Atlassian 9.4 reference first when implementing or reviewing endpoints. Prefer the vendored OpenAPI artifact in docs/reference/atlassian for deterministic local access. Treat docs/spec as reference, not behavior truth; verify with live integration tests and encode mismatches as executable behavior tests.

A single reference source reduces ambiguity and endpoint drift across implementations. At the same time, Bitbucket documentation can be incomplete or inconsistent in practice, so live validation remains mandatory for behavior-level confidence.

## Not chosen

- **Use multiple unofficial sources for endpoint details**: Increases inconsistency and implementation ambiguity.
- **Treat documentation/spec as behavior authority**: Conflicts with observed real-server quirks and documented reliability concerns.
