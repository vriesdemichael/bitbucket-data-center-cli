# System Policy

Settings an administrator applies to a machine, in a file only an administrator
can write. Most of them are mandates: a user who sets the same thing through a
flag, an environment variable or their own configuration is refused or warned,
rather than quietly winning.

## Where policy is read from

| Platform | Sources, in the order they are merged |
|---|---|
| Linux, macOS | `/etc/bb/config.yaml` |
| Windows | `%ProgramData%\bb\config.yaml`, then `HKEY_LOCAL_MACHINE\Software\Policies\bb` |

Later sources win, so on Windows a registry value overrides the same key in the
file. `%ProgramData%` is resolved through the Windows known-folder API rather
than read from the environment, so setting a `ProgramData` variable cannot
redirect where policy comes from.

Keys may sit at the top level of the file or under a `policies:` mapping; both
are read, and `policies:` wins.

```yaml
policies:
  require_keyring: true
  allowed_hosts:
    - https://bitbucket.example.com
  disable_update: true
```

## Keys

| Key | Type | Effect |
|---|---|---|
| `require_keyring` | boolean | Mandate OS keyring storage for credentials and prohibit the plaintext config file fallback. |
| `allowed_hosts` | list of strings | Permitted Bitbucket instance URLs or hostnames. A host outside the list is refused. |
| `ca_file` | string | Path to a PEM CA bundle that `bb` must use. |
| `allow_insecure_skip_verify` | boolean | When `false`, `--insecure-skip-verify` cannot be enabled. |
| `disable_update` | boolean | Disable `bb update` machine-wide. |
| `update_base_url` | string | Base URL of an internal release manifest and asset mirror. |
| `mcp_audit_file` | string | Mandate where `bb ai mcp serve` writes its JSON Lines audit trail. Accepts a path or the literal `stderr`. |
| `update_trusted_root` | string | Path to a Sigstore `trusted_root.json`, so release signatures verify without reaching the Sigstore TUF CDN. |
| `update_tuf_url` | string | Base URL of an internally mirrored Sigstore TUF repository. Mutually exclusive with `update_trusted_root`. |
| `update_signature_identity` | string | Expected certificate SAN of the release signer, for organisations that re-sign mirrored artifacts. |
| `update_signature_issuer` | string | Expected OIDC issuer of the release signer. |
| `allow_unverified_update` | boolean | Permit `bb update` without Sigstore signature verification. A last resort; SHA256 checksum verification still applies. |

## What a user meets when policy refuses them

Policy does not fail silently, and the message names policy as the reason:

- A host outside `allowed_hosts` is refused before any request is sent.
- A `--ca-file` naming a different bundle from the mandated one is an error;
  omitting it uses the mandated bundle.
- `BB_REQUIRE_KEYRING=false` against `require_keyring: true` is ignored, and
  `bb` prints a warning saying policy mandates keyring storage.
- `bb update` under `disable_update` exits `3` (`authorization`) and says to use
  the system package manager.

## `update_base_url` is a default, not a mandate

It is the one key here a user can override. The updater takes the first of
`--base-url`, `BB_UPDATE_BASE_URL`, the workspace configuration, the user's
stored configuration, and only then policy — so setting it in policy supplies a
mirror to anyone who has not chosen one, and does not pin them to it.

Use `disable_update: true` where the requirement is that a machine never fetches
its own binary. Setting `update_base_url` alone does not achieve that.

## Update trust is settable from policy only

`update_trusted_root`, `update_tuf_url`, `update_signature_identity`,
`update_signature_issuer` and `allow_unverified_update` are read from policy and
from nowhere else — there is no flag and no environment variable for any of
them, and the names above are ignored if exported.

That asymmetry is deliberate rather than an oversight. These five decide who may
vouch for a new `bb` binary, so a value settable by anything that can write a
variable into a shell would let that thing approve its own update.

## See also

- [Enterprise Hardening](../advanced/enterprise-hardening.md) — the deployment
  runbook these keys belong to, with worked examples.
- [Environment Variables](environment.md) — the per-user settings, and which of
  them policy overrides.
- [`config.schema.json`](schemas/config.schema.json) — the generated schema,
  which is what these descriptions are derived from.
