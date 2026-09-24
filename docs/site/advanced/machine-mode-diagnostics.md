# Machine Mode and Diagnostics

## Machine mode contract

Use global `--json` for machine-consumable output, or `--yaml` for the same document as YAML.
The two differ only in the encoding: the members, the exit code and the line on stderr are
the same, and passing both is a validation error.

Envelope shape:

<!-- docs-lint: envelope-shape -->
```json
{
  "data": {},
  "meta": {
    "bbVersion": "[[ bb_version_tag ]]"
  }
}
```

`data` holds command-specific payloads. `meta.bbVersion` reports which binary produced the
document -- provenance for stored output, not a compatibility switch -- and `meta.command`
names the command that wrote it, so a document held on its own says which `--describe`
describes it.

The flags choose the member, and nothing that happens during the run changes it: `data`, or
`error` when the run fails; under `--dry-run`, `preview`, a verdict on the real run (see
[Dry-Run Planning](dry-run-planning.md)).

There is no contract version. Adding a field to `data` is additive; removing or renaming one,
changing its type, or changing whether it can be null is a breaking change that cuts a new
major release ([ADR-064](../adr/064-machine-output-carries-no-contract-version.md)). Pin the
binary version to pin the contract.

### Failure envelope

When a command fails while `--json` is set, stdout carries an `error` object where `data` would be:

<!-- docs-lint: envelope-shape -->
```json
{
  "error": {
    "kind": "validation",
    "message": "no Bitbucket host configured: set BITBUCKET_URL or run 'bb auth login <host>'",
    "exitCode": 2
  },
  "meta": {
    "bbVersion": "[[ bb_version_tag ]]"
  }
}
```

**Which key is present tells you the outcome:** `data` on success, `error` on failure. Never both. This stays unambiguous for a command whose successful `data` is legitimately `null`.

`kind` and `exitCode` come from the taxonomy below, so you can branch on either without parsing `message`. `exitCode` always matches the process exit status.

The human-readable line still goes to stderr, exactly as it does without `--json`.

This applies to usage errors too — an unknown flag or command produces an envelope, not just a bare string:

<!-- docs-lint: expect-invalid -->
```bash
bb --json repo list --nonexistent-flag
```

The failure envelope has the same shape for every command, so there is one schema for it rather
than one per command.

## Diagnostics behavior

- Diagnostics are emitted to `stderr` to preserve `stdout` contracts.
- Use `--log-format jsonl` for machine-filterable diagnostics.
- Use `--log-level` to tune verbosity (`error`, `warn`, `info`, `debug`).
- Sensitive values are redacted from diagnostic output.

Example:

```bash
bb --json --log-level warn --log-format jsonl auth status 2> diagnostics.jsonl
```

## Recommended scripting pattern

1. Use `--json` and parse only the `data` payload needed for automation.
2. Branch on the `error` key, or on the exit code, before reading `data`.
3. Keep diagnostics in separate stderr capture.
4. Validate `data` against the schema `bb <command> --describe` prints when integrating with CI.

```bash
if output=$(bb --json pr get 42 2>/dev/null); then
  echo "$output" | jq -r '.data.title'
else
  echo "$output" | jq -r '"\(.error.kind): \(.error.message)"'
fi
```

## Listings that stop at `--limit`

A command that lists something returns at most `--limit` results, and says whether
it reached that limit. `bb --json repo list --limit 1`, on a server holding more than
one repository:

<!-- docs-lint: output-of bb repo list -->
```json
{
  "data": [
    { "projectKey": "PAY", "slug": "payments", "name": "Payments", "public": false }
  ],
  "meta": { "bbVersion": "[[ bb_version_tag ]]", "limitReached": true }
}
```

`limitReached: true` means there may be more: raise `--limit`, or pass `--all`. It is
present, true or false, on every command that takes `--limit`, and absent elsewhere.
Text output gives the same answer as a line on stderr, so a pipeline counting rows still
counts rows, and the MCP list tools return it as `limit_reached`.

## Responses that are not text

