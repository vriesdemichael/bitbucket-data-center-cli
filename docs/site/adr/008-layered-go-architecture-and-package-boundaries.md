---
search:
  boost: 0.3
---

# ADR-008: Layered Go architecture and package boundaries

bb is built in layers, and a dependency points down, never up. `internal/transport` sends requests to Bitbucket and the other servers bb talks to; `internal/services` turns Bitbucket's API into operations; `internal/workflows` strings several operations into one; `internal/cli` and the MCP server in `internal/mcp` put them in front of a person or an agent. A layer may use any layer below it, and none uses a layer above. Something two layers both need lives outside both, as `internal/safederef` does. `TestTheServiceLayerDoesNotImportTheCLI` holds the service layer to this.

Put new code in the narrowest layer that owns it: no request handling in a command, and no flag or output concern in a service. When a change seems to need a dependency that points up, change the design instead.

A Bitbucket quirk is then handled once, in the layer that meets it, and tested there, and the command line and the MCP server present the same operations.

## Not chosen

- **One service package with mixed responsibilities**: Couples HTTP, mapping and workflow behaviour so that none of them can be tested alone.
