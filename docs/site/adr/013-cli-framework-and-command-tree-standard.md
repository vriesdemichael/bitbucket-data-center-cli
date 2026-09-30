---
search:
  boost: 0.3
---

# ADR-013: CLI framework and command tree standard

Implement a structured command tree with groups auth, repo, pr, issue, and admin, following a gh-style UX for discoverability and consistency.

New commands must be added under one of the standard groups with shared flag semantics, predictable help text, and parity between human and JSON output modes. Avoid one-off top-level commands unless justified by a superseding decision.

A stable command taxonomy reduces cognitive load and supports long-term automation compatibility. It also aligns implementation with the project migration and usability goals.

## Not chosen

- **Flat command namespace**: Harder discovery and increased risk of naming and behavior inconsistency.
