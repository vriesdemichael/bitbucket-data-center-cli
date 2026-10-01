---
search:
  boost: 0.3
---

# ADR-091: A command may report the state it read through its exit status

`bb pr checks`, and `bb pr build status` beside it, report the builds they list through the exit status, as `gh pr checks` does: 1 when a build failed, 8 when none failed and one is in progress or has no result, and 0 otherwise, a cancelled build included. The status counts every build, not only the `--limit` shown. Text output only. Under `--json` or `--yaml` the command exits 0 and each build's state is in `data`, as gh's own `--json` does, so a non-zero exit in machine mode still always comes with an error (ADR-075). ADR-011's kinds say what happened to bb; this says what bb found. The command succeeded and its output is complete, so the status travels as `errors.StateExit`, a code and a line for stderr, which is neither a kind nor ever an error envelope. Exit 1 is shared with permanent; the stderr line tells a person which, and a caller that has to tell them apart uses `--json`. A dry run in text exits with the code the real run would by the same path (ADR-096).

Report the state a command read through its exit status only in text output, and only where the command gh or git offers people already gates that way. Return `errors.StateExit` for it, never an `AppError`, and keep machine output at exit 0 with the state in the document. Name the codes in the command's help.

`gh pr checks 42 || exit 1` is how scripts gate on CI, and `bb pr checks` is the spelling ADR-050 gives a gh user. Exiting 0 whatever the builds said would pass every pull request through such a gate. Taking gh's codes, 8 for pending included, makes the same line work against bb.

## Not chosen

- **An opt-in flag, as git diff --exit-code has**: gh gates without one, so a gh user's script would stay silently green.
- **Keep exit 0 and document the difference on the gh parity page**: The gate is what a gh user reaches for bb pr checks to do.
- **An error kind for a failed build**: A failed build is not bb failing, and a kind would put an error envelope where the builds belong in data.
