---
search:
  boost: 0.3
---

# ADR 037: Versioned docs via MkDocs Material and mike

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `037`
- Title: `Versioned docs via MkDocs Material and mike`
- Category: `development`
- Status: `accepted`
- Provenance: `human`
- Source: `docs/decisions/037-versioned-docs-via-mkdocs-material-and-mike.yaml`

## Decision

Adopt MkDocs with the Material theme as the documentation framework and use mike for versioned deployments to GitHub Pages. Build docs in CI and publish versioned docs from the release workflow.

## Agent Instructions

Keep docs source in a dedicated MkDocs docs directory and validate with strict builds. Use Taskfile tasks for docs build/serve/deploy workflows. In release automation, publish the computed release version and update a stable alias (for example `latest`). Prefer uv/uvx as the first-choice tooling for Python-based scripts and docs tasks; use docs/pyproject.toml dependency metadata rather than ad-hoc virtualenv setup.

## Rationale

MkDocs Material provides a modern interface with minimal maintenance overhead in a Python-light repository, and mike adds simple versioned docs semantics that align with release tags. This supports incremental docs delivery while keeping publication automated.

## Rejected Alternatives

- `Keep README-only documentation`: Not sufficient for scalable, navigable, versioned public docs.
- `Use a Node-based docs stack`: Adds avoidable ecosystem/tooling overhead for this repository.
