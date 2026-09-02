# Fleet Hardening Runbook

A practical, recipe-driven deployment and operational runbook for platform engineers, systems administrators, and DevOps teams deploying and governing `bb` across corporate fleets.

For the formal threat model, trust boundaries, and compliance evaluations, see the [Security Architecture and Threat Model](threat-model.md).

---

## 1. Release Verification (Pre-Deployment)

Before packaging or mirroring `bb` into internal registries (e.g. Artifactory, Nexus, internal apt/yum/winget repos), verify the authenticity and build provenance of the downloaded release artifacts. Set the target version (e.g. `[[ bb_version ]]`) in your verification environment:

### A. Sigstore Keyless Signature Verification
Every release publishes keyless OIDC signatures bound to the official GitHub Actions release workflow on `refs/heads/main`:

```bash
VERSION="[[ bb_version ]]"
cosign verify-blob \
  --bundle "bb_${VERSION}_linux_amd64.tar.gz.sigstore.json" \
  --certificate-identity 'https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  "bb_${VERSION}_linux_amd64.tar.gz"
```

### B. GitHub Build Provenance Attestation
Verify that the binary was built on official GitHub-hosted runners directly from the source repository:

```bash
VERSION="[[ bb_version ]]"
gh attestation verify "bb_${VERSION}_linux_amd64.tar.gz" \
  --repo vriesdemichael/bitbucket-data-center-cli
```

### C. Software Bill of Materials (SPDX 2.3 SBOM)
Verify that the released archive matches the signed SPDX dependency graph:

```bash
VERSION="[[ bb_version ]]"
gh attestation verify "bb_${VERSION}_linux_amd64.tar.gz" \
  --repo vriesdemichael/bitbucket-data-center-cli \
  --predicate-type https://spdx.dev/Document
```

---

## 2. Fleet Security Controls

Distinguish between **enforceable technical controls** (which systems engineers deploy via configuration) and **socialized practices** (which developers and CI authors follow).

### Deployable Fleet Controls

1. **System Configuration and Immutable Administrative Policies ([ADR-058](../adr/058-system-wide-configuration-and-policy-enforcement.md))**:
   Deploy a machine-level configuration file (`/etc/bb/config.yaml` on Linux/macOS, `%ProgramData%\bb\config.yaml` on Windows) or native Windows Registry policy keys (`HKLM\Software\Policies\bb`). Policies defined at this tier are immutable and cannot be overridden by user shell environment variables, user config files, or repository workspace configs — including the path the policy file is read from:
   ```yaml
   # yaml-language-server: $schema=https://raw.githubusercontent.com/vriesdemichael/bitbucket-data-center-cli/main/docs/reference/schemas/config.schema.json
   $schema: https://raw.githubusercontent.com/vriesdemichael/bitbucket-data-center-cli/main/docs/reference/schemas/config.schema.json
   require_keyring: true
   ca_file: /etc/ssl/certs/corp-root-ca.pem
   allowed_hosts:
     - https://bitbucket.corp.internal
   allow_insecure_skip_verify: false
   disable_update: true
   update_base_url: https://artifactory.corp.internal/artifactory/bb-releases
   ```
   - **JSON Schema Validation**: All configuration files are validated against [`config.schema.json`](../reference/schemas/config.schema.json). Supplying the `$schema` directive enables live linting and autocompletion in VS Code, IntelliJ, and CI pipelines (e.g. `check-jsonschema`).
   - `require_keyring: true`: Enforces OS keyring storage machine-wide; refuses fallback to plaintext files even if `BB_REQUIRE_KEYRING` is unset or set to `0`. If a user sets `BB_REQUIRE_KEYRING=0`, `bb` outputs an explicit warning to `stderr` and continues enforcing keyring policy.
   - `ca_file: <path>`: Mandates corporate Root CA bundle. Attempts to pass a conflicting CA file abort with an authorization error.
   - `allowed_hosts: [...]`: Whitelists permitted Bitbucket Server / Data Center instances. Connection attempts to unlisted hosts abort with an authorization error.
   - `allow_insecure_skip_verify: false`: Hard-refuses `--insecure-skip-verify` and `BB_INSECURE_SKIP_VERIFY=true`.

