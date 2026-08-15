# Security Policy

## Reporting a vulnerability

**Please do not open a public issue for a security vulnerability.**

Use GitHub's [private vulnerability
reporting](https://github.com/vriesdemichael/bitbucket-data-center-cli/security/advisories/new)
on the Security tab. It keeps the report private until a fix is published, and
gives us somewhere to collaborate on the fix and publish an advisory when it
lands.

Please include:

- the version (`bb --version`) and platform
- the command or MCP tool involved
- what an attacker can do with it, and what access they need first
- a reproduction if you have one

You do not need a proof-of-concept exploit. A clear description of the weakness
is enough.

## What to expect

This project is maintained by one person, so response times reflect that rather
than a staffed security team:

| | |
|---|---|
| Acknowledgement | within 5 working days |
| Initial assessment | within 10 working days |
| Fix or documented mitigation | depends on severity; you will be told which |

You will be credited in the advisory and release notes unless you ask not to be.
If a report turns out to be a non-issue you will get an explanation, not silence.

Please give a reasonable window to ship a fix before disclosing publicly. If you
hear nothing within the acknowledgement window, escalating publicly is fair.

## Supported versions

| Version | Supported |
|---|---|
| Latest release | Yes |
| Anything older | No — upgrade to the latest release |

There are no maintenance branches. Security fixes ship in a new release from
`main`, and the fix for a reported issue will be in the next release after it is
resolved.

## Scope

**In scope**

- The `bb` binary and everything it ships
- The built-in MCP server (`bb ai mcp serve`)
- The release pipeline and the artifacts it publishes
- Credential handling: how tokens are stored, transmitted, and passed to `git`

**Out of scope**

- The local Bitbucket test stack under `docker/`. It is a disposable test
  instance with well-known credentials (`admin`/`admin`) and is not intended to
  be exposed. Its weaknesses are deliberate.
- Vulnerabilities in Bitbucket Data Center itself. Report those to
  [Atlassian](https://www.atlassian.com/trust/security/report-a-vulnerability).
- Findings that require an attacker to already control the machine `bb` runs on,
  unless `bb` meaningfully widens that access.

## Verifying a release

Every release publishes Sigstore/cosign signatures and GitHub build provenance
attestations. To verify a downloaded archive:

```bash
cosign verify-blob \
  --bundle sha256sums.txt.sigstore.json \
  --certificate-identity 'https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  sha256sums.txt
```

Each archive carries its own bundle too, so you can verify an individual
artifact the same way (`bb_2.0.2_linux_amd64.tar.gz.sigstore.json`), or use the
build provenance attestation:

```bash
gh attestation verify bb_2.0.2_linux_amd64.tar.gz \
  --repo vriesdemichael/bitbucket-data-center-cli
```

The signing identity is pinned to the release workflow on `refs/heads/main`;
`bb update` hard-fails on an identity mismatch rather than trusting release
metadata.

## Credential handling

`bb` stores credentials in the OS keyring where one is available, and in a
config file with `0600` permissions inside a `0700` directory where one is not.
Credential handling is in scope for this policy; if you find a way to expose a
token that is not already documented behaviour, please report it privately as
above.

## Telemetry

`bb` sends no telemetry and makes no network calls other than to the Bitbucket
host you configure. The only exception is `bb update`, which contacts
`api.github.com` and only when you run it explicitly.
