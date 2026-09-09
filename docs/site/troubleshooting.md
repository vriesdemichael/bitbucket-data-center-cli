# Troubleshooting

Symptoms a person hits while using `bb`, and what to check first. Fleet-wide
policy failures have their own table in
[Enterprise Hardening](advanced/enterprise-hardening.md#helpdesk-troubleshooting-guide).

## Start here

`bb auth status` answers most questions in one line: which host `bb` will talk
to, how it authenticates, where the credential is stored, and whether the checks
pass.

```bash
bb auth status
```

When something fails and the reason is not obvious, run it again with a trace on
stderr. This never corrupts `--json` output on stdout:

```bash
bb --log-level debug --log-format jsonl repo list
```

To capture what the server actually sent back, set `BB_ERROR_HARVEST` to a file
path, reproduce the problem once, and attach the file to a report. See
[Environment Variables](reference/environment.md#output-and-diagnostics).

## `no Bitbucket host configured`

```text
validation: no Bitbucket host configured: set BITBUCKET_URL or run 'bb auth login <host>'
```

Usually what it says: nothing has been configured yet. Run `bb auth login`, or
set `BITBUCKET_URL` and a credential.

!!! warning "If you are sure you did log in"

    The same message appears when the configuration file exists but cannot be
    parsed — a stray indent is enough. Check the file before doing anything
    else, because `bb auth login` rewrites it and any other hosts in it are
    lost:

    ```bash
    cat "${BB_CONFIG_PATH:-$HOME/.config/bb/config.yaml}"
    ```

    Fix the syntax, or move the file aside and log in again. This conflation is
    tracked in
    [#567](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/567).

## Git asks for a password on push or pull

`bb` installs itself as a git credential helper for one host, matched exactly —
scheme and port included. A remote on a different spelling gets no answer.

```bash
git remote -v
git config --get-all credential.https://bitbucket.example.com.helper
```

Ask the helper directly what it would give git:

```bash
printf 'protocol=https\nhost=bitbucket.example.com\n\n' | bb auth git-credential get
```

Output means it works. **Silence means `bb` has nothing stored for that host** —
run `bb auth login` for it. The silence is deliberate: it lets git fall through
to another helper instead of failing outright.

If another credential manager answers first, re-run `bb auth setup-git`, which
resets the helper list for that host before adding `bb`. See
[Git Authentication](advanced/git-authentication.md).

## `certificate signed by unknown authority`

`bb` trusts the system store plus anything in `BB_CA_FILE`, which is **added**
to that store rather than replacing it.

A common variant: it works in a terminal and fails in an IDE, because a GUI
application did not inherit your shell environment. Set `BB_CA_FILE` in the
IDE's own environment block. See
[Networks, Proxies and TLS](advanced/networks-proxies-and-tls.md).

## `OS keyring is unavailable and keyring-backed storage is required`

A headless Linux host or an SSH session with no D-Bus session bus. Either start
one:

```bash
eval $(dbus-launch --sh-syntax)
```

or supply the credential through `BITBUCKET_TOKEN`, which stores nothing on
disk and is the right answer in CI and containers.

## A command refuses and blames policy

Messages naming administrative policy come from a machine-wide configuration
you cannot override from your own account — `host ... is not permitted`,
`insecure TLS verification is disabled`, `overriding CA bundle is disabled`,
`self-update is disabled`. The keys behind them, and what each refusal means,
are in [System Policy](reference/system-policy.md).

## `bb update` says self-update is disabled in this build

Not policy — the binary itself. Installs from WinGet, Scoop and Homebrew are
`_noupdate` builds, which have self-update compiled out so they cannot replace a
file the package manager owns. Update through that package manager instead. See
[Builds without the self-updater](installation-and-quickstart.md#builds-without-the-self-updater).

## A command exits non-zero and I need to know why

Exit codes are deterministic by error kind:

| Code | Kind |
|---|---|
| `2` | `validation`, including unknown flags and commands |
| `3` | `authentication` or `authorization` |
| `4` | `not_found` |
| `5` | `conflict` |
| `10` | `transient` |
| `1` | `permanent`, `internal`, or unknown |

Under `--json` the failure arrives as an envelope with an `error` key instead of
`data`, carrying the same kind. See
[Machine Mode and Diagnostics](advanced/machine-mode-diagnostics.md#error-kinds-and-exit-codes).

## A command hangs, or refuses to ask me something

`bb` prompts only when it decides a person is present, and treats `CI`,
`TERM=dumb` and a list of coding harnesses as proof that nobody is. When it will
not prompt, it fails naming the flag that would have supplied the value rather
than guessing.

To force that behaviour, pass `--no-input` or set `BB_NO_PROMPT`. If your CI
harness is one `bb` has not heard of, name its variable in `BB_NO_PROMPT_VARS`.
See [Interactivity](reference/environment.md#interactivity).

## Nothing here matches

Open an issue with the command you ran, the output, and the trace from
`--log-level debug --log-format jsonl`. Include `bb --version` and the Bitbucket
version from `bb auth status`.

## See also

- [Environment Variables](reference/environment.md)
- [System Policy](reference/system-policy.md)
- [Machine Mode and Diagnostics](advanced/machine-mode-diagnostics.md)
- [Git Authentication](advanced/git-authentication.md)
