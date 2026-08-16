# ADR 030: Linear rebase workflow with generated coverage artifacts

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `030`
- Title: `Linear rebase workflow with generated coverage artifacts`
- Category: `development`
- Status: `superseded`
- Provenance: `guided-ai`
- Source: `docs/decisions/030-linear-rebase-workflow-with-generated-coverage-artifacts.yaml`

## Decision

Enforce linear PR history (no merge commits) and use a deterministic rebase flow when committed quality artifacts are generated from local test runs. During rebases, do not manually reconcile coverage artifact conflicts; complete the rebase, then regenerate docs/quality/coverage-report.json once and commit it as a final artifact refresh.

## Agent Instructions

Keep PR branches linear against origin/main. Use rebase, never merge main into PR branches. If coverage-report.json conflicts during rebase, avoid interactive/manual conflict edits for historical artifact commits; resolve to continue rebase, then run task pr:linearize (or task quality:coverage:report:update) and commit the refreshed report once at the end. Before push, verify linear history and coverage artifact checks pass. Prefer force-with-lease after branch history rewrites.

## Rationale

Rebase-only merge policies can reject branches that include merge commits even when checks are green. Generated coverage artifacts frequently conflict across rebases and are easy for lower-capability agents to mishandle interactively. A deterministic, scripted flow reduces failure modes, prevents repeated conflict churn, and keeps PRs consistently rebaseable.

## Rejected Alternatives

- `Allow merge commits on PR branches and rely on merge button policy`: Conflicts with the established rebase-only history discipline.
- `Keep resolving coverage artifacts manually during each rebase conflict`: Error-prone and difficult for lightweight models to execute reliably.
- `Stop committing coverage-report.json`: Conflicts with the existing quality contract that verifies committed artifacts.
