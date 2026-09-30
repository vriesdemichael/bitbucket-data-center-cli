---
search:
  boost: 0.3
---

# ADR-012: Git backend abstraction for repository operations

> Changed in part by [ADR-020](020-execgit-as-default-git-backend.md).

Define a Git backend interface for repository operations and keep the implementation pluggable so the project can support both programmatic Go-native backends and shell-based git backends.

Code against the Git backend interface from workflows. Keep backend-specific behavior isolated behind adapter packages. Add compatibility tests to ensure equivalent behavior across backends where supported.

A pluggable backend avoids lock-in and lets the project balance native integration, feature completeness, and behavior parity with standard git.

## Not chosen

- **Hard dependency on wrapping the git binary everywhere**: Reduces portability and testability of git behavior.
- **Hard dependency on one Go-native implementation**: Risks missing edge-case compatibility required by workflows.
