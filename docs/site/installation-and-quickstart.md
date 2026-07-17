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
curl -LO "https://github.com/vriesdemichael/bitbucket-server-cli/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.deb"
sudo dpkg -i "bb_${VERSION#v}_linux_amd64.deb"
# RHEL/Fedora
curl -LO "https://github.com/vriesdemichael/bitbucket-server-cli/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.rpm"
sudo rpm -i "bb_${VERSION#v}_linux_amd64.rpm"
```

## Install from release artifacts

1. Select a release version (example: `v0.1.0`).
2. Download the platform archive, `sha256sums.txt`, and `sha256sums.txt.sigstore.json` from GitHub Releases.
3. Verify the signed checksum manifest with Cosign, then verify checksums and run `bb --help`.

Linux amd64 example:

```bash
VERSION=v0.1.0
curl -LO "https://github.com/vriesdemichael/bitbucket-server-cli/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.tar.gz"
curl -LO "https://github.com/vriesdemichael/bitbucket-server-cli/releases/download/${VERSION}/sha256sums.txt"
curl -LO "https://github.com/vriesdemichael/bitbucket-server-cli/releases/download/${VERSION}/sha256sums.txt.sigstore.json"
curl -LO "https://github.com/vriesdemichael/bitbucket-server-cli/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.tar.gz.sigstore.json"
cosign verify-blob \
	--bundle sha256sums.txt.sigstore.json \
	--certificate-identity "https://github.com/vriesdemichael/bitbucket-server-cli/.github/workflows/release.yml@refs/heads/main" \
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
	--certificate-identity "https://github.com/vriesdemichael/bitbucket-server-cli/.github/workflows/release.yml@refs/heads/main" \
	--certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
	"bb_${VERSION#v}_linux_amd64.tar.gz"
gh attestation verify bb_${VERSION#v}_linux_amd64.tar.gz --repo vriesdemichael/bitbucket-server-cli
```

`bb update` now requires the signed checksum bundle. If Sigstore verification is unavailable or fails, self-update stops and you should use WinGet, Scoop, or manual release installation instead.

## Authenticate to Bitbucket

```bash
bb auth token-url --host https://bitbucket.acme.corp
bb auth login https://bitbucket.acme.corp --token "$BB_TOKEN"
bb auth status
```

If your Bitbucket instance uses a different SSH clone host than its web/API URL, `bb auth login`
will try to discover aliases automatically from the first accessible repository clone links.
You can inspect or manage aliases explicitly with:

```bash
bb auth alias list --host https://bitbucket.acme.corp
bb auth alias discover --host https://bitbucket.acme.corp
bb auth alias add --host https://bitbucket.acme.corp git.acme.corp:7999
```

## First useful commands

```bash
bb repo clone PLATFORM/api
bb browse --repo PLATFORM/api
bb search repos --limit 20
bb search prs --state OPEN
bb --json auth status
```

## Runtime flags and environment variables

Global runtime controls are available as flags and environment variables:

- `--ca-file` / `BB_CA_FILE`
- `--insecure-skip-verify` / `BB_INSECURE_SKIP_VERIFY`
- `--request-timeout` / `BB_REQUEST_TIMEOUT`
- `--retry-count` / `BB_RETRY_COUNT`
- `--retry-backoff` / `BB_RETRY_BACKOFF`
- `--log-level` / `BB_LOG_LEVEL`
- `--log-format` / `BB_LOG_FORMAT`

See [Basic Usage](basic-usage.md) for precedence, dry-run behavior, machine mode, and diagnostics guidance.
