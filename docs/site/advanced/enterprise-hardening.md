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
   Deploy a machine-level configuration file (`/etc/bb/config.yaml` on Linux/macOS, `%ProgramData%\bb\config.yaml` on Windows) or native Windows Registry policy keys (`HKLM\Software\Policies\bb`). Policies defined at this tier are immutable and cannot be overridden by user shell environment variables, user config files, or repository workspace configs:
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
   - **Internal Release Mirrors**: In firewalled or air-gapped enterprise enclaves, configure `bb update` to query internal mirrors (e.g. JFrog Artifactory, Sonatype Nexus) instead of `api.github.com` via `--base-url <url>`, `BB_UPDATE_BASE_URL`, or `update_base_url` in system/user config.

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
$ConfigDir = "C:\ProgramData\bb"
New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
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
- `.data.credential_storage`: Must report `keyring` (on workstations) or `environment` (in CI). If it reports `config-file-plaintext`, `BB_REQUIRE_KEYRING=1` is missing.
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
