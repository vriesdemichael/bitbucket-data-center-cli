# Agent Instructions — bitbucket-server-cli

## Quality artifacts

Coverage **measurements** are never committed. Coverage profiles and the combined report are written
to `.tmp/`, and CI recomputes them on every pull request, publishing them to Codecov and as workflow
artifacts. `coverage.out`, `docs/quality/coverage-report.json` and
`docs/quality/coverage.combined.*.out` are gitignored.

Rebasing therefore needs nothing special:

```bash
task pr:rebase          # or: git rebase origin/main
```

Earlier versions of this file described a procedure for regenerating and amending committed coverage
artifacts after every rebase. That is gone. The artifacts existed because the live suite needed a
licensed Bitbucket that CI could not provide, so a developer's machine was the only place combined
coverage could be produced. CI now provisions its own instance (ADR-043), and the committed copies
turned out to be ~5.7MB that conflicted on every rebase and that no gate ever read. See ADR-045.

Coverage **baselines** are still committed, because they are contracts rather than measurements:

| File | Asserts | Regenerate with |
|---|---|---|
| `docs/quality/command-reach.json` | which CLI commands the live suite proves work | `task quality:command-reach:update` |
| `docs/quality/spec-coverage.json` | which OpenAPI operations are exercised | `task quality:spec-coverage:update` |

These are small, readable in a diff, and verified by static analysis needing no Bitbucket instance.
A diff in them is the point: it is how a reviewer sees that a command lost live coverage.

`generated-operation-contracts.json` used to sit here. It was deleted with the contract-coverage
metric it fed (ADR-065): a hand-written map of operation to test file, verified by nothing, feeding
a percentage measured against a threshold of zero.

Run `task quality:verify` for every gate that needs no Bitbucket instance, and
`task quality:coverage` for the full coverage gate when you want it locally. The latter
needs the stack up and takes about eight minutes; CI runs it on every pull request regardless.

### Iterating on patch coverage

**Do not re-run the suite to re-check the number.** `task quality:coverage:replay` re-applies every
threshold against the profiles already in `.tmp/` and finishes in seconds. The loop when patch
coverage fails is: add tests → `task test:unit:coverage` (~1 min) → replay. Only a change that
alters behaviour the live suite exercises needs `task test:live:coverage` again.

A failing patch gate prints the uncovered locations, so there is no need to read a profile by hand:

```
FAIL: patch coverage 72.63% is below required 85.00% (394 coverable lines >= 30)

Uncovered changed lines (108):
  internal/cli/cmd/auth/gitcredential.go:52-53,68-70,128-130
  internal/config/config.go:754-756
```

Fix the gap by adding tests. Lowering `COVERAGE_MIN_PATCH` in
`.github/coverage-thresholds.env` is not the remedy, and a reviewer will treat it as one to justify.

**`git add` new files before measuring.** The gate diffs against the merge base with `git diff`,
which does not see untracked files — a new `.go` file is simply absent from the patch, so a local
run reports a healthy percentage over the lines it can see and CI, where everything is committed,
reports a lower one. If the changed-line count looks too small for the change, that is why.

### `tools/` is outside the gate and still gets tests

The coverage scope is `cmd/` + `internal/`. Nothing under `tools/` is measured, so a change there
reports "no coverable changed lines" and passes regardless. **That is not permission to skip tests.**

Write table-driven unit tests for tool logic in the same change that introduces it: parsers,
tokenisers, path resolution, diff and coverage arithmetic — anything with branches worth getting
wrong. Skip `main()`, flag registration and `os.Exit` plumbing; a test there proves nothing.

The distinction worth applying: a missing flag or a bad path fails loudly on the next run, so a test
adds little. Arithmetic and parsing fail *quietly and wrongly*. `tools/quality-report` is the
extreme case — it produces the numbers every other gate reads, so a bug there makes the build pass
when it should not.

