---
search:
  boost: 0.3
---

# ADR 084: A removed flag warns for one major before it stops working

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `084`
- Title: `A removed flag warns for one major before it stops working`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/084-a-removed-flag-warns-for-one-major.yaml`

## Decision

A flag, a command, a machine-output field, or an accepted value for any of them, keeps working until the next major release, and is removed in that major. The release that starts warning is what the removal major is counted from: a form deprecated anywhere in 4.x is removed in 5.0.0.
That is a shorter window the later in a major it starts, and a deprecation shipped in the last minor before a major gives almost none -- an adopter going from 4.0.0 to 5.0.0 never runs a binary that warns. A deprecation that lands that late waits for the major after, which is a judgement made when it is registered rather than a number derived from it.
Stderr, never stdout, so a `--json` consumer sees the warning without its document changing. The warning is emitted once per invocation rather than once per occurrence.
Only what a caller passes or invokes can be warned about. bb cannot tell that a consumer read a field, so a deprecated output field or value is announced in the release notes and in the schema (ADR-064) instead, and its removal waits the same major.
Two things are outside this. A change that closes a security hole removes the old form immediately, because a deprecation window on that is an attack window. And a form that never reached a release is not deprecated; it is deleted, since nobody can be depending on it.
Not every deprecation has a replacement. A form may be removed rather than superseded, and its warning then says what to do instead in prose. A deprecation with nothing to point at still names an alternative, because "deprecated" on its own leaves the reader nowhere to go.
Deprecations are registered in `internal/deprecation`, and the removal major is derived from the release that started warning rather than declared beside it. One list feeds the runtime warning, a CI annotation on `next`, and `release:promote:check`, so the three cannot disagree about what is outstanding. None of them fails a build: a breaking change can land on `next` weeks before the major ships, and a gate that went red at that moment would fail every unrelated pull request for the rest of the integration window, which is how a team learns to ignore a red build.

## Agent Instructions

When removing a flag, a command or a payload field, keep it working, register an Entry for it in `internal/deprecation` naming the release that starts warning and what to do instead, and emit the warning from the command -- in the same change that adds the replacement. Do not remove it in that change. The registry is what the runtime warning, the CI annotation and `release:promote:check` all read; a warning written beside it rather than from it is a deprecation the reports cannot see. A commit that only deprecates is a `feat`, not a breaking change: nothing that worked stops working. The removal one major later is the breaking commit. Do not add a deprecation for something that has never been released.

## Rationale

ADR-064 puts all compatibility on the major number, and SECURITY.md supports only the newest release, so an adopter on a change-approval cycle is either out of support or retesting continuously. A warning period converts "retest everything on every major" into "read the warnings", and costs neither an LTS branch nor a version matrix. CI is the case that decides it. A pipeline cannot read release notes, so a line on stderr is the only channel that reaches the consumer this project says it serves (ADR-003). Without one, a removed flag reaches them as an exit code and nothing else. One major is chosen over a time window because releases here are frequent and irregular; a duration would mean something different every time, and the major is already the number adopters act on.

## Rejected Alternatives

- `Keep removed forms working indefinitely`: The compatibility shim outlives the migration it was written for, and the CLI carries both behaviours forever. ADR-066 rejected the same idea for the same reason.
- `Announce removals in release notes only`: That is the current state. It reaches a human who reads the notes and nobody else, which excludes the pipelines and agents this project is built for.
