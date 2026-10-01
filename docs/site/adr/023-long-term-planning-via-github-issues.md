---
search:
  boost: 0.3
---

# ADR-023: Long-term planning via GitHub issues

Work that outlives one branch or one session is tracked in a GitHub issue: features, efforts of several steps, known bugs and technical debt. The issues are the project's plan; local notes and conversation context are not. A large effort is one issue with a checklist or sub-issues. Start a session by reading the open issues that bear on the task. An agent asks before opening an issue, and fixes a problem it meets mid-task on the branch in hand. A commit that resolves an issue names it with a closing keyword in its body (ADR-081).

A session ends and its context goes with it. Issues are shared, searchable and reviewable, by the people and the agents who work here.

## Not chosen

- **Plans in local files**: Nobody else sees them, and they drift from one session to the next.
- **Conversation context as the plan**: It is gone when the session ends.
