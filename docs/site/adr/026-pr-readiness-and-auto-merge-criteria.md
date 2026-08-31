# ADR 026: PR readiness and auto-merge criteria

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `026`
- Title: `PR readiness and auto-merge criteria`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/026-pr-readiness-and-auto-merge-criteria.yaml`

## Decision

A PR is ready to open only when code is fully reviewable and local quality gates pass. A PR is ready for auto-merge when review feedback is addressed, required checks pass, and user approval/review is complete.

## Agent Instructions

Before opening a PR, run repository checks (task quality:verify and relevant live tests), ensure no partial implementations, and remove TODO/FIXME/debug leftovers. Ask user confirmation before opening a PR. Before enabling auto-merge, ensure comments are resolved and required checks are green. Use rebase auto-merge when appropriate.

## Rationale

Strict readiness criteria reduce review churn and prevent low-signal PR cycles. Rebase auto-merge preserves linear history while keeping review completion explicit.
