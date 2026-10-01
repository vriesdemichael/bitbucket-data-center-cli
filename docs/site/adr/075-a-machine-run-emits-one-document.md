---
search:
  boost: 0.3
---

# ADR-075: A machine-mode command emits one document, and what a caller acts on is in error.details

Under `--json` or `--yaml` a command writes exactly one document to stdout. When a run both produced data and failed, the failure wins: a document holds one member (ADR-096), so a run cannot report both. Under `--json` or `--yaml` a non-zero exit always comes with a top-level `error`. A command that reports through its exit status the state it read (ADR-091), a dry run that reached a verdict, and a description all exit 0 with the answer in the document. What a caller needs to act on a failure goes in `error.details`, a string map inside the error object, not only in the message; a failure Bitbucket answered carries its `upstreamStatus` there, and its `upstreamException` when Bitbucket named one. Human output is unaffected: the result goes to stdout and the error line to stderr, so the two do not collide. A cancelled run exits 12 (kind cancelled), not 10 (transient). Cancellation is not a retry signal: re-running an interrupted command repeats the change somebody just stopped.

Do not write a document and then return an error from the same command. Put what a caller acts on in `error.details`, and read it from there rather than by parsing `error.message`. Do not retry exit 12; find out what the interrupted command did first.

Two documents on stdout fail quietly. A strict decoder rejects the second one, but jq reads a value stream: it emits one result per document and exits 0, so a script taking the last line silently gets the wrong document. Keeping the payload and dropping the error loses the exit code, which is the part a script cannot reconstruct.

## Not chosen

- **Carry the payload inside the failure envelope as data**: ADR-046 forbids data alongside error. error.details sits inside the error object, so which key is present still tells the two documents apart.
- **Put the detail in the message only**: Every consumer would scrape a sentence for a value no schema describes.
- **Write the payload and exit non-zero without an error envelope**: Special-cases commands out of the failure contract, so a consumer branching on error kind has to know which commands opt out.
- **Reuse transient for cancellation**: Documented to agents as "retry later". For a mutating command that is the one response that must not be automatic.
