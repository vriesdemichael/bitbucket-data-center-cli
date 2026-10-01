---
search:
  boost: 0.3
---

# ADR-072: Interactivity is decided in one place, and the escape hatch costs nothing per call

bb may prompt a person. `interactive.Detect` in internal/cli/interactive decides whether, and it is the only place that asks. Its rules, in precedence order, each able only to refuse:

1. `--no-input` was given, or machine-readable output was requested.
2. `BB_NO_PROMPT` is set.
3. A variable named in `BB_NO_PROMPT_VARS`, a comma-separated list, is set. This is the extension point: harnesses appear faster than releases, so an operator names the variable theirs sets rather than waiting.
4. A known non-interactive variable is set: `CI`, `DEBIAN_FRONTEND`, `NONINTERACTIVE`, or one a coding harness sets, from a best-effort list.
5. `TERM` is `dumb`. A `TERM` that is merely set proves nothing.
6. Standard input or standard output is not a terminal. Both must be, because output piped to something is not a person watching for a question.

A variable set to empty, 0 or false does not count, so `CI=false` does not silence prompting for the one person who said otherwise. A refusal carries its reason. Terminal checks live only in the helper: `TestOnlyTheSharedHelperDecidesInteractivity` in tools/gateparity fails on a new call site. clone.go is allowed one, to turn echo off while a token is typed, which is a question about how to read rather than whether to ask.

Call `interactive.Detect`. Never test isatty in a command, and never add an environment check beside a prompt. Put the refusal's reason in the error, so the caller learns which fix applies. A new rule may only refuse; nothing may re-enable prompting that an earlier rule turned off. To silence prompting for a harness bb does not know, set `BB_NO_PROMPT_VARS` rather than adding to the built-in list; add to the list once a harness is common enough to be worth a release.

Terminal attachment is not evidence that anyone will answer. A prompt under a pseudo-terminal nobody writes to blocks until it is killed, whether its input is at EOF or held open, while the same prompt under a harness that allocates no terminal returns an empty answer at once. isatty is true in the case that hangs and false in the one that does not, so as a safety signal it is not merely insufficient but anti-correlated. What decides is whether anything will ever write to stdin, which cannot be observed when the question is asked. So the escape hatch is part of the design rather than a fallback, and it is an environment variable because whoever runs the harness sets it once, where a flag costs tokens on every agent call and fails hard the one time it is forgotten. A harness that allocates a terminal, sets no variable and does not bound its commands still hangs until it gives up. That is accepted: hosted CI and the known coding harnesses are refused by the rules, and the variable fixes any other for good. This guarantees only that bb does not prompt when a rule refuses, not that it never blocks on input; reading stdin unasked is ADR-073's to forbid.

## Not chosen

- **Never prompt at all**: Answers the hang by removing the feature. What fails is inferring interactivity from isatty alone, not prompting.
- **Require --interactive to opt in**: Nobody discovers a flag they have to know about first, and it inverts the cost onto the person the feature is for.
- **Detect harnesses by process ancestry**: Several announce nothing at all, and a missed harness is a hang, so the failure is one-sided.
- **Put a timeout on the read**: Converts a hang into a stall, races a person mid-answer, and leaves the terminal in raw mode if aborted under a password read.
