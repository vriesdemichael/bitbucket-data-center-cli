---
search:
  boost: 0.3
---

# ADR 023: Long-term planning via GitHub issues

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `023`
- Title: `Long-term planning via GitHub issues`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/023-long-term-planning-via-github-issues.yaml`

## Decision

All planning that exceeds the scope of a single branch or agent session is tracked through GitHub issues. Issues are the source of truth for feature requests, multi-step epics, known bugs, and technical debt.

## Agent Instructions

For work spanning multiple sessions or branches, create or reference a GitHub issue. Use issue checklists or linked sub-issues for large efforts. Reference resolved issues in commit messages when appropriate (for example: closes #42). Do not keep long-term plans only in local notes or transient chat context. At the start of a session, review open issues to rehydrate project priorities.

## Rationale

Agent sessions are ephemeral and local scratch context is not a durable planning system. GitHub issues provide a shared, searchable, and reviewable project memory that supports coordination between humans and agents over time.

## Rejected Alternatives

- `Track long-term plans in local files only`: Poor visibility, weak collaboration, and high risk of plan drift across sessions.
- `Rely on conversational context for planning continuity`: Session context is temporary and not reliable as a project source of truth.
