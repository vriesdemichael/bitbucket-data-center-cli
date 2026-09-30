---
search:
  boost: 0.3
---

# ADR-075: A machine-mode command emits one document, and what a caller acts on is in error.details

> Changed in part by [ADR-096](096-the-flags-choose-the-member-and-dry-run-answers-with-a-verdict.md).

Under --json a command writes exactly one JSON document to stdout. When a command both produced data and failed, the failure envelope wins: ADR-046 distinguishes the two by which key is present, so a run cannot report both. What a caller needs to act on a failure goes in error.details, a string map inside the error object, not only in the message; an error from Bitbucket carries its upstreamStatus and upstreamException there. Human output is unaffected. The result goes to stdout and the error line to stderr, so the two do not collide. A cancelled run exits 12 (kind cancelled), not 10 (transient). Cancellation is not a retry signal: re-running an interrupted command repeats the change somebody just stopped.

Do not print a payload and then return an error from the same command under --json. Put what a caller acts on in error.details, and read it from there rather than by parsing error.message. Do not retry exit 12; find out what the interrupted command did first.

Two documents on stdout fail quietly. A strict decoder rejects the second one, but jq reads a value stream: it emits one result per document and exits 0, so a script taking the last line silently gets the wrong document. Keeping the payload and dropping the error loses the exit code, which is the part a script cannot reconstruct.

## Not chosen

- **Carry the payload inside the failure envelope as data**: ADR-046 forbids data alongside error. The two documents are told apart by which key is present, and a null data would make a command whose payload is legitimately null ambiguous. error.details is inside the error object, so that discriminator is untouched.
- **Put the detail in the message only**: Every consumer would scrape a sentence for a value no schema describes.
- **Write the payload and exit non-zero without an error envelope**: Special-cases commands out of the failure contract, so a consumer branching on error kind has to know which commands opt out.
- **Reuse transient for cancellation**: Documented to agents as "retry later". For a mutating command that is the one response that must not be automatic.
