# Fleet Controls

The settings an administrator uses to govern `bb` on every machine: system policy, update controls and release mirrors, builds without self-update, and trust for an internal PKI. [System Policy](../reference/system-policy.md) lists every key; this page is how to use them, and [Fleet Deployment](fleet-deployment.md) how to roll them out.

Distinguish between **enforceable technical controls** (which systems engineers deploy via configuration) and **socialized practices** (which developers and CI authors follow).

## Deployable Fleet Controls

1. **System Configuration and Immutable Administrative Policies ([ADR-058](../adr/058-system-wide-configuration-and-policy-enforcement.md))**:
   Deploy a machine-level configuration file (`/etc/bb/config.yaml` on Linux/macOS, `%ProgramData%\bb\config.yaml` on Windows) or native Windows Registry policy keys (`HKLM\Software\Policies\bb`). Policies defined at this tier are immutable and cannot be overridden by user shell environment variables, user config files, or repository workspace configs — including the path the policy file is read from:
   ```yaml
   # yaml-language-server: $schema=https://raw.githubusercontent.com/vriesdemichael/bitbucket-data-center-cli/main/docs/reference/schemas/config.schema.json
   $schema: https://raw.githubusercontent.com/vriesdemichael/bitbucket-data-center-cli/main/docs/reference/schemas/config.schema.json
   require_keyring: true
   ca_file: /etc/ssl/certs/corp-root-ca.pem
   allowed_hosts:
     - https://bitbucket.example.com
   allow_insecure_skip_verify: false
   allow_http_update: false
   disable_update: true
   update_base_url: https://artifactory.example.com/artifactory/bb-releases
   ```
   - **JSON Schema Validation**: All configuration files are validated against [`config.schema.json`](../reference/schemas/config.schema.json). Supplying the `$schema` directive enables live linting and autocompletion in VS Code and IntelliJ. On the host, `bb doctor` reports every key the schema rejects in the deployed file, and the source each policy setting comes from.
   - `require_keyring: true`: Enforces OS keyring storage machine-wide; refuses fallback to plaintext files even if `BB_REQUIRE_KEYRING` is unset or set to `0`. If a user sets `BB_REQUIRE_KEYRING=0`, `bb` outputs an explicit warning to `stderr` and continues enforcing keyring policy.
   - `ca_file: <path>`: Mandates corporate Root CA bundle. Attempts to pass a conflicting CA file abort with an authorization error.
   - `allowed_hosts: [...]`: Whitelists permitted Bitbucket Data Center instances. Connection attempts to unlisted hosts abort with an authorization error.
   - `allow_insecure_skip_verify: false`: Hard-refuses `--insecure-skip-verify` and `BB_INSECURE_SKIP_VERIFY=true`.
   - `allow_http_update: false`: Hard-refuses plain-HTTP update URLs, `bb update --allow-http` and `BB_ALLOW_HTTP_UPDATE=1`.

