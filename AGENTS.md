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
