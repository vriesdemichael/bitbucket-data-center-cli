# Enterprise Deployment and Hardening Guide

A comprehensive guide for systems engineers, enterprise architects, and SecOps compliance officers deploying and governing `bb` across corporate workstations and CI/CD pipelines.

`bb` is engineered to operate inside strictly regulated corporate environments: behind forward proxies, against private Bitbucket Data Center instances with internal PKI, and alongside locked-down developer environments.

---

## 1. Credential Hygiene & Enforced Keyring Storage

Security compliance standards (CIS Benchmarks, NIST SP 800-63B, ISO 27001) mandate that credentials must never be held in plaintext on disk, exposed in process argument tables, or logged in shell history.

### The Threat: Process Argument & History Sniffing
Passing tokens via flags (e.g. `--token <val>`) places sensitive secrets into the process arguments list. This makes them visible to:
- Any local user running `ps aux` or reading `/proc/<pid>/cmdline` on shared Linux hosts.
- Endpoint Detection & Response (EDR) agents (CrowdStrike Falcon, Microsoft Defender for Endpoint).
- Windows Task Manager process properties.
- Plaintext shell history (`~/.bash_history`, `~/.zsh_history`, PowerShell `ConsoleHost_history.txt`).

### The Hardening Policy: Mandatory Stdin Piping
Always pass tokens using standard input:

```bash
# Provide the token via pipe (compatible with bash, zsh, and CI secret runners)
printf "%s" "$BITBUCKET_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin

# Or piping from a secret file
cat /run/secrets/bitbucket_token | bb auth login https://bitbucket.example.com --token-stdin
```

When `--token` or `--password` flags are supplied as command-line arguments, `bb` deliberately prints a warning on `stderr` alerting the operator to the process-table exposure.

---

### Enforcing Keyring Storage Fleet-Wide

By default, if an OS keyring daemon is unavailable, `bb` falls back to an access-restricted plaintext configuration file (`0600` permissions in `~/.config/bb/config.yaml`). In enterprise environments, this silent fallback can violate security policy.

`bb` allows operators to enforce keyring storage, making any failure to reach the OS keyring an immediate, fatal refusal before any secret reaches disk:

```bash
# Enable via environment variable in standard workstation shell profiles
export BB_REQUIRE_KEYRING=1

# Or enforce explicitly during login
bb auth login https://bitbucket.example.com --require-keyring --token-stdin
```

Supported OS Keyrings:
- **Windows**: Windows Credential Manager (DPAPI-backed).
- **macOS**: Apple Keychain.
- **Linux**: Secret Service API over D-Bus (GNOME Keyring, KWallet, or keepassxc).

### Auditing Storage Status
To audit how credentials are stored on a machine, inspect the `credential_storage` field:

```bash
# Human-readable status
bb auth status

# Machine-readable evaluation for compliance scripts
bb auth status --json | jq .data.credential_storage
```

Output values:
- `keyring`: Protected by the operating system credential vault.
- `environment`: Supplied dynamically via `BITBUCKET_TOKEN` (ideal for ephemeral CI/CD pods).
- `config-file-plaintext`: Stored in user config directory (warns on stderr on every execution).
- `none`: No credentials stored.

---

## 2. Git Authentication Without Repository Bleed

A common enterprise vulnerability with git tooling is persisting live authentication tokens directly into local repository configurations (`.git/config`). When repositories are copied, archived into tarballs, or shared across developer machines, those tokens leak.

Furthermore, an unscoped `http.extraHeader` in `.git/config` is transmitted to **every** HTTP remote contacted from that working tree, leaking Bitbucket tokens to external remotes.

### The Hardening Policy: Host-Scoped Credential Helper
`bb` solves this via a native Git Credential Helper protocol implementation ([ADR-044](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/044-git-credential-helper-instead-of-persisted-credentials.md)):

```bash
# Configure git to query bb for Bitbucket credentials
bb auth setup-git
```

This writes a single host-scoped configuration entry into the user's global `~/.gitconfig`:

```ini
[credential "https://bitbucket.example.com"]
	helper = !"bb" auth git-credential
```

### Key Security Properties:
1. **Zero Repository Storage**: Cloned repositories contain no tokens or passwords in `.git/config`.
2. **Host-Scoped Isolation**: Git queries `bb` *only* when contacting `https://bitbucket.example.com`. Credentials are never sent to GitHub, GitLab, or third-party remotes.
3. **Instant Revocation**: If a token is revoked in Bitbucket or removed via `bb auth logout`, all local clones immediately lose access without needing manual git cleanup.

To audit existing clones for legacy plaintext tokens:

```bash
# Check if a repository contains persisted extra headers
git config --local --get http.extraHeader

# Clean up legacy tokens and rely on the credential helper
git config --local --unset-all http.extraHeader
bb auth setup-git
```

---

## 3. Internal PKI & Outbound Proxy Traversal

### Internal Enterprise CA Certificates
Enterprise Bitbucket Data Center instances typically present TLS certificates issued by an internal corporate Certificate Authority. Furthermore, outbound SSL inspection proxies (e.g. Zscaler, Palo Alto Networks, Blue Coat) re-sign traffic using an internal enterprise root CA.

To configure `bb` to trust an internal CA bundle:

