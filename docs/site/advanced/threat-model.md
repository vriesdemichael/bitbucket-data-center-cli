# Security Architecture and Threat Model

A formal security architecture, trust boundary analysis, and threat model for CISOs, security engineers, and compliance officers evaluating `bb` (Bitbucket Data Center CLI).

This document outlines:
1. **Trust boundaries** across the developer workstation, local git repositories, enterprise network, and AI tooling.
2. **Threat vectors**, evaluated against current mitigations and security invariants.
3. **Honest shortcomings & compliance roadmap**, documenting known enterprise architectural limitations and directly tracking open GitHub issues.

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

## 2. Threat Vector Analysis & Current Mitigations

### Threat 1: Process Table & Argument Snooping (TB-1)
- **Threat Vector**: On multi-user jump boxes, terminal servers, or shared CI nodes, unprivileged local users or malicious processes run `ps aux` or inspect `/proc/<pid>/cmdline` to harvest API tokens passed as flags (`--token <val>`). EDR sensors and shell history files log the raw arguments.
- **Current Mitigations**:
  - `bb auth login` supports `--token-stdin` and `--password-stdin`, reading secrets strictly from standard input.
  - Supplying `--token` or `--password` as CLI flags prints a warning to `stderr` highlighting the process-table risk ([ADR-047](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/047-credential-input-and-keyring-enforcement.md)).
  - Headless automation can provide `BITBUCKET_TOKEN` directly via the environment, which is never written to disk or logged.

### Threat 2: Plaintext File Scraping at Rest (TB-2)
- **Threat Vector**: If an OS keyring daemon is unavailable (e.g. headless Linux servers), credential storage might silently degrade to plaintext disk files (`~/.config/bb/config.yaml`), leaving tokens exposed to info-stealer malware or unauthorized disk reads.
- **Current Mitigations**:
  - `BB_REQUIRE_KEYRING=1` (or `--require-keyring`) converts any keyring failure into a fatal refusal before writing or reading secrets.
  - The fallback config file is locked to `0600` permissions inside a `0700` directory.
  - `bb auth status` audits storage mechanisms and prints a prominent warning on `stderr` on every execution if insecure plaintext storage is detected.

### Threat 3: Repository Secret Bleed & Cross-Remote Leakage (TB-3)
- **Threat Vector**: Persisting authentication tokens into repository configs (`.git/config`) leaks credentials whenever repositories are archived, copied, or pushed. An unscoped `http.extraHeader` in `.git/config` is attached to every HTTP request, sending Bitbucket tokens to external remotes.
- **Current Mitigations**:
  - Host-scoped Git Credential Helper ([ADR-044](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/044-git-credential-helper-instead-of-persisted-credentials.md)): `bb auth setup-git` writes a credential helper rule scoped strictly to the Bitbucket hostname.
  - Cloned repositories contain zero credentials in `.git/config`.
  - Token revocation in Bitbucket takes effect immediately across all local repositories because git queries the helper dynamically.

### Threat 4: Man-in-the-Middle (MitM) & Proxy Inspection (TB-4)
- **Threat Vector**: Corporate forward proxies (Zscaler, Palo Alto) re-sign traffic using private internal root CAs. Operators encountering certificate errors may be tempted to disable verification.
- **Current Mitigations**:
  - `BB_CA_FILE` / `--ca-file` appends the enterprise root CA bundle directly to the operating system's trust pool (`x509.SystemCertPool()`), preserving public root verification while trusting the internal Bitbucket host.
  - `--insecure-skip-verify` is deliberately accompanied by loud stderr warnings.
  - Minimum TLS version is pinned to TLS 1.2 (`tlsConfig.MinVersion = tls.VersionTLS12`).

### Threat 5: Malicious Binary Tampering & Supply Chain Compromise (TB-6)
- **Threat Vector**: Release binaries or dependencies could be compromised during packaging or distribution.
- **Current Mitigations**:
  - Sigstore / Cosign keyless signatures: Every archive is signed via OIDC identity pinned to `.github/workflows/release.yml@refs/heads/main`.
  - GitHub build provenance attestations: Verifiable via `gh attestation verify`.
  - Attested SPDX 2.3 SBOM (`sbom.spdx.json`) detailing all compiled dependencies.
  - `bb update` verifies the Sigstore bundle before replacing the running binary and rejects any update whose signing workflow or ref does not match.

### Threat 6: Autonomous Agent Prompt Injection & Rogue Writes (TB-5)
- **Threat Vector**: An AI agent integrated via MCP is manipulated via prompt injection into deleting branches, declining pull requests, or exfiltrating repository data.
- **Current Mitigations**:
  - Safe vs. Unsafe Tool Model ([ADR-039](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/039-built-in-mcp-server-with-host-scoping-and-token-restriction.md)): Mutating and destructive operations are withheld by default unless `--yolo` / `--allow-writes` is explicitly set.
  - Dedicated Token Scoping: `bb ai mcp serve --token <read-only-pat>` forces the MCP server to run under a restricted token, independent of the developer's personal credentials.
  - Explicit allowlisting (`--tools`) and denylisting (`--exclude`).
  - Multi-host protection: Refuses to start without explicit `--host` if multiple instances exist in configuration.

---

## 3. Honest Shortcomings & Compliance Roadmap

For enterprise security architects and compliance officers conducting gap analyses, this section transparently documents current architectural limitations, why they exist, and links directly to the tracked GitHub issues.

### Compliance Gap Tracker

