---
search:
  boost: 0.3
---

# ADR-098: MCP tools that decide a merge ask the person through the client

Every MCP tool is exposed. Some tools ask the person to confirm each call in the MCP client before they run: a tool that merges, one that changes whether or when a pull request merges, one that feeds a check deciding whether it may, and `create_tag`. `update_pull_request` asks only when a call sets the draft flag. The confirmation is an elicitation with one required, unticked checkbox that names the target, and the tool acts only when it is accepted. A client that cannot show one gets -32021 MissingRequiredClientCapability, whatever revision it speaks. An answer accepts only the call it was asked about. The request state is signed, and holds the tool, a digest of the scoped arguments, an expiry, and a nonce that can be used once. A merge and an auto-merge are held to the pull request version the person was shown. How the person answered goes into the audit record (ADR-062).

`--read-only` exposes only the tools annotated read-only. `--yolo` and `--allow-writes` are deprecated and do nothing (ADR-084).

Annotations say what the specification defines them to say, whether or not a tool asks. Every tool declares all four hints and a title. openWorldHint is false, because one configured Data Center instance is a closed domain.

Declare a tool that decides a merge with `askingSpec`, and every other tool with `toolSpec`. `TestToolsThatAskAreTheOnesThatDecideAMerge` holds the set. Annotate a tool with readOnly or writes, and set each hint by its definition, never by whether the tool asks. Build a confirmation from the arguments, and read Bitbucket only for what the person must see that the call does not carry. Quote text other people wrote, and put no link in it. Never skip a confirmation for a client, and never make tools/list depend on what a client can do. If bb serves Bitbucket Cloud, set openWorldHint to true on the tools that read or publish content there.

A tool that is withheld fails without saying why, and a flag that exposes it runs it with nobody asked. MCP carries the person's decision itself. If you cannot trust a harness with the annotations and the confirmations, do not make changes in Bitbucket through it: run it read-only and make them yourself. The confirmation is in the handler, one wrapper applied to every tool that asks, because go-sdk's bridge for handshake-era clients sits below the middleware and never sees an input request the middleware returns. The specification treats a request state as written by an attacker, hence the signature. Clients that approve on their own accept a form with no field, hence the checkbox.

## Not chosen

- **Keep --yolo as the override for a client that cannot ask**: It makes the change with nobody asked. A client that cannot confirm a change should not be making it.
- **Withhold the tools that ask from a client that cannot elicit**: tools/list must not vary per connection, and a tool that is missing fails without saying why.
- **A tool error instead of -32021 for clients on older revisions**: Two answers to one refusal. The code the current revision defines says what is missing.
- **Derive destructiveHint from whether a tool asks**: It would call `create_tag` destructive, though it only adds, and `update_pull_request` additive, though it overwrites.
- **Confirm by typing the pull request reference**: Merging is routine and no tool deletes anything. Typing is for a tool that removes what cannot be restored.
