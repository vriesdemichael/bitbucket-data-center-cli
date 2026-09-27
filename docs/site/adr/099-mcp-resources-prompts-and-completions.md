---
search:
  boost: 0.3
---

# ADR 099: The MCP server serves Bitbucket content as resources, prompts and completions

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `099`
- Title: `The MCP server serves Bitbucket content as resources, prompts and completions`
- Category: `architecture`
- Status: `accepted`
- Amends: `62, 94`
- Provenance: `guided-ai`
- Source: `docs/decisions/099-mcp-resources-prompts-and-completions.yaml`

## Decision

Beside its tools, bb ai mcp serve serves resource templates, a resource list, prompts and completions. A resource URI is bitbucket://projects/{project}/repos/{repo}/..., a name bb resolves with its own credentials: the client asks bb over the same connection and never contacts Bitbucket. The templates are a pull request, its diff, its open threads, a file at a ref and a commit. Each answers with what the matching tool answers, bounded as ADR-094 asks. The list holds the caller's open pull requests and those waiting on their review. Completions list project keys, repository slugs, pull requests, paths and refs the way the shell does. Prompts embed the resources they are about.
Templates, prompts and capabilities are the same for every connection, and the list varies only with the credentials. Every template and prompt names project and repo, and the governance middleware binds reads, the list, prompts and completions to the scope as it binds tools/call. Reads, the list and prompts are audited; completions are not.
A missing resource is -32602, and reads and the list are cacheScope private. A tool result with a matching resource links it beside its content, never instead of it.

## Agent Instructions

Add a resource template to AllResourceSpecs and a prompt to AllPromptSpecs, naming project and repo; TestEveryResourceTemplateNamesItsProjectAndRepository and TestEveryPromptTakesItsProjectAndRepository hold them to it. Answer a resource through the function its tool answers through, so the two cannot disagree. In middleware, change the result the SDK produced rather than answering in its place: a result that bypasses the SDK's dispatcher goes out without resultType. Never make a list depend on the connection or the client.

## Rationale

A skill teaches an agent commands. Resources let the person put a pull request, a diff or a file in front of the model from a picker, in any MCP client, with bb's token, scope and audit applied, which is the reason to run the server rather than the skill alone.

## Rejected Alternatives

- `https:// URIs of Bitbucket's own pages`: The specification lets a client fetch those itself, and it holds none of bb's credentials.
- `A resource list of every repository or file`: Unbounded. A picker needs a starting point; templates with completions reach the rest.
- `Render pull requests and commits as Markdown for the person`: A second presentation of what the tools answer, free to disagree with it.
