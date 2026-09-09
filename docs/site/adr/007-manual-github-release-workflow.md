---
search:
  boost: 0.3
---

# ADR 007: Manual GitHub release workflow

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `007`
- Title: `Manual GitHub release workflow`
- Category: `development`
- Status: `superseded`
- Superseded By: `033`
- Provenance: `guided-ai`
- Source: `docs/decisions/007-manual-github-release-workflow.yaml`

## Decision

Use a manually triggered GitHub Actions release workflow for tagging, changelog generation, and publishing release artifacts. Do not auto-release on every push.

## Agent Instructions

Design release automation around workflow_dispatch and commit-history-based changelog generation. Ensure release notes and versioning are derived from Conventional Commits. Keep normal development local-first and reserve CI release automation for explicit release actions.

## Rationale

Manual release triggering reduces accidental releases while preserving reproducibility. It also aligns with local-first development where contributors run checks locally but still need a reliable, repeatable release process with auditable artifacts.

## Rejected Alternatives

- `Fully automatic release on merge`: Higher risk of unintended publication and less operator control.
- `Entirely manual release steps without workflow automation`: Repetitive, error-prone, and harder to audit.
