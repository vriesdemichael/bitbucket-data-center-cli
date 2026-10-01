---
search:
  boost: 0.3
---

# ADR-032: Refactor CLI root into command packages

Commands live in packages under internal/cli/cmd, one for each top-level group, each exposing `New(Dependencies)` for the root to call. internal/cli keeps what spans the tree: the root and its global flags, the wiring each constructor receives, and the passes over the finished tree. Helpers several groups need, such as output writing and repository or pull request selection, live in shared packages under internal/cli.

Put a new command in its group's package and hand it what it needs through Dependencies. Move a helper another group needs to a shared package rather than copying it.

Splitting construction by domain keeps a change, and its review, to the group it touches.

## Not chosen

- **Keep all command construction in one root.go file**: Increases maintenance cost and makes architectural boundaries harder to enforce.
