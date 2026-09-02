# Machine Mode and Diagnostics

## Machine mode contract

Use global `--json` for machine-consumable output.

Envelope shape:

```json
{
  "data": {},
  "meta": {
    "bbVersion": "v4.0.0"
  }
}
```

`data` holds command-specific payloads. `meta.bbVersion` reports which binary produced the
document -- provenance for stored output, not a compatibility switch.

There is no contract version. Adding a field to `data` is additive; removing or renaming one,
changing its type, or changing whether it can be null is a breaking change that cuts a new
major release ([ADR-064](../adr/064-machine-output-carries-no-contract-version.md)). Pin the
binary version to pin the contract.

### Failure envelope

When a command fails while `--json` is set, stdout carries an `error` object where `data` would be:

```json
{
  "error": {
    "kind": "validation",
    "message": "no Bitbucket host configured: set BITBUCKET_URL or run 'bb auth login <host>'",
    "exitCode": 2
  },
  "meta": {
    "bbVersion": "v4.0.0"
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
4. Validate bulk artifacts against published schemas when integrating with CI.

```bash
if output=$(bb --json pr get 42 2>/dev/null); then
  echo "$output" | jq -r '.data.title'
else
  echo "$output" | jq -r '"\(.error.kind): \(.error.message)"'
fi
```

## Error kinds and exit codes

Command failures use deterministic exit codes by error kind.

- `validation` -> exit code `2` (includes unknown flags and commands)
- `authentication` or `authorization` -> exit code `3`
- `not_found` -> exit code `4`
- `conflict` -> exit code `5`
- `transient` -> exit code `10`
- `not_implemented` -> exit code `11`
- `cancelled` -> exit code `12` (interrupted or timed out; not something to retry automatically)
- `permanent` and `internal` (or unknown) -> exit code `1`

### Handles on the failure envelope

A failure may carry an optional `error.details` object: a flat map of strings naming what you
need to act on it. It is absent when there is nothing to carry.

`bb bulk apply` sets `operationId` there, because on the failure path the error envelope is
the only document written (ADR-075) and the status artifact is reached by id:

```bash
operationId=$(bb bulk apply --from-plan plan.json --json | jq -r '.error.details.operationId // empty')
bb bulk status "$operationId" --json
```

Read handles from `error.details`, not by parsing `error.message`.

#### Changed in v4: `bb bulk apply --json` on the failure path

Through v3, a `bb bulk apply --json` run that failed printed **two** documents: the status
envelope, then the failure envelope. A strict JSON parser rejects that outright; `jq` reads a
value stream, so it prints a result per document and exits `0` — meaning a pipeline that took
the last line silently read the wrong one.

From v4 a failing or cancelled run writes only the failure envelope, so `.data` is absent.
Scripts that read the artifact from `bb bulk apply` output must fetch it by id instead:

```bash
# Through v3 — no longer returns the artifact
bb bulk apply --from-plan plan.json --json | jq -r '.data.summary.failedTargets'

# v4
output=$(bb bulk apply --from-plan plan.json --json) || true
operationId=$(printf '%s' "$output" | jq -r '.error.details.operationId // empty')
bb bulk status "$operationId" --json | jq -r '.data.summary.failedTargets'
```

Human output is unchanged: the status still goes to stdout and the error line to stderr.

Example failure behavior:

```bash
bb tag list --repo BADFORMAT
echo $?
```

```text
validation: --repo must be in PROJECT/slug format
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

```json
{
  "error": {
    "kind": "validation",
    "message": "unknown flag: --nonexistent-flag",
    "exitCode": 2
  },
  "meta": { "bbVersion": "v4.0.0" }
}
```

This matters for automation: `internal` means *the CLI broke*, and a caller that retries or
escalates on it would do the wrong thing with its own typo. Genuine failures — a refused
connection, an unexpected server response — still report `internal` and exit `1`.