2. **Restricting What `bb` May Be Used For ([ADR-100](../adr/100-administrators-can-switch-bb-off-or-make-it-read-only.md))**:
   Three policy keys decide whether `bb`, or part of it, may be used on a managed machine. No user setting lifts them:
   ```yaml
   policy:
     disable_mcp_server: true   # no MCP client can start bb ai mcp serve
     read_only: true            # bb reads and previews, and never changes Bitbucket
   ```
   - `disable_mcp_server: true`: For an organisation that has not approved AI agents working through `bb`. `bb ai mcp serve` is refused, and people keep using `bb` in the terminal.
   - `read_only: true`: For machines, or agents, that may read Bitbucket but never change it. Every command that changes Bitbucket is refused before a request is sent, `bb api` sends only `GET` and `HEAD`, and `bb ai mcp serve` offers only the tools that read.
   - `disable_bb: true`: For a machine where `bb` has no business running, a copy a user downloaded themselves included. Every command is refused except help, `--describe`, `--version`, `bb doctor` and printing a completion script.

   These govern the `bb` binary, not the account. Against a person or an agent with a shell, the control that holds is still a token with read permissions only; see [Switching bb off, or part of it](../reference/system-policy.md#switching-bb-off-or-part-of-it).

3. **Enterprise Update Controls and Release Mirrors ([ADR-059](../adr/059-enterprise-update-controls-and-release-mirrors.md))**:
   - **Disabling In-Place Self-Updates**: On managed corporate machines where software must be installed exclusively through IT package managers (e.g. Jamf, Ansible, Intune, SCCM), disable `bb update` by setting `disable_update: true` in system configuration or `export BB_DISABLE_UPDATE=1`. Alternatively, deploy the `_noupdate` builds described below, which cannot self-update whatever the configuration says.
   - **Internal Release Mirrors**: In firewalled or air-gapped enterprise enclaves, configure `bb update` to query internal mirrors (e.g. JFrog Artifactory, Sonatype Nexus) instead of `api.github.com` via `--base-url <url>`, `BB_UPDATE_BASE_URL`, or `update_base_url` in system/user config. A mirror alone is not sufficient on a host with no internet access: pair it with an offline trust root, below. Mirror URLs must be `https`: a plain-HTTP mirror needs `bb update --allow-http` or `BB_ALLOW_HTTP_UPDATE=1`, and `allow_http_update: false` in system configuration refuses it for every user (`true` permits it fleet-wide).
   - **Offline Signature Verification ([ADR-063](../adr/063-offline-release-signature-verification.md))**: By default, `bb update` fetches Sigstore trust material from `https://tuf-repo-cdn.sigstore.dev` on every run. Deploy a `trusted_root.json` alongside the corporate CA bundle and point at it to verify releases with no internet access at all:
     ```yaml
     update_trusted_root: /etc/bb/trusted_root.json
     ```
     Produce the file once, on a host that does have access:
     ```bash
     cosign trusted-root create > trusted_root.json
     ```
     This verifies the public releases as published — signed certificate timestamps, the Rekor inclusion promise, and observer timestamps are all checked against keys inside the file — so mirroring artifacts byte-for-byte needs no re-signing. Refresh the file when Sigstore rotates its keys, which is a multi-year event and another file push. Organisations that mirror the Sigstore TUF repository itself can set `update_tuf_url: <url>` instead; the two are mutually exclusive, it must be an absolute `https` URL, and the TUF fetch uses the configured `ca_file` and client certificates.
   - **Mirrored Trust Root**: Hosts that reach an internal mirror but not the internet can point `update_tuf_url` at a mirror of Sigstore's public TUF repository instead of deploying a file. `bb` verifies what that mirror serves against the Sigstore root compiled into `bb`, so it has to be a byte-for-byte mirror of `https://tuf-repo-cdn.sigstore.dev`, not a TUF repository of your own. The metadata it fetches is cached under `.sigstore/root` in the home directory of the user running `bb update`.
   - **Re-Signed Artifacts**: Organisations that rebuild or re-sign `bb` against their own Fulcio instance replace the pinned signer with `update_signature_identity` (certificate SAN) and `update_signature_issuer` (OIDC issuer).
   - **Unverified Updates (Last Resort)**: `allow_unverified_update: true` skips signature verification entirely. SHA256 checksum verification remains mandatory, so this still catches corruption but not tampering; every run prints a warning to stderr and reports `trust.signatureSkipped: true` in `--json` output. Prefer an offline trust root.
   - **Policy Only**: `update_trusted_root`, `update_tuf_url`, `update_signature_identity`, `update_signature_issuer` and `allow_unverified_update` are read from system configuration and Windows registry policy only — never from an environment variable or a flag. Each decides who may vouch for a binary `bb` is about to execute, and that decision does not belong to whoever can set a variable in a user's shell. `update_base_url` keeps its flag and environment forms, because signature verification still gates whatever the mirror serves.
   - **Mirror Layout**: The mirror serves a GitHub-release-shaped manifest at `/repos/vriesdemichael/bitbucket-data-center-cli/releases/latest`, or — for generic Artifactory/Nexus repositories — at `/releases/latest` or `/latest`, which `bb` tries in that order:
     ```json
     {
       "tag_name": "v[[ bb_version ]]",
       "html_url": "https://artifactory.example.com/artifactory/bb-releases",
       "assets": [
         { "name": "bb_[[ bb_version ]]_linux_amd64.tar.gz", "browser_download_url": "bb_[[ bb_version ]]_linux_amd64.tar.gz" },
         { "name": "sha256sums.txt", "browser_download_url": "sha256sums.txt" },
         { "name": "sha256sums.txt.sigstore.json", "browser_download_url": "sha256sums.txt.sigstore.json" }
       ]
     }
     ```
     Mirror `bb_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows), `sha256sums.txt`, and `sha256sums.txt.sigstore.json` at the base URL. The mirror has to allow anonymous downloads of them: `bb update` sends no credentials, only a TLS client certificate when one is configured. Where the artifact server requires a login for every download, install `bb` through your own tooling instead, with the [`_noupdate` builds](#builds-with-self-update-compiled-out). Asset URLs may be relative, as above, or absolute mirror URLs; a manifest copied verbatim from GitHub also works, since `bb` fetches an off-mirror asset URL from `{base_url}/{asset_name}`, and never from the `github.com` address the manifest names. Verify a mirror without replacing any binary:
     ```bash
     bb update --dry-run --base-url https://artifactory.example.com/artifactory/bb-releases
     ```
     The dry run verifies whatever release the mirror serves, the installed version included: the signature on `sha256sums.txt` against the configured trust material, that file's entry for this platform's archive, and the archive against that entry. It reports the trust material used and each check that passed (`preview.data.trust` under `--json`), and fails with exit `5` (`conflict`) when the mirror serves a release older than the installed one, the sign of a mirror that has stopped receiving releases.

4. **Mandate Keyring Storage (Advisory / User Tier)**:
   ```bash
   export BB_REQUIRE_KEYRING=1
   ```
   When set in user environments where system policy is not yet deployed, `bb` refuses to read credentials from or write credentials to the plaintext configuration fallback (`~/.config/bb/config.yaml` on Linux, `~/Library/Application Support/bb/config.yaml` on macOS, or `%AppData%\bb\config.yaml` on Windows). Any command that would otherwise rely on plaintext fallback aborts with an error ([ADR-047](../adr/047-credential-input-and-keyring-enforcement.md)).

5. **Configure Host-Scoped Git Credential Helper**:
   ```bash
   bb auth setup-git
   ```
   Writes a host-scoped credential helper into the user's global `~/.gitconfig`:
   ```ini
   [credential "https://bitbucket.example.com"]
   	helper = !"/usr/local/bin/bb" auth git-credential
   ```
   *Note: `bb` writes the absolute executable path into the git configuration.* Git queries `bb` dynamically on demand for that specific host, ensuring zero credentials are ever written into local repository `.git/config` files and credentials are never offered to external remotes ([ADR-044](../adr/044-git-credential-helper-instead-of-persisted-credentials.md)).

6. **Disable Stored Config for Headless CI**:
   ```bash
   export BB_DISABLE_STORED_CONFIG=1
   ```
   An ephemeral CI/CD runner then reads its credential from `BITBUCKET_TOKEN` only: no stored credential is read, and no desktop keyring is contacted.

## Builds With Self-Update Compiled Out

Every release publishes each platform twice. The `_noupdate` archives are built
with `-tags no_self_update`, so `bb update` in one of them exits `3`
(`authorization`) with:

```text
self-update is disabled in this build; update bb using your system package manager
```

Everything else behaves identically.

This matters for a fleet in two ways.

**It is what your users already have.** The WinGet, Scoop and Homebrew manifests
all reference `_noupdate` archives, so a developer who installed through a
package manager cannot self-update regardless of policy. A binary that replaces
itself behind a package manager leaves that manager's records describing a
version no longer on disk.

**It survives a machine you do not control.** `disable_update` and
`BB_DISABLE_UPDATE` are configuration, and configuration can be missing —
an imaging step that has not run yet, a container built from a bare archive, a
developer who installed the binary by hand. A `_noupdate` build refuses in all
of those cases because the capability is not compiled in. Prefer it wherever the
requirement is that a host never fetches its own binary; use `disable_update`
where you need the same result on hosts that already have the ordinary build.

Both spellings are published, versioned and version-less:

```text
bb_[[ bb_version ]]_linux_amd64_noupdate.tar.gz
bb_linux_amd64_noupdate.tar.gz
```

## Socialized Developer Practices

1. **Pipe Tokens via Stdin (Never in CLI Flags)**:
   No flag takes a credential value; the secret goes over stdin or `BITBUCKET_TOKEN`. Flag values are visible to local processes in `ps aux`, `/proc/<pid>/cmdline`, Windows Task Manager, EDR sensors, and shell history files.
   ```bash
   # Provide the token via pipe
   printf "%s" "$BITBUCKET_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin

   # Or stream directly from a secure file
   cat /run/secrets/bitbucket_token | bb auth login https://bitbucket.example.com --token-stdin
   ```

2. **Remove Tokens from Existing Clones**:
   A clone's local `.git/config` can hold a plaintext token in `http.extraHeader`:
   ```bash
   git config --local --unset-all http.extraHeader
   ```



## Critical Imaging Order: Deploy CA Before Env Var
`bb` initializes its TLS transport during client construction. If `BB_CA_FILE` points to a non-existent path, `bb` immediately aborts with:
```
read CA bundle: open /etc/ssl/certs/corp-root-ca.pem: no such file or directory
```
Ensure provisioning scripts place the CA certificate on disk **before** exporting `BB_CA_FILE`.

## Trust, Client Certificates and Proxies
[Networks, Proxies and TLS](networks-proxies-and-tls.md) covers each setting: a CA bundle is added to the system's trust store rather than replacing it, a client certificate and key for mutual TLS at an ingress gateway can be set per host or per runner ([ADR-060](../adr/060-mutual-tls-client-certificate-authentication.md)), and `HTTPS_PROXY` and `NO_PROXY` are honoured for every request. For a fleet, set the CA bundle through system policy (`ca_file`) so that no user can replace it.

## Multi-Server Estates (Avoid Blanket `BITBUCKET_URL`)
!!! warning "Do Not Pin `BITBUCKET_URL` Fleet-Wide in Multi-Server Estates"
    If your organization operates multiple Bitbucket instances (e.g. post-acquisition environments, production vs. staging), do **not** export a static `BITBUCKET_URL` in `/etc/profile.d/` or `/etc/zshenv`. Setting `BITBUCKET_URL` globally overrides local repository context discovery. Instead, let developers configure server profiles (`bb auth login <host>`) or rely on automatic clone URL discovery ([Repository Discovery and Server Switching](repository-discovery-and-server-switching.md)).


