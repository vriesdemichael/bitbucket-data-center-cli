---
search:
  boost: 0.3
---

# ADR-020: bb runs git through one backend, which runs the git binary

Every git operation bb performs runs through internal/git. `git.Backend` is the interface for what a command does to a repository: clone, fetch, checkout, remotes and configuration. A command takes its backend from a factory a test can replace, so its git behaviour is tested without a repository. The one implementation, internal/git/execgit, runs the system git binary, and also carries the readers completion uses to list refs, commits and trees. Nothing else in the bb binary starts git.

execgit runs git with the variables that point it at a repository removed from its environment, so the working directory alone decides which repository it acts on. A command that works locally is stopped once it has run for the backend's timeout; a clone or a fetch runs as long as it reports progress, and is stopped once it has reported none for that long. Credentials are handed to one invocation and never written into a repository (ADR-044), and every message execgit returns redacts them. A failed command is reported with the command it ran; one stopped for time is transient, or unknown_outcome when it changes something, and an interrupt is cancelled.

Add a git operation to execgit, and to `git.Backend` when a command needs it replaced in a test. Never start git from a command, a workflow or a service. Pass git its arguments as a slice, never as a string a shell parses. Decide a command's environment, its timeout, its credentials and how its failure is reported in execgit, not at the call site, so every command gets the same answer. Another backend, if one is added, is opt-in, never the default, and passes the same tests as execgit.

Running the git binary is what the user's own git does: their configuration, credential helpers and protocol support apply, and the behaviour is upstream git's. A library reimplements git and differs from it at the edges, where a workflow notices.
