# Contributing

Thanks for considering a contribution.

This project validates command behaviour against a **real Bitbucket Data Center
instance** rather than against mocks. That instance is provisioned and licensed
automatically, so you can run the full test suite locally with no Atlassian
licence and no repository secrets — the same suite that runs on your pull
request.

## Prerequisites

| Tool | Why |
|---|---|
| **Go** (see `go.mod`, currently 1.26) | building and testing |
| **[Task](https://taskfile.dev)** | every workflow in this repo is a `task` target |
| **Docker** | runs the local Bitbucket instance for live tests |
| **Bash** + **curl** | `scripts/bootstrap-bitbucket.sh` needs both |
| **~6GB disk, ~4GB RAM** | the Bitbucket instance is a real JVM application |

Install Task with:

```bash
go install github.com/go-task/task/v3/cmd/task@latest
```

Install the git hooks with:

```bash
lefthook install
```

On Windows, run the shell scripts from Git Bash or WSL. Line endings are handled
for you: `.gitattributes` pins the whole tree to LF regardless of your
`core.autocrlf` setting.

No Python is needed for any of this. One maintenance task, `docs:refresh-openapi`,
still shells out to `python3` to sanity-check the vendored Atlassian spec after
downloading it, and the documentation site builds through `uv` in `docs/` — but
neither is part of building, testing, or running the live suite.

## First run

```bash
git clone https://github.com/vriesdemichael/bitbucket-data-center-cli
cd bitbucket-data-center-cli
go build ./cmd/bb
task test:unit
```

Unit tests need no network and no Bitbucket instance. If they pass, you have a
working environment.

## Running the live suite

This is the gate that matters, and it is the one most changes need.

```bash
task stack:up
```

First run downloads roughly 800MB of Bitbucket artifacts and takes a few
minutes. Subsequent starts reuse a cached volume.

```bash
bash scripts/bootstrap-bitbucket.sh http://localhost:7990 admin admin
```

Bitbucket 10 disables basic authentication by default even once it reports
`RUNNING`; this enables it. Then:

```bash
BITBUCKET_URL=http://localhost:7990 ADMIN_USER=admin ADMIN_PASSWORD=admin task test:live
```

A full run takes about five minutes. `task stack:down` when you are finished.

**The instance's licence lasts three hours.** It is issued by the Atlassian
Plugin SDK on each start and reissued on restart — but not by `task stack:up`,
which reuses a running container rather than recreating it. `task stack:restart`
is what reissues it.

Check before starting a long session:

```bash
task stack:status
```

```text
SDK licence: 148m remaining (healthcheck retires the container at 2h45m)
```

You do not have to remember. The live suite refuses to start against an expired
instance and tells you what to run, and `task stack:up` fails with the same
advice. Both exist because an expired licence does not look like one: Bitbucket
keeps reporting `RUNNING` and only refuses writes, so the first symptom used to
be a `git push` failing partway through seeding with `License limit exceeded` —
which reads like a broken test. See [`docker/README.md`](docker/README.md).

## Making a change

**Branch from `main`.** `main` requires a pull request; direct pushes are
rejected.

**Use [Conventional Commits](https://www.conventionalcommits.org/).** The commit
type determines whether merging your PR publishes a release, so it is worth
getting right:

| Type | Effect on release |
|---|---|
| `feat` | minor version |
| `fix`, `perf`, `revert` | patch version |
| any type with `!`, or a `BREAKING CHANGE:` footer | major version |
| `ci`, `chore`, `docs`, `style`, `refactor`, `test`, `build` | **no release** |

Non-releasing commits are not second-class — they simply ship with the next
`feat` or `fix` rather than publishing a version of their own. Reserve `!` for
changes that actually break the CLI contract: a removed or renamed command or
flag, a changed exit code, or a change to the `bb.machine` JSON envelope.

**Keep history linear.** Rebase onto `main`; never merge `main` into your
branch. This is a convention rather than a gate: the check that enforced it
existed to keep committed coverage artifacts from conflicting on every rebase,
and ADR-045 deleted those artifacts.

```bash
git fetch origin && git rebase origin/main
```

**Add a live test for new commands.** A command with no live test — or one whose
only live test skips when the call fails — is treated as uncovered and fails CI.
This is deliberate: `bb pr task *` called an endpoint Atlassian removed in
Bitbucket 8.0 and CI stayed green for years because the test skipped on error. A
skipped test is not a passing test.

```bash
task quality:command-reach:update
git add docs/quality/command-reach.json
```

**Regenerate committed artifacts you affect.** The command reference, ADR pages,
and JSON schemas are generated and verified in CI:

```bash
task docs:generate
```

**Documented commands must actually work.** Every `bb ...` line in a ```` ```bash ````
block is parsed against the real command tree, so an example using a flag that
does not exist fails the build:

```bash
task docs:lint
```

Write examples in shell-tagged blocks so they are checked — an untagged block is
invisible to the linter, which avoids the check rather than passing it. If an
example is *meant* to be invalid, mark the block with
`<!-- docs-lint: expect-invalid -->`; that inverts the check, so it also fails if
the command later becomes valid.

## Before opening a pull request

```bash
task quality:verify
task test:unit
task docs:validate
```

`quality:verify` includes the formatting and line-ending gates, so the usual fix
for a failure there is:

```bash
gofmt -w ./cmd ./internal ./tools
```

### Line endings

`.gitattributes` pins every file to LF, in the repository and in your working
tree, on every platform. You do not need to set `core.autocrlf`, and setting it
will not override this.

This is not a style preference. Before it existed, line endings varied file by
file depending on whether git had checked a file out or a tool had rewritten it,
which broke three things at once: `gofmt -l` reported 188 of 248 Go files as
unformatted, tools parsing repository text saw a stray `\r` inside the last
token, and `docker/harness/start-bitbucket.sh` — which the Dockerfile copies
into a Linux image — acquired a CRLF shebang and failed with
`$'\r': command not found`.

CI checks that no committed file contains a carriage return. If it ever fails:

```bash
git add --renormalize .
```

### What the git hooks actually do

Both hooks are heavier than most projects', and neither is hung when it appears
to stall. [lefthook](https://github.com/evilmartians/lefthook) runs them; they
install with `lefthook install`.

**`pre-commit` — roughly 3 minutes.** Runs `task test:unit`, which is the
whole non-live Go test suite across `./cmd/...`, `./internal/...` and
`./tools/...`. Not a fast subset of tests related to your change: all of them,
on every commit. `internal/cli` alone accounts for most of it. Amending several
times in a row pays this each time.

**`pre-push` — well under a minute.** Runs `task docs:validate` and
`task quality:verify`: ADR validation, the OpenAPI spec-coverage and CLI
live-coverage baselines, and a check that generated docs are current. All of it
is static analysis, so it needs no Bitbucket instance and works on a fresh
clone.

The full coverage gate deliberately does **not** run here. CI runs it on every
pull request, including from forks, so running it again before every push
duplicated CI at roughly eight minutes per attempt. Run it yourself when you
want it — before a large change, for instance:

```bash
task stack:up
BITBUCKET_URL=http://localhost:7990 ADMIN_USER=admin ADMIN_PASSWORD=admin task quality:coverage
```

Do not bypass hooks with `--no-verify`.

### Chasing a patch-coverage failure locally

Patch coverage is the gate contributors hit most: at least 85% of the lines your
branch changes must be covered. When it fails, the report names the lines:

```
FAIL: patch coverage 72.63% is below required 85.00% (394 coverable lines >= 30)

Uncovered changed lines (108):
  internal/cli/cmd/auth/gitcredential.go:52-53,68-70,128-130
  internal/config/config.go:754-756
```

You do **not** need to re-run the suite to iterate on this. The gate reads the
coverage profiles left in `.tmp/`, so once you have run it once, re-evaluating
against your latest commits takes seconds:

```bash
task quality:coverage:replay
```

That recomputes the diff against `origin/main` and re-applies every threshold
using the profiles already on disk. Add tests, `go test` them, then re-run
`task test:unit:coverage` (about a minute) and replay — only a change that
alters *live* behaviour needs `task test:live:coverage` again.

Doing this locally is worth the setup. On CI the same loop costs a full
live-suite run per attempt, and the profile stays on a runner you cannot
inspect.

### What is not enforced, and is still expected

Some conventions this project relies on have no hook or CI check behind them.
They are still expected, and a reviewer will ask:

- **Conventional Commit subjects.** No `commit-msg` hook validates them, so
  nothing stops a malformed subject locally. The release workflow parses commit
  subjects to decide whether to publish, so a wrong type has a real effect —
  see the table above.
- **Tests for `tools/`.** The coverage gate is scoped to `cmd/` and `internal/`,
  so a change under `tools/` reports "no coverable changed lines" and passes
  whatever you do. Write the tests anyway when the code has logic worth testing:
  parsing, tokenising, path resolution, arithmetic. Skip `main()` and flag
  plumbing — a test there proves nothing, and the point is not to chase a
  percentage.

  The reason is asymmetry, not tidiness. A tool that shells out or reads a file
  fails loudly on the next run. A tool that *computes* fails quietly and
  wrongly, and `tools/quality-report` produces the numbers every other gate
  reads — a bug there makes CI pass when it should not. ADR-049 has the
  measurements behind leaving `tools/` out of the gate.
- **golangci-lint.** There is no configuration and no lint job. `gofmt` *is*
  enforced (see below), but nothing checks for unused parameters, shadowing, or
  the other things a linter would catch.

## What CI checks

| Job | What it does |
|---|---|
| ADR Validation | validates `docs/decisions/*.yaml` |
| Unit Tests | non-live tests, that the live-tagged tree compiles, that generated artifacts are current, and that every documented `bb ...` invocation parses |
| Docs Site | builds the MkDocs site |
| Live Integration Tests | starts Bitbucket and runs the live suite |
| Coverage Gates | global and patch coverage thresholds, against the profiles the live job produced |
| Codecov | publishes coverage history and the README badge |

Coverage gates are a separate job from the live suite on purpose: they fail for
unrelated reasons, and reporting a patch-coverage breach as "Live Integration
Tests failed" sends you looking for a broken test that does not exist.

Live tests **run on pull requests from forks**. If they fail on your PR, the
failure is real — please do not assume it is infrastructure.

## Things that will bite you

Collected from actually doing this, not hypothetical:

- **Tests that shell out to `git` must scope their environment.** Git exports
  `GIT_DIR` to every hook it runs, and git honours it over `-C` — so a raw
  `exec.Command("git", "-C", tmpdir, "init")` running under `pre-commit`
  reinitialises *this* repository instead. Use `execgit.ScopeFreeEnv()`, which
  strips git's repository-scoping variables. A `TestMain` guard fails any
  package whose tests change this repository's git configuration.
- **Line endings on Windows.** The repo has no `.gitattributes` and
  `core.autocrlf` is typically on, so Go tools that write LF make every
  generated file look modified. Check `git diff --numstat` — files showing
  `0 0` are line-ending noise and normalise away on commit. Shell scripts may
  need `tr -d '\r'` before running in a Linux container.
- **Tests that shell out to `git` must use `t.TempDir()`.** A guard fails the
  package if a test mutates the repository's own git config. This is not
  hypothetical — it once wrote an `http.extraHeader` credential into the
  project's `.git/config` and broke pushes to GitHub.
- **New mutating commands must be registered** in `dryRunProfiles`
  (`internal/cli/dryrun.go`) or `TestAllMutatingCommandsHaveDryRunProfile`
  fails.
- **Use `-count=1`** when testing config loading or environment variables; Go's
  test cache will otherwise mask state pollution.

## Where the deeper detail lives

- [`AGENTS.md`](AGENTS.md) — repository-specific mechanics and gotchas. Written
  for AI agents, but the content applies to anyone.
- [`docs/decisions/`](docs/decisions/) — architecture decision records. If you
  want to know *why* something works the way it does, it is usually there.
  Relevant here: ADR-005 (coverage policy), ADR-006 (conventional commits),
  ADR-016 (test classification), ADR-025 (git discipline), ADR-026 (PR
  readiness), ADR-033 (release automation), ADR-043 (the test instance).
- [`docker/README.md`](docker/README.md) — the local Bitbucket stack.

## Reporting bugs

Open an issue and pick **Bug report**. It asks for `bb --version`, the exact
command and what you expected instead. For unexpected behaviour,
`--log-level debug --log-format jsonl` gives a diagnostic trace worth attaching.

Anything that is not a bug — a feature, a question, an idea worth arguing about —
goes in a blank issue. Those are deliberately unstructured and there is no form
to fill in. Discussions are not enabled here, so issues are the venue for design
conversation too.

For security vulnerabilities, see [`SECURITY.md`](SECURITY.md) — please do not
open a public issue.

## Opening a pull request

The template asks for three things, and deliberately not for a checklist: CI
already runs the gates listed under [What CI checks](#what-ci-checks) and knows
whether they passed better than a ticked box does.

What it does ask for is what no gate can work out on its own — what the change
does, **why** the current behaviour was wrong, and whether the change is a
*decision* that needs an ADR in `docs/decisions/`. Contracts, defaults, the
meaning of a flag and the shape of parsed output are all decisions; a bug fix
that restores documented behaviour is not.

## Code of conduct

Participation is covered by the [Code of Conduct](CODE_OF_CONDUCT.md). In short:
argue about the code as much as you like, not about the person.