`bb api` returns whatever the endpoint answers, and a JSON string cannot carry
arbitrary bytes. Under `--json` a response body that is not text goes into `data`
as base64, and `meta` says so: `meta.encoding` is `base64` and `meta.contentType`
names the body's type. JSON and other text arrive as before, with no
`meta.encoding`. A body larger than 64 MiB is refused under `--json`; without it,
`bb api` writes the bytes as they arrive. An answer with no body, such as a `204`
to a `DELETE`, is `data: null`.

## Error kinds and exit codes

Command failures use deterministic exit codes by error kind.

- `validation` -> exit code `2` (includes unknown flags and commands)
- `authentication` or `authorization` -> exit code `3`
- `not_found` -> exit code `4`
- `conflict` -> exit code `5`
- `transient` -> exit code `10`
- `not_implemented` -> exit code `11`
- `cancelled` -> exit code `12` (interrupted; not something to retry automatically)
- `unknown_outcome` -> exit code `13` (the request was sent and whether it was applied is unknown)
- `unsupported` -> exit code `14` (the Bitbucket instance's version cannot do it; a newer one can)
- `permanent` and `internal` (or unknown) -> exit code `1`

`unknown_outcome` is the one worth wiring into a script deliberately. It means bb
cannot say whether the server applied the request: a mutation bb does not send twice,
such as a `POST`, whose connection was lost, timed out or was interrupted after it was
sent, that a gateway answered with `502` or `504`, or that Bitbucket answered with `500`
or with an error it raised while writing the answer. Retrying it may repeat work that
already happened, so the answer is to check the state and then decide. That is why it
sits outside `transient`, which is the code a retry loop should key on. A `GET`, `PUT`
or `DELETE` in the same position is idempotent: bb sends it again itself, and reports
`transient` when every attempt fails, or `cancelled` when it was interrupted.

One command reports what it found through the exit status as well as what happened to
it: `bb pr checks` (and `bb pr build status`), as `gh pr checks` does. Without `--json`
it exits `1` when a build failed and `8` when none failed and one is still running or has
no result. Neither is a failure of bb's: the output is complete, and a line on stderr counts
the builds. With `--json` it exits `0` and each build's state is in `data`
([ADR-091](../adr/091-a-command-may-report-the-state-it-read-through-its-exit-status.md)).

`bb webhook create` and `bb project webhook create` make that check themselves: when the
webhook they asked for is there, they report it and exit `0`.

A failure retrying cannot fix is `permanent`: a rejected TLS certificate, a host that
does not resolve. bb does not retry those itself either.

### Handles on the failure envelope

A failure may carry an optional `error.details` object: a flat map of strings naming what you
need to act on it. It is absent when there is nothing to carry. Read handles from
`error.details`, not by parsing `error.message`.

A failure Bitbucket answered carries `upstreamStatus`, the HTTP status, and
`upstreamException` when Bitbucket named its exception. The exception name is the stable
part: branch on it rather than on the wording of `error.message`, which Bitbucket
rewords between releases.

For such a failure, `error.message` is Bitbucket's own message, redacted and cut to 300
characters unless `--full-error-body` is passed.

Example failure behavior:

```bash
bb tag list --repo BADFORMAT
echo $?
```

```text
validation: invalid repository selector (expected PROJECT/slug)
2
```

Under `--json`, the same failure additionally produces the envelope shown above on stdout.

### Malformed invocations

An unknown flag, unknown command, bad flag value or wrong argument count is the caller's mistake,
and reports `validation` with exit code `2` — the same as any other input the CLI rejects:

<!-- docs-lint: expect-invalid -->
```bash
bb --json repo list --nonexistent-flag
```

<!-- docs-lint: envelope-shape -->
```json
{
  "error": {
    "kind": "validation",
    "message": "unknown flag: --nonexistent-flag",
    "exitCode": 2
  },
  "meta": { "bbVersion": "[[ bb_version_tag ]]" }
}
```

This matters for automation: `internal` means *the CLI broke*, and a caller that retries or
escalates on it would do the wrong thing with its own typo. A failure bb did not cause keeps
the kind it was classified as: a refused connection is `transient`, a rejected certificate
`permanent`, and a server response whatever its status maps to.
