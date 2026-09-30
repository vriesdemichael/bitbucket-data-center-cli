---
search:
  boost: 0.3
---

# ADR-007: Manual GitHub release workflow

> Replaced by [ADR-033](033-automated-conventional-commit-release-on-main.md).

Use a manually triggered GitHub Actions release workflow for tagging, changelog generation, and publishing release artifacts. Do not auto-release on every push.

Design release automation around workflow_dispatch and commit-history-based changelog generation. Ensure release notes and versioning are derived from Conventional Commits. Keep normal development local-first and reserve CI release automation for explicit release actions.

Manual release triggering reduces accidental releases while preserving reproducibility. It also aligns with local-first development where contributors run checks locally but still need a reliable, repeatable release process with auditable artifacts.

## Not chosen

- **Fully automatic release on merge**: Higher risk of unintended publication and less operator control.
- **Entirely manual release steps without workflow automation**: Repetitive, error-prone, and harder to audit.
