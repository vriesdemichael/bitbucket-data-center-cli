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
- Amended By: `101`
- Amends: `62, 94`
- Provenance: `guided-ai`
- Source: `docs/decisions/099-mcp-resources-prompts-and-completions.yaml`

## Decision

Beside its tools, bb ai mcp serve serves resource templates, a resource list, prompts and completions. A resource URI is bitbucket://projects/{project}/repos/{repo}/..., a name bb resolves with its own credentials: the client asks bb over the same connection and never contacts Bitbucket. The templates are a pull request, its diff, its open threads, a file at a ref and a commit. Each answers with what the matching tool answers, bounded as ADR-094 asks. The list holds the caller's open pull requests and those waiting on their review. Completions list project keys, repository slugs, pull requests, paths and refs the way the shell does. Prompts embed the resources they are about.
Templates, prompts and capabilities are the same for every connection, and the list varies only with the credentials. A template is served while the tool it answers like is exposed, a prompt while every tool it embeds is, and the list while list_pull_requests and get_pull_request are, so --tools and --exclude decide them as they decide tools. Every template and prompt names project and repo, and the governance middleware binds reads, the list, prompts and completions to the scope as it binds tools/call. Reads, the list and prompts are audited; completions are not.
A missing resource is -32602, and reads and the list are cacheScope private. A tool result with a matching resource links it beside its content, never instead of it, for a client whose revision has resource links.

## Agent Instructions

Add a resource template to AllResourceSpecs and a prompt to AllPromptSpecs, naming project and repo; TestEveryResourceTemplateNamesItsProjectAndRepository and TestEveryPromptTakesItsProjectAndRepository hold them to it. Answer a resource through the function its tool answers through, so the two cannot disagree. In middleware, change a tool, prompt or read result the SDK produced rather than answering in its place: one that bypasses the SDK's dispatcher goes out without resultType. A list or completion result gets it after the middleware has run. Build and parse resource URIs with the functions in resources.go, which escape every value as UTF-8 and cannot fail; the URI template library go-sdk routes with encodes non-ASCII wrongly. Never make a list depend on the connection or the client. Before dropping or extending the links in tool results, find out which clients act on a resource link: no widely used server links its own resources beside structured content, and what clients do with such a link was not verified when this was decided.

## Rationale

A skill teaches an agent commands. Resources let the person put a pull request, a diff or a file in front of the model from a picker, in any MCP client, with bb's token, scope and audit applied, which is the reason to run the server rather than the skill alone. A link beside a tool's content is how a client learns the URI of what it was shown, to attach it or read it again.

## Rejected Alternatives

- `https:// URIs of Bitbucket's own pages`: The specification lets a client fetch those itself, and it holds none of bb's credentials.
- `A resource list of every repository or file`: Unbounded. A picker needs a starting point; templates with completions reach the rest.
- `Render pull requests and commits as Markdown for the person`: A second presentation of what the tools answer, free to disagree with it.
- `Carry a tool's content as an embedded resource with its URI, as GitHub's MCP server returns files`: The model then sees the text only in clients that pass an embedded resource on to it. Text content reaches it in every client (ADR-094).
- `No resource links in tool results, as GitHub's server outside files, Playwright, Sentry, Notion, GitLab and most others do`: A client then cannot learn the URI of what a tool showed it. A link beside complete content costs one item.
- `A link instead of the content for a large file, as GitHub's server does from 1 MB`: A chat client cannot follow it. bb describes a file it will not read (ADR-094).
- `Link the Bitbucket page, as Grafana's server links a datasource's configuration page`: The page is in the result already, for the person. A resource link names what the client can read through bb.
- `A link for every item of a listing, as AWS's code interpreter lists its files`: A listing names its items already. The links go where a result is one resource.
