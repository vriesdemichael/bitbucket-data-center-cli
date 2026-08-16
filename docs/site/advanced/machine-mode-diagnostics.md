# Machine Mode and Diagnostics

## Machine mode contract

Use global `--json` for machine-consumable output.

Envelope shape:

```json
{
  "version": "v2",
  "data": {},
  "meta": {
    "contract": "bb.machine"
  }
}
```

`data` holds command-specific payloads. Additive fields are allowed in `v2`; breaking changes require versioning.

### Failure envelope

When a command fails while `--json` is set, stdout carries an `error` object where `data` would be:

```json
{
  "version": "v2",
  "error": {
    "kind": "validation",
    "message": "no Bitbucket host configured: set BITBUCKET_URL or run 'bb auth login <host>'",
    "exit_code": 2
  },
  "meta": {
    "contract": "bb.machine"
  }
}
```

**Which key is present tells you the outcome:** `data` on success, `error` on failure. Never both. This stays unambiguous for a command whose successful `data` is legitimately `null`.

`kind` and `exit_code` come from the taxonomy below, so you can branch on either without parsing `message`. `exit_code` always matches the process exit status.

The human-readable line still goes to stderr, exactly as it does without `--json`.

This applies to usage errors too — an unknown flag or command produces an envelope, not just a bare string:

```bash
bb --json repo view --nonexistent-flag
```

The published schema is
[`output.error.schema.json`](../reference/schemas/output/output.error.schema.json), and it is the
same document for every command.

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
- `permanent` and `internal` (or unknown) -> exit code `1`

Example failure behavior:

```bash
bb repo view --repo BADFORMAT
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

```bash
bb --json repo view --nonexistent-flag
```

```json
{
  "version": "v2",
  "error": {
    "kind": "validation",
    "message": "unknown flag: --nonexistent-flag",
    "exit_code": 2
  },
  "meta": { "contract": "bb.machine" }
}
```

This matters for automation: `internal` means *the CLI broke*, and a caller that retries or
escalates on it would do the wrong thing with its own typo. Genuine failures — a refused
connection, an unexpected server response — still report `internal` and exit `1`.
