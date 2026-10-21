# Use bb in a CI Pipeline

A pipeline runs `bb` with nobody to answer a question, and branches on what it
returns. What that takes, in one place.

## The credential

Give the job `BITBUCKET_URL` and `BITBUCKET_TOKEN` from the CI system's secret
store. `bb` reads them from the environment and writes nothing to disk, so no
`bb auth login` is needed. On a runner that other jobs share, set
`BB_DISABLE_STORED_CONFIG=1` as well: a configuration file another job left
behind is then not read.

No flag takes a token: a value on the command line is visible to other processes
on the runner.

## Nobody to ask

With no terminal, `bb` never waits for an answer: a command missing a value
fails and names it. `--no-input` makes that explicit wherever a pipeline might
have a terminal attached. A command that changes something, such as a merge,
is not refused for want of a person; one that deletes asks for `--yes`.

## Exit codes

Every failure exits with the code of its kind, so a step can tell them apart
without reading the message:

| Exit code | Kind | What to do |
|---|---|---|
| `0` | | It went through |
| `2` | `validation` | Fix the command |
| `3` | `authentication`, `authorization` | Check the token and its rights |
| `4` | `not_found` | Check the project, repository or id |
| `5` | `conflict` | Read the state again, then decide |
| `10` | `transient` | Retry |
| `11` | `not_implemented` | Stop: it is not implemented |
| `12` | `cancelled` | It was interrupted: do not retry it automatically |
| `13` | `unknown_outcome` | Do not retry: read the state first, since the change may have been made |
| `14` | `unsupported` | This Bitbucket release cannot do it; a newer one can |
| `1` | `permanent`, `internal` | Stop |

`unknown_outcome` is the one to handle on purpose. The request was sent, and
whether Bitbucket applied it is unknown, so running it again may do it twice.
[Machine Mode and Diagnostics](../advanced/machine-mode-diagnostics.md#error-kinds-and-exit-codes)
lists every kind.

Under `--json` a step gets one document on stdout: the result under `data`, or
the failure under `error`, with its `kind`.

## Wait for the builds

`bb pr checks` reports the builds through its exit code, as `gh pr checks`
does: `1` when one failed, `8` when none failed and one is still running or has
no result, and `0` otherwise. A pipeline that waits for them:

```bash
status=0
while bb pr checks 42 --repo PAY/payments --no-input || status=$?; [ "$status" -eq 8 ]; do
  status=0
  sleep 30
done
[ "$status" -eq 0 ] || { echo "a build of pull request 42 failed" >&2; exit 1; }
```

## Check before changing

`--dry-run` says what the real run would do, and in text it exits as the real
run would. So a step can refuse to go on unless the change would go through:

```bash
bb pr merge 42 --repo PAY/payments --dry-run --no-input
bb pr merge 42 --repo PAY/payments --no-input
```

Under `--json` a dry run exits `0` whenever it reached a verdict, and the
verdict is in the document: `preview.error` is there exactly when the run would
fail. [Dry-Run Planning](../advanced/dry-run-planning.md) says what each
command checks.

## A whole step

```bash
#!/usr/bin/env bash
set -euo pipefail
# BITBUCKET_URL and BITBUCKET_TOKEN come from the CI system's secret store.
export BB_DISABLE_STORED_CONFIG=1

repo=PAY/payments
pr=42

# Wait while a build is still running.
status=0
while bb pr checks "$pr" --repo "$repo" --no-input || status=$?; [ "$status" -eq 8 ]; do
  status=0
  sleep 30
done
[ "$status" -eq 0 ] || { echo "a build of pull request $pr failed" >&2; exit 1; }

# Merge once the preview says it would go through.
bb pr merge "$pr" --repo "$repo" --dry-run --no-input

status=0
bb pr merge "$pr" --repo "$repo" --no-input || status=$?
if [ "$status" -eq 13 ]; then
  # Sent, and whether it merged is unknown: read the state rather than retry.
  state=$(bb pr get "$pr" --repo "$repo" --json --no-input | jq -r '.data.pullRequest.state')
  [ "$state" = MERGED ] || { echo "pull request $pr is $state after an unknown outcome" >&2; exit 1; }
elif [ "$status" -ne 0 ]; then
  exit "$status"
fi
```
