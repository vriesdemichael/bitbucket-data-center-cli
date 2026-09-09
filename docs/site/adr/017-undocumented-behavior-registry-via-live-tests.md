---
search:
  boost: 0.3
---

# ADR 017: Undocumented behavior registry via live tests

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `017`
- Title: `Undocumented behavior registry via live tests`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/017-undocumented-behavior-registry-via-live-tests.yaml`

## Decision

Treat undocumented or surprising Bitbucket behavior as first-class compatibility knowledge by encoding each finding as an explicit live test with a descriptive name and rationale.

## Agent Instructions

When discovering quirks, add or update a targeted live test and include a concise explanation in test naming or adjacent documentation. Do not rely on memory or ad-hoc notes for behavior exceptions.

## Rationale

Executable behavior knowledge prevents regressions and creates durable project memory. It is especially important for APIs with inconsistent documentation quality.

## Rejected Alternatives

- `Track quirks only in prose docs`: Not enforceable and prone to drift.
