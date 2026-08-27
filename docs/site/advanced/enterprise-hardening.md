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

1. **Mandate Keyring Storage**:
   ```bash
   export BB_REQUIRE_KEYRING=1
   ```
   When set, `bb` hard-refuses to read credentials from or write credentials to the plaintext configuration fallback (`~/.config/bb/config.yaml` on Linux, `~/Library/Application Support/bb/config.yaml` on macOS, or `%AppData%\bb\config.yaml` on Windows). Any command that would otherwise rely on plaintext fallback aborts with an error ([ADR-047](../adr/047-credential-input-and-keyring-enforcement.md)).

2. **Configure Host-Scoped Git Credential Helper**:
   ```bash
   bb auth setup-git
   ```
   Writes a host-scoped credential helper into the user's global `~/.gitconfig`:
   ```ini
   [credential "https://bitbucket.example.com"]
   	helper = !"/usr/local/bin/bb" auth git-credential
   ```
   *Note: `bb` writes the absolute executable path into the git configuration.* Git queries `bb` dynamically on demand for that specific host, ensuring zero credentials are ever written into local repository `.git/config` files and credentials are never offered to external remotes ([ADR-044](../adr/044-git-credential-helper-instead-of-persisted-credentials.md)).

3. **Disable Stored Config for Headless CI**:
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

macOS developer workstations authenticate through the **Apple Keychain** (Security framework). Because macOS defaults to `zsh`, system-wide environment variables belong in `/etc/zshenv`.

```bash
# 1. Distribute via Homebrew or universal binary
brew install vriesdemichael/tap/bb

# 2. Deploy Enterprise Root CA to System Keychain
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain \
  /Library/Application\ Support/Corporate/Certs/corp-root-ca.pem

# 3. Deploy Managed Environment (/etc/zshenv)
# IMPORTANT: BB_CA_FILE must point to a file that already exists on disk.
sudo tee -a /etc/zshenv >/dev/null <<'EOF'
export BB_REQUIRE_KEYRING=1
export BB_CA_FILE="/Library/Application Support/Corporate/Certs/corp-root-ca.pem"
EOF
```

!!! warning "GUI Applications and Environment Variables"
    macOS GUI applications (such as VS Code or Cursor) do not inherit environment variables from `/etc/zshenv` or `/etc/profile.d/`. When configuring IDE MCP servers, provide `BB_CA_FILE` directly in the IDE settings `"env"` block.

---

### B. Linux Workstations (Ansible)

Linux workstations authenticate through the **Secret Service API over D-Bus** (GNOME Keyring / KWallet).

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

    - name: Configure system-wide environment variables
      copy:
        dest: /etc/profile.d/bb.sh
        mode: '0644'
        content: |
          export BB_REQUIRE_KEYRING=1
          export BB_CA_FILE=/etc/ssl/certs/corp-root-ca.pem
```

---

### C. Windows Workstations (Microsoft Intune / PowerShell)

Windows workstations authenticate through **Windows Credential Manager** (DPAPI).

```powershell
# Run as Administrator via Intune or administrative PowerShell
$Version = "[[ bb_version ]]"

# 1. Install via WinGet
winget install --id vriesdemichael.bb --exact --version $Version --accept-source-agreements --accept-package-agreements

# 2. Deploy Corporate Root CA
$CertDir = "C:\ProgramData\Corporate\Certs"
New-Item -ItemType Directory -Force -Path $CertDir | Out-Null
Copy-Item ".\corp-root-ca.pem" -Destination "$CertDir\corp-root-ca.pem"

# 3. Configure Machine-Level Environment Variables
# IMPORTANT: BB_CA_FILE must exist before setting the variable.
[Environment]::SetEnvironmentVariable("BB_REQUIRE_KEYRING", "1", "Machine")
[Environment]::SetEnvironmentVariable("BB_CA_FILE", "$CertDir\corp-root-ca.pem", "Machine")
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
