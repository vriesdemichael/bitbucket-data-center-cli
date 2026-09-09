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

Every field in that trace is redacted before it is written. A field whose name
contains `token`, `password`, `secret`, `authorization`, `cookie`, `apikey`,
`api-key` or `credential` is replaced with `[REDACTED]`, and any value that
parses as a URL has its embedded credentials and sensitive query parameters
rewritten the same way.

Two things that leaves in place, both worth checking before you send a trace
anywhere:

- **URLs keep everything but their credentials.** Only embedded userinfo and
  sensitive query parameters are rewritten — the scheme, host, port and path
  survive. A trace therefore contains your internal hostnames, and the project
  keys and repository slugs in every request path.
- **Redaction is by field name.** A secret pasted into free text under an
  innocent-looking field is not caught, because nothing there looks like a
  credential to a name match.

**Read a trace before you send it to anyone.**

To capture what the server answered rather than what `bb` did, set
`BB_ERROR_HARVEST` to a file path and reproduce the problem once. It records the
method, path, status and Bitbucket's own exception name — never request headers
and never response bodies, only their size.

## `no Bitbucket host configured`

```text
validation: no Bitbucket host configured: set BITBUCKET_URL or run 'bb auth login <host>'
```

Usually what it says: nothing has been configured yet. Run `bb auth login`, or
set `BITBUCKET_URL` and a credential.

!!! warning "If you are sure you did log in"

    The same message appears when a configuration file exists but cannot be
    parsed — a stray indent is enough. Check the files before doing anything
    else, because `bb auth login` rewrites the user one and any other hosts in
    it are lost.

    | Platform | User | System |
    |---|---|---|
    | Linux, macOS | `~/.config/bb/config.yaml` | `/etc/bb/config.yaml` |
    | Windows | `%APPDATA%\bb\config.yaml` | `%ProgramData%\bb\config.yaml` |

    `BB_CONFIG_PATH` overrides the user one, so check that first if it is set.

### Validating a configuration file

Both files are described by a published JSON Schema, which catches a malformed
file and a well-formed one using a key that does not exist:

```bash
uvx check-jsonschema --schemafile https://raw.githubusercontent.com/vriesdemichael/bitbucket-data-center-cli/main/docs/reference/schemas/config.schema.json ~/.config/bb/config.yaml
```

Better, add the reference to the file itself and let your editor validate it as
you type:

```yaml
$schema: https://raw.githubusercontent.com/vriesdemichael/bitbucket-data-center-cli/main/docs/reference/schemas/config.schema.json
default_host: bitbucket.example.com
policies:
  require_keyring: true
```

`$schema` is an accepted key; `bb` ignores it. This is worth doing on the system
policy file in particular, where a typo silently drops a control rather than
reporting one — see [System Policy](reference/system-policy.md).

## Git asks for a password on push or pull

`bb` installs itself as a git credential helper for one host, matched exactly —
scheme and port included. A remote on a different spelling gets no answer.

```bash
git remote -v
git config --get-all credential.https://bitbucket.example.com.helper
```

Ask the helper directly what it would give git:

```bash
printf 'protocol=https
host=bitbucket.example.com

' | bb auth git-credential get
```

```powershell
"protocol=https`nhost=bitbucket.example.com`n`n" | bb auth git-credential get
```

Working, it answers with the credential it would hand git:

```text
username=alice
password=<your token>
```

!!! danger "That output is your token"

    It is printed in full, because that is what git asks for. Do not paste it
    into an issue, a chat message or a screenshot.

**No output means `bb` has nothing stored for that host** — run `bb auth login`
for it. The silence is deliberate: it lets git fall through to another helper
instead of failing outright.

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

## A command refuses and blames administrative policy

```text
host "https://bitbucket.example.com" is not permitted by administrative policy
insecure TLS verification is disabled by administrative policy
overriding CA bundle is disabled by administrative policy
```

These come from a machine-wide configuration file that only an administrator can
write, and nothing you set in your own environment overrides them — that is the
point of them. There is no local workaround, and looking for one wastes time.

What the message is worth to you is the specific key it names, so you can ask
for the right change: `allowed_hosts`, `allow_insecure_skip_verify`, `ca_file`.
[System Policy](reference/system-policy.md) lists what each one controls.

The exception is the CA bundle. `overriding CA bundle is disabled` also appears
when you have set `BB_CA_FILE` yourself and it differs from the mandated one;
unsetting yours resolves it, because omitting it uses the mandated bundle.

## `bb update` says self-update is disabled in this build

Not policy — the binary itself. Installs from WinGet, Scoop and Homebrew are
`_noupdate` builds, with self-update compiled out: if `bb` replaced its own file,
your package manager would still believe the old version was installed, and the
next upgrade or uninstall would act on that stale record. Update through the
package manager instead. See
[Builds With Self-Update Compiled Out](advanced/enterprise-hardening.md#builds-with-self-update-compiled-out).

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

Open an issue with the command you ran, what it printed, and a trace from
`bb --log-level debug --log-format jsonl <your command>`. Include `bb --version`
and the target from `bb auth status`.

Before attaching anything, read it through and remove:

- **Tokens and passwords.** The trace redacts fields it recognises by name, but
  not a secret sitting in free text, and `bb auth git-credential get` prints your
  token in full by design.
- **Internal hostnames and URLs**, if your instance is not public. A hostname is
  usually not needed to reproduce a problem; `https://bitbucket.example.com` in
  its place loses nothing.
- **Project keys, repository names and usernames** that reveal work you cannot
  share.

`BB_ERROR_HARVEST` output is the safest thing to attach: it records the method,
path, status and Bitbucket's exception name, and never a request header or a
response body.

## See also

- [Environment Variables](reference/environment.md)
- [System Policy](reference/system-policy.md)
- [Machine Mode and Diagnostics](advanced/machine-mode-diagnostics.md)
- [Git Authentication](advanced/git-authentication.md)
