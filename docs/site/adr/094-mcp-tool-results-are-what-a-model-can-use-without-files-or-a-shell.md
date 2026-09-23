---
search:
  boost: 0.3
---

# ADR 094: MCP tool results are what a model can use without files or a shell

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `094`
- Title: `MCP tool results are what a model can use without files or a shell`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/094-mcp-tool-results-are-what-a-model-can-use-without-files-or-a-shell.yaml`

## Decision

An MCP tool answers a client that may be a chat app speaking MCP and nothing else: no file system, no shell, no bb. What the model needs arrives as text or as an image, the two kinds of content every such client shows it, converted on the server; audio, or a video in an embedded resource, may come as well, as an extra a client can ignore. A result never points the model at a command, a local path, a download or a link it cannot follow. It may give a page in Bitbucket for the person the model is talking to. get_file_content is the case in point. Text comes back in numbered windows the model pages through, Word, PowerPoint and Excel files as their extracted text, archives as a listing, images as images scaled to what clients take, with a note when they were scaled, and anything else as a description of what it is and how large.

## Agent Instructions

Put what a model needs into text or image content, converted in the server, and bound every answer with a window, a cap or a scale. What cannot be converted is described: what it is, how large, and its page in Bitbucket for a person to open. Never tell the model to run bb or any other command, to read a path, or to download something, and do not answer with resource links or templates. Convert with the standard library and golang.org/x/image; a new converter dependency is for the owner to agree to.

## Rationale

A model in a chat client can use only what the client shows it. A command it cannot run, a path it cannot open and a link the client renders as text are dead ends that read like answers. The server holds the bytes and can convert them, so every client gets the same usable answer.

## Rejected Alternatives

- `Return a file's bytes as text`: A binary file came back corrupted, and a large one filled the context in one piece.
- `Point the model at bb repo cat for a file the tool cannot show`: The client may have no shell and no bb, which leaves the model an instruction it cannot follow.
- `Resource links and resource templates`: Most clients show a link to the model as text and never read it.
- `A PDF library to extract a PDF's text`: Declined; a PDF is described, with its page in Bitbucket for a person to open.
