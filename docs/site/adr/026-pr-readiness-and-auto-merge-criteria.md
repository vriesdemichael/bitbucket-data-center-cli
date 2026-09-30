---
search:
  boost: 0.3
---

# ADR-026: PR readiness and auto-merge criteria

A PR is ready to open only when code is fully reviewable and local quality gates pass. A PR is ready for auto-merge when review feedback is addressed, required checks pass, and user approval/review is complete.

Before opening a PR, run repository checks (task quality:verify and relevant live tests), ensure no partial implementations, and remove TODO/FIXME/debug leftovers. Ask user confirmation before opening a PR. Before enabling auto-merge, ensure comments are resolved and required checks are green. Use rebase auto-merge when appropriate.

Strict readiness criteria reduce review churn and prevent low-signal PR cycles. Rebase auto-merge preserves linear history while keeping review completion explicit.
