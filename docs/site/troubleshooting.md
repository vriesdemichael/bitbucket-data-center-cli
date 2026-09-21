---
search:
  boost: 2.0
---

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

<!-- docs-lint: message-of bb -->

## `no Bitbucket host configured`

```text
validation: no Bitbucket host configured: set BITBUCKET_URL or run 'bb auth login <host>'
```

Nothing has been configured yet. Run `bb auth login`, or set `BITBUCKET_URL` and
a credential.

<!-- docs-lint: message-of bb -->

## `the stored configuration at ... could not be read`

```text
permanent: the stored configuration at /home/alice/.config/bb/config.yaml could not be read; run 'bb doctor' to list every problem in it. Fix or remove that file; bb will not rewrite a file it could not read (invalid YAML configuration (yaml: line 5: found character that cannot start any token))
```

A configuration file exists and bb cannot read it -- a stray indent is enough.
Every command stops here rather than carry on as if you had never logged in, and
`bb auth login` and `bb auth logout` refuse to write over the file, so the other
hosts in it are not lost. Repair it using the line the message names, or move it
aside and log in again.

The same message names a workspace's `.bb/config.yaml`. For the system file it
says to ask your administrator instead: that file carries policy, and bb stops
rather than run without it.

| Platform | User | System |
|---|---|---|
| Linux | `~/.config/bb/config.yaml` | `/etc/bb/config.yaml` |
| macOS | `~/Library/Application Support/bb/config.yaml` | `/etc/bb/config.yaml` |
| Windows | `%APPDATA%\bb\config.yaml` | `%ProgramData%\bb\config.yaml` |

`BB_CONFIG_PATH` overrides the user file. In CI, `BB_DISABLE_STORED_CONFIG=1`
skips it entirely, so a stray file on a shared runner cannot fail the run.

### Checking the configuration

`bb doctor` reads the stored, workspace and system files each on its own, so it
reports every problem in every file where a command stops at the first. It needs
no host and no network, so it works when nothing else does:

```bash
bb doctor
```

```text
Configuration files
  stored     /home/alice/.config/bb/config.yaml
             invalid: the schema rejects 1 key
             line 6: hosts.corp.username: got number, want string
             ignored: require_keyring is read only from the system configuration
  workspace  none found above the working directory
  system     /etc/bb/config.yaml
             invalid: the schema rejects 1 key
             line 2: policies.require_keyrng: unknown key
```

A misspelled key is valid YAML; the
[configuration schema](reference/schemas/config.schema.json) is what rejects it,
and `bb doctor` names each one with its line. A key spelled correctly in the
wrong file is listed as `ignored`: policy in your own file mandates nothing.

Below the files, `Settings` lists every effective setting, where it came from —
a flag, an environment variable, a `.env` file, one of the files, the Windows
registry or the default — and what it overrides. A token or password shows as
configured, with where it is held, and never its value.

It exits `0` only when there is nothing to fix; any issue it reports exits `1`.
Under `--json`, a run with issues prints the failure envelope instead of the
report: `error.message` summarises the issues, and `error.details` names each
one under its own key, such as `violation/system/policies/require_keyrng` or
`setting/retry_count`.

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

If another credential manager answers first, re-run `bb auth setup-git --force`,
which replaces the helper configured for that host with `bb`. Without `--force`
it refuses rather than overwrite somebody else's helper. See
[Git Authentication](advanced/git-authentication.md).

<!-- docs-lint: message-of crypto/x509 -->

## `certificate signed by unknown authority`

`bb` trusts the system store plus anything in `BB_CA_FILE`, which is **added**
to that store rather than replacing it.

A common variant: it works in a terminal and fails in an IDE, because a GUI
application did not inherit your shell environment. Set `BB_CA_FILE` in the
IDE's own environment block. See
[Networks, Proxies and TLS](advanced/networks-proxies-and-tls.md).

<!-- docs-lint: message-of bb -->

## `OS keyring is unavailable and keyring-backed storage is required`

A headless Linux host, an SSH session, or a container. `bb` reaches the keyring
through the freedesktop Secret Service API, so it needs two things on Linux: a
D-Bus session bus, and a Secret Service provider listening on it. A session bus
alone is not enough — with no provider there is nothing to answer, and the
storage still fails.

```bash
eval "$(dbus-launch --sh-syntax)"
gnome-keyring-daemon --start --components=secrets
```

The daemon also has to be **unlocked**. A login keyring created with a password
stays locked until something supplies it, and a locked keyring refuses reads the
same way a missing one does. `libsecret`, KWallet's Secret Service interface and
`keepassxc` with Secret Service enabled all work in place of
`gnome-keyring-daemon`.

**On a server, in a container or in CI, do not do any of this.** Supply the
credential through `BITBUCKET_TOKEN` instead. It stores nothing on disk, needs
no session bus, and satisfies a `BB_REQUIRE_KEYRING` policy, because there is no
plaintext fallback to refuse.

<!-- docs-lint: message-of bb -->

## `bitbucket API returned 401: Authentication failed`

```text
authentication: bitbucket API returned 401: Authentication failed. Please check your credentials and try again.
```

The token is wrong, revoked, or expired. Bitbucket personal access tokens can be
created with an expiry, and nothing warns you as it approaches. Exit status is
`3`, and the error `kind` is `authentication`.

