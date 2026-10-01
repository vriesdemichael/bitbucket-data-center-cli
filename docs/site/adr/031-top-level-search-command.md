---
search:
  boost: 0.3
---

# ADR-031: Top-level search command tree for discovery

`bb search` finds things across the instance: `bb search repos`, `bb search commits` and `bb search prs`, each on a Bitbucket listing endpoint and its filters, with the paging flags (`--limit`, `--start`, `--all`) and the output contract every listing has. Put discovery that is not bound to one resource here, as `bb search <resource>`.

Some discovery has no one resource to hang from: the pull requests you are involved in across every repository, or a repository whose project you do not know. gh puts it under a search group, and that is where its users look.

## Not chosen

- **Filters on the resource listings, such as `bb repo list --name foo`**: Fit one resource, and leave cross-project discovery nowhere to go.
