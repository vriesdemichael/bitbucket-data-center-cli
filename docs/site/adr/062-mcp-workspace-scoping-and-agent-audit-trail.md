---
search:
  boost: 0.3
---

# ADR-062: The MCP server can be confined to a workspace, and records what agents attempt there

`bb ai mcp serve --project` and `--repo` confine the server to a project or to one repository, and `--audit-file` records what agents attempt, as JSON Lines. Both are off by default. Both are enforced in one middleware, never in a handler. Every tool call, resource read, resource list, prompt and completion passes through it.

Each tool has a scope rule:

- A tool that takes a project and a repository has them filled in when a call leaves them out, and is refused when they name something else.
- A tool whose project is only a filter has that filter pinned under a project scope, and is withheld under a repository scope.
- A tool whose target Bitbucket does not scope to a project, such as a build status keyed by commit, is withheld under any scope. Withheld means left out of tools/list as well as refused.
- `list_pull_requests`, `show` and `refresh_view` take the project and the repository as optional, because leaving the repository out asks for the caller's own pull requests across repositories. Under a project scope, that answer is narrowed to the project. A filter applied to what Bitbucket answered is no boundary elsewhere; here it is accepted, because the answer holds only the caller's own work and the filter decides everything the agent receives.

An audit record says what was asked for, with which arguments and resource, in which scope, by whom, and how it ended. A credential is redacted wherever it appears in them, as it is in the error message. For a tool that asks the person (ADR-098), the question is a record of its own, status asked, written before anyone answers, so a question nobody answers still leaves a trace. The decision is another record, which says how the person answered: accepted, declined, cancelled, or unavailable. A call the answer stopped is recorded as denied. A client that is asked within the one call leaves only the decision. Tool calls, reads, the list and prompts are audited; completions and the lists of tools, templates and prompts are not. The record is written before the answer goes back. When the record cannot be written, the answer is an error instead, unless `--audit-failure=warn`. The record goes to a file or to stderr, never to stdout, which carries the protocol. An administrator can set the file with the `mcp_audit_file` policy key. It binds only where the policy is out of the developer's reach (ADR-058).

Give every new tool a scope rule in the same change. `TestEveryToolHasAScopeRule` fails without one, in both directions. Choose the rule by what the tool's arguments can bound, and keep a tool's project and repository required unless leaving them out asks Bitbucket something of its own. Never write a scope check that lets a call through because it found no argument to check: it permits everything it does not understand, while reading like enforcement. Do not turn auditing on by default. Do not describe either control as more than it is: the trail is not tamper-evident, and an agent with a shell bypasses both.

A token bounds the server to what its owner can read, and on a shared instance that is most of it. A scope bounds the server to the part the work is about. One middleware makes that hold for tools nobody has written yet, where a check in each handler is one more chance to forget. The trail is not Bitbucket's audit log over again. A call the scope refuses never reaches Bitbucket, and a blocked attempt is the sign of prompt injection worth alerting on. Bitbucket also cannot tell an agent apart from the person whose token it holds.

## Not chosen

- **Compare project and repository only when the caller supplies them**: Fails open on every call that leaves them out, while reading like enforcement.
- **Refuse a pull request listing without a repository under a project scope**: Leaves an agent confined to a project no way to ask for its own pull requests, and refuses an argument the scope is meant to fill in.
- **Let unboundable tools through under a scope**: Sells a boundary that does not exist, since a commit SHA is not project-scoped in the API.
- **Write audit records asynchronously**: Loses the denials, which are the records worth having.
- **Ship direct SIEM integrations**: Puts a network call, a credential and retry buffering in the tool-call path of a process started for each IDE session. Every collector already tails a file.
