# ADR 075: A machine-mode command emits one document, and a failed run is named by its id

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `075`
- Title: `A machine-mode command emits one document, and a failed run is named by its id`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/075-a-machine-run-emits-one-document.yaml`

## Decision

Under --json a command writes exactly one JSON document to stdout. When a command both produced data and failed, the failure envelope wins: ADR-046 distinguishes the two by which key is present, so a run cannot report both. bb bulk apply is the case where that costs something, because the artifact is the record of what was applied. Its failure and cancellation errors therefore name the operation id, and bb bulk status <id> --json returns the full artifact. The id is the handle; stdout is not. Human output is unaffected. The status goes to stdout and the error line to stderr, so the two do not collide and no id is needed to recover the detail. A cancelled run exits 12 (kind cancelled), not 10 (transient). Cancellation is not a retry signal: re-running a bulk apply replays mutations across every repository in the plan.

## Agent Instructions

Do not print a payload and then return an error from the same command under --json. The caller gets two top-level documents and jq fails on the second. When a failure has an artifact behind it, put the identifier in the error message. That is what makes the run recoverable once stdout is spent on the envelope. Do not retry exit 12. Read the artifact and decide.

## Rationale

bb bulk apply wrote its status envelope and then returned an error, so cmd/bb wrote an error envelope after it. Two documents on stdout is the parse failure #474 was filed about, and it was already the behaviour of the ordinary partial-failure path -- the cancellation work only made it reachable a second way. Suppressing the error to keep the payload was the alternative that loses the exit code, which is the part a script cannot reconstruct. Keeping the envelope and losing the payload loses nothing permanently, because the artifact is on disk either way.

## Rejected Alternatives

- `Carry the status inside the failure envelope`: ADR-046 forbids data alongside error. The two documents are told apart by which key is present, and a null data would make a command whose payload is legitimately null ambiguous.
- `Write the status envelope and exit non-zero without an error envelope`: Special-cases one command out of the failure contract, so a consumer branching on error kind has to know which commands opt out.
- `Reuse transient for cancellation`: Documented to agents as "retry later". For a mutating bulk run that is the one response that must not be automatic.
