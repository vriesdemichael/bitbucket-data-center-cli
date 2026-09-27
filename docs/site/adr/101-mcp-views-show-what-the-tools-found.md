---
search:
  boost: 0.3
---

# ADR 101: The show tool puts what the tools found in front of the person as an MCP view

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `101`
- Title: `The show tool puts what the tools found in front of the person as an MCP view`
- Category: `architecture`
- Status: `accepted`
- Amends: `62, 94, 99`
- Provenance: `guided-ai`
- Source: `docs/decisions/101-mcp-views-show-what-the-tools-found.yaml`

## Decision

bb ai mcp serve offers MCP Apps views through one tool, show, which the model calls once it has what the person asked about: a pull request, a list of pull requests, or a pull request's diff. The data tools carry no view. show declares its view statically in its _meta, and its result carries what the view draws in _meta beside a short text for the model. A client that renders no views is told nothing was shown, and nothing is fetched for it. A kind is offered while the tool whose answer it shows is exposed.
The view is one page, ui://bb/view: plain JavaScript and CSS embedded in the binary. It holds no Bitbucket data, stays out of resources/list, may be cached by anyone, and declares no network domains. bb fetches avatars from /users/{slug}/avatar.png with its own credentials and embeds them as data. Text from Bitbucket only ever becomes a text node. The page uses Bitbucket's own words and icon metaphors, and the host's theme variables.

## Agent Instructions

Add a view as a kind of show, never as _meta.ui on a data tool. Build elements with el() and never parse a string as HTML; TestViewScriptsBuildNoHTMLFromStrings and the browser tests under -tags views hold that. Put what a view draws in the result's _meta, and keep its text for the model short. Take wording from Bitbucket's UI strings. Look at a change with go run ./tools/view-preview against the local stack.

## Rationale

A client mounts a live view for every call of a tool that has one, and an agent calls the data tools many times in a turn. Data in the result lets a stored conversation render without bb, and keeps the page the same for everyone. bb reaches Bitbucket with its credentials, CA bundle, client certificate and proxy, where a request from the client's webview has none of them, and Bitbucket answers an anonymous avatar request with its default image. A page with no build step keeps go build complete, and the reference SDK's weight out of every view.

## Rejected Alternatives

- `A view on get_pull_request, list_pull_requests and get_pr_diff`: An agent that looks at six pull requests would leave six live views in its turn, above an answer about one of them.
- `Declare the Bitbucket host in the view's policy and load avatars from it`: The request comes from the client's webview, without bb's credentials, CA bundle or client certificate, and gets the default image. The declaration admits scripts and styles too.
- `React and the reference SDK, bundled with Vite`: Node in every build, some 400 KB in every view, and a go build without the bundle has no views.
- `Fetch what a view draws from the view, once it renders`: A re-rendered conversation would need bb running, and each fetch is a tool call a host may confirm.
