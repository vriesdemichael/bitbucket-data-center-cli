# Networks, Proxies and TLS

`bb` is built to run inside a corporate network: behind an outbound proxy,
against a Bitbucket presenting a certificate from an internal CA, with no
special configuration beyond what is already in the environment.

## Proxies

**`HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` are honoured.** Nothing needs
enabling. `bb` builds its HTTP transport from Go's default, which reads these
variables, so an environment already configured for other tooling works
unchanged.

```bash
export HTTPS_PROXY=http://proxy.corp.example:3128
export NO_PROXY=.internal.example,localhost,127.0.0.1
bb repo list --limit 10
```

Lowercase spellings (`https_proxy`, `no_proxy`) work too. Where both spellings
are set the **uppercase one wins**, for all three variables.

Two details that catch people out, both inherited from Go's standard library:

- **`HTTP_PROXY` does not apply to `https://` URLs.** Requests are matched by
  the scheme of the target, so an HTTPS Bitbucket uses `HTTPS_PROXY` and ignores
  `HTTP_PROXY` entirely. Setting only `HTTP_PROXY` against an HTTPS host looks
  like the proxy being ignored.
- **`NO_PROXY` matches on host suffix, not on URL prefix.** `NO_PROXY=example.com`
  covers `bitbucket.example.com`. It does not understand paths, and `*` as a
  whole value means "never proxy anything".

To confirm what is actually happening, raise the log level — the request and its
outcome are reported on stderr, so this stays safe to combine with `--json`:

```bash
bb --log-level debug --log-format jsonl repo list --limit 1
```

### Proxies and git are configured separately

`bb repo clone` and `bb pr checkout` do their work by running `git`, and **git
does not read `HTTPS_PROXY` the way `bb` does** — it has its own `http.proxy`
setting. So `bb` reaching Bitbucket does not by itself mean `git` will.

If API calls work and clones hang, configure git as well:

```bash
git config --global http.proxy http://proxy.corp.example:3128
```

Git does consult the environment as a fallback, but its precedence and matching
rules differ from Go's, and an explicit `http.proxy` removes the ambiguity.

## TLS with an internal CA

Where Bitbucket presents a certificate issued by an internal authority, point
`bb` at the CA bundle:

```bash
bb --ca-file /etc/ssl/certs/corp-root-ca.pem repo list
```

or set it once:

```bash
export BB_CA_FILE=/etc/ssl/certs/corp-root-ca.pem
```

**The bundle is added to the system trust store, not substituted for it.** `bb`
loads the platform's own roots first and appends yours, so a single `--ca-file`
does not break verification of any other host — which matters because the same
process also talks to GitHub when checking for updates.

The file must be PEM. If it contains no parseable certificate, `bb` fails with
`parse CA bundle: no certificates found` rather than silently continuing
unverified.

Connections negotiate **TLS 1.2 at minimum**.

### Turning verification off

```bash
bb --insecure-skip-verify repo list
```

This disables certificate verification for the whole invocation, which means an
interception proxy or a spoofed host cannot be distinguished from the real one.
`bb` prints a warning every time it is used, and the warning is deliberate: this
is for a development instance with a self-signed certificate, not for making a
production error message go away.

The fix for a production trust failure is `--ca-file`, and the difference matters
— one tells `bb` who to trust, the other tells it to stop checking.

## Diagnosing a connection

Work outward from the network to the credential:

```bash
curl -sS -o /dev/null -w '%{http_code}\n' "$BITBUCKET_URL/status"
```

`/status` needs no authentication, so a failure here is network, proxy or TLS,
and never a token problem. Then:

```bash
bb --log-level debug auth status
```

which exercises the same transport `bb` uses for everything else, with the
credential in play.

| Symptom | Usual cause |
|---|---|
| `x509: certificate signed by unknown authority` | Internal CA not trusted — set `--ca-file`. |
| Hangs, then a timeout | Proxy required and not configured, or `NO_PROXY` missing an internal host. |
| Works for `bb`, hangs for `bb repo clone` | Git's proxy configured separately — see above. |
| `401` or `403` from `bb`, `200` from `/status` | Network is fine; this is authentication. See [`bb auth status`](../installation-and-quickstart.md#authenticate-to-bitbucket). |

## See also

- [Environment Variables](../reference/environment.md)
- [Git authentication](git-authentication.md) — how `bb` supplies credentials to git
- [Machine Mode and Diagnostics](machine-mode-diagnostics.md)
