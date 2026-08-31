# JSON Schemas

## Per-command `--json` output schemas

Every data-returning command that supports `--json` has a published JSON Schema describing its full
`bb.machine v2` envelope (the `version`, `data`, and `meta` fields).  Use these schemas to:

- Validate `bb` output in scripts without running the binary.
- Enable IDE auto-completion for piped JSON in tooling that honours `$schema`.
- Detect breaking changes to output contracts via schema diff.

Schemas are named `output.<command-path>.schema.json` and published under:

```
https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/reference/schemas/output/
```

Each published snapshot identifies itself by the release it belongs to: the copy served
under `latest/` carries the `$id` of whichever version `latest` currently points at.  Pin
to a version directory — `.../v3.4.0/reference/schemas/output/` — when you need a contract
that cannot change under you.

### Auth command output schemas

| Schema file | Command |
|---|---|
| [output.auth.status.schema.json](schemas/output/output.auth.status.schema.json) | `bb auth status --json` |
| [output.auth.login.schema.json](schemas/output/output.auth.login.schema.json) | `bb auth login --json` |
| [output.auth.identity.schema.json](schemas/output/output.auth.identity.schema.json) | `bb auth identity --json` |
| [output.auth.token-url.schema.json](schemas/output/output.auth.token-url.schema.json) | `bb auth token-url --json` |
| [output.auth.logout.schema.json](schemas/output/output.auth.logout.schema.json) | `bb auth logout --json` |
| [output.auth.server.list.schema.json](schemas/output/output.auth.server.list.schema.json) | `bb auth server list --json` |
| [output.auth.server.use.schema.json](schemas/output/output.auth.server.use.schema.json) | `bb auth server use --json` |

### Tag command output schemas

| Schema file | Command |
|---|---|
| [output.tag.list.schema.json](schemas/output/output.tag.list.schema.json) | `bb tag list --json` |
| [output.tag.view.schema.json](schemas/output/output.tag.view.schema.json) | `bb tag view --json` |
| [output.tag.create.schema.json](schemas/output/output.tag.create.schema.json) | `bb tag create --json` |
| [output.tag.delete.schema.json](schemas/output/output.tag.delete.schema.json) | `bb tag delete --json` |

### Repository command output schemas

| Schema file | Command |
|---|---|
| [output.repo.list.schema.json](schemas/output/output.repo.list.schema.json) | `bb repo list --json` |

### Commit command output schemas

| Schema file | Command |
|---|---|
| [output.commit.list.schema.json](schemas/output/output.commit.list.schema.json) | `bb commit list --json` |
| [output.commit.get.schema.json](schemas/output/output.commit.get.schema.json) | `bb commit get --json` |

### Branch command output schemas

| Schema file | Command |
|---|---|
| [output.branch.create.schema.json](schemas/output/output.branch.create.schema.json) | `bb branch create --json` |
| [output.branch.delete.schema.json](schemas/output/output.branch.delete.schema.json) | `bb branch delete --json` |
| [output.branch.get-default.schema.json](schemas/output/output.branch.get-default.schema.json) | `bb branch get-default --json` |
| [output.branch.set-default.schema.json](schemas/output/output.branch.set-default.schema.json) | `bb branch set-default --json` |

### Pull request command output schemas

| Schema file | Command |
|---|---|
| [output.pr.get.schema.json](schemas/output/output.pr.get.schema.json) | `bb pr get --json` |
| [output.pr.comment.list.schema.json](schemas/output/output.pr.comment.list.schema.json) | `bb pr comment list --json` |

`bb pr get` carries a `review_summary` describing outstanding review feedback, and
`bb pr comment list` returns comment threads and tasks together. Two details matter when consuming
them:

- **A missing count means "not measured", not zero.** The count fields are omitted when the source
  that would have produced them was unavailable. Check `review_summary.counts_source` before
  treating an absent count as "nothing outstanding".
- **`open_tasks` is a subset of `unresolved_threads`,** not a separate bucket, so the two must not
  be added together. The same holds for `summary.open_tasks` and `summary.unresolved`.

`bb pr comment list --full` emits the raw Bitbucket comment objects under `comments` instead of the
`summary`/`threads` pair; the schema models the two as mutually exclusive.

### Bulk workflow output schemas

| Schema file | Command |
|---|---|
| [output.bulk.plan.schema.json](schemas/output/output.bulk.plan.schema.json) | `bb bulk plan --json` |
| [output.bulk.apply.schema.json](schemas/output/output.bulk.apply.schema.json) | `bb bulk apply --json` |
| [output.bulk.status.schema.json](schemas/output/output.bulk.status.schema.json) | `bb bulk status --json` |

Output schema source-of-truth is in `internal/cli/outputschemas/` and `internal/cli/cmd/*/schema.go`.

---

## Bulk workflow artifact schemas

The project also publishes JSON schemas for the bulk workflow's standalone plan and policy
artifacts, which are read and written as files independent of `--json` output.

- [bulk-policy.schema.json](schemas/bulk-policy.schema.json)
- [bulk-plan.schema.json](schemas/bulk-plan.schema.json)
- [bulk-apply-status.schema.json](schemas/bulk-apply-status.schema.json)

Schema source-of-truth is generated from Go workflow models in `internal/workflows/bulk/schema.go`.

## Regenerate schemas

```bash
task docs:export-bulk-schemas
task docs:publish-bulk-schemas
task docs:export-output-schemas
task docs:publish-output-schemas
```

or regenerate all docs artifacts:

```bash
task docs:generate
```

## IDE integration for YAML policy files

Add a schema comment at the top of a bulk policy YAML file:

```yaml
# yaml-language-server: $schema=https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/reference/schemas/bulk-policy.schema.json
apiVersion: bb.io/v1alpha1
selector:
  projectKey: TEST
operations:
  - type: repo.permission.user.grant
    username: ci-bot
    permission: REPO_WRITE
```

Equivalent repository-relative schema association is also valid for local development:

```yaml
# yaml-language-server: $schema=../reference/schemas/bulk-policy.schema.json
```

## Schema usage guidance

- Use policy schema for authoring bulk policy YAML/JSON input files.
- Use plan schema to validate reviewed plan artifacts produced by `bb bulk plan`.
- Use apply-status schema to validate outputs from `bb bulk apply` and `bb bulk status`.
- Use `output.*` schemas to validate `--json` output captured from any data-returning command.
