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
| `command-reach.json` | which CLI commands the live suite proves work against a real Bitbucket | `task quality:command-reach:update` | `task quality:command-reach:verify` |
| `spec-coverage.json` | which `(method, path)` operations from the Bitbucket spec the CLI reaches | `task quality:spec-coverage:update` | `task quality:spec-coverage:verify` |
| `unit-test-mock-inventory.json` | every mocked Bitbucket server left in the unit suite, and what each one assumes | `go run ./tools/mock-inventory -write` | `go run ./tools/mock-inventory` |

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
| Locally, full | `task quality:coverage` — needs the stack up, ~8 minutes |
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

## command reach (`command-reach.json`)

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

## mocked servers (`unit-test-mock-inventory.json`)

Indexes every `httptest` server left in the unit suite and classifies what each one assumes, which
is what ADR-079 turns into work. `unit-test-mock-inventory-tasks.md` is the same data as a task
list; both are regenerated together and committed, so a mock arriving in a class that should be
empty shows up in the diff rather than in a later sweep.

A mock whose class the scanner reads wrongly is corrected in place with a directive above it:

```go
// mock-inventory: routing-beacon — the reply is never read as Bitbucket's; the subject is which
// of the two listeners the request reached.
```

The reason is required. A class asserted without one is how the classification stops meaning
anything.

### Read the total, do not infer it

The tool prints the total on its first line and the per-class counts under it:

```
mocked servers: 174 across 60 files and 165 functions
  transport-fault      76
  ...
```

Three commit messages in the v4 migration (`24791a53`, `67e8e9f6`, `bf34feb7`) quote totals that
were filled in from the direction of travel rather than read off that line, because the output was
being tailed past it. The per-class figures in them are right; the totals beside them are not, and
one of the three explains a rise that did not happen — the commit cut five suites and moved no
mocks, because all five hung off shared constructors that are still there.

Nothing verifies a number in a commit message, which is the whole reason to read it rather than
work it out.

## Release prose (`docs/release-notes/`)

A release may carry a written introduction. Put it in
`docs/release-notes/<version>.md` — `docs/release-notes/v4.0.0.md`, matching the
tag the version computation will produce — and the release job prepends it to
the generated notes.

It has to exist **before** the push that triggers the release. The notes are
rendered into the versioned docs snapshot in the same run, and a mike snapshot
is immutable in practice: no later commit reaches the `/vX.Y.Z/` page. Prose
added afterwards can be edited into the GitHub release, but the pinned docs page
keeps the ledger for good.

Absent for an ordinary release, which is the normal case and changes nothing.
Above forty entries the generated ledger folds into a `<details>` block, so the
prose is what a reader meets first; breaking changes stay in the open however
many there are.

To see what a release would publish, without releasing anything:

```bash
awk '/Generate changelog from Conventional Commits/,/^      - name: Create and push/' .github/workflows/release.yml \
  | sed -n "/python - <<'PY'/,/^          PY$/p" | sed '1d;$d' | sed 's/^          //' > /tmp/gen_changelog.py
VERSION=v4.0.0 PREVIOUS_TAG=v3.5.2 \
  REPOSITORY_URL="https://github.com/vriesdemichael/bitbucket-data-center-cli" \
  python /tmp/gen_changelog.py && cat RELEASE_NOTES.md
```