Do not add `tools/` to `-scope-include`, and do not lower a threshold to accommodate a tool change.
ADR-049 records the measurements behind that line.
### Line endings are LF, enforced

`.gitattributes` pins every file to LF in the repository and in every working tree, overriding
whatever `core.autocrlf` a contributor has set. `task quality:verify` fails if any committed file
contains a carriage return, and if the Go tree is not gofmt-clean.

Do not add a `-text` or `eol=crlf` exemption to `.gitattributes` to work around a tool. That is the
one way CR can still reach the index, and it is what the line-ending gate exists to catch. Fix the
tool instead — a parser that chokes on `\r` should normalise its input.

If a `git status` shows files as modified but `git diff` is empty, the index has stale stat data
after a bulk rewrite; `git update-index --refresh` settles it. Nothing is actually different.

### Tests must not reconfigure the repository they run in

Every package whose tests start a git process installs the guard, as one line:

```go
func TestMain(m *testing.M) { gittest.Guard(m) }
```

`gittest.Guard` does two things. It puts this repository out of git's reach for the run, by setting
a ceiling at its root, so a command that would find it by searching upward fails with "not inside a
git repository" instead of succeeding against your checkout — which is the shape both recorded
incidents took. And it snapshots the repository-scoped configuration before the tests and compares
it afterwards, failing the package on any change and naming the exact keys. A repository a test
creates and addresses directly is unaffected, because a ceiling only stops an upward search.

The set of packages that need it is computed rather than listed here; this paragraph named three of
them when there were four.

This is not hypothetical. `Backend.Clone` persists `http.extraHeader` into the repository it clones
into so later fetches carry authentication. A test that pointed it at the working copy instead of a
temporary directory wrote this into the project's own `.git/config`:

```
http.extraheader    = Authorization: Basic <base64 of dummy-user:dummy-password>
user.name           = Test User
user.email          = test@example.local
remote.upstream.url = https://example.local/scm/PRJ/upstream.git
```

An unscoped `http.extraHeader` is attached to every HTTP request git makes, and an explicit
`Authorization` header beats any credential helper, so every push to GitHub sent
`dummy-user:dummy-password` and was rejected with *"Password authentication is not supported for Git
operations"* — a message that reads like a bad token and sends you hunting in the wrong place. The
identity override meanwhile authored real commits as `Test User <test@example.local>`.

**Any test that shells out to git must operate on a directory it created**, normally `t.TempDir()`.
If the guard fires, look for a git invocation missing `-C` or a helper defaulting to the current
directory. It reports rather than repairs; undo damage with `git config --local --unset <key>`.

The `--local` scope is not private to your worktree. In a linked worktree it resolves to
`$GIT_COMMON_DIR/config` — the main checkout's `.git/config`, shared by all of them. A sibling
worktree running `git branch`, `git checkout -b`, `git push -u` or `git fetch` rewrites the file
the guard is watching mid-run. That surfaced as several guarded packages failing at once with no
`--- FAIL` line, and passing on a re-run. Git's own bookkeeping for those operations —
`branch.*`, `remote.*.fetch`, `lfs.*` — is excluded from the shared scope for that reason, and
only from the shared scope. Everything else still fails, so **if the guard names a key, believe
it**: it is not the worktrees.

The cost is that a test writing `branch.*` into this repository is no longer caught. Do not widen
the exclusion to buy quiet; if a key you think is noise fires, it came from a test.

### Documented commands are parsed, not just written

