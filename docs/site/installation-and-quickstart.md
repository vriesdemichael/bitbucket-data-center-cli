# Installation and Quickstart

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

## Install on Arch Linux from the AUR

```bash
yay -S bb-bin
```

## Install on Debian/Ubuntu or RHEL/Fedora

Download the `.deb` or `.rpm` for your architecture from GitHub Releases and install it:

```bash
VERSION=v1.0.0
# Debian/Ubuntu
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.deb"
sudo dpkg -i "bb_${VERSION#v}_linux_amd64.deb"
# RHEL/Fedora
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.rpm"
sudo rpm -i "bb_${VERSION#v}_linux_amd64.rpm"
```

## Install from release artifacts

1. Select a release version (example: `v0.1.0`).
2. Download the platform archive, `sha256sums.txt`, and `sha256sums.txt.sigstore.json` from GitHub Releases.
3. Verify the signed checksum manifest with Cosign, then verify checksums and run `bb --help`.

Linux amd64 example:

```bash
VERSION=v0.1.0
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.tar.gz"
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/download/${VERSION}/sha256sums.txt"
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/download/${VERSION}/sha256sums.txt.sigstore.json"
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.tar.gz.sigstore.json"
cosign verify-blob \
	--bundle sha256sums.txt.sigstore.json \
	--certificate-identity "https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main" \
	--certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
	sha256sums.txt
sha256sum -c sha256sums.txt --ignore-missing
tar -xzf "bb_${VERSION#v}_linux_amd64.tar.gz"
install -m 0755 bb /usr/local/bin/bb
bb --help
```

Archive-level provenance verification remains available when you want to inspect a specific artifact directly:

```bash
cosign verify-blob \
	--bundle "bb_${VERSION#v}_linux_amd64.tar.gz.sigstore.json" \
	--certificate-identity "https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main" \
	--certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
	"bb_${VERSION#v}_linux_amd64.tar.gz"
gh attestation verify bb_${VERSION#v}_linux_amd64.tar.gz --repo vriesdemichael/bitbucket-data-center-cli
```

`bb update` now requires the signed checksum bundle. If Sigstore verification is unavailable or fails, self-update stops and you should use WinGet, Scoop, or manual release installation instead.

## Authenticate to Bitbucket

```bash
bb auth token-url --host https://bitbucket.acme.corp
printf '%s' "$BB_TOKEN" | bb auth login https://bitbucket.acme.corp --token-stdin
bb auth status
```

!!! warning "Do not pass secrets as flag values"
    `--token <value>` puts the token in the process argument list, where any local user can read it
    via `ps` or `/proc/<pid>/cmdline`, where Windows shows it in Task Manager details, and where
    process-auditing and EDR tooling records it. Your shell also keeps it in history. `--token-stdin`
    and `--password-stdin` avoid all of that. The flag forms still work and warn on stderr.

### Where credentials are stored

`bb auth login` stores the secret in your operating system's keyring — Credential Manager on
Windows, Keychain on macOS, Secret Service on Linux.

Where no keyring is available — headless servers, most containers, WSL without `gnome-keyring` —
bb falls back to writing the secret in plaintext into its config file (`0600`, in a `0700`
directory) and warns on stderr. `bb auth status` reports which is in use:

```bash
bb auth status
```

```text
Target Bitbucket: https://bitbucket.acme.corp (auth=token, source=stored)
Credential storage: keyring
```

To refuse the plaintext fallback, pass `--require-keyring` at login, or set `BB_REQUIRE_KEYRING=1`
to enforce it fleet-wide. With the policy on, bb fails rather than degrading — including on later
commands, if the config file already holds a plaintext credential from before the policy was set.

In CI and containers, prefer supplying `BITBUCKET_TOKEN` per invocation instead of logging in at
all. An environment variable never touches the config file and satisfies `BB_REQUIRE_KEYRING`.

If your Bitbucket instance uses a different SSH clone host than its web/API URL, `bb auth login`
will try to discover aliases automatically from the first accessible repository clone links.
You can inspect or manage aliases explicitly with:

```bash
bb auth alias list --host https://bitbucket.acme.corp
bb auth alias discover --host https://bitbucket.acme.corp
bb auth alias add --host https://bitbucket.acme.corp git.acme.corp:7999
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

See [Git Authentication](advanced/git-authentication.md) for how it works and how
to clean up clones made by older versions of `bb`.

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
win over environment variables, which win over stored configuration.

**[Environment Variables](reference/environment.md)** is the complete list, with
defaults and what each one does.

Behind a proxy, or against a certificate from an internal CA, see
[Networks, Proxies and TLS](advanced/networks-proxies-and-tls.md).

See [Basic Usage](basic-usage.md) for precedence, dry-run behavior, machine mode, and diagnostics guidance.
