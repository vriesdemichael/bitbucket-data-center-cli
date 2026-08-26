# Fleet Hardening Runbook

A practical, recipe-driven deployment and hardening runbook for platform engineers, systems administrators, and DevOps teams deploying `bb` across corporate fleets.

For the formal threat analysis, trust boundaries, and compliance evaluations, see the [Security Architecture and Threat Model](threat-model.md).

---

## 1. The 3 Non-Negotiable Hardening Baselines

Apply these three controls across every managed workstation and developer image:

### Baseline 1: Enforce OS Keyring Storage
Prevent silent degradation to plaintext configuration files (`~/.config/bb/config.yaml`) if an OS keyring daemon is unavailable:

```bash
export BB_REQUIRE_KEYRING=1
```

When set, any command requiring credentials hard-fails if the operating system vault is unreachable, ensuring zero secrets reach disk unencrypted ([ADR-047](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/047-credential-input-and-keyring-enforcement.md)).

### Baseline 2: Pipe Tokens via Standard Input
Never pass tokens via command-line flags (`--token <val>`). Flags are exposed in process tables (`/proc/<pid>/cmdline`, `ps aux`), Windows Task Manager, EDR sensor telemetry, and plaintext shell history files.

```bash
# Provide the token via pipe
printf "%s" "$BITBUCKET_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin

# Or stream directly from a secure file
cat /run/secrets/bitbucket_token | bb auth login https://bitbucket.example.com --token-stdin
```

### Baseline 3: Host-Scoped Git Credential Helper
Do not write tokens or extra headers into repository configurations (`.git/config`). Configure git to query `bb` dynamically:

```bash
bb auth setup-git
```

This writes a single rule into the user's global `~/.gitconfig` scoped strictly to your Bitbucket host ([ADR-044](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/044-git-credential-helper-instead-of-persisted-credentials.md)):

```ini
[credential "https://bitbucket.example.com"]
	helper = !"bb" auth git-credential
```

Credentials are never stored in repositories and are never offered to external remotes (such as GitHub or GitLab).

---

## 2. Multi-OS Fleet Deployment Recipes

Enterprise workstation fleets comprise macOS, Linux, and Windows machines. Below are deployment configurations tailored to the management tooling of each operating system.

### A. macOS (Jamf Pro / Kandji / Intune)

macOS developer laptops authenticate through the **Apple Keychain** (backed by the macOS Security framework). Because macOS defaults to `zsh`, system-wide environment variables must be deployed to `/etc/zshenv` or `/etc/zprofile` (macOS has no `/etc/profile.d/`).

#### 1. Distribute Binary
Package `bb` via Homebrew (`brew install vriesdemichael/tap/bb`) or distribute the signed universal binary directly to `/usr/local/bin/bb` via your MDM.

#### 2. Deploy Hardened Environment (`/etc/zshenv`)
Deploy a script via Jamf/Kandji to write managed defaults to `/etc/zshenv`:

```bash
# /etc/zshenv - Managed by Enterprise MDM
export BB_REQUIRE_KEYRING=1
export BB_CA_FILE="/Library/Application Support/Corporate/Certs/corp-root-ca.pem"
export BITBUCKET_URL="https://bitbucket.corp.internal"
```

#### 3. Trust Corporate CA in System Keychain
Ensure the enterprise root CA is installed in the macOS System Keychain:

```bash
sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain /Library/Application\ Support/Corporate/Certs/corp-root-ca.pem
```

---

### B. Linux Workstations (Ansible / Puppet)

Managed Linux workstations (Ubuntu, Debian, RHEL, Fedora) store credentials via the **Secret Service API over D-Bus** (GNOME Keyring, KWallet, or keepassxc).

#### Ansible Hardening Task

