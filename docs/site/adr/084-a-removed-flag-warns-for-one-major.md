---
search:
  boost: 0.3
---

# ADR 084: A removed flag warns for one major before it stops working

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `084`
- Title: `A removed flag warns for one major before it stops working`
- Category: `architecture`
- Status: `proposed`
- Provenance: `guided-ai`
- Source: `docs/decisions/084-a-removed-flag-warns-for-one-major.yaml`

## Decision

A flag, a machine-output field, or an accepted value for either, keeps working for one major release after its replacement ships. Using it writes one line to stderr naming the replacement, and the command otherwise behaves as it did. It is removed in the major after that.
Stderr, never stdout, so a `--json` consumer sees the warning without its document changing. The warning is emitted once per invocation rather than once per occurrence.
Two things are outside this. A change that closes a security hole removes the old form immediately, because a deprecation window on that is an attack window. And a form that never reached a release is not deprecated; it is deleted, since nobody can be depending on it.

## Agent Instructions

When removing a flag or a payload field, keep it accepting input, mark it deprecated, and add the warning naming what replaces it -- in the same change that adds the replacement. Do not remove it in that change. A commit that only deprecates is a `feat`, not a breaking change: nothing that worked stops working. The removal one major later is the breaking commit. Do not add a deprecation for something that has never been released.

## Rationale

ADR-064 puts all compatibility on the major number, and SECURITY.md supports only the newest release, so an adopter on a change-approval cycle is either out of support or retesting continuously. A warning period converts "retest everything on every major" into "read the warnings", and costs neither an LTS branch nor a version matrix. CI is the case that decides it. A pipeline cannot read release notes, so a line on stderr is the only channel that reaches the consumer this project says it serves (ADR-003). Today a removed flag reaches them as an exit code and nothing else. One major is chosen over a time window because releases here are frequent and irregular; a duration would mean something different every time, and the major is already the number adopters act on.

## Rejected Alternatives

- `Keep removed forms working indefinitely`: The compatibility shim outlives the migration it was written for, and the CLI carries both behaviours forever. ADR-066 rejected the same idea for the same reason.
- `Announce removals in release notes only`: That is the current state. It reaches a human who reads the notes and nobody else, which excludes the pipelines and agents this project is built for.