`task docs:lint` parses every `bb ...` line in a ```` ```bash ```` block against the real Cobra
command tree. It runs inside `task quality:verify`, so the pre-push hook and CI both cover it.

Write examples in shell-tagged blocks. An untagged or `text`-tagged block is invisible to the
linter — that avoids the check rather than passing it. When an example is meant to be invalid, mark
the block with `<!-- docs-lint: expect-invalid -->`, which inverts the check so the block also fails
if the command later becomes valid. Do not add an unconditional ignore.

If the linter flags something you believe is correct, suspect a trailing carriage return before
suspecting the documentation: on a CRLF checkout `\r` ends up inside the last token and pflag
reports it as an unknown flag, with nothing visible in the message to say so. See ADR-048.

### command reach artifact

`docs/quality/command-reach.json` records which CLI commands the live suite actually proves work
against a real Bitbucket. CI verifies it via `task quality:command-reach:verify` (static analysis, no liveinfra needed — it is static analysis of the Cobra tree and the live test sources).

It fails when:

- a command that used to be covered loses its live coverage,
- a new command arrives with no live test invoking it, or
- a command becomes **masked** — its only live coverage comes from a test that calls `t.Skip` when the
  call fails, so the suite passes whether or not the command works.

That last case is the one that matters. `bb pr task *` called an endpoint Atlassian removed in
Bitbucket 8.0, and the live tests hid it behind
`if strings.Contains(err.Error(), "not_found") { t.Skipf(...) }` — CI stayed green for years. A skipped
test is not a passing test. Fix the command or the test; do not add a skip.

When you add a command, add a live test that runs it and asserts, then:

```bash
task quality:command-reach:update
git add docs/quality/command-reach.json
```

### OpenAPI spec coverage artifact

`docs/quality/spec-coverage.json` is a separate committed artifact that does **not** depend on coverage profiles or live tests. If you change the OpenAPI spec, the generated client, or how `internal/services` calls the API, regenerate it and commit the result:

```bash
task quality:spec-coverage:update
git add docs/quality/spec-coverage.json
```

CI verifies it via `task quality:spec-coverage:verify` (static analysis, no live infra).

### Adding a governance test: break it first

A governance test asserts an invariant about the codebase rather than a
behaviour — every command is classified, every MCP tool has a scope rule, no
decision record names a tool that does not exist. They are the strongest thing
here, and their failure mode is silent: one that has stopped guarding still runs,
still passes, and still occupies the slot.

Before you add one, and before you trust one you did not write, break the thing
it guards and confirm it fails. Then write down the invariant, what breaks it,
and that you saw it fail.

Two have already been found tautological this way. `TestAllMutatingCommandsHaveDryRunProfile`
asked whether every mutating command was in `dryRunProfiles` while defining
"mutating" as "in `dryRunProfiles`", so it could not fail; it is now
`TestCommandVerbsAgreeWithTheirDryRunClassification`, which compares the
classification against the command name — a signal from outside the registry.
`TestAllRunnableCommandsDeclareArgsPolicy` looked tautological for the same
reason, because `enforceNoArgsDefaults` fills in a missing policy immediately
before the test reads it; it turned out to guard the narrower case the enforcer
skips, a command whose `Use` carries a positional placeholder. The sabotage is
what told the two apart.

Where the sabotage can be expressed as a test, write it as one — see
`TestCommandVerbClassificationDetectsAMisplacedCommand` and
`TestParityComparisonDetectsDrift`. That turns "verified once" into something CI
re-verifies.

Where two classifications describe the same thing, check both directions. The
MCP tool safety flag and the tool annotations answer the same question — is this
dangerous — and were cross-checked one way only: a gated tool claiming to be
harmless failed, a tool exposed without `--yolo` while annotating itself
destructive did not. The unguarded direction was the worse one, because the
annotation is advice a client may ignore while the flag is what the server
enforces.

The governance guards, so the set is knowable:

| Guard | Invariant |
|---|---|
| `TestAllRunnableCommandsDeclareArgsPolicy` | a command with a positional placeholder declares an `Args` policy |
| `TestAllCommandsExhaustivelyClassifiedForDryRun` | every command is in exactly one dry-run category |
| `TestCommandVerbsAgreeWithTheirDryRunClassification` | the command name and the category do not contradict |
| `TestClassifyUsageErrorMatchesCobrasRealMessages` | the usage-error markers still match Cobra |
| `TestEveryMCPToolIsAccountedFor`, `TestEveryMappedCLICommandExists` | the MCP and CLI surfaces stay in step |
| `TestEveryToolHasAScopeRule` | no MCP tool escapes workspace scoping |
| `TestADRDoesNotNameToolsThatDoNotExist` | decision records do not name tools that were removed |
| `TestGatedToolsAreTheOnesThatMergeOrGate` | the `--yolo` set is exactly the tools that merge or gate |
| `TestUnsafeToolsAreAnnotatedDestructive`, `TestSafeToolsAreNotAnnotatedDestructive` | the safety flag and the annotations agree, both ways |
| `TestEveryToolHasCallArguments` | every MCP tool is actually called by a test |
| `TestEveryToolReturnsAClientCompatibleResult` | a tool result is a JSON object with a text fallback |
| `gittest` ambient config snapshot | no test reconfigures the repository it runs in |
| `TestEveryHookRunnableGateRunsOnBothSides` | every gate needing no Bitbucket runs locally and in CI |

### When running tests also uncovers a broken test

If the rebase brought in API changes from `main` (e.g. a command's flag changed from `--host` to a positional argument), tests added on the branch may need updating. Fix them in the same amend so history stays clean.

## Development Tips & Gotchas

### Stateful Dry-Run Interceptor
Bitbucket server-mutating CLI commands (ending in words like `create`, `update`, `delete`, `add`, etc.) are intercepted by the global dry-run interceptor (`internal/cli/dryrun.go`). Any new mutating command must be registered in the `dryRunProfiles` map as `Stateful: true` (or `Stateful: false` if it has stateless behaviour). For commands that are server-mutating but where dry-run does not add any operational benefit or is not supported (such as `bulk apply`), you must explicitly register them with `DryRunDoesNotAddBenefit: true`. Failing to register a mutating command will cause the unit test `TestAllMutatingCommandsHaveDryRunProfile` in `internal/cli/dryrun_test.go` to fail.

### Generating CLI Reference Documentation
The CLI command reference documentation (`docs/site/reference/commands/index.md`) is generated from Cobra command definitions. When adding commands, modifying flags, or changing help descriptions, always regenerate the documentation using:
```bash
task docs:generate
```
Verify the generated docs with:
```bash
task docs:verify-generated
```

### Mocking Stdin for CLI Prompts
When testing CLI commands that prompt the user for confirmation (e.g., typing `y` or `n`), mock the standard input (`os.Stdin`) directly using `os.Pipe()` rather than relying solely on Cobra's `InOrStdin()`. Many standard scanner functions (like `fmt.Scanln`) read directly from `os.Stdin`, bypass Cobra's stream overrides, and will block/fail if real stdin is empty.

### Go Test Caching Bypass
When testing configuration loading, validation errors, or environment variables, always use `-count=1` with `go test` to ensure that cached test results do not mask test execution or state pollution.

### Handling Missing OpenAPI Fields
The generated OpenAPI client model may sometimes omit fields (e.g., the `Id` field in `RestWebhook`). When a generated model is missing necessary fields for CLI representation or JSON output, define a custom local struct (e.g., `WebhookModel` in `internal/cli/project_webhook.go`) to correctly decode the server's response.

### Stateful Dry-Run Permission Mocking
Stateful dry-runs require verifying project/repository administrator status before proceeding (e.g., `CheckProjectAdmin`). In CLI integration tests simulating dry-run execution, ensure the mock API server registers the user permissions check endpoint (`/rest/api/latest/projects/{projectKey}/permissions/users` or similar) to prevent 404/authorization errors during dry-run validation.

### PowerShell Argument Parsing
In PowerShell, passing arguments like `-flag=value` where `value` contains forward slashes or dots (e.g., `-coverprofile=.tmp/coverage.unit.out`) can result in argument splitting. Always use space-separated syntax (e.g., `-coverprofile .tmp/coverage.unit.out`) or wrap the argument in quotes to ensure correct flag parsing.