```yaml
- name: Deploy bb enterprise baseline configuration
  hosts: workstations
  become: true
  tasks:
    - name: Install bb package (Debian/Ubuntu)
      apt:
        deb: /tmp/bb_linux_amd64.deb
      when: ansible_os_family == "Debian"

    - name: Install bb package (RHEL/CentOS)
      yum:
        name: /tmp/bb_linux_amd64.rpm
        state: present
      when: ansible_os_family == "RedHat"

    - name: Configure system-wide environment variables
      copy:
        dest: /etc/profile.d/bb.sh
        mode: '0644'
        content: |
          export BB_REQUIRE_KEYRING=1
          export BB_CA_FILE=/etc/ssl/certs/corp-root-ca.pem
          export BITBUCKET_URL=https://bitbucket.corp.internal
```

> [!NOTE]
> **D-Bus Requirement**: On Linux desktop sessions, GNOME Keyring unlocks automatically on user login via PAM. In remote SSH sessions or jump hosts, ensure a D-Bus session bus is available (`eval $(dbus-launch --sh-syntax)`) if developers interactively run `bb auth login`.

---

### C. Windows Workstations (Microsoft Intune / PowerShell)

Windows workstations authenticate through **Windows Credential Manager** (backed by DPAPI).

#### PowerShell Machine Provisioning Script (Run as Administrator)

```powershell
# 1. Install bb via WinGet (or internal MSI/ZIP)
winget install --id vriesdemichael.bb --exact --accept-source-agreements --accept-package-agreements

# 2. Set Machine-Level Environment Variables (Persistent across all users)
[Environment]::SetEnvironmentVariable("BB_REQUIRE_KEYRING", "1", "Machine")
[Environment]::SetEnvironmentVariable("BB_CA_FILE", "C:\ProgramData\Corporate\corp-root-ca.pem", "Machine")
[Environment]::SetEnvironmentVariable("BITBUCKET_URL", "https://bitbucket.corp.internal", "Machine")

# 3. Verify System Certificate Store
# Windows automatically reads the "Trusted Root Certification Authorities" store,
# but BB_CA_FILE guarantees explicit resolution for Go's x509 crypto pool.
```

---

### D. CI/CD & Headless Containers (Docker / Kubernetes)

Headless CI/CD runners and ephemeral build containers do not have desktop session buses (no D-Bus, no Keychain). **Do not use `bb auth login` in container pipelines.**

#### The Hardening Pattern: Direct Environment Injection
Pass short-lived Personal Access Tokens directly via `BITBUCKET_TOKEN`. When `BITBUCKET_TOKEN` is present, `bb` reads the secret directly from process memory and never touches disk or seeks a keyring daemon:

```dockerfile
# Hardened CI Container Pattern
FROM alpine:latest
COPY --from=ghcr.io/vriesdemichael/bb:latest /usr/local/bin/bb /usr/local/bin/bb

# Embed corporate CA
COPY corp-root-ca.pem /etc/ssl/certs/corp-root-ca.pem
ENV BB_CA_FILE=/etc/ssl/certs/corp-root-ca.pem
ENV BITBUCKET_URL=https://bitbucket.corp.internal

# In CI execution:
# docker run -e BITBUCKET_TOKEN=$RUNNER_SECRET my-build-image bb repo list
```

---

## 3. Internal PKI & Corporate Forward Proxies

### Internal Certificate Authorities (Custom Root CAs)
Enterprise Bitbucket Data Center instances typically present certificates issued by an internal CA. Furthermore, corporate forward proxies (Zscaler, Blue Coat, Palo Alto) re-sign outbound traffic.

To trust an internal CA bundle:

```bash
export BB_CA_FILE=/etc/ssl/certs/corp-root-ca.pem
```

> [!IMPORTANT]
> **Additive Trust Pool**: `bb` appends your corporate CA bundle to the operating system's default trust pool (`x509.SystemCertPool()`). It does **not** overwrite standard system roots, ensuring that both internal Bitbucket calls and external services (e.g. GitHub release verification) resolve reliably.

