---
search:
  boost: 1.5
---

# Installation and Quickstart

## Which Bitbucket versions work

`bb` works with the Bitbucket Data Center releases in
[Bitbucket Versions](reference/bitbucket-versions.md), and is tested against
[[ bitbucket_version ]] on every pull request. A newer release is adopted once
the suite passes against it.

!!! warning "Data Center only — Bitbucket Cloud is not supported"

    Bitbucket Cloud (bitbucket.org) is a different API, and no `bb` command
    will work against it. Installing `bb` for a bitbucket.org repository will
    not get you anywhere.

## Install on Windows via WinGet

```powershell
winget install vriesdemichael.bb
```

## Install on Windows via Scoop

```powershell
scoop bucket add vriesdemichael https://github.com/vriesdemichael/scoop
scoop install vriesdemichael/bb
```

## Install on macOS or Linux via Homebrew

```bash
brew install vriesdemichael/tap/bb
```

## Install on Debian/Ubuntu or RHEL/Fedora

Download the `.deb` or `.rpm` for your architecture from GitHub Releases and install it:

```bash
# Debian/Ubuntu
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/latest/download/bb_linux_amd64.deb"
sudo dpkg -i bb_linux_amd64.deb
# RHEL/Fedora
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/latest/download/bb_linux_amd64.rpm"
sudo rpm -i bb_linux_amd64.rpm
```

To install a specific release rather than the newest, use the versioned name and
a release tag: `.../releases/download/[[ bb_version_tag ]]/bb_[[ bb_version ]]_linux_amd64.deb`.

## Install from release artifacts

1. Download the platform archive, `sha256sums.txt`, and `sha256sums.txt.sigstore.json` from GitHub Releases.
2. Verify the signed checksum manifest with Cosign, then verify checksums and run `bb --help`.

Linux amd64 example:

```bash
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/latest/download/bb_linux_amd64.tar.gz"
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/latest/download/sha256sums.txt"
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/latest/download/sha256sums.txt.sigstore.json"
cosign verify-blob \
	--bundle sha256sums.txt.sigstore.json \
	--certificate-identity "https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main" \
	--certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
	sha256sums.txt
sha256sum -c sha256sums.txt --ignore-missing
tar -xzf bb_linux_amd64.tar.gz
install -m 0755 bb /usr/local/bin/bb
bb --help
```

`sha256sums.txt` lists both the version-less and the versioned filename for every
artifact, so `--ignore-missing` verifies whichever you downloaded. To pin a
release, swap `latest/download` for `download/[[ bb_version_tag ]]` and use the
versioned names.

Per-archive signatures, build provenance and SBOMs are covered in
[Release Verification](advanced/release-verification.md).

## Keep bb up to date

Installed through WinGet, Scoop, Homebrew or a `.deb` or `.rpm`, `bb` is updated
the way it was installed: `winget upgrade vriesdemichael.bb`,
`scoop update vriesdemichael/bb`, `brew upgrade vriesdemichael/tap/bb`, or the
newer package from GitHub Releases. Those builds leave self-update out, and
`bb update` says so. A binary from a release archive updates itself:

```bash
bb update
```

## Authenticate to Bitbucket

```bash
bb auth token-url --host https://bitbucket.example.com
printf '%s' "$BITBUCKET_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin
bb auth status
```

!!! note "Secrets cannot be passed as flag values"
    There are no `--token` or `--password` flags: a flag value lands in the process argument
    list, where any local user can read it via `ps` or `/proc/<pid>/cmdline`, where Windows shows
    it in Task Manager details, and where process-auditing and EDR tooling records it -- and your
    shell keeps it in history. Use `--token-stdin` or `--password-stdin`, or set `BITBUCKET_TOKEN`.

### What the token lets bb do

`bb` does nothing the token does not allow. A personal access token carries the
permission it was created with, for projects and for repositories: read, write or
admin. It never has more rights than you, and it can have fewer: a token created
to read cannot write, even where you can in the web interface.

With the permission it needs, `bb` does from the command line what you do in the
web interface, up to the level of a whole project:

| To | The token needs |
|---|---|
| Read repositories, pull requests, builds and settings | read |
| Change what is in a repository: branches, tags, pull requests | write |
| Change a repository's settings, permissions, webhooks and restrictions | admin |
| The same for every repository in a project at once, and the project's own permissions | project admin |

Creating a project, and administering Bitbucket itself, are beyond a personal
access token.

A request the token does not allow is refused by Bitbucket, and `bb` reports it as
`authorization` with Bitbucket's own words:

```text
authorization: bitbucket API returned 401: You are not permitted to access this resource
```

If you can do the same thing in the web interface, the token is what stops it.
Create one with the permission you need, at the page `bb auth token-url` prints,
and log in with it.

