# Security Architecture and Threat Model

A formal security architecture, trust boundary analysis, and threat model for Chief Information Security Officers (CISOs), enterprise security architects, and compliance auditors evaluating `bb` (Bitbucket Data Center CLI).

This document details:
1. **Trust boundaries** across the developer workstation, local git repositories, enterprise network, and AI tooling.
2. **Operating system policy enforcement realities** across macOS, Linux, and Windows.
3. **Threat domains** evaluated through a **Threat $\leftrightarrow$ Mitigation $\leftrightarrow$ Audit $\leftrightarrow$ Residual Gap** triad.
4. **Compliance matrix and risk treatment plan**, transparently linking to tracked GitHub issues.

---

## 1. System Overview & Trust Boundaries

`bb` operates as a client-side CLI and Model Context Protocol (MCP) server communicating directly with self-hosted Bitbucket Data Center instances.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ DEVELOPER WORKSTATION / CI RUNNER (TRUST BOUNDARY 1)                        │
│                                                                             │
│  ┌──────────────┐   stdin pipe    ┌───────────────────────────────────────┐ │
│  │ Shell / User │ ──────────────> │ bb CLI Process                        │ │
│  └──────────────┘                 │                                       │ │
│                                   │  - Argument parser (Cobra)            │ │
│  ┌──────────────┐   stdio pipe    │  - Input validator (Fail-fast)        │ │
│  │ IDE / Agent  │ <─────────────> │  - MCP Server (bb ai mcp serve)       │ │
│  └──────────────┘                 │  - HTTP Transport (network.SafeClient)│ │
│                                   └───────┬───────────────────────┬───────┘ │
│                                           │                       │         │
│                        D-Bus / DPAPI /    │                       │ git -c  │
│                        Keychain API       │                       │ helper  │
│                                           ▼                       ▼         │
│                         ┌───────────────────────┐   ┌─────────────────────┐ │
│                         │ OS Keyring Store      │   │ Git Engine (execgit)│ │
│                         │ (TRUST BOUNDARY 2)    │   │ (TRUST BOUNDARY 3)  │ │
│                         └───────────────────────┘   └─────────────┬───────┘ │
└───────────────────────────────────────────────────────────────────┼─────────┘
                                                                    │
      HTTPS / REST API (TLS 1.2+)                                   │ Git-over-
      (TRUST BOUNDARY 4)                                            │ HTTP/SSH
                                                                    │
                                    ▼                               ▼
                 ┌──────────────────────────────────────────────────────┐
                 │ BITBUCKET DATA CENTER ENTERPRISE INSTANCE            │
                 │                                                      │
                 │  - Reverse Proxy / Load Balancer (Internal PKI / CA) │
                 │  - Bitbucket Core REST API & Git Services            │
                 └──────────────────────────────────────────────────────┘
