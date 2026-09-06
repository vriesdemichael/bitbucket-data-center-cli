# ADR 053: Raw API escape hatch for uncovered endpoints and version gaps

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `053`
- Title: `Raw API escape hatch for uncovered endpoints and version gaps`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/053-raw-api-escape-hatch.yaml`

## Decision

Introduce a top-level `bb api <path>` command that acts as a direct REST passthrough escape hatch to Bitbucket Data Center instances, modeled after the UX of `gh api`.
`bb api` reuses core CLI capabilities: - Configuration, credential resolution (keyring, tokens, basic auth), and host aliasing (`internal/config`). - Transport resilience, automatic retry with exponential backoff, and 429 `Retry-After` handling (`internal/transport/httpclient`). - TLS trust configuration (`--ca-file`, `--insecure-skip-verify`). - Bitbucket Data Center pagination traversal (`--paginate`) following `isLastPage` and `nextPageStart`. - Machine-mode output envelopes under `--json`. - Strict `--dry-run` safety: non-GET mutating methods (`POST`, `PUT`, `DELETE`, `PATCH`) are safely refused.

## Agent Instructions

Recommend `bb api` when users or automation need to interact with newer Bitbucket REST endpoints not yet exposed as dedicated CLI subcommands, or when testing custom Bitbucket plugin endpoints. Ensure `--dry-run` safety is preserved when constructing scripts that invoke `bb api`.

## Rationale

Bitbucket Data Center has a vast and evolving REST API surface. With 220+ commands, gaps inevitably exist between pinned OpenAPI specifications and the latest server versions (e.g. 10.4+). A robust raw API escape hatch allows users and AI agents to immediately unblock themselves without waiting for a dedicated CLI release, while ensuring security, retry resilience, and output consistency are fully maintained.
