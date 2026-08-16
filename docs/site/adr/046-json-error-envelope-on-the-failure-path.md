# ADR 046: Emit a JSON error envelope on the failure path

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `046`
- Title: `Emit a JSON error envelope on the failure path`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/046-json-error-envelope-on-the-failure-path.yaml`

## Decision

When --json is set and a command fails, write a bb.machine v2 envelope to stdout carrying an error object in place of data, with kind, message and exit_code drawn from the ADR-011 taxonomy. The human-readable line continues to go to stderr unchanged, and exit codes are unaffected. A consumer decides success or failure by which key is present: data on success, error on failure. The failure shape does not vary by command, so it is published once as docs/reference/schemas/output/output.error.schema.json rather than per command. Machine output requested but not parsed still counts as requested. When flag parsing itself fails, --json is recovered from the raw arguments, because an unknown flag or unknown command is precisely when a script most needs a parseable answer.

## Agent Instructions

Emit failures under --json through jsonoutput.WriteError. Do not print a bare string to stdout on the error path, and do not move the human-readable line off stderr. Do not add data to the failure envelope or error to the success envelope. The two documents are distinguished by which key is present; a null data alongside error would make a command whose successful payload is legitimately null ambiguous. When adding an error kind to internal/domain/errors, add it to Kinds(). The published schema derives its enum from that function and tests validate real emitted envelopes against it, so a kind missing there publishes a contract the CLI can violate. Exit codes stay owned by the taxonomy. The envelope reports exit_code; it does not decide it.

## Rationale

--json is documented as a stable machine contract and ADR-011 promises structured JSON error payloads, but cmd/bb printed the error as plain text regardless of the flag. The envelope machinery was wired only to the success path, so a failing command left stdout empty. The primary consumer of --json is an agent or a CI script. Empty stdout plus an unparseable string on stderr cannot be distinguished from a command that produced malformed output, and it cannot be branched on by error kind — even though the classification already existed one line above, and was already being serialised into the diagnostics logger. Carrying error where data would sit keeps the envelope self-describing without a discriminator field, and keeps the success schema, which forbids additional properties, unchanged. Nothing that parsed the old output breaks: previously there was nothing on stdout to parse. Publishing one failure schema rather than one per command matches the shape of the problem and lets a consumer handle an error from a command it has never seen.

## Rejected Alternatives

- `Write the error envelope to stderr instead of stdout`: Keeps stdout free of non-data, but leaves the original defect in place: a script reading stdout for --json still sees nothing and still cannot tell failure from malformed output. It would also mix the envelope with diagnostics, which ADR-014 deliberately keeps on stderr.
- `Add error alongside a null data in the existing envelope`: Requires the success schema to allow a new property and to relax data, and makes a command whose successful data is legitimately null indistinguishable from a failure without also checking error for null. Presence of one key or the other is unambiguous.
- `Publish a failure schema per command, as with success payloads`: The failure shape does not vary by command, so per-command copies would be identical files that drift. One schema also lets a consumer validate an error from a command released after it was written.
- `Reclassify Cobra usage errors as validation while adding the envelope`: An unknown flag currently reports kind internal and exit 1, which is misleading, but changing it changes exit codes for existing scripts. That is a separate behavioural decision and does not belong in a change whose point is to stop losing information.
