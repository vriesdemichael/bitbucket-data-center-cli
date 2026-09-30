# Advanced Topics

This section covers safety, enterprise governance, and automation topics beyond basic command usage.

- [Enterprise Hardening](enterprise-hardening.md): deploying and governing `bb` across a fleet, in four parts:
    - [Release Verification](release-verification.md): signatures, build provenance and SBOMs of a release
    - [Fleet Controls](fleet-controls.md): system policy, update controls and mirrors, builds without self-update, internal PKI
    - [Fleet Deployment](fleet-deployment.md): recipes per operating system and for CI, verification, a helpdesk table, day-2 work
    - [MCP Server Governance](mcp-governance.md): confining, restricting and auditing the MCP server
- [Security Architecture & Threat Model](threat-model.md): trust boundaries, threat vectors, mitigations, and honest enterprise gap tracker
- [Repository Discovery and Server Switching](repository-discovery-and-server-switching.md): remote-based repo inference, precedence, and multi-server workflows
- [Dry-Run Planning](dry-run-planning.md): what `--dry-run` checks for each command, and what its verdict means
- [Git Authentication](git-authentication.md): letting plain `git` authenticate through `bb` without storing a token in a repository
- [Networks, Proxies and TLS](networks-proxies-and-tls.md): outbound proxies, internal certificate authorities, and diagnosing a connection
- [Machine Mode and Diagnostics](machine-mode-diagnostics.md): JSON contract and supportability patterns
- [Shell Completion](shell-completion.md): completing commands, flags and the values they take, and setting it up in each shell
- [Server-Side Hooks](server-side-hooks.md): why `bb` does not manage plugin hooks or hook scripts, and what to use instead
- [Webhook Secrets](webhook-secrets.md): the shared secret and endpoint credentials — where `bb` reads them, and what it refuses to print

These guides are aligned with accepted ADRs and generated reference artifacts.
