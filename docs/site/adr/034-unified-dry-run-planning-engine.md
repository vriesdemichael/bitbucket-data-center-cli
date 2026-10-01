---
search:
  boost: 0.3
---

# ADR-034: Unified dry-run planning engine for server mutating commands

`--dry-run` is one global flag, and every command that changes state answers it through one planning engine: the profiles and the interceptor in `internal/cli/dryrun.go`, and the preview in `internal/cli/dryrunpreview`. A command that changes the server checks, in its own handler, the state its change depends on, and builds the preview from what it found. A command that checks nothing first, and one that changes only this machine, gets a static preview from the interceptor: the change it would make, predicted. Neither may make its change under `--dry-run`. Every preview answers with a verdict and its tier (ADR-096, ADR-078).

Implement a preview through these shared abstractions, never through a flag or an output of the command's own. Plan a server mutation statefully wherever the state it depends on can be read. Test a stateful preview against a live Bitbucket, and read the state back afterwards to show the dry run changed nothing.

One model keeps previews from diverging between commands and keeps their output the same for scripts and people. A check against live state predicts what a static preview cannot: a create that would conflict, a set that is already the value asked for, a permission the caller lacks.