### Forward Proxy Configuration
`bb` natively respects standard proxy environment variables:
- `HTTPS_PROXY`: Forward proxy for HTTPS traffic (`http://proxy.corp.example:3128`).
- `HTTP_PROXY`: Forward proxy for HTTP traffic.
- `NO_PROXY`: Comma-separated domain suffixes that bypass the proxy (`.corp.internal,localhost,127.0.0.1`).

```bash
export HTTPS_PROXY=http://proxy.corp.example:3128
export NO_PROXY=.corp.internal,localhost,127.0.0.1
bb repo list --limit 5
```

---

## 4. AI & IDE MCP Server Governance (`bb ai mcp serve`)

`bb` includes a built-in Model Context Protocol (MCP) server for integration with AI developer tools (VS Code Agent, Cursor, Claude Desktop, Copilot). In corporate environments, AI access must be constrained by the Principle of Least Privilege.

### Principle 1: Safe vs. Unsafe Tool Isolation
`bb` divides its MCP tools into two distinct exposure tiers ([ADR-039](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/039-built-in-mcp-server-with-host-scoping-and-token-restriction.md)):
- **Safe Tools (Enabled by default)**: Read operations, diff inspection, drafting PRs, and posting review comments. Side effects are reversible and low-blast-radius.
- **Unsafe Tools (Withheld by default)**: Mutating or irreversible operations (such as deleting branches or declining PRs). These are withheld unless `--yolo` (or `--allow-writes`) is explicitly configured.

Inspect the complete catalog and exposure state from your terminal:

```bash
bb ai mcp tools
```

### Principle 2: Dedicated Read-Only Token Scoping
Never allow an IDE MCP server to run with full personal administrative privileges. Generate a dedicated read-only Personal Access Token and bind the MCP server directly to it:

```bash
# Launch the MCP server restricted to a read-only token
bb ai mcp serve --host https://bitbucket.example.com --token <read-only-pat>
```

When `--token` is provided to `bb ai mcp serve`, all tool executions use that token rather than the user's personal credentials stored in the OS keyring.

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
          "get_pull_request,list_pull_requests,get_pull_request_diff,list_pull_request_comments,add_pull_request_comment"
        ]
      }
    }
  }
}
```

---

## 5. Air-Gapped Release Verification Runbook

For zero-trust pipelines and air-gapped security validation, every release artifact publishes cryptographic signatures and supply-chain attestations.

### 1. Sigstore Keyless Verification (Cosign)
Verify the downloaded release archive against the official release workflow on `refs/heads/main`:

```bash
cosign verify-blob \
  --bundle bb_linux_amd64.tar.gz.sigstore.json \
  --certificate-identity 'https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  bb_linux_amd64.tar.gz
```

### 2. GitHub Build Provenance Attestation
Verify that the binary was built on official GitHub-hosted runners directly from the source repository:

```bash
gh attestation verify bb_linux_amd64.tar.gz \
  --repo vriesdemichael/bitbucket-data-center-cli
```

### 3. Software Bill of Materials (SBOM)
Verify the link between the released binary and its attested SPDX 2.3 dependency SBOM:

```bash
gh attestation verify bb_linux_amd64.tar.gz \
  --repo vriesdemichael/bitbucket-data-center-cli \
  --predicate-type https://spdx.dev/Document
```

---

## Related Security Documents
- [Security Architecture and Threat Model](threat-model.md): Detailed trust boundaries, multi-OS policy enforcement analysis, threat vectors, and honest enterprise gap tracker.
- [Git Authentication Guide](git-authentication.md): Deep dive into host-scoped credential helper mechanics.
- [Networks, Proxies and TLS](networks-proxies-and-tls.md): Network diagnosis and connection testing.
- [Security Policy](https://github.com/vriesdemichael/bitbucket-data-center-cli/blob/main/SECURITY.md): Vulnerability reporting and disclosure policy.
