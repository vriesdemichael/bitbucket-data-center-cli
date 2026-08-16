# Quality artifacts

This directory holds **baselines**: small, committed files that assert what must remain true, and
that CI verifies by static analysis without needing a Bitbucket instance.

It deliberately does not hold **measurements**. Coverage profiles and the combined coverage report
are computed per run into `.tmp/`, and CI publishes them to Codecov and as workflow artifacts. See
[ADR-045](../site/adr/045-coverage-measurements-are-computed-not-committed.md) for why that split
exists.

## Files

| File | Asserts | Update | Verify |
|---|---|---|---|
| `cli-live-coverage.json` | which CLI commands the live suite proves work against a real Bitbucket | `task quality:cli-live-coverage:update` | `task quality:cli-live-coverage:verify` |
| `spec-coverage.json` | which `(method, path)` operations from the Bitbucket spec the CLI reaches | `task quality:spec-coverage:update` | `task quality:spec-coverage:verify` |
| `generated-operation-contracts.json` | mapping of generated client operations to the tests providing contract coverage | — | consumed by `quality-report` |

Both verify commands are static analysis: they read the Cobra command tree, the live test sources,
the OpenAPI spec and the services source. Neither starts Bitbucket, so both run in the fast CI job
and in the pre-push hook.

`task quality:verify` runs all of them together.

## Where coverage numbers live

| | |
|---|---|
| Gate | CI, on every pull request including from forks, in the live-tests job |
| Thresholds | `.github/coverage-thresholds.env` |
| Trend history | Codecov |
| Raw profiles | workflow artifacts on each CI run, retained 14 days |
| Locally, full | `task quality:coverage:origin-main` — needs the stack up, ~8 minutes |
| Locally, re-check | `task quality:coverage:replay` — reuses the profiles in `.tmp/`, seconds |

Nothing needs regenerating after a rebase. `task pr:rebase` is a plain rebase.

## Fixing a patch-coverage failure

The gate requires 85% of the lines a branch changes to be covered. When it fails it lists the
locations, grouped by file with consecutive lines collapsed:

```
Uncovered changed lines (47):
  internal/cli/cmd/auth/gitcredential.go:52-53,68-70,128-130
  internal/config/config.go:754-756
```

Iterating on that does not need another suite run. `task quality:coverage:replay` recomputes the
diff against `origin/main` and re-applies every threshold using the profiles already in `.tmp/`, so
the loop is: add tests → `task test:unit:coverage` → replay. Re-run `task test:live:coverage` only
when the change affects behaviour the live suite exercises.

The same loop on CI costs a full live run per attempt, on a profile you cannot inspect — which is
why the locations are printed rather than just the percentage.

## CLI live coverage (`cli-live-coverage.json`)

Records which commands the live suite actually exercises against a real server. The verify step
fails when a command loses live coverage, arrives without it, or becomes **masked** — its only live
coverage coming from a test that calls `t.Skip` when the call fails, so the suite passes whether or
not the command works.

That last case is why the baseline is committed rather than computed. `bb pr task *` called an
endpoint Atlassian removed in Bitbucket 8.0, and the live test hid it behind a skip-on-error branch;
CI stayed green for years. A skipped test is not a passing test.

## OpenAPI spec coverage (`spec-coverage.json`)

Answers "how much of the Bitbucket OpenAPI surface does the CLI implement, and what is still
missing?" — distinct from Go statement coverage.

Coverage is measured at the `(HTTP method, path)` level and combines **both** ways the CLI reaches
the API:

1. The generated typed client (`internal/openapi/generated`), restricted to operations actually
   called from `internal/services`.
2. The hand-rolled `internal/transport/httpclient` (`GetJSON`/`PostJSON`/…), whose request paths are
   resolved statically from the services source.

Tracking both matters: services such as `pullrequest` are built entirely on the raw httpclient, so a
generated-client-only metric would report them as uncovered even though they are fully implemented.

Print current coverage with `task quality:spec-coverage`. The `gaps` array lists unimplemented
operations (method, path, tag, summary) and is a useful source when scoping new commands.
