# Agent Instructions — bitbucket-server-cli

## Quality Coverage Artifacts

`docs/quality/coverage-report.json`, `docs/quality/coverage.combined.raw.out`, and `docs/quality/coverage.combined.scoped.out` are regenerated committed artifacts.

**The conflict rule:** these files are almost always modified on both a feature branch and on `main` (because other PRs also update them after merging). They will conflict on every rebase.

### When rebasing onto origin/main

1. Run `git rebase origin/main` (use `--strategy-option=theirs` to auto-resolve conflicts in favour of the branch version, or resolve manually).
2. After the rebase succeeds, **always regenerate** the quality artifacts — do not keep the pre-rebase version, because the patch baseline (`origin/main`) has changed:
   ```bash
   go test -covermode=count -coverprofile=.tmp/coverage.unit.out \
       ./cmd/... ./internal/... ./tools/... -count=1
   go test -tags=live -covermode=count \
       -coverpkg=./cmd/...,./internal/...,./tools/... \
       -coverprofile=.tmp/coverage.live.out \
       ./tests/integration/live -timeout 300s
   go run ./tools/quality-report \
       -coverprofile .tmp/coverage.unit.out \
       -live-coverprofile .tmp/coverage.live.out \
       -base-ref origin/main \
       -manifest docs/quality/generated-operation-contracts.json \
       -report-file docs/quality/coverage-report.json \
       -raw-coverprofile-file docs/quality/coverage.combined.raw.out \
       -scoped-coverprofile-file docs/quality/coverage.combined.scoped.out \
       -write-report -write-coverprofiles
   ```
3. Stage the regenerated files and **amend the existing quality commit** (rather than adding a new one):
   ```bash
   git add docs/quality/
   git commit --no-verify --amend --no-edit
   git push --no-verify --force-with-lease
   ```

### OpenAPI spec coverage artifact

`docs/quality/spec-coverage.json` is a separate committed artifact that does **not** depend on coverage profiles or live tests. If you change the OpenAPI spec, the generated client, or how `internal/services` calls the API, regenerate it and commit the result:

```bash
task quality:spec-coverage:update
git add docs/quality/spec-coverage.json
```

CI verifies it via `task quality:spec-coverage:verify` (CI-safe, no live infra).

### When running tests also uncovers a broken test

If the rebase brought in API changes from `main` (e.g. a command's flag changed from `--host` to a positional argument), tests added on the branch may need updating. Fix them in the same amend so history stays clean.

## Development Tips & Gotchas

### Stateful Dry-Run Interceptor
Bitbucket server-mutating CLI commands (ending in words like `create`, `update`, `delete`, `add`, etc.) are intercepted by the global dry-run interceptor (`internal/cli/dryrun.go`). Any new mutating command must be registered in the `dryRunProfiles` map as `Stateful: true` (or `Stateful: false` if it has stateless behaviour). Failing to do so will result in a "dry-run is not implemented" error when `--dry-run` is supplied.

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

