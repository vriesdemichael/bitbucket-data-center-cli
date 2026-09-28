---
search:
  boost: 0.3
---

# ADR 101: The MCP server adopts MCP Apps, and shows its views through one tool

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `101`
- Title: `The MCP server adopts MCP Apps, and shows its views through one tool`
- Category: `architecture`
- Status: `accepted`
- Amends: `62, 94, 99`
- Provenance: `guided-ai`
- Source: `docs/decisions/101-mcp-server-adopts-mcp-apps.yaml`

## Decision

bb ai mcp serve adopts MCP Apps. In a client that renders them, the model can put what it found in front of the person as an interactive view instead of describing it; a client that renders none gets text as before.
Views are kinds of one tool, show, which the model calls once it has what the person asked about. The data tools carry no view. A kind is offered while the tool whose answer it shows is exposed, and a client that renders no views is told nothing was shown, and nothing is fetched for it.
One page, ui://bb/view, draws every view: plain JavaScript and CSS embedded in the binary. It holds no Bitbucket data, stays out of resources/list, may be cached by anyone, and declares no network domains. What a view draws travels in the result's _meta beside a short text for the model, avatars included, which bb fetches with its own credentials. Text from Bitbucket is never parsed as HTML: the page builds elements with the DOM API, draws Markdown with a parser of its own, and opens a link only through the host, only to a web address. It uses Bitbucket's own words and icon metaphors, and the host's theme.
A count in a view is Bitbucket's count of the whole, and a view that lists fewer says so. What needs the person is never behind a click. Inline, a view keeps one shape and grows a step at a time when the person asks; fullscreen shows everything the result carries. A view says when its data was read.

## Agent Instructions

Add a view as a kind of show, never as _meta.ui on a data tool. Build elements with el() and never parse a string as HTML; TestViewScriptsBuildNoHTMLFromStrings and the browser tests under -tags views hold that. Put what a view draws in the result's _meta, and keep its text for the model short. Count from Bitbucket's totals, never from what a view lists. Take wording from Bitbucket's UI strings. Look at a change with go run ./tools/view-preview against the local stack.

## Rationale

A person takes in a pull request faster laid out as Bitbucket lays it out than from a model's account of it, and MCP Apps is how a server draws in any client that renders views. A client mounts a live view for every call of a tool that has one, and an agent calls the data tools many times in a turn. Data in the result lets a stored conversation render without bb, and keeps the page the same for everyone. bb reaches Bitbucket with its credentials, CA bundle, client certificate and proxy, where a request from the client's webview has none of them, and Bitbucket answers an anonymous avatar request with its default image. A page with no build step keeps go build complete, and the reference SDK's weight out of every view. A count taken from part of a list reads as the whole, a tidy view that hides a failed build misleads more than a cluttered one, and a chat holds a view of about one screen.

## Rejected Alternatives

- `A view on each data tool`: An agent that looks at six pull requests would leave six live views in its turn, above an answer about one of them.
- `Declare the Bitbucket host in the view's policy and load avatars from it`: The request comes from the client's webview, without bb's credentials, CA bundle or client certificate, and gets the default image. The declaration admits scripts and styles too.
- `React and the reference SDK, bundled with Vite`: Node in every build, some 400 KB in every view, and a go build without the bundle has no views.
- `Fetch what a view draws from the view, once it renders, or more of it as the person scrolls`: A re-rendered conversation would need bb running, and each fetch is a tool call a host may confirm.
- `Draw everything a result carries, inline`: A list or a diff of a thousand rows buries the conversation, and a host scrolls a view it caps inside the chat.
