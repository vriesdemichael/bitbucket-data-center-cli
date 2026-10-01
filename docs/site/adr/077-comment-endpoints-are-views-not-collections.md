---
search:
  boost: 0.3
---

# ADR-077: Comment endpoints are views, not collections

Bitbucket exposes no "the comments on this pull request" or "the comments on this commit" resource. Every read is a view from the web interface, and bb reconstructs the resource the CLI wants out of them. Treat the endpoint as a view, and say which one answered. The path-scoped listings are the file diff view and require a path -- both of them, whatever the vendored spec says. The blocker-comments endpoint is the Tasks tab. The activity timeline is the Activity tab, and it is the only pathless way to read a pull request's comments; there is no pathless read for a commit at all. A comment must therefore be anchored to a file to be findable, so `bb repo comment create` takes `--path`, `--line` and `--line-type`. A reply inherits the anchor of what it answers. The activity timeline is a feed of actions, not a list of comments: it carries a commentAction per entry and can repeat a comment across entries. Anything that walks it dedupes by id. Bitbucket sends an anchor's path as a plain string from every comment endpoint and as an object elsewhere. Every comment response is repaired by `commentanchor.NormalizeResponsePaths` before it is decoded, listings and single reads included.

Route a new comment read through the decoders in internal/services/comment: `decodeCommentPage` for a listing, `decodeReadComment` for one comment, `decodeWrittenComment` for what a write returns. Never use the generated WithResponse wrapper: it decodes straight into RestComment, and one anchored comment makes the whole response fail. Publish which view answered. A summary from the timeline covers the pull request, from the path-scoped endpoint one file, and from blocker-comments only tasks -- they are not comparable. Do not add a pathless comment listing. The server refuses it, and the spec saying otherwise is the spec being wrong. Prove a comment change against a real Bitbucket: a fixture written from the spec describes a server that does not exist.

The API is shaped for the page that renders it: a diff view fetches comments per file, so path is required, and the tabs beside it are the only pathless reads. Nothing is shaped for "give me the comments on this thing", which is the only question a CLI asks. A defect in bb's comment commands is usually this mismatch between a view-shaped API and a resource-shaped CLI, and the next one will look different again.

## Not chosen

- **Trust the vendored OpenAPI spec on what each endpoint requires**: It types the commit listing's path as optional and the server answers 400. The spec is a description of the API, not the API; where they disagree the server wins and the sanitizer or a comment records the correction.
- **Build a comment cache so bb can answer resource-shaped questions**: It would make bb wrong in a new way -- stale -- and the reason to want it is that listing by path is awkward, not that it is incorrect.
- **Normalise anchor paths once, in the generated client**: The generated file is regenerated from the spec and would lose it. commentanchor is where the outbound shape is built, so the inbound repair lives beside it.
