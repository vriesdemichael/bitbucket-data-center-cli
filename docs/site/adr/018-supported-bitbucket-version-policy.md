---
search:
  boost: 0.3
---

# ADR-018: Supported Bitbucket version policy

> Replaced by [ADR-042](042-track-newest-containerisable-bitbucket-version.md).

Support Atlassian Bitbucket 9.4.16 as the primary compatibility target initially. Additional versions may be introduced through explicit decision updates and expanded live test coverage.

Assume 9.4.16 behavior as baseline unless a decision explicitly extends support. Version-specific handling must be documented and validated with live tests.

Narrowing the initial compatibility surface enables faster delivery and stronger correctness. Controlled expansion avoids accidental multi-version support with unverified behavior.

## Not chosen

- **Unbounded multi-version support from day one**: Too broad for reliable behavior validation in early phases.
