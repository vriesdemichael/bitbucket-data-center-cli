# ADR 077: Comment endpoints are views, not collections

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `077`
- Title: `Comment endpoints are views, not collections`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/077-comment-endpoints-are-views-not-collections.yaml`

## Decision

Bitbucket exposes no "the comments on this pull request" or "the comments on this commit" resource. Every read is a view from the web interface, and bb reconstructs the resource the CLI wants out of them. Treat the endpoint as a view, and say which one answered. The path-scoped listings are the file diff view and require a path -- both of them, whatever the vendored spec says. The blocker-comments endpoint is the Tasks tab. The activity timeline is the Activity tab, and it is the only pathless way to read a pull request's comments; there is no pathless read for a commit at all. A comment must therefore be anchored to a file to be findable, so bb repo comment create takes --path, --line and --line-type. A reply inherits the anchor of what it answers. The activity timeline is a feed of actions, not a list of comments: it carries a commentAction per entry and can repeat a comment across entries. Anything that walks it dedupes by id. Bitbucket sends an anchor's path as a plain string from every comment endpoint and as an object elsewhere. Every comment response is repaired by commentanchor.NormalizeResponsePaths before it is decoded, listings and single reads included.

## Agent Instructions

Route a new comment read through decodeCommentPage or decodeCreatedComment. Never use the generated WithResponse wrapper: it decodes straight into RestComment, and one anchored comment makes the whole response fail. Publish which view answered. A summary from the timeline covers the pull request, from the path-scoped endpoint one file, and from blocker-comments only tasks -- they are not comparable. Do not add a pathless comment listing. The server refuses it, and the spec saying otherwise is the spec being wrong. Prove a comment change against a real Bitbucket. Every defect this records passed the unit suite, and two of them passed because a fixture and the spec both described a server that does not exist.

## Rationale

The API is shaped for the page that renders it: a diff view fetches comments per file, so path is required; the tabs beside it became the only pathless reads. Nothing is shaped for "give me the comments on this thing", which is the only question a CLI asks. Every comment defect found on this branch is a consequence. Replies were published as a count because the flat model read one level of a nested view. A reply on a commit reached no bb command, because a commit has no Activity tab to fall back to. bb could create commit comments anchored to nothing and then never list them. And repo comment list, get and update failed outright on any comment written through the web interface, because an anchored path decodes as a string and only the create path repaired it -- the commands for reading review feedback broke on exactly the comments a reviewer leaves. Recording it as one finding rather than four is the point: they are one mismatch between a view-shaped API and a resource-shaped CLI, and the next one will look different again.

## Rejected Alternatives

- `Trust the vendored OpenAPI spec on what each endpoint requires`: It types the commit listing's path as optional and the server answers 400. The spec is a description of the API, not the API; where they disagree the server wins and the sanitizer or a comment records the correction.
- `Build a comment cache so bb can answer resource-shaped questions`: It would make bb wrong in a new way -- stale -- and the reason to want it is that listing by path is awkward, not that it is incorrect.
- `Normalise anchor paths once, in the generated client`: The generated file is regenerated from the spec and would lose it. commentanchor is where the outbound shape is built, so the inbound repair lives beside it.
