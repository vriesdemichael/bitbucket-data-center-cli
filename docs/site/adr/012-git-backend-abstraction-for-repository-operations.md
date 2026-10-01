---
search:
  boost: 0.3
---

# ADR-012: Git backend abstraction for repository operations

Every git operation bb performs runs through internal/git. `git.Backend` is the interface for what a command does to a repository: clone, fetch, checkout, remotes and configuration. A command takes its backend from a factory a test can replace, so its git behaviour is tested without a repository. bb has one implementation, internal/git/execgit (ADR-020), which also carries the readers completion uses to list refs, commits and trees. Nothing else in the bb binary starts git.

Add a git operation to execgit, and to `git.Backend` when a command needs it replaced in a test. Never start git from a command, a workflow or a service. A second implementation, if one is added, passes the same tests as execgit.

How git is run -- its environment, its timeouts, the credentials it is handed and how its failures are reported -- is decided in one package, so every command gets the same answer, and a command's own logic can be tested apart from it.

## Not chosen

- **Run git from each command that needs it**: Every command would settle environment, timeout and credential handling on its own, and its tests would need a real repository.