2. **Enterprise Update Controls and Release Mirrors ([ADR-059](../adr/059-enterprise-update-controls-and-release-mirrors.md))**:
   - **Disabling In-Place Self-Updates**: On managed corporate machines where software must be installed exclusively through IT package managers (e.g. Jamf, Ansible, Intune, SCCM), disable `bb update` by setting `disable_update: true` in system configuration or `export BB_DISABLE_UPDATE=1`. Alternatively, install builds compiled with `-tags no_self_update`.
   - **Internal Release Mirrors**: In firewalled or air-gapped enterprise enclaves, configure `bb update` to query internal mirrors (e.g. JFrog Artifactory, Sonatype Nexus) instead of `api.github.com` via `--base-url <url>`, `BB_UPDATE_BASE_URL`, or `update_base_url` in system/user config. A mirror alone is not sufficient on a host with no internet access: pair it with an offline trust root, below.
   - **Offline Signature Verification ([ADR-063](../adr/063-offline-release-signature-verification.md))**: By default, `bb update` fetches Sigstore trust material from `https://tuf-repo-cdn.sigstore.dev` on every run. Deploy a `trusted_root.json` alongside the corporate CA bundle and point at it to verify releases with no internet access at all:
     ```yaml
     update_trusted_root: /etc/bb/trusted_root.json
     ```
     Produce the file once, on a host that does have access:
     ```bash
     cosign trusted-root create > trusted_root.json
     ```
     This verifies the public releases as published — signed certificate timestamps, the Rekor inclusion promise, and observer timestamps are all checked against keys inside the file — so mirroring artifacts byte-for-byte needs no re-signing. Refresh the file when Sigstore rotates its keys, which is a multi-year event and another file push. Organisations that mirror the Sigstore TUF repository itself can set `update_tuf_url: <url>` instead; the two are mutually exclusive, it must be an absolute `https` URL, and the TUF fetch uses the configured `ca_file` and client certificates.
   - **Re-Signed Artifacts**: Organisations that rebuild or re-sign `bb` against their own Fulcio instance replace the pinned signer with `update_signature_identity` (certificate SAN) and `update_signature_issuer` (OIDC issuer).
   - **Unverified Updates (Last Resort)**: `allow_unverified_update: true` skips signature verification entirely. SHA256 checksum verification remains mandatory, so this still catches corruption but not tampering; every run prints a warning to stderr and reports `signature_skipped: true` in `--json` output. Prefer an offline trust root.
   - **Policy Only**: `update_trusted_root`, `update_tuf_url`, `update_signature_identity`, `update_signature_issuer` and `allow_unverified_update` are read from system configuration and Windows registry policy only — never from an environment variable or a flag. Each decides who may vouch for a binary `bb` is about to execute, and that decision does not belong to whoever can set a variable in a user's shell. `update_base_url` keeps its flag and environment forms, because signature verification still gates whatever the mirror serves.
   - **Mirror Layout**: The mirror serves a GitHub-release-shaped manifest at `/repos/vriesdemichael/bitbucket-data-center-cli/releases/latest`, or — for generic Artifactory/Nexus repositories — at `/releases/latest` or `/latest`, which `bb` tries in that order:
     ```json
     {
       "tag_name": "v[[ bb_version ]]",
       "html_url": "https://artifactory.corp.internal/artifactory/bb-releases",
       "assets": [
         { "name": "bb_[[ bb_version ]]_linux_amd64.tar.gz", "browser_download_url": "bb_[[ bb_version ]]_linux_amd64.tar.gz" },
         { "name": "sha256sums.txt", "browser_download_url": "sha256sums.txt" },
         { "name": "sha256sums.txt.sigstore.json", "browser_download_url": "sha256sums.txt.sigstore.json" }
       ]
     }
     ```
     Mirror `bb_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows), `sha256sums.txt`, and `sha256sums.txt.sigstore.json` at the base URL. Asset URLs may be relative, as above, or absolute mirror URLs; a manifest copied verbatim from GitHub also works, since `bb` fetches off-mirror asset URLs from `{base_url}/{asset_name}` first rather than stalling on a firewalled `github.com` address. Verify a mirror without replacing any binary:
     ```bash
     bb update --dry-run --base-url https://artifactory.corp.internal/artifactory/bb-releases
     ```
     The dry run reports which trust material was used and whether the manifest signature and checksum entry were found.

3. **Mandate Keyring Storage (Advisory / User Tier)**:
   ```bash
   export BB_REQUIRE_KEYRING=1
   ```
   When set in user environments where system policy is not yet deployed, `bb` refuses to read credentials from or write credentials to the plaintext configuration fallback (`~/.config/bb/config.yaml` on Linux, `~/Library/Application Support/bb/config.yaml` on macOS, or `%AppData%\bb\config.yaml` on Windows). Any command that would otherwise rely on plaintext fallback aborts with an error ([ADR-047](../adr/047-credential-input-and-keyring-enforcement.md)).

4. **Configure Host-Scoped Git Credential Helper**:
   ```bash
   bb auth setup-git
   ```
   Writes a host-scoped credential helper into the user's global `~/.gitconfig`:
   ```ini
   [credential "https://bitbucket.example.com"]
   	helper = !"/usr/local/bin/bb" auth git-credential
   ```
   *Note: `bb` writes the absolute executable path into the git configuration.* Git queries `bb` dynamically on demand for that specific host, ensuring zero credentials are ever written into local repository `.git/config` files and credentials are never offered to external remotes ([ADR-044](../adr/044-git-credential-helper-instead-of-persisted-credentials.md)).

5. **Disable Stored Config for Headless CI**:
   ```bash
   export BB_DISABLE_STORED_CONFIG=1
   ```
   Ensures that ephemeral CI/CD runners read authentication strictly from `BITBUCKET_TOKEN`, guaranteeing that no stored credential profile is read and no desktop keyring daemon is contacted.

### Socialized Developer Practices

1. **Pipe Tokens via Stdin (Never in CLI Flags)**:
   Avoid `--token <val>` or `--password <val>` flags. Flags are visible to local processes in `ps aux`, `/proc/<pid>/cmdline`, Windows Task Manager, EDR sensors, and shell history files.
   ```bash
   # Provide the token via pipe
   printf "%s" "$BITBUCKET_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin

   # Or stream directly from a secure file
   cat /run/secrets/bitbucket_token | bb auth login https://bitbucket.example.com --token-stdin
   ```

2. **Clean Up Legacy Clones**:
   Existing clones made before `bb auth setup-git` may contain plaintext tokens in their local `.git/config`:
   ```bash
   git config --local --unset-all http.extraHeader
   ```

---

## 3. Multi-OS Fleet Deployment Recipes

### A. macOS (Jamf Pro / Kandji / Intune)

macOS developer workstations authenticate through the **Apple Keychain** (Security framework). Deploy system configuration to `/etc/bb/config.yaml` so that all terminal sessions and GUI applications (like VS Code or Cursor invoking MCP servers) automatically inherit policy without relying on shell environment inheritance:

```bash
# 1. Distribute via Homebrew or universal binary
brew install vriesdemichael/tap/bb

