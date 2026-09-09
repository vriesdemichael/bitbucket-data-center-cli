---
search:
  boost: 0.3
---

# ADR 002: Taskfile for development automation

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `002`
- Title: `Taskfile for development automation`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/002-taskfile-for-development-automation.yaml`

## Decision

Use Taskfile as the primary interface for developer workflows including local stack management, validation, and test orchestration.

## Agent Instructions

Prefer existing Taskfile tasks over ad-hoc shell commands. When adding recurring workflows, add or update a Taskfile task with clear naming and description.

## Rationale

Task provides a discoverable, cross-platform command interface with dependency support and clear namespacing for project workflows.

## Rejected Alternatives

- `Shell scripts only`: Harder to discover, compose, and standardize across contributors.
- `Makefile as primary interface`: Task offers clearer YAML syntax and easier workflow composition.
