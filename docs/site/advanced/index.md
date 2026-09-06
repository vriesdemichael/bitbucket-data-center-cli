# Advanced Topics

This section covers safety, enterprise governance, and automation topics beyond basic command usage.

- [Enterprise Hardening](enterprise-hardening.md): fleet-wide deployment, enforced keyring storage, internal PKI, proxy traversal, and AI/MCP governance
- [Security Architecture & Threat Model](threat-model.md): trust boundaries, threat vectors, mitigations, and honest enterprise gap tracker
- [Repository Discovery and Server Switching](repository-discovery-and-server-switching.md): remote-based repo inference, precedence, and multi-server workflows
- [Bulk Operations](bulk-operations.md): reviewed multi-repository policy workflows
- [Dry-Run Planning](dry-run-planning.md): mutation previews, capability signaling, and safety guarantees
- [Git Authentication](git-authentication.md): letting plain `git` authenticate through `bb` without storing a token in a repository
- [Networks, Proxies and TLS](networks-proxies-and-tls.md): outbound proxies, internal certificate authorities, and diagnosing a connection
- [Machine Mode and Diagnostics](machine-mode-diagnostics.md): JSON contract and supportability patterns
- [Server-Side Hooks](server-side-hooks.md): why `bb` does not manage plugin hooks or hook scripts, and what to use instead
- [Webhook Secrets](webhook-secrets.md): the shared secret and endpoint credentials — where `bb` reads them, what it refuses to print, and how a bulk plan names one without holding it

These guides are aligned with accepted ADRs and generated reference artifacts.
