# ADR 064: Machine output carries no contract version; breaking payload changes ride the release major

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `064`
- Title: `Machine output carries no contract version; breaking payload changes ride the release major`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/064-machine-output-carries-no-contract-version.yaml`

## Decision

The machine envelope carries no constant fields. The contract version is removed, and so is meta.contract, which named the format and never varied. meta.bbVersion is added, reporting the binary that produced the document: provenance for an operator reading stored output, not a compatibility switch. Nothing in bb branches on it and nothing outside bb should. What identifies the document is its shape -- meta.bbVersion, and exactly one of data or error. Compatibility rides the release major instead. Adding a field to data is additive. Removing or renaming one, changing its type, or changing whether it can be null is breaking, and the commit must carry a ! or a BREAKING CHANGE footer so the release automation cuts a major. Whether a change is breaking is therefore computed from the schemas rather than judged, so the guarantee is exactly as wide as the commands that have one. Making that total is the point of deriving each schema from a typed result the command already builds, rather than maintaining schema files by hand. The MCP surface is out of scope. ADR-061's per-tool contracts stay: an agent connecting to bb ai mcp serve does not choose the binary and cannot pin, so in-band declaration does real work there. The two surfaces get different answers on purpose.

## Agent Instructions

Do not add a version field to the envelope, and do not reintroduce one per command. When changing a data payload, ask whether an existing consumer breaks. If it does, mark the commit breaking. The release version is the only compatibility signal consumers have, so an unmarked break reaches them silently through package managers and bb update. If the command you are changing has no schema, add one in the same change. A diff cannot see a payload it does not have.

## Rationale

A payload version exists so a server can tell clients which shape they are getting, because those clients cannot choose the server's code. A CLI inverts that: the consumer installs the binary, so the binary version already is the contract version. The field also did not work. It was a single global constant shared by all 233 commands, so a breaking change to one payload could not be signalled without falsely signalling it for the other 232. It never moved, and the one time it did, v1 to v2, no migration notes were produced anywhere in the repository. Removing it makes the guarantee enforceable rather than declarative: a schema diff computes breakage, and the release automation turns that into a version number consumers already act on.

## Rejected Alternatives

- `Keep the global version and bump it on any breaking change`: One number for 233 independent payloads. Bumping it tells 232 consumers their contract changed when it did not, which is why it was never bumped.
- `Give each command its own payload version`: Correct in principle and unmaintainable in practice: 233 numbers to hand-maintain, each needing the same judgement the release major already gets from a schema diff. It also does not help a consumer who cannot pin, because they install the binary either way.
- `Keep the version field but stop maintaining it`: A field that never changes reads as a guarantee. Leaving it publishes a compatibility signal that is always the same value, which is worse than none.
- `Keep meta.contract as a document-type tag`: The same objection, one field over. It never varies, and the job a type tag exists to do -- telling one document from another -- is already done by which key is present, since the success and failure envelopes are distinguished by data versus error rather than by it. Persistent artifacts do need identity, which is why the bulk policy, plan and apply-status files keep apiVersion and kind; transient stdout from a command you just ran does not.
