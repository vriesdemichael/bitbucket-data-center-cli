# ADR 076: Machine output field names are camelCase, matching the Bitbucket API

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `076`
- Title: `Machine output field names are camelCase, matching the Bitbucket API`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/076-machine-output-field-names-are-camelcase.yaml`

## Decision

Every field name in machine output is camelCase, including the envelope: bbVersion, limitReached, exitCode. Single-word names are unchanged, since a single word is already camelCase. A field mirroring an upstream Bitbucket object keeps the upstream name exactly, so a reader comparing bb output against the Atlassian API documentation sees the same word. Diverge only where the upstream name would mislead a reader who does not know Bitbucket's internal vocabulary. Then pick the concise description of what is returned, and say in the field description what it corresponds to upstream. Renaming because a name is merely unfamiliar is not a reason; renaming because it describes an implementation detail the caller does not have is. It covers every document bb writes to stdout under --json, not only command payloads: the dry-run preview and the keys inside error.details are output too, and both were missed on the first pass because they are not what anyone pictures when they say "the output". Where a human rendering prints key=value pairs, the key is the field name, so it changes with the field. The dry-run preview printed predicted_action= beside a payload carrying predictedAction, which leaves a reader comparing the two renderings unsure they are the same thing. This is a naming rule for output. Input flags stay kebab-case, which is what a CLI reader expects and what every flag already uses. Structured log lines on stderr are a separate surface and are left alone.

## Agent Instructions

Name a new output field in camelCase. Do not introduce snake_case, and do not publish a Go struct without JSON tags -- an untagged field publishes its Go name, which is PascalCase. When a field mirrors an upstream object, copy the upstream name rather than improving it. When it does not mirror anything, name it for what it holds.

## Rationale

The Bitbucket Data Center API is consistent, contrary to the impression that prompted this. Measured across the vendored spec -- 295 schemas, 5416 property occurrences, 533 distinct names -- it is 187 single-word lowercase and 307 camelCase, with zero snake_case and zero PascalCase. The 36 kebab-case names belong to five SSO configuration entities bb never calls, so across bb's reachable surface it is entirely camelCase. bb was the inconsistent one. It emitted three conventions at once: 30 snake_case names of its own, upstream camelCase passed straight through, and PascalCase leaking from 49 payloads that embedded an untagged Go struct. One payload could carry displayId beside default_branch. Matching upstream costs bb's own invented fields a rename and buys every pass-through field an exact correspondence with the API documentation. The bulk artifacts already chose this -- apiVersion, planHash, cancelledOperations -- so it is also the convention with the most of the surface already on it.

## Rejected Alternatives

- `snake_case throughout`: Defensible, and the JSON default, but it diverges from a consistent upstream rather than cleaning up a messy one. Every pass-through field would need a translation that exists only to change the spelling, and a reader cross-referencing Atlassian's documentation would do one mental transform per field forever.
- `upstream names for pass-through fields, snake_case for bb's own`: The state that prompted this decision. It sounds principled and reads as an accident: a caller cannot tell which rule a field followed without knowing whether it came from Bitbucket, so the shape has to be memorised rather than predicted.
- `leave the field names alone`: The PascalCase leak is not a style preference, it is a defect: a consumer reading repository.projectKey gets nothing because the payload says ProjectKey. Something had to change, and changing it twice costs a second major.