| Capability | Current State | Enterprise Impact | Tracked Issue |
|---|---|---|---|
| **System-Wide Policy Hierarchy** | Config is resolved only from user directory or env vars. | IT admins cannot deploy an immutable `/etc/bb/config.yaml` or Windows Registry GPO policy to lock mandatory settings machine-wide. | [#420](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/420) |
| **Enterprise Update Controls** | `bb update` queries public GitHub API and overwrites local binary. | Bypasses workstation package managers (`.deb`, `.rpm`, WinGet) and fails in air-gapped networks. | [#421](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/421) |
| **Mutual TLS (mTLS)** | Server CA certificates supported via `BB_CA_FILE`; client certs unsupported. | Cannot authenticate against zero-trust reverse proxies requiring client certificates at the TLS layer. | [#422](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/422) |
| **MCP Workspace Scoping & Audit Trail** | MCP server scopes to the entire Bitbucket instance; logs go to stderr. | AI agents cannot be restricted to a single project/repo, and tool invocations lack a structured SIEM audit stream. | [#423](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/423) |
| **OAuth 2.0 / Browser SSO Flow** | Authenticates via Personal Access Tokens (PATs) or Basic Auth. | Parity with `gh auth login --web` requires extensive Bitbucket server-side admin changes and consumer provisioning. | [#424](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/424) |

---

### Detailed Analysis of Gaps

#### 1. System-Wide Configuration Hierarchy ([#420](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/420))
`config.ConfigPath()` currently inspects `BB_CONFIG_PATH` or `os.UserConfigDir()` (`~/.config/bb/config.yaml`). If an IT administrator wants to enforce `BB_REQUIRE_KEYRING=1` or configure a corporate CA bundle, they must inject environment variables into every developer shell. A local user can bypass these controls simply by unsetting the variable.
- **Roadmap Resolution**: Introduce a system-level configuration tier (`/etc/bb/config.yaml` on POSIX and `%ProgramData%\bb\config.yaml` / Windows Registry on Windows) with support for locked, un-overridable enterprise security directives.

#### 2. Enterprise Update Controls & Mirrors ([#421](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/421))
Running `bb update` on a managed workstation overwrites the binary installed by package managers (`apt`, `dnf`, WinGet), creating checksum drift and bypassing Change Advisory Board (CAB) reviews. Additionally, `bb update` is hardcoded to `api.github.com`, failing in air-gapped environments.
- **Roadmap Resolution**: Introduce `BB_DISABLE_UPDATE=1` and a compile-time build tag (`-tags no_self_update`) for enterprise package maintainers, alongside `BB_UPDATE_BASE_URL` to support internal Artifactory or Nexus mirrors.

#### 3. Mutual TLS (mTLS) Client Certificates ([#422](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/422))
`internal/transport/network/transport.go` configures root trust pools but does not accept client certificate/key pairs (`tls.Certificate`). In defense, banking, and zero-trust networks where edge ingress terminates mTLS before the Bitbucket application, `bb` cannot connect.
- **Roadmap Resolution**: Add `--client-cert` and `--client-key` flags (and `BB_CLIENT_CERT` / `BB_CLIENT_KEY` environment variables).

#### 4. MCP Workspace Scoping & Audit Logging ([#423](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/423))
`bb ai mcp serve` currently grants the AI agent access to any project or repository the underlying token can reach on that Bitbucket instance. Furthermore, stdio RPC logs are not formatted for enterprise SIEM ingestion.
- **Roadmap Resolution**: Add `--project <KEY>` and `--repo <KEY/SLUG>` flags to strictly sandbox the MCP server to authorized workspace boundaries, and implement structured JSONL audit logging (`--audit-file`).

#### 5. SSO / Browser-Based Authentication Flow ([#424](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/424))
Users accustomed to `gh auth login --web` often ask why `bb` does not default to an interactive browser login.
- **The Structural Constraint**: Unlike GitHub.com (which provides a centralized, pre-registered OAuth app ID), Bitbucket Data Center instances ship with **zero configured OAuth clients**. Non-admin users are strictly forbidden from creating clients (`POST /rest/oauth2/latest/client` returns `401 Unauthorized`), and Dynamic Client Registration (RFC 7591) is not supported.
- **Administrative Requirement**: Facilitating a browser login flow requires an organization's Bitbucket System Administrator to manually create an Incoming Link / OAuth consumer in the admin console, register local redirect URIs (`http://127.0.0.1:<port>/callback`), and distribute the generated `client_id` across the fleet.
- **Headless Agents & CI**: Autonomous agents and CI pipelines cannot complete browser redirects. Personal Access Tokens (PATs) and `BITBUCKET_TOKEN` remain the primary, zero-configuration standard for automation.
- **Roadmap Resolution**: Provide an opt-in `--web` flow for organizations that explicitly provision an OAuth 2.0 consumer.

---

## 4. Security Invariants Summary

- **Strict Non-Interactive CLI Contract ([ADR-054](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/054-strict-non-interactive-cli-contract.md))**: `bb` never prompts on stdin for confirmation or surveys, preventing hanging processes in CI/CD and agent execution.
- **Zero External Telemetry ([SECURITY.md](https://github.com/vriesdemichael/bitbucket-data-center-cli/blob/main/SECURITY.md))**: `bb` sends no usage statistics, telemetry, or analytics, contacting only the configured Bitbucket server.
- **Structured Error Envelope ([ADR-011](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/011-error-taxonomy-and-cli-exit-contract.md), [ADR-046](file:///C:/Users/vries/.gemini/antigravity/worktrees/bitbucket-server-cli/investigate_issue_three_ninety/docs/site/adr/046-json-error-envelope-on-the-failure-path.md))**: Fatal failures emit a predictable JSON error envelope with categorized error taxonomy.