```

### Trust Boundary Definitions

| Boundary | Components | Trust Level | Security Invariants |
|---|---|---|---|
| **TB-1: Process & Terminal** | `bb` runtime process, memory, arguments, stdout/stderr | Trusted Local User | Zero secret leakage in process argument table (`/proc/<pid>/cmdline`). Zero telemetry. |
| **TB-2: OS Keyring Store** | Windows Credential Manager, macOS Keychain, Linux Secret Service | System Security Enclave | Secrets held encrypted at rest. Immediate failure if keyring is missing when `BB_REQUIRE_KEYRING=1` is set. |
| **TB-3: Git Working Tree** | Local git clone, `.git/config`, `git` CLI child processes | Semi-Trusted Filesystem | Zero credentials persisted into repository `.git/config`. Host-scoped credential querying. |
| **TB-4: Network Perimeter** | Corporate forward proxies, internal enterprise PKI, Bitbucket DC | Untrusted / Inspected Network | Minimum TLS 1.2. Additive corporate CA trust pool. Strict non-interactive timeout enforcement. |
| **TB-5: AI / MCP Surface** | IDE agent (Cursor, VS Code, Claude Desktop), stdio RPC pipe | Constrained Automation | Safe-by-default tool gating. Unsafe tools withheld without explicit `--yolo`. Capability allowlisting. |
| **TB-6: Supply Chain** | GitHub Actions builder, release packaging, Sigstore/Cosign | Cryptographic Verification | Keyless OIDC signatures, SLSA build provenance attestations, attested SPDX 2.3 SBOM. |

---

## 2. Multi-OS Policy Enforcement Realities

Security auditors must understand how enterprise policy enforcement mechanisms vary across the three primary operating systems in developer fleets:

| Operating System | Fleet Management Tooling | Native Policy Channel | Credential Enclave | Enforcement Reality & Hardening Level |
|---|---|---|---|---|
| **Windows** | Microsoft Intune, SCCM, Active Directory Group Policy (GPO) | **Windows Registry (`HKLM\Software\Policies\bb`)** | Windows Credential Manager (DPAPI) | **Highest**. Standard enterprise users lack permissions to write to `HKLM`. Policies enforced via GPO/Registry are tamper-proof against unprivileged users. |
| **macOS** | Jamf Pro, Kandji, Microsoft Intune for Mac, Apple MDM | **Configuration Profiles (`/Library/Managed Preferences/`)** or `/etc/zshenv` | Apple Keychain (`security` framework) | **High**. Managed Preferences deployed via MDM are root-owned and cannot be modified by non-admin users. Shell variables deployed to `/etc/zshenv` apply system-wide across all zsh sessions. |
| **Linux (Workstations)** | Ansible, Puppet, SaltStack, Chef, Red Hat Satellite | **Root-owned `/etc/bb/config.yaml`** or `/etc/profile.d/bb.sh` | Secret Service D-Bus (GNOME Keyring / KWallet) | **Moderate to High**. On multi-user jump boxes where developers lack `sudo`, root-owned configs are immutable. On developer laptops where engineers have `sudo`, environment-based enforcement is advisory without root-owned config files. |
| **Linux (CI Runners)** | Kubernetes, Docker, GitHub Actions / GitLab Runners | Container Environment (`BITBUCKET_TOKEN`) | Ephemeral Process Memory (`/dev/shm`) | **Special Case**. Headless runners lack desktop D-Bus daemons. Forcing desktop keyrings in CI fails; `BITBUCKET_TOKEN` must be injected into memory, bypassing disk entirely. |

---

## 3. Threat Domain Analysis

Every security domain is evaluated using the **Threat $\leftrightarrow$ Mitigation $\leftrightarrow$ Audit $\leftrightarrow$ Residual Gap** triad.

---

### Domain 1: Secret Hygiene & Storage at Rest (TB-1 & TB-2)

#### 1. Threat & Attacker Vector
On shared jump hosts, terminal servers, or developer laptops, unprivileged local users or malicious processes run `ps aux` or inspect `/proc/<pid>/cmdline` to harvest tokens passed as flags (`--token <val>`). EDR sensors and shell history files (`~/.bash_history`) record arguments in plaintext. Additionally, if an OS keyring is unavailable, tools may silently fall back to unencrypted files on disk (`~/.config/bb/config.yaml`), leaving credentials vulnerable to info-stealer malware.

#### 2. Current Architectural Mitigations
- **Mandatory Stdin Piping**: `bb auth login` supports `--token-stdin` and `--password-stdin`, ingesting secrets strictly from standard input.
- **Process Table Warnings**: Supplying `--token` or `--password` as CLI flags prints an explicit warning to `stderr` highlighting process-table exposure ([ADR-047](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/047-credential-input-and-keyring-enforcement.md)).
- **Enforced Keyring Storage**: Setting `BB_REQUIRE_KEYRING=1` converts any keyring failure into a fatal error before writing or reading secrets, preventing silent fallback to plaintext files.
- **Headless CI Exemption**: In CI/CD pipelines, `BITBUCKET_TOKEN` is read directly from the process environment and never written to disk.

#### 3. Audit & Verification Command
Inspect the credential storage mechanism currently in use:

```bash
bb auth status --json
```

Assert that `.data.credential_storage` equals `keyring` (on workstations) or `environment` (in CI runners), and never `config-file-plaintext`.

#### 4. Residual Gap & Tracked Issue
- **The "Advisory" Environment Limitation**: Currently, `BB_REQUIRE_KEYRING=1` is an environment variable. A local user can unset it (`unset BB_REQUIRE_KEYRING`) to bypass the check. True enterprise enforcement requires a system-level configuration tier (`/etc/bb/config.yaml` or Windows Registry `HKLM\Software\Policies\bb`) that cannot be modified by unprivileged users.
- **Tracked Issue**: **[Issue #420: feat: system-wide configuration and policy enforcement](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/420)**.

---

### Domain 2: Git Transport & Repository Boundaries (TB-3)

#### 1. Threat & Attacker Vector
Traditional git tools frequently persist authentication tokens directly into local repository configurations (`.git/config` via `http.extraHeader`). When repositories are archived, copied, or pushed, those tokens leak. Furthermore, an unscoped `http.extraHeader` in `.git/config` is attached to every HTTP request, sending Bitbucket credentials to third-party remotes (e.g. GitHub or public mirrors).

#### 2. Current Architectural Mitigations
- **Host-Scoped Credential Helper**: `bb auth setup-git` writes a credential helper rule scoped strictly to the Bitbucket hostname into the global `~/.gitconfig` ([ADR-044](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/044-git-credential-helper-instead-of-persisted-credentials.md)):
  ```ini
  [credential "https://bitbucket.example.com"]
  	helper = !"bb" auth git-credential
  ```
- **Zero Repository Storage**: Cloned repositories contain zero credentials or tokens in `.git/config`.
- **Instant Revocation**: If a token is revoked in Bitbucket or removed via `bb auth logout`, all local clones immediately lose access without requiring repository cleanup.

#### 3. Audit & Verification Command
Verify that a repository contains no persisted extra headers:

```bash
git config --local --get http.extraHeader
```

A properly configured repository returns an empty result (exit code 1).

---

### Domain 3: Network Perimeter, Proxies & Internal PKI (TB-4)

#### 1. Threat & Attacker Vector
Corporate forward proxies (Zscaler, Blue Coat, Palo Alto) inspect outbound HTTPS traffic by generating certificates signed by an internal enterprise root CA. Tools that verify certificates only against public Mozilla trust roots fail with `x509: certificate signed by unknown authority`. Developers under deadline pressure may attempt to bypass verification by disabling TLS checks.

#### 2. Current Architectural Mitigations
- **Additive Corporate CA Trust Pool**: `BB_CA_FILE` / `--ca-file` appends the internal root CA bundle directly to the operating system's trust pool (`x509.SystemCertPool()`). Public roots remain valid (ensuring release verification against GitHub continues to work) while trusting the internal Bitbucket host.
- **Corporate Proxy Support**: Natively honors `HTTPS_PROXY`, `HTTP_PROXY`, and `NO_PROXY` via Go's cloned `http.DefaultTransport`.
- **Minimum TLS 1.2 Enforced**: Hardcoded `tlsConfig.MinVersion = tls.VersionTLS12`.
- **Zero External Telemetry**: Architectural invariant documented in [SECURITY.md](https://github.com/vriesdemichael/bitbucket-data-center-cli/blob/main/SECURITY.md). `bb` makes zero network calls to telemetry or analytics services.

#### 3. Audit & Verification Command
Inspect the active TLS configuration and proxy routing:

```bash
bb --log-level debug --log-format jsonl repo list --limit 1
```

#### 4. Residual Gap & Tracked Issue
- **Lack of Mutual TLS (mTLS) Client Certificates**: In zero-trust networks where reverse proxies require client certificates at the TLS layer before granting ingress, `bb` cannot authenticate because it lacks `--client-cert` and `--client-key` support.
- **Tracked Issue**: **[Issue #422: feat: mutual TLS (mTLS) client certificate support](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/422)**.

---

### Domain 4: Autonomous AI & MCP Server Governance (TB-5)

#### 1. Threat & Attacker Vector
When autonomous AI agents (Cursor, VS Code Agent, Claude Desktop) are integrated via `bb ai mcp serve`, prompt injection vulnerabilities in untrusted source code could trick the agent into performing destructive actions (e.g. deleting repositories, declining pull requests, or exfiltrating confidential source code across unrelated projects).

#### 2. Current Architectural Mitigations
- **Safe vs. Unsafe Tool Isolation**: Mutating and destructive operations (deleting branches, declining PRs) are withheld by default ([ADR-039](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/039-built-in-mcp-server-with-host-scoping-and-token-restriction.md)). Only safe, low-blast-radius tools are exposed unless `--yolo` / `--allow-writes` is explicitly configured.
- **Dedicated Read-Only Token Scoping**: Running `bb ai mcp serve --token <ro-pat>` forces the MCP server to run under a dedicated token with read-only permissions on Bitbucket Server, completely decoupled from the developer's personal credentials.
- **Explicit Capability Allowlists**: Administrators can constrain the exposed MCP surface via `--tools` or `--exclude`.
- **Multi-Host Safety**: `bb ai mcp serve` refuses to start without an explicit `--host` parameter if multiple Bitbucket instances are configured, preventing accidental execution against production.

#### 3. Audit & Verification Command
Inspect exposed MCP tools and gating status:

```bash
bb ai mcp tools
```

#### 4. Residual Gap & Tracked Issue
- **Instance-Wide Blast Radius & Lack of Audit Logs**: Currently, `--host` scopes to the entire Bitbucket instance. An agent with `list_repositories` can inspect any repository the token can reach across the company. Additionally, MCP stdio communication lacks a structured JSONL audit stream for enterprise SIEM ingestion.
- **Tracked Issue**: **[Issue #423: feat: MCP server workspace scoping and audit logging](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/423)**.

---

### Domain 5: Supply Chain & Software Distribution (TB-6)

#### 1. Threat & Attacker Vector
Compromised dependencies, tampered release archives, or rogue in-place updates on developer workstations could introduce malicious code into the development lifecycle.

#### 2. Current Architectural Mitigations
- **Sigstore / Cosign Keyless Signing**: Every release archive is signed via OIDC identity bound directly to `.github/workflows/release.yml@refs/heads/main`.
- **GitHub Build Provenance Attestations**: Build origin is verifiable via `gh attestation verify`.
- **Attested SPDX 2.3 SBOM**: Every release publishes `sbom.spdx.json`, attested against each released binary.
- **Update Verification**: `bb update` cryptographically verifies the Sigstore bundle before replacing the running binary.

#### 3. Audit & Verification Command
Verify release integrity using Cosign:

```bash
cosign verify-blob \
  --bundle bb_linux_amd64.tar.gz.sigstore.json \
  --certificate-identity 'https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  bb_linux_amd64.tar.gz
