---
search:
  boost: 0.3
---

# ADR-013: CLI framework and command tree standard

bb is one Cobra command tree in gh's shape: a top-level group names a kind of Bitbucket resource, and its subcommands are what you do to it, as in `bb pr merge`. Every command writes the same result as text or as the machine document (ADR-095).

Add a command under the group of the resource it acts on, as a verb. Add a top-level command that is not a resource group only with a record saying why, as ADR-031 does for `bb search`. Give a flag one meaning across the tree, and the same description wherever it means the same.

A stable taxonomy lets a person or an agent guess a command from the ones they know, and lets automation rely on paths that do not move.

## Not chosen

- **Flat command namespace**: Harder discovery and increased risk of naming and behavior inconsistency.
