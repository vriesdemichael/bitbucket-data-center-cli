---
search:
  boost: 0.3
---

# ADR-011: Error taxonomy and CLI exit contract

Every error bb reports has a kind, and the kind decides the exit code and the structured error payload. The kinds are authentication, authorization, validation, not_found, conflict, transient, permanent, not_implemented, cancelled, unknown_outcome, unsupported and internal; internal is what an unclassified error becomes, so every error has a kind.

unsupported is exit 14 and means the Bitbucket instance's release cannot do what was asked and bb cannot make up for it (ADR-088). It is not not_implemented, which is bb lacking something: the remedy is a newer Bitbucket, not a newer bb.

unknown_outcome is exit 13 and means the work was sent and no usable answer came back, so bb cannot say whether it was applied: a request whose connection was lost, timed out or was interrupted after it was sent, that a gateway answered with 502 or 504, that Bitbucket answered with 500, or that Bitbucket answered with an error it raised while writing the answer; and a git command that changes something and that bb stopped for running out of time. For a request it applies only to a method the retry policy will not replay. A GET, PUT or DELETE in the same position is transient: it is idempotent, so sending it again is how the caller finds out. A 500 is Bitbucket raising an error rather than refusing the request, and it can raise one after applying part of what was asked, which is why the retry policy does not replay a 5xx on such a method. 503 stays transient: it says the request was not processed. unknown_outcome sits apart from transient, which invites a retry; from permanent, which says the work did not happen; and from cancelled, exit 12, an interrupt that stopped the work before the server could act on it. A mutation that may have been applied has to be checked rather than repeated.

A status that arrived says whether the work was refused, not that it landed. A reply cut short after a 4xx is permanent: nothing was applied, and sending it again will be refused again. A 2xx settles nothing, because behind SSO or a proxy a login page can answer 200 to a write nobody authenticated, so a mutation whose body was lost stays unknown_outcome.

A command may resolve an unknown outcome by reading the state back. A webhook create does: it lists the scope's webhooks before and after, and exactly one new webhook matching the request is reported as created; anything else stays unknown_outcome. The mutation is never sent again. This is worth doing where a listing identifies what was made; it is not required of every mutation, and most of them report exit 13 and leave the check to the caller.

The exit status says what happened to bb. A command that reports through its exit status the state it read, as `bb pr checks` does, uses a code that is not a kind and never an error payload (ADR-091).

Map transport and service errors onto a kind before returning from a workflow. Do not report a failed mutation as transient when its outcome is unknown: exit 10 tells a caller's wrapper to replay a request the retry policy itself refuses to replay. Keep human and JSON output on the same classification. Do not pass a raw upstream error straight to the user.

A script can act on a failure, and an operator can trust it, only when the same failure always gets the same answer. One taxonomy lets retries, diagnostics and support follow from the kind.

## Not chosen

- **Free-form error strings and ad-hoc exit codes**: Breaks machine consumption and makes behaviour unpredictable.
- **Send the mutation again when the check does not find what it made**: A request that timed out can still be in progress, so not finding its result is not proof it will not land, and a second send then duplicates it.