```

#### 4. Residual Gap & Tracked Issue
- **Self-Update Kill-Switch & Mirror Support**: Managed workstation fleets require disabling `bb update` so users cannot bypass package managers (`apt`, `dnf`, WinGet) or change approval boards. Additionally, air-gapped networks require pointing updates to internal mirrors (Artifactory/Nexus).
- **Tracked Issue**: **[Issue #421: feat: enterprise update controls and mirror support](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/421)**.

---

### Domain 6: Enterprise Identity & Federation (SSO)

#### 1. Threat & Attacker Vector
Static personal access tokens with indefinite lifespans present risk if users depart or credentials leak. Organizations seek centralized Single Sign-On (SSO) and immediate token revocation through identity providers (Okta, Microsoft Entra ID, PingFederate).

#### 2. Current Architectural Mitigations
- **Fine-Grained Scoping & TTL**: `bb auth token create` supports explicit expiration dates (`--expiry-days`) and project-level permission boundaries.
- **Immediate Server-Side Revocation**: Tokens revoked in Bitbucket Server immediately invalidate all CLI and git helper access.

#### 3. Audit & Verification Command
List active Personal Access Tokens and their expiration status:

```bash
bb auth token list
```

#### 4. Residual Gap & Tracked Issue
- **Bitbucket Data Center OAuth 2.0 Architectural Constraints**: Unlike GitHub.com (which provides a centralized OAuth App ID), Bitbucket Data Center instances ship with **zero configured OAuth clients**. Non-admin users cannot create clients (`POST /rest/oauth2/latest/client` returns `401 Unauthorized`), and dynamic registration (RFC 7591) is unsupported. Supporting a browser login flow requires extensive server-administrator changes (provisioning an Incoming Link / OAuth consumer and distributing the `client_id`). Headless agents and CI runners must continue to use PATs or `BITBUCKET_TOKEN`.
- **Tracked Issue**: **[Issue #424: feat: opt-in OAuth 2.0 browser login for enterprise SSO](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/424)**.

---

## 4. Compliance Matrix & Risk Treatment Plan

| Security Control Requirement | Regulatory Framework | Current Implementation State | Tracked Backlog Issue |
|---|---|---|---|
| **System-Wide Policy Locking** | CIS Benchmark §1.1, NIST SP 800-53 AC-3 | Supported via user env (`BB_REQUIRE_KEYRING=1`); needs root-owned `/etc/bb` / Windows Registry GPO hierarchy. | [#420](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/420) |
| **Controlled Fleet Software Updates** | NIST SP 800-53 SI-2, ISO 27001 A.12.5.1 | Cryptographically verified via Sigstore; needs `BB_DISABLE_UPDATE=1` killswitch for managed fleets. | [#421](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/421) |
| **Mutual TLS (mTLS) Ingress** | Zero Trust Architecture (NIST SP 800-207) | Internal root CAs supported via `BB_CA_FILE`; client certificates currently unsupported. | [#422](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/422) |
| **AI Agent Least Privilege & Audit** | OWASP Top 10 for LLM (LLM01 / LLM06) | Safe/unsafe tool gating and `--token` scoping supported; needs `--project`/`--repo` scoping and JSONL audit stream. | [#423](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/423) |
| **Centralized Identity Federation** | NIST SP 800-63B, CIS Benchmark §5.2 | Scoped PATs with TTL supported; browser OAuth flow requires Bitbucket DC admin consumer provisioning. | [#424](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/424) |

---

## 5. Security Invariants Summary

- **Strict Non-Interactive CLI Contract ([ADR-054](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/054-strict-non-interactive-cli-contract.md))**: `bb` never prompts on stdin for confirmation or interactive surveys, guaranteeing zero hanging processes in automation or CI/CD pipelines.
- **Zero External Telemetry ([SECURITY.md](https://github.com/vriesdemichael/bitbucket-data-center-cli/blob/main/SECURITY.md))**: `bb` sends no telemetry, metrics, or usage statistics to any third-party server.
- **Structured Error Taxonomy ([ADR-011](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/011-error-taxonomy-and-cli-exit-contract.md), [ADR-046](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/046-json-error-envelope-on-the-failure-path.md))**: Fatal failures emit a predictable JSON error envelope with categorized error taxonomy (`validation`, `auth`, `not_found`, `conflict`, `system`).
