---
search:
  boost: 0.3
---

# ADR-094: MCP tools pass content to the model in a format it can ingest

An MCP tool may be answering a chat app that speaks MCP and nothing else: no file system, no shell, no bb. So what the model needs arrives as text or as an image, the two kinds of content every such client shows it, converted on the server. Audio, or a video in an embedded resource, may come as well, as an extra a client can ignore. A result never points the model at a command, a local path, a download, or a link it cannot follow. It may give a page in Bitbucket for the person the model is talking to. `get_file_content` is the case in point. Text comes back in numbered windows the model pages through. Word, PowerPoint and Excel files come back as their extracted text, and archives as a listing. Images come back as images, scaled to what clients take, with a note when they were scaled. Anything else is described: what it is and how large. What a view draws for the person travels beside the text, in the result's `_meta` (ADR-101). The text is still what the model gets.

Put what a model needs into text or image content, converted in the server, and bound every answer with a window, a cap or a scale. Describe what cannot be converted: what it is, how large, and its page in Bitbucket for a person to open. Never tell the model to run bb or any other command, to read a path, or to download something. Never answer with a resource link or template in place of content. A link beside complete content names the resource the result came from (ADR-099). Convert with the standard library and golang.org/x/image. A new converter dependency is for the owner to agree to.

A model in a chat client can use only what the client shows it. A command it cannot run, a path it cannot open and a link the client shows as plain text are dead ends that read like answers. The server holds the bytes and can convert them, so every client gets the same usable answer.

## Not chosen

- **Return a file's bytes as text**: A binary file comes back corrupted, and a large one fills the context in one piece.
- **Point the model at `bb repo cat` for a file the tool cannot show**: The client may have no shell and no bb, which leaves the model an instruction it cannot follow.
- **Resource links and resource templates in place of content**: Most clients show a link to the model as text and never read it.
- **A PDF library to extract a PDF's text**: Declined. A PDF is described, with its page in Bitbucket for a person to open.
