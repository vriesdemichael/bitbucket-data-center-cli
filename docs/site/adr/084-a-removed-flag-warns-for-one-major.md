---
search:
  boost: 0.3
---

# ADR-084: A removed flag warns for one major before it stops working

A flag, a command, a machine-output field, or an accepted value for any of them keeps working until the next major release, and is removed in that major. The removal major is counted from the release that starts warning: a form deprecated anywhere in 4.x is removed in 5.0.0. That is a shorter window the later in a major it starts, and a deprecation shipped in the last minor before a major gives almost none: an adopter going from 4.0.0 to 5.0.0 never runs a binary that warns. A deprecation that lands that late waits for the major after, which is a judgement made when it is registered rather than a number derived from it.

The warning goes to stderr, never stdout, so a `--json` consumer sees it without its document changing, and it is emitted once per invocation rather than once per occurrence. Only what a caller passes or invokes can be warned about. bb cannot tell that a consumer read a field, so a deprecated output field or value is announced in the release notes and in its schema description instead, and its removal waits the same major. Not every deprecation has a replacement; a form may be removed rather than superseded, and its warning then says in prose what to do instead, because "deprecated" on its own leaves the reader nowhere to go.

Three things are outside this. A change that closes a security hole removes the old form at once, because a deprecation window on that is an attack window. A form that never reached a release is not deprecated but deleted, since nobody can be depending on it. And a change to a document's shape that cannot carry the old form beside the new one, as with the dry-run preview (ADR-096) and the `--describe` description (ADR-097), lands in a major with no warning period.

Deprecations are registered in `internal/deprecation`, and the removal major is derived from the release that started warning rather than declared beside it. One list feeds the runtime warning, a CI annotation on `next`, and `release:promote:check`, so the three cannot disagree about what is outstanding. None of them fails a build: a breaking change can land on `next` weeks before the major ships, and a gate that went red then would fail every unrelated pull request for the rest of the integration window, which is how a team learns to ignore a red build.

When removing a flag, a command or a payload field, keep it working, register an `Entry` for it in `internal/deprecation` naming the release that starts warning and what to do instead, and emit the warning from the command, in the same change that adds the replacement. Do not remove it in that change. The registry is what the runtime warning, the CI annotation and `release:promote:check` all read; a warning written beside it rather than from it is a deprecation the reports cannot see. A commit that only deprecates is a `feat`, not a breaking change: nothing that worked stops working. The removal one major later is the breaking commit. Do not add a deprecation for something that has never been released.

ADR-064 puts all compatibility on the major number, and SECURITY.md supports only the newest release, so an adopter on a change-approval cycle is either out of support or retesting continuously. A warning period turns "retest everything on every major" into "read the warnings", and costs neither an LTS branch nor a version matrix. CI is the case that decides it. A pipeline cannot read release notes, so a line on stderr is the only channel that reaches the consumer this project says it serves (ADR-003); without one, a removed flag reaches it as an exit code and nothing else. One major is chosen over a time window because releases here are frequent and irregular; a duration would mean something different every time, and the major is already the number adopters act on.

## Not chosen

- **Keep removed forms working indefinitely**: The compatibility shim outlives the migration it was written for, and the CLI carries both behaviours forever. ADR-066 rejected the same idea for the same reason.
- **Announce removals in release notes only**: Reaches a human who reads the notes and nobody else, which excludes the pipelines and agents this project is built for.
- **A warning period for a document's shape**: A document cannot hold the old shape and the new one without the ambiguity the change removes.
