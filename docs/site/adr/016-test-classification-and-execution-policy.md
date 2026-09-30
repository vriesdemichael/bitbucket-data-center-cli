---
search:
  boost: 0.3
---

# ADR-016: Test classification and execution policy

Maintain unit and live integration suites as the default test layers. Contract tests are intentionally out of scope for now and may be added only after live-test maturity criteria are met and documented.

Tag and organize tests by execution profile (fast unit vs live integration). Keep quick local checks free of live dependencies by default. Proposals for contract tests must include maturity evidence and migration rationale.

This policy optimizes for real behavior correctness first while preserving developer velocity. It prevents premature abstraction around contracts before behavior is fully characterized.

## Not chosen

- **Introduce contract tests immediately**: Premature while live behavior knowledge is still evolving.
