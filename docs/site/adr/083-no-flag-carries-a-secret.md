---
search:
  boost: 0.3
---

# ADR-083: No flag carries a secret

No flag takes a credential as its value, on any command. A secret reaches bb from stdin behind a `--<name>-stdin` flag, such as `--token-stdin` or `--secret-stdin`, from a prompt when a person is there, from the environment, or from where `bb auth login` stored it (ADR-047). A header or a URL is no way round that: `bb api` refuses an `Authorization`, `Proxy-Authorization` or `Cookie` header and a URL carrying a user or password, and authenticates a request with the credential resolved for its host, as every command does. The MCP server takes its credential from the env block its client starts it with, so a read-only token set there as `BITBUCKET_TOKEN` gives the server that token's rights and no more (ADR-039).

Do not add a flag whose value is a secret. When generating MCP client configuration, put the credential in the client's env block. Never emit a bb command line containing a credential, in documentation, in a shell snippet or in a configuration file.

A flag's value is readable by every local user through `ps` and `/proc/<pid>/cmdline`, is captured by process auditing and EDR tooling, and lands in shell history. For `bb ai mcp serve` the exposure lasts the whole session, because the server is long-lived. The env block is supported by every MCP client, keeps the agent's token apart from the operator's, and stays out of the argument list.

## Not chosen

- **Keep the flags and warn when they are used**: The exposure stays, and documentation and client configurations go on passing the flag whatever the warning says.
- **Remove the auth flags but keep --token on bb ai mcp serve**: It is the worst case rather than an exception: the server runs for a whole IDE session, so the credential is exposed for the whole of it. Keeping one credential-bearing flag also keeps the rule unstatable, and a rule with an exception cannot be checked.
- **Accept the flag value from a file path instead**: A path is not a secret, so this would be safe, but it adds a third input form where stdin and the environment already cover the interactive and the automated case.
