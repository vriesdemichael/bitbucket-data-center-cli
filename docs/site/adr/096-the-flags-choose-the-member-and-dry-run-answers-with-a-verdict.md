---
search:
  boost: 0.3
---

# ADR-096: The flags choose a document's member, and --dry-run answers with a verdict

> Changes part of [ADR-046](046-json-error-envelope-on-the-failure-path.md), [ADR-064](064-machine-output-carries-no-contract-version.md), [ADR-070](070-every-command-is-explicitly-classified-for-dry-run.md), [ADR-075](075-a-machine-run-emits-one-document.md), [ADR-078](078-dry-run-confidence-is-derived-from-a-tier.md), [ADR-084](084-a-removed-flag-warns-for-one-major.md), [ADR-091](091-a-command-may-report-the-state-it-read-through-its-exit-status.md).

A document holds meta and exactly one of four members, chosen by the flags and never by what happens in the run: data or error for a run, preview under --dry-run, and description under --describe (ADR-097). meta.command names the command that wrote it by its canonical path, when one resolved. A preview is a verdict on the real run. It holds a tier, the weakest of its checks, in place of a confidence label (ADR-078); the effects, each an action, a target, an outcome of would-apply, no-op or would-fail, and the reasons for it; and then either data, for a command that only reads and so runs for real, or the error the real run would fail with. An invalid invocation, a 404 and a veto are verdicts. A top-level error under --dry-run means there is none: transient, cancelled or internal. In text, a dry run prints its verdict and reasons to stdout and exits with the code the real run would. Under --json or --yaml only a top-level error exits non-zero: a dry run with a verdict, and a description, exit 0. Each command's --dry-run help line says what the flag does for it, and bb help dry-run explains the outcomes, tiers and exit codes. bb ai mcp serve does not take --dry-run, so no command refuses it. The preview and the description take these shapes in 5.0.0 with no warning period, as an exception to ADR-084: a document cannot carry an old shape beside a new one.

Choose the member from the flags, never from the outcome. Put everything the check learns about the real run in the preview, and return a top-level error under --dry-run only for transient, cancelled and internal. Give every effect its reasons, and print them in text as well. Generate each --dry-run help line from the command's classification and tier.

The key alone tells a caller what it holds, and the schema of data is true of every document that has data. A dry run is a gate: bb pr merge 42 --dry-run && bb pr merge 42 has to stop where the merge would fail. Under --json the exit code says what happened to bb and the document says what bb found, as ADR-091 does for bb pr checks.

## Not chosen

- **Keep the preview in data and add meta.mode**: data would keep three shapes, told apart only by a field beside it.
- **Exit with the predicted code under --json as well**: A document without a top-level error would exit non-zero, which ADR-075 rejected.
- **Warn about the old preview for a major first**: A document cannot hold the old and the new preview without the ambiguity this removes.
