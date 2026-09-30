---
search:
  boost: 0.3
---

# ADR-039: Built-in MCP server with explicit host scoping and token capability restriction

`bb ai mcp serve` is an MCP server over stdio, and it serves one Bitbucket instance. When more than one is configured, `--host` is required, and without it the server exits at once with an error saying so rather than pick one. It acts with the credential its environment supplies: an MCP client that sets a read-only token as `BITBUCKET_TOKEN` in the server's env block gets a server with that token's rights and no more. No flag takes the credential (ADR-083). `--tools` and `--exclude` narrow the tools it offers, and `bb ai mcp tools` lists them with whether each writes and whether it asks. Which tools ask the person, and what `--read-only` leaves, is ADR-098.

No record or hand-written page lists the tools. `mcp.AllSpecs()` is the catalogue, `bb ai mcp tools` prints it, and a client receives it when it connects. `TestADRDoesNotNameToolsThatDoNotExist` fails when a record names a tool the server does not have.

When generating MCP client configuration, make `bb ai mcp serve` the command and put the credential in the client's env block. When `bb auth server list` shows more than one instance, ask which one and pass it as `--host`; never pick one silently.

Stdio is how an IDE starts a local MCP server. A server that chose an instance by itself would sooner or later act on the wrong one, and the mistake would surface late. A token per server lets a person run a read-only server beside one that writes, with no access control inside bb.

## Not chosen

- **HTTP transport instead of stdio**: Needs a port and firewall rules, and IDE clients start local servers over stdio.
- **Use the active server context when several are configured**: Silently targets the wrong instance in a multi-tenant setup, and the mistake surfaces late.
- **A token per tool**: Much more to configure and to reason about. A server per token covers the read-only and writing pair.
- **Tool filtering in a configuration file instead of flags**: One more file to find. Flags sit in the client's configuration, where the rest of the server's setup is.