```bash
bb auth status
```

That names the host the credential was tried against, which is the other half of
the answer: a token that is valid on one instance is not on another. Create a
replacement and store it:

```bash
bb auth token-url --host https://bitbucket.example.com
printf '%s' "$BB_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin
```

If `BITBUCKET_TOKEN` is set in your environment it wins over anything stored, so
logging in again changes nothing until you unset it. `bb auth status` reports
which one is in use as `source=env` rather than `source=stored`.

<!-- docs-lint: message-of bb -->

## `bitbucket API returned 401: You are not currently licensed to use Bitbucket`

```text
authorization: bitbucket API returned 401: You are not currently licensed to use Bitbucket.
Please contact your administrator to resolve this issue.
```

The credential is valid and the account is not licensed. Bitbucket answers this
with `401` rather than `403`, but it is not an authentication problem: the error
`kind` is `authorization`, because logging in again cannot fix it. Only an
administrator granting the licence can.

The same `kind` covers a licensed account that lacks a permission, which
Bitbucket also reports as `401`:

```text
authorization: bitbucket API returned 401: You are not permitted to access this resource
```

Both exit `3`. To tell them apart in a script, read `error.details.upstreamException`
under `--json`: a licence problem is `NoAccessAuthenticationException` and a
permission problem is `AuthorisationException`. Branch on that rather than on the
wording, which Bitbucket rewrites between releases.

<!-- docs-lint: message-of bb -->

## `repository is required (use --repo PROJECT/slug ...)`

```text
validation: repository is required (use --repo PROJECT/slug or set BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)
```

The command needs a repository and could not work one out. `bb` infers one from
the git remotes of the directory you are standing in, so this means either you
are not inside a Bitbucket clone, or its remotes point somewhere `bb` has no
host configured for. Name the repository instead:

```bash
bb pr list --repo PROJECT/my-repo
```

`git remote -v` shows what `bb` had to work with. A URL on a hostname that is
not your configured Bitbucket host — an SSH clone host on a different name, for
instance — is not matched; `bb auth alias add` teaches `bb` that the two are the
same instance.

<!-- docs-lint: message-of bb -->

## `ambiguous git remote context ...`

```text
validation: ambiguous git remote context (origin=PROJ/api@https://bitbucket.example.com, upstream=PLATFORM/api@https://bitbucket.example.com); specify --repo PROJECT/slug and/or set active server with auth server use --host
```

The clone has remotes naming more than one repository and `bb` will not pick. A
side remote next to `origin` is not ambiguous — `origin` wins, which is git's
own convention — but an `upstream` remote is the deliberate exception, because
the convention that puts a fork on `origin` puts the repository people work
against on `upstream`. There is no defensible default between the two.

The message lists every candidate it found, as `remote=PROJECT/slug@host`. Pass
`--repo PROJECT/slug` for the one you meant. When the candidates differ by host
rather than by repository, `bb auth server use` sets which instance is the
default.

## A command refuses and blames administrative policy

```text
host "https://bitbucket.example.com" is not permitted by administrative policy
insecure TLS verification is disabled by administrative policy
overriding CA bundle is disabled by administrative policy
--allow-http is refused: plain-HTTP update URLs are disabled by administrative policy (allow_http_update in the system configuration file /etc/bb/config.yaml)
```

These come from a machine-wide configuration file that only an administrator can
write, and nothing you set in your own environment overrides them — that is the
point of them. There is no local workaround, and looking for one wastes time.

What the message is worth to you is the specific key it names, so you can ask
for the right change: `allowed_hosts`, `allow_insecure_skip_verify`, `ca_file`,
`allow_http_update`.
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

## `bb update` refuses a plain-HTTP mirror

```text
update URL "http://mirror.example.com/bb" uses plain HTTP; pass --allow-http or set BB_ALLOW_HTTP_UPDATE=1 to permit it
```

`bb update` fetches from release mirrors over `https` only. Serve the mirror over
`https` if you can. If it has no TLS, permit plain HTTP for one run with
`bb update --allow-http`, or for every run with `BB_ALLOW_HTTP_UPDATE=1`; each run
then warns that anyone on the network path can read or withhold what the mirror
serves.

The refused URL is not always the one you configured. It can be a download the
mirror's manifest points at, or an address the mirror redirects to, and the
message says which.

When the message says `--allow-http is refused` or `which administrative policy
forbids`, the machine's policy sets `allow_http_update: false`, and the section
above applies.

## A command exits non-zero and I need to know why

Exit codes are deterministic by error kind:

| Code | Kind |
|---|---|
| `2` | `validation`, including unknown flags and commands |
| `3` | `authentication` or `authorization` |
| `4` | `not_found` |
| `5` | `conflict` |
| `10` | `transient` |
| `11` | `not_implemented` |
| `12` | `cancelled`: the command was interrupted |
| `13` | `unknown_outcome`: the request reached the server and no usable answer came back; check whether it was applied before running it again |
| `14` | `unsupported`: the Bitbucket instance's version cannot do it, and the message names the version that can |
| `1` | `permanent` (including a rejected TLS certificate or a host that does not resolve), `internal`, or unknown |

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
