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
| `BB_WORKSPACE_CONFIG_PATH` | unset | Path to the per-workspace configuration file. Unset, `bb` looks for `.bb/config.yaml`, searching upward from the working directory and stopping at the repository root. |

`BITBUCKET_USER`, `ADMIN_USER` and `ADMIN_PASSWORD` exist because the test
harness sets them. They work, but prefer the primary names.

## TLS and proxies

| Variable | Default | Effect |
|---|---|---|
| `BB_CA_FILE` | unset | Path to a PEM bundle of additional trusted CAs. **Added to** the system trust store, not a replacement for it. Equivalent flag: `--ca-file`. |
| `BB_CLIENT_CERT` | unset | Path to a PEM-encoded client certificate or certificate chain for mutual TLS (mTLS). Must be set together with `BB_CLIENT_KEY`. Equivalent flag: `--client-cert`. |
| `BB_CLIENT_KEY` | unset | Path to a PEM-encoded client private key for mutual TLS (mTLS). Must be set together with `BB_CLIENT_CERT`. Equivalent flag: `--client-key`. |
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

## Webhook credentials

| Variable | Default | Effect |
|---|---|---|
| `BB_WEBHOOK_SECRET` | none | Shared secret for `webhook create` and `webhook update`, used to sign deliveries. There is no flag that takes it as a value ([ADR-047](../adr/047-credential-input-and-keyring-enforcement.md)); the alternative is `--secret-stdin`. |
| `BB_WEBHOOK_PASSWORD` | none | Password for the endpoint's basic authentication, paired with `--credentials-username`. The alternative is `--credentials-password-stdin`. |

An empty value counts as unset. A `--*-stdin` flag wins over the variable, and
`--no-secret` / `--no-credentials` override both — a variable exported for every
command must not make removing a credential impossible. Bulk policies name these
variables rather than holding a value; see
[Webhook Secrets](../advanced/webhook-secrets.md).

## Interactivity

`bb` prompts only when a person is there to answer, and decides that in one
place ([ADR-072](../adr/072-interactivity-is-decided-in-one-place.md)). These
two variables are how you tell it nobody is.

| Variable | Default | Effect |
|---|---|---|
| `BB_NO_PROMPT` | unset | Any value other than `0`, `false` or empty turns prompting off for every command in the process. A command that then needs a value it cannot ask for fails naming the flag that would have supplied it, rather than falling back to a default. Equivalent flag: `--no-input`. |
| `BB_NO_PROMPT_VARS` | unset | Comma-separated names of *further* variables whose presence also means nobody is watching. |

`bb` already treats `CI`, `DEBIAN_FRONTEND`, `NONINTERACTIVE`, `TERM=dumb` and a
list of coding harnesses as proof that nobody is there. `BB_NO_PROMPT_VARS` is
for the harness it has not heard of: set it once in the environment to the
variable that harness does set, instead of waiting for a release or passing
`--no-input` on every call.

```bash
export BB_NO_PROMPT_VARS=MY_BUILD_RUNNER,ACME_AGENT
```

`--json` suppresses prompting on its own: a machine reading structured output is
not a person who can answer a question.

## Updates

| Variable | Default | Effect |
|---|---|---|
| `BB_DISABLE_UPDATE` | unset | `1` or `true` disables `bb update`. The command explains that it is disabled and points at your system package manager. Administrative policy can disable it too, and is reported separately. |
| `BB_UPDATE_BASE_URL` | GitHub releases | Base URL the updater fetches manifests and artifacts from, for an internal mirror. The `--base-url` flag wins over it; it wins over the workspace, stored and system configuration. |

!!! note "Update trust is settable from system policy only"

    `BB_UPDATE_TRUSTED_ROOT`, `BB_UPDATE_SIGNATURE_IDENTITY` and
    `BB_ALLOW_UNVERIFIED_UPDATE` are **deliberately not read** from the
    environment. A variable that could redirect the trust root would let
    anything able to set a variable in your shell approve its own update.
    Configure these through system policy instead — see
    [Enterprise Hardening](../advanced/enterprise-hardening.md).

    `BB_DISABLE_UPDATE` and `BB_UPDATE_BASE_URL` are honoured because neither
    weakens verification: the first only refuses to update, and an artifact from
    a mirror still has to pass the same signature check.

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
| `BB_ERROR_HARVEST` | Names a file every non-2xx response is recorded to, so live tests can capture what the server actually returns. Unset in every real run, and nothing is opened. |
| `BB_SYSTEM_CONFIG_PATH` | Overrides where system policy is read from. **Honoured only under `go test`** — the shipped binary ignores it, so a user's shell cannot replace the policy tier and with it `require_keyring`, `allowed_hosts` and `disable_update`. |

## See also

- [Config and auth precedence](../basic-usage.md#config-and-auth-precedence)
- [Networks, Proxies and TLS](../advanced/networks-proxies-and-tls.md)
- [Repository Discovery and Server Switching](../advanced/repository-discovery-and-server-switching.md)