# 2. Deploy Enterprise Root CA to System Keychain
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain \
  /Library/Application\ Support/Corporate/Certs/corp-root-ca.pem

# 3. Deploy Immutable System Configuration (/etc/bb/config.yaml)
# Note: Unlike /etc/zshenv, /etc/bb/config.yaml is read directly by bb in GUI IDEs as well.
sudo mkdir -p /etc/bb
sudo tee /etc/bb/config.yaml >/dev/null <<'EOF'
require_keyring: true
ca_file: /Library/Application Support/Corporate/Certs/corp-root-ca.pem
allowed_hosts:
  - https://bitbucket.corp.internal
allow_insecure_skip_verify: false
disable_update: true
EOF
sudo chmod 644 /etc/bb/config.yaml
```

---

### B. Linux Workstations (Ansible)

Linux workstations authenticate through the **Secret Service API over D-Bus** (GNOME Keyring / KWallet). Deploy `/etc/bb/config.yaml` to enforce security postures across all local users:

```yaml
- name: Deploy and harden bb across Linux workstations
  hosts: workstations
  become: true
  vars:
    bb_version: "[[ bb_version ]]"
  tasks:
    - name: Deploy Corporate Root CA bundle
      copy:
        src: files/corp-root-ca.pem
        dest: /etc/ssl/certs/corp-root-ca.pem
        owner: root
        group: root
        mode: '0644'

    - name: Download verified bb Debian package
      get_url:
        url: "https://artifactory.corp.internal/binaries/bb_{{ bb_version }}_linux_amd64.deb"
        dest: "/tmp/bb_{{ bb_version }}_linux_amd64.deb"
        mode: '0644'
      when: ansible_os_family == "Debian"

    - name: Install bb package (Debian/Ubuntu)
      apt:
        deb: "/tmp/bb_{{ bb_version }}_linux_amd64.deb"
      when: ansible_os_family == "Debian"

    - name: Deploy system-wide policy configuration
      copy:
        dest: /etc/bb/config.yaml
        owner: root
        group: root
        mode: '0644'
        content: |
          require_keyring: true
          ca_file: /etc/ssl/certs/corp-root-ca.pem
          allowed_hosts:
            - https://bitbucket.corp.internal
          allow_insecure_skip_verify: false
          disable_update: true
          update_base_url: https://artifactory.corp.internal/artifactory/bb-releases
