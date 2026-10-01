---
search:
  boost: 0.3
---

# ADR-009: Transport policy for Bitbucket API calls

bb reaches Bitbucket through two clients, internal/transport/httpclient and the generated client internal/openapi builds, and both take their behaviour from internal/transport: TLS, the CA bundle and the client certificate from network, retry decisions from retrypolicy, and what a failed exchange means from outcome. They inject the credential, hold an API call to request_timeout from connecting to the last byte, and retry as retry_count and retry_backoff say. A body that may be large or slow goes through internal/transport/download instead (ADR-093). A listing is paged with openapi.PageThrough.

Retries are limited to the methods that are idempotent by definition: GET, HEAD, OPTIONS, PUT, DELETE and TRACE. POST and PATCH are never replayed after a transport error or a 5xx, because a response lost after the write landed cannot be told apart from a request that never arrived. A 429 is retried whatever the method: the server is stating that it did not process the request, so replaying it creates nothing twice. A Retry-After header decides the wait when the server sends one.

outcome classifies a failed exchange once, for both clients, and only a transient failure is retried. A rejected certificate, a refused TLS handshake, a plain-HTTP answer to https and a host that does not resolve are permanent; an interrupt is cancelled. For a method that is never replayed, whether the request reached the connection decides between transient and unknown_outcome, and a gateway's 502 or 504, a 500, or the 400 Bitbucket raises while writing its answer (openapi.FailedWritingAnswer) is unknown_outcome (ADR-011).

Do not write HTTP behaviour in a service or workflow package; use the clients, their error mapping and their paging. Decide a retry through retrypolicy, never with a status or method check at the call site, and have any new transport that loops on failure consult it. A request whose body cannot be rewound returns an error rather than a response whose body an earlier attempt already consumed. Wrap a transport error with apperrors.Transport, which keeps its classification; TestNoFailureIsWrappedAsTransient fails on New(KindTransient, message, err).

Authentication, timeouts, retries and paging written once behave the same in every command.

## Not chosen

- **Per-service custom HTTP clients**: Produces inconsistent behavior and duplicated reliability logic.
- **Retry POST and PATCH when the failure looks like the request never left**: Go does not report reliably whether the bytes reached the server, so the distinction would be inferred from error shapes. Reading one wrong duplicates a mutation silently; refusing costs one manual retry with an honest message. On a CLI the operator is present, which makes the second the cheaper mistake.
- **Send an idempotency key with every mutation**: Bitbucket Data Center has no such header, so there is nothing for the server to honour.
