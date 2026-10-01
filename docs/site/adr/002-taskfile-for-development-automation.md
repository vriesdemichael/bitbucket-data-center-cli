---
search:
  boost: 0.3
---

# ADR-002: Taskfile for development automation

Every recurring developer workflow is a task in `Taskfile.yml`: the local Bitbucket stack, code and docs generation, the gates, the test suites, the docs site and the release checks. CI and the git hooks run the same tasks, so a check means the same thing wherever it runs. Use an existing task rather than running its commands by hand, and add a recurring workflow as a task with a clear name and description.

Task lists what exists (`task` on its own prints the list), runs on every platform bb is developed on, and composes tasks from other tasks.

## Not chosen

- **Shell scripts alone**: Harder to discover, to compose and to keep consistent across contributors.
- **A Makefile**: Task's YAML is clearer to read, and composing workflows in it is simpler.
