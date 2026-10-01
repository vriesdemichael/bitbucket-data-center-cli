---
search:
  boost: 0.3
---

# ADR-096: The flags choose a document's member, and --dry-run answers with a verdict

A document holds `meta` and exactly one of four members, chosen by the flags and never by what happens in the run: `data` or `error` for a run, `preview` under `--dry-run`, and `description` under `--describe` (ADR-097). `meta.command` names the command that wrote it by its canonical path, when one resolved.

A preview is a verdict on the real run. It holds a tier (ADR-078); the effects, each an action, a target, an outcome of would-apply, no-op or would-fail, and the reasons for it; and then either `data`, for a command that only reads and so runs for real, or the `error` the real run would fail with. An invalid invocation, a 404 and a veto are verdicts. A top-level `error` under `--dry-run` means no verdict was reached: transient, cancelled, unknown_outcome or internal. In text, a dry run prints its verdict and reasons to stdout and exits with the code the real run would. Under `--json` or `--yaml` a dry run that reached a verdict exits 0, since the verdict is in the document. Each command's help says under "Dry run" what the flag does for it, and `bb help dry-run` explains the outcomes, tiers and exit codes.

Choose the member from the flags, never from the outcome. Put everything the check learns about the real run in the preview, and return a top-level error under `--dry-run` only when no verdict was reached. Give every effect its reasons, and print them in text as well. Generate each command's dry-run help line from its classification and declared tier.

The key alone tells a caller what it holds, and the schema of `data` is true of every document that has `data`. A dry run is a gate: `bb pr merge 42 --dry-run && bb pr merge 42` has to stop where the merge would fail. Under `--json` the exit code says what happened to bb and the document says what bb found, as ADR-091 does for `bb pr checks`.

## Not chosen

- **Keep the preview in data and add meta.mode**: data would keep three shapes, told apart only by a field beside it.
- **Exit with the predicted code under --json as well**: A document without a top-level error would exit non-zero, which ADR-075 rules out.
