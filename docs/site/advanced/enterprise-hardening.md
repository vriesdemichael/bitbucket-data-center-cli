# Enterprise Hardening

For platform engineers, systems administrators and DevOps teams who deploy and
govern `bb` across a fleet. In the order a rollout usually takes:

1. [Release Verification](release-verification.md): check that a release is the
   one this project built, before you mirror or package it.
2. [Fleet Controls](fleet-controls.md): the system policy, update controls,
   builds without self-update and PKI settings that govern `bb` on every machine.
3. [Fleet Deployment](fleet-deployment.md): recipes for each operating system and
   for CI, verifying a machine, a helpdesk table, and rotating tokens, upgrading
   and removing `bb`.
4. [MCP Server Governance](mcp-governance.md): what an AI agent may do through
   `bb ai mcp serve`, and how to confine and audit it.

## Related security documents

- [Security Architecture and Threat Model](threat-model.md): the STRIDE analysis,
  trust boundaries and compliance matrix these controls answer.
- [Git Authentication](git-authentication.md): how the host-scoped credential
  helper works.
- [Networks, Proxies and TLS](networks-proxies-and-tls.md): proxies, an internal
  CA, mutual TLS, and diagnosing a connection.
- [Security Policy](https://github.com/vriesdemichael/bitbucket-data-center-cli/blob/main/SECURITY.md):
  reporting a vulnerability.
