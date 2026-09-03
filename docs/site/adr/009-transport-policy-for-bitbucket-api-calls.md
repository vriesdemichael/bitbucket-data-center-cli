# ADR 009: Transport policy for Bitbucket API calls

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `009`
- Title: `Transport policy for Bitbucket API calls`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/009-transport-policy-for-bitbucket-api-calls.yaml`

## Decision

Centralize all HTTP transport behavior in a shared client package that provides authentication injection, timeout defaults, retry/backoff policy, rate-limit handling, and pagination primitives.
Retries are limited to methods that are idempotent by definition -- GET, HEAD, OPTIONS, PUT, DELETE, TRACE. POST and PATCH are never replayed after a transport error or a 5xx, because a response lost after the write landed cannot be told apart from a request that never arrived. A 429 is retried whatever the method: the server is stating that it did not process the request, so replaying it creates nothing twice.

## Agent Instructions

Do not implement ad-hoc HTTP behavior in service or workflow packages. New endpoints must use the shared transport interfaces and error mapping. Retry decisions go through internal/transport/retrypolicy, not through a status or method check written at the call site. A new transport that loops on failure must consult it, and a request whose body cannot be rewound must return an error rather than a response whose body an earlier attempt already consumed.

## Rationale

Bitbucket behavior and reliability concerns should be handled once and reused everywhere. Centralization prevents subtle drift in auth, timeout, and pagination logic.
The retry rule is stated here because this document previously said only that retries should be "safe for idempotent operations", which reads as a guard that existed. None did: both transports replayed every method twice by default, so a lost response could open the same pull request three times and report success for whichever attempt answered (#454). Saying which methods and which statuses makes the sentence checkable.

## Rejected Alternatives

- `Per-service custom HTTP clients`: Produces inconsistent behavior and duplicated reliability logic.
- `Retry POST and PATCH when the failure looks like the request never left`: Go does not report reliably whether the bytes reached the server, so the distinction would be inferred from error shapes. Reading one wrong duplicates a mutation silently; refusing costs one manual retry with an honest message. On a CLI the operator is present, which makes the second the cheaper mistake.
- `Send an idempotency key with every mutation`: Bitbucket Data Center has no such header, so there is nothing for the server to honour.
