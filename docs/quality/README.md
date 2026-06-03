# Quality Coverage Artifacts

This directory contains committed quality artifacts used for local pre-push and GitHub Actions verification.

## Files

- `coverage-report.json`: deterministic snapshot generated from combined unit + live coverage and operation mapping metrics.
- `coverage.combined.raw.out`: committed merged Go coverprofile (unit + live, raw scope) retained for deterministic local quality artifacts.
- `coverage.combined.scoped.out`: committed merged Go coverprofile (unit + live, scoped include/exclude policy) for Codecov ingestion.
- `generated-operation-contracts.json`: mapping of generated client operations used by hand-written services to test files that provide contract coverage.
- `spec-coverage.json`: OpenAPI spec path coverage report — which `(method, path)` operations from the Bitbucket spec the CLI actually reaches, plus the list of unimplemented gaps. See below.

## Workflow

- Update artifacts before push: `task quality:coverage:report:update`
- Local verify (recompute + compare): `task quality:coverage:report:verify`
- CI verify (artifact threshold + profile parse checks only): `task quality:coverage:report:verify:committed`

## OpenAPI spec coverage (`spec-coverage.json`)

This report answers "how much of the Bitbucket OpenAPI surface does the CLI
implement, and what is still missing?" — distinct from Go statement coverage.

Coverage is measured at the `(HTTP method, path)` level and combines **both**
ways the CLI reaches the API:

1. The generated typed client (`internal/openapi/generated`), restricted to the
   operations actually called from `internal/services`.
2. The hand-rolled `internal/transport/httpclient` (`GetJSON`/`PostJSON`/...),
   whose request paths are resolved statically from the services source.

Tracking both transports matters: services such as `pullrequest` are built
entirely on the raw httpclient, so a generated-client-only metric reports them
as uncovered even though they are fully implemented.

- Print coverage: `task quality:spec-coverage`
- Update artifact: `task quality:spec-coverage:update`
- Verify artifact (CI-safe, no live execution): `task quality:spec-coverage:verify`

The `gaps` array lists unimplemented operations (method, path, tag, summary) and
is a useful source when scoping new commands.

## Notes

- Live integration tests are not run in GitHub Actions due Bitbucket license/infrastructure constraints.
- This report-driven workflow keeps CI reproducible by validating committed local quality outputs.
- `spec-coverage.json` is regenerated deterministically from the spec, the generated client, and the services source — it does not depend on the coverage profiles, so it is updated and verified independently of the live-test pipeline.