```

---

### C. Windows Workstations (Microsoft Intune / PowerShell / GPO)

Windows workstations authenticate through **Windows Credential Manager** (DPAPI). Administrators can deploy system configuration via `%ProgramData%\bb\config.yaml` or through native Windows Group Policy / Intune CSP targeting the Windows Registry (`HKLM\Software\Policies\bb`):

```powershell
# Run as Administrator via Intune or administrative PowerShell
$Version = "[[ bb_version ]]"

# 1. Install via WinGet
winget install --id vriesdemichael.bb --exact --version $Version --accept-source-agreements --accept-package-agreements

# 2. Deploy Corporate Root CA
$CertDir = "C:\ProgramData\Corporate\Certs"
New-Item -ItemType Directory -Force -Path $CertDir | Out-Null
Copy-Item ".\corp-root-ca.pem" -Destination "$CertDir\corp-root-ca.pem"

# 3. Option A: Deploy System Configuration File (%ProgramData%\bb\config.yaml)
#    Create this directory as an administrator before any developer runs bb. C:\ProgramData
#    lets an unprivileged account create a subdirectory and become its owner, and bb never
#    creates the directory itself (ADR-058, point 5). The ACL below removes the inherited
#    Users write entry so the policy file cannot be replaced by the accounts it governs.
$ConfigDir = "C:\ProgramData\bb"
New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
$Acl = Get-Acl $ConfigDir
$Acl.SetAccessRuleProtection($true, $false)
foreach ($Identity in "SYSTEM", "Administrators") {
    $Acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule(
        $Identity, "FullControl", "ContainerInherit,ObjectInherit", "None", "Allow")))
}
$Acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule(
    "Users", "ReadAndExecute", "ContainerInherit,ObjectInherit", "None", "Allow")))
Set-Acl -Path $ConfigDir -AclObject $Acl
@"
require_keyring: true
ca_file: $CertDir\corp-root-ca.pem
allowed_hosts:
  - https://bitbucket.corp.internal
allow_insecure_skip_verify: false
disable_update: true
update_base_url: https://artifactory.corp.internal/artifactory/bb-releases
"@ | Set-Content -Path "$ConfigDir\config.yaml" -Encoding UTF8

# 4. Option B: Native Windows Registry GPO Policies (HKLM\Software\Policies\bb)
$RegPath = "HKLM:\Software\Policies\bb"
if (!(Test-Path $RegPath)) { New-Item -Path $RegPath -Force | Out-Null }
Set-ItemProperty -Path $RegPath -Name "RequireKeyring" -Value 1 -Type DWord
Set-ItemProperty -Path $RegPath -Name "CAFile" -Value "$CertDir\corp-root-ca.pem" -Type String
Set-ItemProperty -Path $RegPath -Name "AllowedHosts" -Value "https://bitbucket.corp.internal" -Type String
Set-ItemProperty -Path $RegPath -Name "AllowInsecureSkipVerify" -Value 0 -Type DWord
Set-ItemProperty -Path $RegPath -Name "DisableUpdate" -Value 1 -Type DWord
```

---

### D. CI/CD & Headless Containers (Docker / Kubernetes)

Headless runners do not have interactive desktop sessions or D-Bus daemons. Configure runners to read secrets entirely from process memory and bypass disk storage.

```dockerfile
# Hardened CI Container Pattern
FROM alpine:3.21

