# ADR 062: The MCP server can be confined to a workspace, and records what agents attempt there

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `062`
- Title: `The MCP server can be confined to a workspace, and records what agents attempt there`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/062-mcp-workspace-scoping-and-agent-audit-trail.yaml`

## Decision

bb ai mcp serve accepts --project and --repo, confining every tool call to that project or repository, and --audit-file, recording every invocation as JSON Lines. Both are off by default. Both are enforced in one middleware over tools/call, never in handlers. Each tool has a scope rule, held to the catalogue by a test. A tool whose arguments identify its target has them injected when omitted and refused when they name something else. A tool whose project argument is only a filter, or that addresses something Bitbucket does not scope to a project at all, is withheld: dropped from the catalogue as well as refused on call. Audit writes are synchronous and a failed write refuses the call, unless --audit-failure=warn. The sink is a file or stderr; stdout carries the protocol and is rejected. Administrators can mandate the destination with policy.mcp_audit_file, which binds only where the policy file is out of the developer's reach -- see ADR-058 for the deployment step that makes that true.

## Agent Instructions

Add a scope rule for every new tool in the same change. TestEveryToolHasAScopeRule fails without one, in both directions. Choose it by what the tool's arguments can actually bound: a tool keyed by commit SHA cannot be bounded and must be withheld. Never write a scope check that skips when it finds no argument to check. That permits every call it does not understand while reading like enforcement. Do not add per-handler enforcement, and do not enable auditing by default. Do not describe either control as preventing what it does not: the trail is not tamper-evident, and an agent with shell access bypasses this layer entirely.

## Rationale

ADR-039 bounds a server to one instance and one token's rights, not to a part of the instance. On a multi-tenant Bitbucket a read-only token still reaches most of what its owner can read. Middleware rather than handlers is the load-bearing choice: 24 handlers is 24 chances to forget, silently, and one choke point makes the property hold for tools nobody has written yet. The trail is not a duplicate of Bitbucket's. A call refused by the scope never reaches Bitbucket, so no server-side record can exist -- and attempted-and-blocked is the prompt-injection signal worth alerting on. Bitbucket also cannot tell an agent from the person whose token it uses.

## Rejected Alternatives

- `Compare project and repository only when the caller supplies them`: Fails open on every call that omits them, while reading like enforcement.
- `Let unboundable tools through under a scope`: Sells a boundary that does not exist; a commit SHA is not project-scoped in the API.
- `Write audit records asynchronously`: Loses the denials, which are the records worth having.
- `Ship direct SIEM integrations`: Puts a network call, a credential and retry buffering in the tool-call path of a process spawned per IDE session. Every collector already tails a file.
