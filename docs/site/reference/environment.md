# Environment Variables

Every environment variable `bb` reads, what it does, and what happens when it is
unset.

Command-line flags take precedence over environment variables, which take
precedence over stored configuration. See
[Config and auth precedence](../basic-usage.md#config-and-auth-precedence) for
the full order.

## Connection

| Variable | Default | Effect |
|---|---|---|
| `BITBUCKET_URL` | none | Base URL of the Bitbucket Data Center instance, including scheme and any context path — `https://bitbucket.example.com` or `https://example.com/bitbucket`. |
| `BITBUCKET_VERSION_TARGET` | unset | Pins the Bitbucket version `bb` assumes when behaviour differs between releases. Unset means "whatever the server reports". Most operators never set this. |
| `BB_REQUEST_TIMEOUT` | `20s` | Per-request HTTP timeout, as a Go duration (`45s`, `2m`). Equivalent flag: `--request-timeout`. |
| `BB_RETRY_COUNT` | `2` | Retry attempts for transient failures — connection errors, 429, 5xx. `0` disables retrying. Equivalent flag: `--retry-count`. |
| `BB_RETRY_BACKOFF` | `250ms` | Base delay between retries, multiplied by the attempt number. A `Retry-After` header from the server wins over this. Equivalent flag: `--retry-backoff`. |

## Authentication

**A token on the command line ends up in your shell history and in the process
list, where other users on the machine can read it.** `BITBUCKET_TOKEN` is the
answer to that: it keeps the secret out of `argv` without storing anything on
disk, which is what makes it the right choice for CI and containers.

| Variable | Default | Effect |
|---|---|---|
| `BITBUCKET_TOKEN` | none | Personal access token, sent as a bearer token. Takes precedence over username/password. |
| `BITBUCKET_USERNAME` | none | Username for basic authentication. Falls back to `BITBUCKET_USER`, then `ADMIN_USER`. |
| `BITBUCKET_PASSWORD` | none | Password for basic authentication. Falls back to `ADMIN_PASSWORD`. |
| `BB_REQUIRE_KEYRING` | unset | `1` makes `bb` refuse to read or write credentials through the plaintext config fallback. Use it where storing a secret unencrypted is not acceptable — see [keyring storage](../installation-and-quickstart.md#where-credentials-are-stored). |
| `BB_DISABLE_STORED_CONFIG` | unset | `1` ignores `~/.config/bb/config.yaml` entirely, so only flags and environment variables are consulted. Useful in CI, where a stray config file on a shared runner would otherwise be picked up. |
| `BB_CONFIG_PATH` | `~/.config/bb/config.yaml` | Path to the stored configuration file. |

`BITBUCKET_USER`, `ADMIN_USER` and `ADMIN_PASSWORD` exist because the test
harness sets them. They work, but prefer the primary names.

## TLS and proxies

| Variable | Default | Effect |
|---|---|---|
| `BB_CA_FILE` | unset | Path to a PEM bundle of additional trusted CAs. **Added to** the system trust store, not a replacement for it. Equivalent flag: `--ca-file`. |
| `BB_INSECURE_SKIP_VERIFY` | unset | `true` disables TLS certificate verification. Prints a warning on every invocation. Equivalent flag: `--insecure-skip-verify`. |
| `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY` | unset | Standard proxy configuration, honoured for all `bb` HTTP traffic. Lowercase spellings work too. |

See [Networks, Proxies and TLS](../advanced/networks-proxies-and-tls.md) for how
these interact, and for the difference between `bb`'s own requests and the git
subprocesses it starts.

## Repository context

| Variable | Default | Effect |
|---|---|---|
| `BITBUCKET_PROJECT_KEY` | `TEST` | Project key used when `--repo` is omitted and no repository can be inferred from a git remote. |
| `BITBUCKET_REPO_SLUG` | none | Repository slug, used with `BITBUCKET_PROJECT_KEY`. |

!!! warning "`BITBUCKET_PROJECT_KEY` defaults to `TEST`"

    That default is a leftover from the test harness. It means a command with no
    `--repo`, no inferable git remote and no `BITBUCKET_PROJECT_KEY` will address
    a project literally named `TEST` rather than telling you the context is
    missing. If you get a confusing "project does not exist" error, this is
    usually why. Set `BITBUCKET_PROJECT_KEY` explicitly, or pass `--repo`.

## Output and diagnostics

| Variable | Default | Effect |
|---|---|---|
| `BB_LOG_LEVEL` | `error` | Diagnostic verbosity: `error`, `warn`, `info`, `debug`. Diagnostics go to stderr, so they never corrupt `--json` output on stdout. Equivalent flag: `--log-level`. |
| `BB_LOG_FORMAT` | `text` | `text` or `jsonl`. Equivalent flag: `--log-format`. |
| `NO_COLOR` | unset | Any value disables coloured output, following [no-color.org](https://no-color.org). Equivalent flag: `--no-color`. |

For a report of what went wrong, `--log-level debug --log-format jsonl` gives a
structured trace worth attaching to an issue.

## Bulk operations

| Variable | Default | Effect |
|---|---|---|
| `BB_BULK_STATUS_DIR` | OS temp directory | Where `bb bulk` writes plan and run state. Set it somewhere durable if you need runs to survive a reboot, or somewhere shared for a team runner. |

## Development only

These exist for this repository's own test suite. They are not part of the
supported interface and may change without notice.

| Variable | Effect |
|---|---|
| `BB_BLOCK_EXTERNAL_NETWORK` | `1` makes any HTTP request to a non-loopback host fail immediately. Used so unit tests cannot reach the internet. |

## See also

- [Config and auth precedence](../basic-usage.md#config-and-auth-precedence)
- [Networks, Proxies and TLS](../advanced/networks-proxies-and-tls.md)
- [Repository Discovery and Server Switching](../advanced/repository-discovery-and-server-switching.md)