ARG BB_VERSION=[[ bb_version ]]

# Install runtime dependencies (ca-certificates and git)
RUN apk add --no-cache ca-certificates git curl

# Download and install official release binary
RUN curl -fsSL "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/download/v${BB_VERSION}/bb_${BB_VERSION}_linux_amd64.tar.gz" \
    | tar -xz -C /usr/local/bin bb \
    && chmod +x /usr/local/bin/bb

# Install corporate CA
COPY corp-root-ca.pem /etc/ssl/certs/corp-root-ca.pem

# Environment flags for headless isolation
ENV BB_CA_FILE=/etc/ssl/certs/corp-root-ca.pem
ENV BB_DISABLE_STORED_CONFIG=1

# Execution in CI: pass token via environment, zero disk persistence
# docker run --rm -e BITBUCKET_TOKEN=$SECRET -e BITBUCKET_URL=https://bitbucket.corp.internal my-image bb repo list
```

---

## 4. Internal PKI, Proxies & Multi-Server Estates

### Critical Imaging Order: Deploy CA Before Env Var
`bb` initializes its TLS transport during client construction. If `BB_CA_FILE` points to a non-existent path, `bb` immediately aborts with:
```
read CA bundle: open /etc/ssl/certs/corp-root-ca.pem: no such file or directory
```
Ensure provisioning scripts place the CA certificate on disk **before** exporting `BB_CA_FILE`.

### Additive Trust Pool
`bb` appends your corporate CA bundle to the system's root certificate pool (`x509.SystemCertPool()`). It does not replace public roots, allowing connections to public services (e.g. GitHub release verification) to succeed alongside internal Bitbucket calls.

### Mutual TLS (mTLS) Client Authentication
In zero-trust or defense networks requiring hardware- or PKI-backed mutual TLS at ingress gateways (Envoy, NGINX, F5, Cloudflare Access), configure client certificates and private keys ([ADR-060](../adr/060-mutual-tls-client-certificate-authentication.md)):

```bash
bb --client-cert /etc/ssl/certs/client.pem --client-key /etc/ssl/private/client.key repo list
```

In CI/CD runners or shell environments:
```bash
export BB_CLIENT_CERT=/etc/ssl/certs/client.pem
export BB_CLIENT_KEY=/etc/ssl/private/client.key
```

Or persist client certificate paths per host in stored profiles:
```bash
bb auth login https://bitbucket.corp.example --token abc --client-cert /etc/ssl/certs/client.pem --client-key /etc/ssl/private/client.key
```

Private keys are loaded directly in-memory via Go's standard `crypto/tls` package and are never logged, serialized into machine JSON envelopes, or written to configuration files.

### Corporate Forward Proxies
Configure standard proxy environment variables:
```bash
export HTTPS_PROXY=http://proxy.corp.example:3128
export NO_PROXY=.corp.internal,localhost,127.0.0.1
```

### Multi-Server Estates (Avoid Blanket `BITBUCKET_URL`)
!!! warning "Do Not Pin `BITBUCKET_URL` Fleet-Wide in Multi-Server Estates"
    If your organization operates multiple Bitbucket instances (e.g. post-acquisition environments, production vs. staging), do **not** export a static `BITBUCKET_URL` in `/etc/profile.d/` or `/etc/zshenv`. Setting `BITBUCKET_URL` globally overrides local repository context discovery. Instead, let developers configure server profiles (`bb auth login <host>`) or rely on automatic clone URL discovery ([Repository Discovery and Server Switching](repository-discovery-and-server-switching.md)).

---

## 5. AI & IDE MCP Server Governance (`bb ai mcp serve`)

`bb` includes a built-in Model Context Protocol (MCP) server for integration with AI developer tools (VS Code Agent, Cursor, Claude Desktop).

### Principle 1: Safe vs. Unsafe Tool Isolation
`bb` gates mutating and high-blast-radius operations ([ADR-039](../adr/039-built-in-mcp-server-with-host-scoping-and-token-restriction.md)):
- **Safe Tools (Enabled by default)**: Read operations, diff inspection (`get_pr_diff`), pull request listing (`list_pull_requests`), and comment threads (`list_pr_comments`, `add_pr_comment`).
- **Unsafe Tools (Withheld by default)**: High-impact operations (`submit_pr_review`, `merge_pull_request`, `enable_auto_merge`, `set_build_status`) are withheld unless `--yolo` (or `--allow-writes`) is explicitly configured. Gating `submit_pr_review` ensures an agent cannot approve its own pull requests.

Inspect exposed tools and their gating status:
```bash
bb ai mcp tools
```

### Principle 2: Dedicated Read-Only Token Scoping
Never run IDE MCP servers under personal developer credentials. Generate a dedicated read-only PAT and bind the MCP server to it:

```bash
bb ai mcp serve --host https://bitbucket.example.com --token <read-only-pat>
```

### Principle 3: Workspace Scoping

`--token` bounds what an agent may *do*; `--project` and `--repo` bound *where* ([ADR-062](../adr/062-mcp-workspace-scoping-and-agent-audit-trail.md)). On a multi-tenant instance a read-only PAT still reaches every repository its owner can read, which for most developers is most of the organisation.

```bash
bb ai mcp serve --host https://bitbucket.example.com --project PAYMENTS
```

```bash
bb ai mcp serve --host https://bitbucket.example.com --repo PAYMENTS/ledger
```

Scoping is enforced at a single choke point over every tool call, not per tool. Three behaviours are worth knowing before you configure it:

- **Omitted arguments are bound, not rejected.** `list_pull_requests` with no project reaches every repository the token can see. Under a scope the arguments are filled in, so the unbounded mode becomes the bounded one and the agent never needs to know.
- **Conflicting arguments are refused.** A call naming another project fails with an error the agent can read and correct.
- **Tools that cannot be bounded are withheld entirely.** `get_build_status` and `set_build_status` address a commit SHA, which Bitbucket does not scope to a project. They disappear from `tools/list` while a scope is set. `search_repositories` is withheld under `--repo` for the same reason: pinning its project filter would still list sibling repositories, and a filter is not a boundary.

### Principle 4: Agent Audit Trail

`--audit-file` appends one JSON Lines record per tool call, for SIEM collection:

```bash
bb ai mcp serve --host https://bitbucket.example.com --project PAYMENTS --audit-file /var/log/bb/mcp-audit.jsonl
```

```json
{"timestamp":"2026-08-29T09:30:00Z","event":"mcp_tool_invocation","tool":"get_pull_request","project":"PAYMENTS","repo":"ledger","status":"success","duration_ms":45,"user_identity":"alice","host":"https://bitbucket.example.com","scope":"PAYMENTS"}
```

`status` is `success`, `error`, or `denied`. Argument values are recorded, with tokens, passwords and URL credentials redacted. When the client sends W3C trace context, `trace_id` carries it so a record correlates with the agent's own trace.

Auditing is **off by default** — a developer who never turns it on should not accumulate a log file they will not find. Turn it on by fleet policy, not by asking developers to.

**Why audit here when Bitbucket already has an audit log.** The two answer different questions, and the CLI one is not a duplicate:

- **Attribution.** Every MCP call arrives at Bitbucket as the same user with the same PAT. Bitbucket cannot distinguish a developer reviewing a PR in a browser from an agent acting autonomously in their IDE. That distinction exists only here.
- **Denied attempts.** A call refused by the scope boundary or the safety gate **never reaches Bitbucket**, so its audit log has no record of it. Attempted-and-blocked is precisely the prompt-injection signal worth alerting on: one successful read is noise, forty denied cross-project reads in ten seconds is an incident.
- **Reads in practice.** Bitbucket's repository read events sit at *Full* coverage, which most operators do not run in production because of volume. "Bitbucket already logs everything" holds far better for writes than for reads — and an exfiltrating agent is doing reads.

Bitbucket's audit log remains authoritative for what actually changed. Correlate the two on `(timestamp, user_identity)`.

**Two limitations to state plainly.**

*This log is not tamper-evident.* It is written on the developer's machine, as the developer, to a path they can edit. Against a determined insider it proves nothing. Against a prompt-injected agent confined to MCP tools — the ADV-3 threat it is designed for — it holds, because that agent has no shell.

*An agent with shell access can bypass all of this.* Nothing stops it running `bb pr merge` directly, or any of the 233 CLI commands, none of which are scoped, gated, or audited. That is not a gap this feature can close: an agent that can run shell commands can also edit the audit file. **The control that survives it is `--token`**, because a read-only PAT binds at the Bitbucket server and does not care which local process made the call. Treat MCP scoping and auditing as defence in depth over a correctly scoped token, never as a substitute for one.

### Principle 5: Mandating Audit by Policy

An audit destination a developer can change by editing their IDE config records only what they permit. Mandate it machine-wide instead ([ADR-058](../adr/058-system-wide-configuration-and-policy-enforcement.md)):

```yaml
# /etc/bb/config.yaml  (or %ProgramData%\bb\config.yaml)
policy:
  mcp_audit_file: /var/log/bb/mcp-audit.jsonl