### Where credentials are stored

`bb auth login` stores the secret in your operating system's keyring — Credential Manager on
Windows, Keychain on macOS, Secret Service on Linux.

Where no keyring is available — headless servers, most containers, WSL without `gnome-keyring` —
the login fails and stores nothing, unless you ask for plaintext:

```bash
printf '%s' "$BITBUCKET_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin --allow-insecure-storage
```

The secret then goes into the config file in plaintext (`0600`, in a `0700` directory), and bb
warns on stderr at login and whenever it uses it. `bb auth status` reports which is in use:

```bash
bb auth status
```

```text
Target Bitbucket: https://bitbucket.example.com (auth=token, source=stored)
Credential storage: keyring
- authentication: Alice Smith (alice)
- git credential helper: configured for https://bitbucket.example.com
```

`bb auth status` does not just report the configuration, it checks it: the
authentication line proves the host is reachable, its certificate is trusted,
any proxy is working, and the credential is still valid. A failing line says
what to do about it.

Lines marked `!` are advisory — worth knowing, but not a broken setup. The git
credential helper is one: it is needed to `git push` and irrelevant if you only
call the API, so it is reported and never fails the command.

In CI, `--check` makes it exit non-zero when a non-advisory check fails:

```bash
bb auth status --check
```

Without it the exit status is zero whatever the findings. Under `--json` the exit status is always zero and the verdict is
the `ok` field.

To refuse plaintext even when a login asks for it, set `BB_REQUIRE_KEYRING=1`, or the
[`require_keyring` policy](reference/system-policy.md#keys) fleet-wide. With either on, bb fails
rather than degrading — including on later commands, if the config file already holds a plaintext
credential from before it was set.

In CI and containers, prefer supplying `BITBUCKET_TOKEN` per invocation instead of logging in at
all. An environment variable never touches the config file and satisfies `BB_REQUIRE_KEYRING`.

If your Bitbucket instance uses a different SSH clone host than its web/API URL, `bb auth login`
will try to discover aliases automatically from the first accessible repository clone links.
You can inspect or manage aliases explicitly with:

```bash
bb auth alias list --host https://bitbucket.example.com
bb auth alias discover --host https://bitbucket.example.com
bb auth alias add --host https://bitbucket.example.com git.example.com:7999
```

## Let git authenticate too

`bb auth login` authenticates `bb` itself. Plain `git` — `git push`, `git pull`
and `git fetch` inside a clone — does not go through `bb`, so it needs telling
where to get credentials:

```bash
bb auth setup-git
```

Git now asks `bb` for a credential whenever it contacts your Bitbucket host,
using what you just stored. No token is written into any repository, and
revoking one takes effect immediately.

Run this once; it applies to every clone of that host. If you clone over SSH you
do not need it — SSH authenticates with your key.

See [Git Authentication](advanced/git-authentication.md) for how it works, and how
to take a stored token out of a clone's configuration.

## Turn on tab completion

`bb` completes pull request numbers, branches, repositories and people, as well
as its commands and flags. Homebrew and the `.deb` and `.rpm` packages set this
up for bash, zsh and fish, and Scoop for PowerShell. Otherwise:

```bash
bb completion install
```

[Shell Completion](advanced/shell-completion.md) has what it writes, and how to
set it up for every user of a machine.

## When something is wrong

`bb doctor` is the first thing to run. It needs no host and no network, and
reports every problem in every configuration file and where each setting comes
from:

```bash
bb doctor
```

`bb auth status` is the other half: it proves the host is reachable, the
certificate is trusted and the credential still works.

[**Troubleshooting**](troubleshooting.md) has the messages people actually hit,
each with what to do about it. [Coming from `gh`](gh-parity.md) maps the `gh`
command you were about to type to its `bb` counterpart.

## First useful commands

```bash
bb repo clone PLATFORM/api
bb browse --repo PLATFORM/api
bb search repos --limit 20
bb search prs --state OPEN
bb --json auth status
```

## Runtime flags and environment variables

Most global runtime controls exist as both a flag and an environment variable —
`--ca-file` / `BB_CA_FILE`, `--retry-count` / `BB_RETRY_COUNT`, and so on. Flags
win over environment variables, which win over the configuration files;
[Configuration](reference/configuration.md) gives the whole order.

**[Environment Variables](reference/environment.md)** is the complete list, with
defaults and what each one does.

Behind a proxy, or against a certificate from an internal CA, see
[Networks, Proxies and TLS](advanced/networks-proxies-and-tls.md).

See [Basic Usage](basic-usage.md) for precedence, dry-run behavior, machine mode, and diagnostics guidance,
and [Troubleshooting](troubleshooting.md) when a command does not do what you expected.
