---
search:
  boost: 0.3
---

# ADR-064: Machine output carries no contract version; breaking payload changes ride the release major

The machine document carries no contract version and no field that never varies. `meta.bbVersion` reports the binary that wrote it: provenance for an operator reading stored output, not a compatibility switch. Nothing in bb branches on it and nothing outside bb should. A document is identified by its shape: `meta` and the one member the flags chose (ADR-096).

Compatibility rides the release major. Adding a field to a member is additive, and so is adding one to `meta`: `meta` is open, while the set of top-level members is closed. Removing or renaming a field, changing its type, or changing whether it can be null is breaking, and the commit carries a `!` or a `BREAKING CHANGE` footer so the release automation cuts a major. What a command promises is the schema derived from its result type (ADR-010). The generated command reference prints each command's fields from it, and `docs:verify-generated` keeps the reference in step, so a change to a payload shows in the diff. The MCP server is outside this rule: each tool declares its output schema in-band (ADR-061), because an agent connecting to `bb ai mcp serve` does not choose the binary and cannot pin it.

Do not add a version field to the document, and do not add one per command. When changing a payload, ask whether an existing consumer breaks, and if it does, mark the commit breaking. The release version is the only compatibility signal consumers have, so an unmarked break reaches them silently through package managers and `bb update`.

A payload version exists so a server can tell clients which shape they are getting, because those clients cannot choose the server's code. A CLI inverts that: the consumer installs the binary, so the binary's version already is the contract version, and pinning the binary pins the contract.

## Not chosen

- **One version for every payload, bumped on any breaking change**: A break in one payload would tell the consumers of every other one that their contract changed.
- **A version per command**: A number per command to maintain by hand, each needing the judgement the release major already gets, and no help to a consumer who installs the binary either way.
- **A version field that is never bumped**: A field that never changes reads as a guarantee. A compatibility signal that is always the same value is worse than none.
- **A document-type tag in meta**: It never varies, and which member is present already tells one document from another. A file kept for later needs to say what it is; stdout from a command you just ran does not.
