# ADR 072: Interactivity is decided in one place, and the escape hatch costs nothing per call

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `072`
- Title: `Interactivity is decided in one place, and the escape hatch costs nothing per call`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/072-interactivity-is-decided-in-one-place.yaml`

## Decision

bb may prompt a person. internal/cli/interactive.Detect decides whether, and it is the only place that asks. Rules, in precedence order, each able only to refuse:
   1. --no-input, or machine-readable output was requested.
   2. BB_NO_PROMPT is set.
   3. A variable named in BB_NO_PROMPT_VARS is set. This is the extension point: harnesses appear
      faster than releases, so an operator names the variable theirs sets rather than waiting.
   4. A known non-interactive variable is set: CI, DEBIAN_FRONTEND, NONINTERACTIVE, or a coding
      harness from a best-effort list.
   5. TERM is dumb. A TERM that is merely set proves nothing.
   6. Standard input and standard output are both terminals. Both, because output piped to
      something is not a person watching for a question.
A variable set to empty, 0 or false does not count, so CI=false does not silence prompting for the one person who said otherwise. Terminal checks live only in the helper. TestOnlyTheSharedHelperDecidesInteractivity fails on a new call site; clone.go is allowed one, to suppress echo while a token is typed, which is a question about how to read rather than whether to ask.

## Agent Instructions

Call interactive.Detect. Never test isatty in a command, and never add an environment check beside a prompt. A refusal carries a reason. Put it in the error so the caller learns which fix applies. A new rule may only refuse. Nothing may re-enable prompting that an earlier rule turned off. To silence prompting for a harness bb does not know, set BB_NO_PROMPT_VARS rather than adding to the built-in list; add to the list only once a harness is common enough to be worth a release.

## Rationale

Terminal attachment is not evidence that anyone will answer. Measured: a prompt under a real pseudo-terminal blocked until killed, whether its input side was at EOF or held open, while the same prompt under a harness that allocates no terminal returned an empty string in two milliseconds. isatty was true in the cases that hung and false in the case that did not, so as a safety signal it is not merely insufficient -- it is anti-correlated. What actually decides is whether anything will ever write to stdin, which is not observable at the point of the check. So the escape hatch is part of the design rather than a fallback, and it is an environment variable because that is set once by whoever runs the harness: a flag costs tokens on every agent call and fails hard the one time it is forgotten. The residual exposure is a harness that allocates a terminal, sets no variable, and does not bound its commands. It hangs there until that harness gives up. That is accepted: the environments this project actually serves -- hosted CI and the coding harnesses above -- are all refused correctly by the rules, and the variable fixes any other permanently. What this does not guarantee: that bb never blocks on input. It guarantees only that bb does not prompt when any rule refuses. Reading stdin without being asked to is a separate matter, and ADR-054 governs it.

## Rejected Alternatives

- `Never prompt at all`: Answers the hang by removing the feature. The measurements refute inferring interactivity from isatty alone, not prompting.
- `Require --interactive to opt in`: Nobody discovers a flag they have to know about first, and it inverts the cost onto the person the feature is for.
- `Detect harnesses by process ancestry`: Several announce nothing at all, and the reference implementation separates an agent from a human by the value of PAGER. A missed harness is a hang, so the failure is one-sided.
- `Put a timeout on the read`: Converts a hang into a stall, races a person mid-answer, and leaves the terminal in raw mode if aborted under a password read.