```bash
# Configure via environment variable (recommended for managed fleet images)
export BB_CA_FILE=/etc/ssl/certs/corp-root-ca.pem

# Or pass via CLI flag
bb --ca-file /etc/ssl/certs/corp-root-ca.pem repo list
```

> [!IMPORTANT]
> **Additive Trust Pool**: `bb` appends your corporate CA bundle to the operating system's default trust pool (`x509.SystemCertPool()`). It does **not** overwrite standard system roots, ensuring that both internal Bitbucket calls and external services (e.g. GitHub release verification) resolve reliably.

### Outbound Forward Proxies
`bb` natively respects standard proxy environment variables:
- `HTTPS_PROXY`: Forward proxy for HTTPS traffic (`http://proxy.corp.example:3128`).
- `HTTP_PROXY`: Forward proxy for HTTP traffic.
- `NO_PROXY`: Comma-separated domain suffixes that bypass the proxy (`.corp.internal,localhost,127.0.0.1`).

```bash
export HTTPS_PROXY=http://proxy.corp.example:3128
export NO_PROXY=.corp.internal,localhost,127.0.0.1
bb repo list --limit 5
```

> [!CAUTION]
> **Do not use `--insecure-skip-verify` in production.** This flag completely disables TLS certificate validation and hostname verification, exposing traffic to Man-in-the-Middle (MitM) attacks. If certificate verification fails, provide the internal CA bundle via `BB_CA_FILE`.

---

## 4. AI & MCP Server Governance (`bb ai mcp serve`)

`bb` includes a built-in Model Context Protocol (MCP) server designed for integration with AI developer tools (VS Code Agent, Cursor, Claude Desktop, Copilot). In corporate environments, AI access must be constrained by the Principle of Least Privilege.

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

### Principle 3: Explicit Tool Allowlists
To enforce strict boundary controls, specify an explicit tool allowlist:

```bash
# Expose only pull request inspection and review tools
bb ai mcp serve --host https://bitbucket.example.com --tools get_pull_request,list_pull_requests,get_pull_request_diff,list_pull_request_comments
```

Alternatively, exclude specific sensitive capabilities:

```bash
# Strip branch and PR deletion tools from the exposed catalog
bb ai mcp serve --host https://bitbucket.example.com --exclude delete_branch,decline_pull_request
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
          "get_pull_request,list_pull_requests,get_pull_request_diff,list_pull_request_comments,add_pull_request_comment"
        ]
      }
    }
  }
}
```

---

## 5. Enterprise Packaging & Internal Distribution

In enterprise fleets, software must be distributed through approved internal package registries rather than fetched from public CDNs.

### Internal Mirrors (Artifactory / Nexus)
`bb` publishes official Linux packages (`.deb` and `.rpm`) with every release:
- **Debian / Ubuntu**: Distribute via internal APT repositories (`apt-get install bb`).
- **RHEL / Rocky / CentOS / Fedora**: Distribute via internal YUM/DNF repositories (`dnf install bb`).
- **Windows**: Distribute `.zip` or publish to private WinGet repository manifests via Microsoft Intune or System Center (SCCM).

### Automated Shell Configuration for Fleets
Provisioning scripts or Ansible roles should pre-configure shell profiles (`/etc/profile.d/bb.sh` on Linux):

```bash
# /etc/profile.d/bb.sh
export BB_REQUIRE_KEYRING=1
export BB_CA_FILE=/etc/ssl/certs/corp-root-ca.pem
export BITBUCKET_URL=https://bitbucket.example.com
```

---

## 6. Supply Chain Verification in Air-Gapped Environments

For zero-trust pipelines and air-gapped security validation, every release artifact publishes cryptographic signatures and supply-chain attestations.

### Sigstore Keyless Verification
Each release archive is signed via Sigstore with keyless OIDC identity bound directly to the official GitHub Actions release workflow:

```bash
# Verify release bundle integrity using Cosign
cosign verify-blob \
  --bundle bb_linux_amd64.tar.gz.sigstore.json \
  --certificate-identity 'https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  bb_linux_amd64.tar.gz
```

### GitHub Build Provenance Attestation
Verify that the binary was built on GitHub-hosted runners directly from the source repository:

```bash
gh attestation verify bb_linux_amd64.tar.gz \
  --repo vriesdemichael/bitbucket-data-center-cli
```

### Software Bill of Materials (SBOM)
Every release includes an attested SPDX 2.3 SBOM (`sbom.spdx.json`) listing all compiled Go dependencies and transitive licenses:

```bash
# Verify the SBOM attestation against the downloaded artifact
gh attestation verify bb_linux_amd64.tar.gz \
  --repo vriesdemichael/bitbucket-data-center-cli \
  --predicate-type https://spdx.dev/Document
```

---

## Related Security Documents
- [Security Architecture & Threat Model](threat-model.md): Detailed trust boundaries, attack vectors, and honest enterprise gap tracker.
- [Git Authentication Guide](git-authentication.md): Deep dive into host-scoped credential helper mechanics.
- [Networks, Proxies and TLS](networks-proxies-and-tls.md): Network diagnosis and connection testing.
- [Security Policy](https://github.com/vriesdemichael/bitbucket-data-center-cli/blob/main/SECURITY.md): Vulnerability reporting and disclosure policy.
