---
search:
  boost: 0.3
---

# ADR-002: Taskfile for development automation

Use Taskfile as the primary interface for developer workflows including local stack management, validation, and test orchestration.

Prefer existing Taskfile tasks over ad-hoc shell commands. When adding recurring workflows, add or update a Taskfile task with clear naming and description.

Task provides a discoverable, cross-platform command interface with dependency support and clear namespacing for project workflows.

## Not chosen

- **Shell scripts only**: Harder to discover, compose, and standardize across contributors.
- **Makefile as primary interface**: Task offers clearer YAML syntax and easier workflow composition.
