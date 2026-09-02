# JSON Schemas

## Per-command `--json` output schemas

**Ask the binary, not the site.** Every command answers `--describe` with the JSON Schema for
its own `--json` output, read from the copy compiled in:

```bash
bb pr get --describe
bb pr get --describe --json    # wrapped in the standard envelope
```

This needs no network, no configuration and no arguments — asking what a command returns does
not require knowing what it takes. It also cannot disagree with the binary that printed it,
which is the failure mode a published file has: the site serves whichever release `latest`
points at, and that may not be what is installed.

The payload has a fixed shape — `command`, `described`, and then either `schema` or `reason`.
Check `described` first:

- `"described": true` — `schema` is the contract for that command.
- `"described": false`, reason mentioning **no output schema yet** — the shape is real but not
  guaranteed. Parse defensively.
- `"described": false`, reason mentioning **no data payload** — `bb api` streams the upstream
  body, `bb ai skill show` prints a document. No schema is coming.
- `"described": false`, reason mentioning **no shape bb can promise** — the command forwards
  what Bitbucket sent without reading a field, so the envelope is guaranteed and its contents
  are not.

Almost every command is now in the first group. Each of those schemas is derived from the
typed result the command already builds, so it cannot drift from the payload; the rest say
which of the others they are, and why.

Per-command schema *files* are no longer published. They were hand-maintained, drifted from the
commands they described — two named a `branch get-default` subcommand that has never existed —
and nothing consumed them that `--describe` does not serve better.

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
- Use `bb <command> --describe` to get the schema for a command's `--json` output.
