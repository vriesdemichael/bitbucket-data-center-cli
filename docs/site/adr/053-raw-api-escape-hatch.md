---
search:
  boost: 0.3
---

# ADR-053: Raw API escape hatch for uncovered endpoints and version gaps

`bb api <endpoint>` sends a request to the Bitbucket REST API as given, as `gh api` does, for what no command covers: an endpoint a newer Bitbucket added, or a plugin's. It uses what every command uses: the configured instance or `--host`, the stored credential, the TLS settings, and the retry policy of ADR-009. A credential given on the command line is refused (ADR-083). `--paginate` follows `isLastPage` and `nextPageStart` into one page. Under `--json` the response is the document's data. A GET or HEAD only reads, so it runs under `--dry-run`, where any other request is shown and not sent. The `read_only` policy refuses anything but GET and HEAD (ADR-100). The MCP server has no such passthrough: every MCP tool declares a scope rule and whether it asks the person (ADR-062, ADR-098), and a request to any path can declare neither.

Recommend `bb api` for an endpoint no command covers, rather than waiting for one. A script that calls it should pass its own `--dry-run` through.

Bitbucket's REST surface outgrows any command set with each release. A passthrough on the same configuration, credentials, transport and envelope unblocks a person or an agent at once, without a release and without giving up the safety the rest of bb has.
