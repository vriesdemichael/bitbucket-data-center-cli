# Views in your agent

Ask your agent about a pull request, and in a client that renders MCP Apps it
answers with the pull request itself, laid out as Bitbucket lays it out, right
in the chat. It does the same for a list of pull requests and for a diff.

![A pull request card: its title, author and branches, who requested changes, a failed and a running build, an open task, and how many approvals and builds passed](assets/views/card-light.webp#only-light)
![A pull request card: its title, author and branches, who requested changes, a failed and a running build, an open task, and how many approvals and builds passed](assets/views/card-dark.webp#only-dark)

The card puts what asks something of someone on one line: who requested
changes, a conflict, failed and running builds, open tasks. The counts under it
are Bitbucket's, however many reviewers and builds there are. **Overview** opens
the rest, and **Open in Bitbucket** takes you there.

## Where it works

Views need a client that renders MCP Apps, such as Claude Desktop, VS Code or
Goose, with `bb ai mcp serve` as one of its MCP servers ([setting it
up](ai-and-llms.md#the-mcp-server)). There is nothing to turn on: the model calls
the `show` tool once it has found what you asked about. A terminal client, such
as Claude Code, shows the plain answer, and `bb ai mcp serve --exclude show`
turns views off.

## The overview

Fullscreen where the client has it, or opened out in place where it does not:
the description as Bitbucket formats it, the reviewers by their decision, every
build by its state, and the comments and tasks.

![A pull request's overview: its description beside the reviewers grouped by decision, and its builds grouped by state](assets/views/overview-light.webp#only-light)
![A pull request's overview: its description beside the reviewers grouped by decision, and its builds grouped by state](assets/views/overview-dark.webp#only-dark)

## A list of pull requests

A row for each pull request, flagging only what needs attention: a request for
changes, a draft, failed builds. Fullscreen adds Bitbucket's dashboard columns
and filters.

![A list of four pull requests, two of them with changes requested and a failed build, one a draft](assets/views/list-light.webp#only-light)
![A list of four pull requests, two of them with changes requested and a failed build, one a draft](assets/views/list-dark.webp#only-dark)

## A diff

File by file, with Bitbucket's change lozenges and the files in their
directories. Select lines to add them to the chat, or to ask about them. A file
too large to carry is named, with a link to its diff in Bitbucket.

![A pull request's diff: the changed files by directory beside the modified, renamed and deleted files with their hunks](assets/views/diff-light.webp#only-light)
![A pull request's diff: the changed files by directory beside the modified, renamed and deleted files with their hunks](assets/views/diff-dark.webp#only-dark)

## What you can rely on

- A view carries what it draws. It renders again when you come back to the
  conversation, even without bb running, and it says when it was read.
- A count is Bitbucket's count of the whole, and a view that lists fewer says
  so. A failed build or a request for changes is never behind a click.
- What you click opens in Bitbucket, through the client. A client that will not
  open a link shows you its address to copy.
- bb reads everything a view shows, avatars included, with its own credentials:
  the client never contacts Bitbucket.
