---
search:
  boost: 0.3
---

# ADR-013: The command tree has gh's shape, one package per top-level group

bb is one Cobra command tree in gh's shape: a top-level group names a kind of Bitbucket resource, and its subcommands are what you do to it, as in `bb pr merge`. Every command writes the same result as text or as the machine document (ADR-095). `bb search` is a top-level group that is not a resource: `bb search repos`, `bb search commits` and `bb search prs` find things across the instance, each on a Bitbucket listing endpoint and its filters, with the paging flags (`--limit`, `--start`, `--all`) and the output contract every listing has.

Each top-level group is a package under internal/cli/cmd exposing `New(Dependencies)` for the root to call. internal/cli keeps what spans the tree: the root and its global flags, the wiring each constructor receives, and the passes over the finished tree. Helpers several groups need, such as output writing and repository or pull request selection, live in shared packages under internal/cli.

Add a command under the group of the resource it acts on, as a verb, in that group's package, and hand it what it needs through Dependencies. Put discovery that is not bound to one resource under `bb search`, as `bb search <resource>`. Add another top-level command that is not a resource group only with a record saying why. Give a flag one meaning across the tree, and the same description wherever it means the same. Move a helper another group needs to a shared package rather than copying it.

A stable taxonomy lets a person or an agent guess a command from the ones they know, and lets automation rely on paths that do not move. Some discovery has no one resource to hang from: the pull requests you are involved in across every repository, or a repository whose project you do not know. gh puts it under a search group, and that is where its users look. A package per group keeps a change, and its review, to the group it touches.
