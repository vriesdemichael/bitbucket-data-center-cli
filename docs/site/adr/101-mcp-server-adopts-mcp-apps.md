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
A view keeps itself current while it is on screen. It draws what its result carries, then calls refresh_view, a read-only tool offered to views and not to the model, with the show call it answers and a fingerprint of what it draws; the answer carries the data only when it has changed. It asks often while something is in motion, such as a running build, less often the longer nothing changes, never off screen or after teardown, and not again once a pull request is closed and still. Status changes in place; a changed diff is offered rather than swapped under the person's reading. A view that changed tells the model, through ui/update-model-context.
A view opens what else the person asks for in place, through refresh_view: a pull request's other views, a list's pull requests, a list in another state; it goes back, and tells the model what the person opened. What the person does in a view goes through the model's own tool, so the scope, the audit trail and the confirmation of a tool that asks apply as to the model's call. A view offers only the kinds and tools the server exposes. The pull request form is a kind like the others: the model drafts, the person submits, and nothing is created before.
Tools only views call, refresh_view and the form's suggest_form_values, are read-only, offered to views and not to the model, and go with show whatever --tools names; --exclude withholds them.

## Agent Instructions

Add a view as a kind of show, never as _meta.ui on a data tool. Build elements with el() and never parse a string as HTML; TestViewScriptsBuildNoHTMLFromStrings and the browser tests under -tags views hold that. Put what a view draws in the result's _meta, and keep its text for the model short. Count from Bitbucket's totals, never from what a view lists. Keep a new kind refreshable, and out of its fingerprint anything that differs between two reads of the same state. Take wording from Bitbucket's UI strings. Look at a change with go run ./tools/view-preview against the local stack.

## Rationale

A person takes in a pull request faster laid out as Bitbucket lays it out than from a model's account of it, and MCP Apps is how a server draws in any client that renders views. A client mounts a live view for every call of a tool that has one, and an agent calls the data tools many times in a turn. Data in the result lets a stored conversation render without bb, and keeps the page the same for everyone. bb reaches Bitbucket with its credentials, CA bundle, client certificate and proxy, where a request from the client's webview has none of them, and Bitbucket answers an anonymous avatar request with its default image. A page with no build step keeps go build complete, and the reference SDK's weight out of every view. A count taken from part of a list reads as the whole, a tidy view that hides a failed build misleads more than a cluttered one, and a chat holds a view of about one screen. A view left open goes stale while the person watches a build finish or a reviewer answer, and the model answers from what show told it; asking only for what is on screen, and less for what is quiet, keeps the load on Bitbucket to what someone is looking at. Bitbucket leaves a pull request's version and update time alone when a comment is added or resolved, so only the view's own data tells whether it changed.

## Rejected Alternatives

- `A view on each data tool`: An agent that looks at six pull requests would leave six live views in its turn, above an answer about one of them.
- `Declare the Bitbucket host in the view's policy and load avatars from it`: The request comes from the client's webview, without bb's credentials, CA bundle or client certificate, and gets the default image. The declaration admits scripts and styles too.
- `React and the reference SDK, bundled with Vite`: Node in every build, some 400 KB in every view, and a go build without the bundle has no views.
- `Fetch what a view draws from the view once it renders, or more of it as the person scrolls`: A re-rendered conversation would show nothing without bb running, and a view would wait on a tool call before it drew.
- `Refresh a view by calling show again`: Every refresh would carry the whole payload, changed or not, and show is the model's tool.
- `Draw everything a result carries, inline`: A list or a diff of a thousand rows buries the conversation, and a host scrolls a view it caps inside the chat.
