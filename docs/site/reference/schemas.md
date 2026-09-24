# JSON Schemas

## What a command returns

**Ask the binary, not the site.** Every command answers `--describe` with what its `--json`
output looks like, read from the copy compiled in:

```bash
bb pr get --describe          # an outline of the fields, for a person
bb pr get --describe --json   # the JSON Schemas, in the description member
```

This needs no network, no configuration and no arguments — asking what a command returns does
not require knowing what it takes. It also cannot disagree with the binary that printed it,
which is the failure mode a published file has: the site serves whichever release `latest`
points at, and that may not be what is installed.

Under `--json` or `--yaml` the answer is the document's `description` member:

| Field | What it holds |
|---|---|
| `run.outputSchema` | the JSON Schema of the whole document a run writes: `data` and `meta`, or `error` and `meta` |
| `run.reason` | why a command has no schema, or why its `data` promises no shape |
| `dryRun.behaviour` | what `--dry-run` does for it: `runs` (it only reads), `verifies` or `predicts` |
| `dryRun.tier` | how far that verdict can be trusted |
| `dryRun.outputSchema` | the JSON Schema of the document `--dry-run` writes |

`dryRun` is absent for a command that does not take the flag. Almost every command has a
`run.outputSchema`, derived from the typed result it already builds, so it cannot drift from
the payload. The rest say why in `run.reason`: `bb api` and `bb ai skill show` write no
document of their own, and a few commands forward what Bitbucket sent without reading a field,
so the document around their `data` is described and `data` itself is left open.

`bb <group> --describe`, and `bb --describe` itself, list the commands beneath with what
`--dry-run` does for each, so one call covers the whole tool. A path that names no command
answers with `description.error`.

There are no per-command schema files on this site, and there is nothing to link to instead.
A file describing a command is a second copy of a contract that `--describe` already answers
from the binary, and a copy that cannot be checked against the command is one that is wrong
sooner or later. Ask the binary.

---

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

## Schema usage guidance

- Use the configuration schema to author or validate a `bb` configuration file.
- Use `bb <command> --describe --json` for the JSON Schema of a command's `--json` document, for a run and for `--dry-run`.

## The failure envelope

`--describe --json` describes each command's whole document. The failure envelope is the same
for every command, so it is also published once:

- [`output/output.error.schema.json`](schemas/output/output.error.schema.json)
  is the failure envelope. It carries the full `error.kind` vocabulary and the
  set of exit codes, so a consumer can branch on a failure from a command it has
  never seen without provoking one first. Which code each kind maps to is in
  [Machine Mode and Diagnostics](../advanced/machine-mode-diagnostics.md#error-kinds-and-exit-codes).
- `meta` is described there too: `meta.command`, `meta.bbVersion`, and
  `meta.limitReached`, which says whether a listing was capped by `--limit`.
  `meta` is open: it may gain fields in a minor release, so validate the fields
  you use rather than rejecting ones you do not know. Every command that takes
  `--limit` emits `limitReached`, true or false; other commands omit it.

A document carries exactly one of `data`, `error`, `preview` and `description`, and the
flags choose which (ADR-096): `data` or `error` for a run, `preview` under `--dry-run`,
`description` under `--describe`. Which key is present is how a consumer tells them apart,
and that is why none is ever null.