```

The server then audits whether or not `--audit-file` is passed, and refuses a `--audit-file` pointing anywhere else with an authorization error.

**The mandate is worth what the policy file's permissions are worth.** `bb` reads the system configuration file and never creates the directory holding it, which is deliberate — see [ADR-058](../adr/058-system-wide-configuration-and-policy-enforcement.md), point 5. On Linux and macOS creating `/etc/bb/` already requires root. On Windows it does not: `C:\ProgramData` lets any account add a subdirectory and hands its creator full control of it, so `C:\ProgramData\bb` must be created by an administrator, before any developer runs `bb`, with unprivileged accounts left read access only. Until that is done, "the developer cannot redirect this" is not a claim you can make on a Windows workstation.

`mcp_audit_file` is also the one policy setting with no `HKLM\Software\Policies\bb` value, so on Windows it is set through the file and not by GPO. The registry is the stronger channel for everything it does carry.

When a record cannot be written the call is **refused**. An audit trail that silently stops recording is worse than none, because the absence of a record then carries no information. `--audit-failure=warn` relaxes this for an operator who would rather lose records than lose the server.

**Collection.** The audit log is a file because every SIEM already tails files — Splunk Universal Forwarder, Datadog Agent, Fluent Bit, Vector, Filebeat. `bb` deliberately ships no direct SIEM integration: it would put a network call, an auth secret and retry buffering inside the tool-call path of a process that is spawned per IDE session and killed without warning. For a containerised or wrapper-managed deployment, pass `--audit-file stderr` and let the cluster log collector read the process streams. Rotation is the collector's job; `bb` appends and never truncates.

### Recommended IDE Configuration (`.vscode/settings.json`)

```json
{
  "mcp": {
    "servers": {
      "bitbucket": {
        "type": "stdio",
        "command": "bb",
        "args": [
          "ai",
          "mcp",
          "serve",
          "--host",
          "https://bitbucket.example.com",
          "--token",
          "${env:BITBUCKET_RO_TOKEN}",
          "--tools",
          "get_pull_request,list_pull_requests,get_pr_diff,list_pr_comments,add_pr_comment"
        ],
        "env": {
          "BB_CA_FILE": "/Library/Application Support/Corporate/Certs/corp-root-ca.pem"
        }
      }
    }
  }
}
```

---

## 6. Verification and Troubleshooting

### Verification Audit
Run the machine-readable auth status check to verify configuration:

```bash
bb auth status --json
```

Confirm:
- `.data.credentialStorage`: Must report `keyring` (on workstations) or `environment` (in CI). If it reports `config-file-plaintext`, `BB_REQUIRE_KEYRING=1` is missing.
- Check active git helper for your Bitbucket host:
  ```bash
  git config --global --get "credential.https://bitbucket.example.com.helper"
  ```

### Helpdesk Troubleshooting Guide

| Symptom / Error Message | Root Cause | Remediation |
|---|---|---|
| `read CA bundle: open ...: no such file or directory` | Imaging race condition: `BB_CA_FILE` was set before the CA certificate was written to disk. | Ensure the provisioning script copies the `.pem` file before setting the environment variable. |
| `OS keyring is unavailable and keyring-backed storage is required` | Running on a headless Linux host or remote SSH session without an active D-Bus session bus. | Launch a temporary D-Bus session: `eval $(dbus-launch --sh-syntax)` or supply credentials via `BITBUCKET_TOKEN`. |
| Git prompts for password on `git push`/`git pull` | Git credential helper is not scoped to the exact URL or scheme used by the remote. | Run `git remote -v` and configure: `bb auth setup-git --host <remote-url>`. |
| `certificate signed by unknown authority` | `BB_CA_FILE` is not set, or a GUI IDE failed to inherit shell environment variables. | Set `BB_CA_FILE` in the IDE's `"env"` block or export it in `/etc/zshenv` / `/etc/profile.d/bb.sh`. |
| `host "..." is not permitted by administrative policy` | Target Bitbucket instance is not listed in `allowed_hosts` in system configuration or registry policy. | Connect only to approved corporate hosts, or request security to add the instance to `allowed_hosts`. |
| `insecure TLS verification is disabled by administrative policy` | Attempted `--insecure-skip-verify` when prohibited by `allow_insecure_skip_verify: false` in system policy. | Configure the corporate CA certificate rather than disabling TLS verification. |
| `overriding CA bundle is disabled by administrative policy` | Attempted to override mandated corporate CA bundle with a conflicting custom certificate. | Remove user-level `BB_CA_FILE` override and use the mandated corporate CA. |
| `self-update is disabled by administrative policy` | Self-update is disabled machine-wide (`disable_update: true` or `BB_DISABLE_UPDATE=1`). | Update `bb` through your IT system package manager (`apt`, `dnf`, `brew`, `winget`). |
| `could not load the Sigstore trust material needed to verify the release manifest` | The host cannot reach `https://tuf-repo-cdn.sigstore.dev`, and no offline trust root is configured. The release itself is not implicated. | Deploy a `trusted_root.json` and set `update_trusted_root` in system configuration (or `update_tuf_url` for a mirrored TUF repository). |
| `update_trusted_root is invalid` | The configured trusted root path does not exist on this host — typically an imaging race, the same one that bites `ca_file`. | Ensure the provisioning script writes `trusted_root.json` before the configuration file that references it. |
| `update_trusted_root and update_tuf_url are mutually exclusive` | Both Sigstore trust sources are configured. | Keep the trusted root file for air-gapped hosts, or the TUF mirror URL — not both. |
| `update_tuf_url must be an absolute https URL` | The configured mirror is a bare hostname, a relative path, or plain `http`. | Give the full origin, for example `https://artifactory.corp.internal/tuf`. |

