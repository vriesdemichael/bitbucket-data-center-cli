# JSON Schemas

## Per-command `--json` output schemas

**Ask the binary, not the site.** Every command answers `--describe` with the JSON Schema for
the `data` payload of its `--json` output, read from the copy compiled in:

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

Almost every command falls in the first group. Each of those schemas is derived from the
typed result the command already builds, so it cannot drift from the payload; the rest say
which of the others they are, and why.

There are no per-command schema files on this site, and there is nothing to link to instead.
A file describing a command is a second copy of a contract that `--describe` already answers
from the binary, and a copy that cannot be checked against the command is one that is wrong
sooner or later. Ask the binary.

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

## Configuration file schema

[config.schema.json](schemas/config.schema.json) describes every `bb`
configuration file — the system one, a workspace `.bb/config.yaml`, and your own.
It is what `bb doctor` checks a file against, so an editor validating from it
reports the same keys `bb doctor` would.

## IDE integration for YAML files

Add a schema comment at the top of the file, and your editor validates as you
type:

```yaml
# yaml-language-server: $schema=https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/reference/schemas/config.schema.json
policies:
  require_keyring: true
  allowed_hosts:
    - https://bitbucket.example.com
```

A relative reference works for local development, where the files sit beside
each other:

```yaml
# yaml-language-server: $schema=../reference/schemas/config.schema.json
```

The bulk policy schema is used the same way, by a bulk policy file:

```yaml
# yaml-language-server: $schema=https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/reference/schemas/bulk-policy.schema.json
apiVersion: bb.io/v1alpha1
```

## Schema usage guidance

- Use the configuration schema to author or validate a `bb` configuration file.
- Use `bb <command> --describe` to get the schema for a command's `--json` data payload.
- Use the bulk policy schema for authoring a `bb bulk` policy file, the plan schema to validate what `bb bulk plan` wrote, and the apply-status schema for `bb bulk apply` and `bb bulk status` output.

## The envelope, and the failure envelope

`--describe` answers at one level: the `data` payload a command returns. It
does not describe the envelope around it, so it cannot on its own validate a
whole `--json` document. That envelope is the same for every command, so its
parts are published once rather than repeated in each schema.

Two things `--describe` does not cover. Under `--dry-run` a command that changes
something answers with a preview rather than its normal `data`, and `--describe`
still returns the normal schema. And only `bb bulk`'s whole documents are published
as schemas; for every other command, validate `data` against `--describe` and the
rest against the parts below. Describing every document whole changes the shape of
output that exists today, so it is planned for the next major release
([#616](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/616)).

- [`output/output.error.schema.json`](schemas/output/output.error.schema.json)
  is the failure envelope. It carries the full `error.kind` vocabulary and the
  set of exit codes, so a consumer can branch on a failure from a command it has
  never seen without provoking one first. Which code each kind maps to is in
  [Machine Mode and Diagnostics](../advanced/machine-mode-diagnostics.md#error-kinds-and-exit-codes).
- `meta` is described there too: `meta.bbVersion`, and `meta.limitReached`,
  which says whether a listing was capped by `--limit`. `meta` is open: it may
  gain fields in a minor release, so validate the fields you use rather than
  rejecting ones you do not know. Every command that takes `--limit` emits
  `limitReached`, true or false; other commands omit it.

A success document carries `data` and no `error`; a failure carries `error` and
no `data`. Which key is present is how a consumer tells them apart, and that is
why neither is ever null (ADR-046).
