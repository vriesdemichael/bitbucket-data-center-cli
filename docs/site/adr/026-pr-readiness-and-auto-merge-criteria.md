---
search:
  boost: 0.3
---

# ADR-026: PR readiness and auto-merge criteria

A pull request is opened when it is complete and reviewable: no partial implementation, no TODO, FIXME or debugging left in, `task quality:verify`, `task test:unit` and `task docs:validate` passing, and the live tests that cover what it changes run against the local stack. CI runs the rest.

A pull request into `next` that changes documentation, or anything a user or a caller of bb meets, waits for a maintainer to merge it. That covers the docs pages, the README and the decision records; commands, flags, arguments and help text; text and JSON output, exit codes and error messages; MCP tools, configuration keys and environment variables; and what the packages install. A pull request that mixes the two kinds is of this kind. Any other pull request into `next`, to tests, CI, the local stack or internal code with no visible effect, may merge itself by rebase auto-merge, which lands it once `CI Complete` passes. Resolve its review comments before turning auto-merge on.

A maintainer reviews what users see. Everything else lands on `next`, which releases nothing, and is looked at as a whole when `next` is promoted. Rebase auto-merge keeps the history linear and leaves the waiting to the required check.