---

## 7. Day-2 Operations

### PAT Expiration & Rotation
Personal Access Tokens expire based on enterprise TTL policies (e.g. 90 days). When rotating a token:
1. Generate a replacement token in Bitbucket Server (`bb auth token create "Dev Token" --expiry-days 90` or via web UI).
2. Update the stored credential in the OS keyring without downtime:
   ```bash
   printf "%s" "$NEW_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin
   ```
3. Because git queries `bb` dynamically, all local repositories immediately begin using the new token without needing `.git/config` updates.

### Fleet Upgrades & Rollback
- **Upgrades**: Deploy new packages via system package managers (`apt`, `dnf`, `brew`, `winget`). Stored credentials and git helpers persist across version upgrades.
- **De-provisioning & Rollback**:
  ```bash
  # 1. Log out and remove secrets from the OS Keyring
  bb auth logout --host https://bitbucket.example.com

  # 2. Remove git credential helper configuration
  git config --global --unset-all "credential.https://bitbucket.example.com.helper"

  # 3. Remove package
  apt remove bb   # or: brew uninstall bb / winget uninstall vriesdemichael.bb
  ```

---

## Related Security Documents
- [Security Architecture and Threat Model](threat-model.md): Detailed STRIDE methodology, trust boundaries, multi-OS policy analysis, and compliance matrix.
- [Git Authentication Guide](git-authentication.md): Deep dive into host-scoped credential helper mechanics.
- [Networks, Proxies and TLS](networks-proxies-and-tls.md): Network diagnosis and connection testing.
- [Security Policy](https://github.com/vriesdemichael/bitbucket-data-center-cli/blob/main/SECURITY.md): Vulnerability reporting and disclosure policy.
