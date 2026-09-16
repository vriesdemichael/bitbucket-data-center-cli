---
search:
  boost: 0.3
---

# ADR 011: Error taxonomy and CLI exit contract

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `011`
- Title: `Error taxonomy and CLI exit contract`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/011-error-taxonomy-and-cli-exit-contract.yaml`

## Decision

Define a stable error taxonomy and map it to deterministic CLI exit codes and structured JSON error payloads. The kinds are authentication, authorization, validation, not_found, conflict, transient, permanent, not_implemented, cancelled, unknown_outcome and internal; internal is what an unclassified error becomes, so every error has a kind.
unknown_outcome is exit 13 and means the request reached the server and no usable answer came back, so bb cannot say whether it was applied: a mutation whose connection was lost, timed out or was interrupted after it was sent, that a gateway answered with 502 or 504, that Bitbucket answered with 500, or that Bitbucket answered with an error it raised while writing the answer. A 500 is Bitbucket raising an error rather than refusing the request, and it can raise one after applying part of what was asked; the retry policy already refuses to replay a 5xx on a method it does not replay, so reporting it as transient asked the caller to do what the policy would not. 503 stays transient: it says the request was not processed. It sits apart from transient, which invites a retry; from permanent, which says the work did not happen; and from cancelled, exit 12, an interrupt that stopped the request before the server could act on it. A mutation that may have been applied has to be checked rather than repeated.
A status that arrived says whether the work was refused, not that it landed. A reply cut short after a 4xx is reported as refused rather than unknown: nothing was applied, and sending it again will be refused again. A 2xx settles nothing, because behind SSO or a proxy a login page can answer 200 to a write nobody authenticated, so a mutation whose body was lost stays unknown_outcome.
Where bb can tell what a mutation made, it makes that check itself. A webhook create lists the scope's webhooks before and after, and one new webhook matching the request is reported as created; anything else stays unknown_outcome. The mutation is never sent again.

## Agent Instructions

Map transport and service errors into canonical categories before returning from workflows. Do not report a failed mutation as transient when its outcome is unknown: exit 10 tells a caller's wrapper to replay a request the retry policy itself refuses to replay. Keep human output and JSON output consistent with the same underlying error classification. Avoid leaking raw upstream errors directly to users.

## Rationale

Deterministic error behavior is required for both scriptability and operator trust. A consistent taxonomy simplifies retries, diagnostics, and support workflows.

## Rejected Alternatives

- `Free-form error strings and ad-hoc exit codes`: Breaks machine consumption and makes behavior unpredictable.
- `Send the mutation again when the check does not find what it made`: A request that timed out can still be in progress, so not finding its result is not proof it will not land, and a second send then duplicates it: the failure #454 removed from the retry policy.
