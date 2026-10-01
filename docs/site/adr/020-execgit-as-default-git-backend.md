---
search:
  boost: 0.3
---

# ADR-020: Execgit as default Git backend

bb's git backend is internal/git/execgit, which runs the system git binary. Any other backend would be opt-in, never the default. execgit runs git with the variables that point it at a repository removed from its environment, so the working directory alone decides which repository it acts on. A command that works locally is stopped once it has run for the backend's timeout; a clone or a fetch runs as long as it reports progress, and is stopped once it has reported none for that long. Credentials are handed to one invocation and never written into a repository (ADR-044), and every message execgit returns redacts them. A failed command is reported with the command it ran; one stopped for time is transient, or unknown_outcome when it changes something, and an interrupt is cancelled.

Pass git its arguments as a slice, never as a string a shell parses. Decide a command's environment, its timeout and how its failure is reported in execgit, not at the call site.

Running the git binary is what the user's own git does: their configuration, credential helpers and protocol support apply, and the behaviour is upstream git's. A library reimplements git and differs from it at the edges, where a workflow notices.

## Not chosen

- **A Go-native git library as the default backend**: Higher risk of parity gaps and edge-case incompatibilities, and it does not apply the configuration the user's own git does.
