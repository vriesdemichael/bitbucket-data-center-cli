---
search:
  boost: 0.3
---

# ADR-015: Live test harness and deterministic seeding

Every live test seeds the state it needs and owns it. The harness in tests/integration/live gives a test a project of its own (`seedRepo`, `seedIsolatedProject`), with repositories filled by real git pushes, a key beginning `LT` and a random suffix (ADR-085), and deletes it in `t.Cleanup`, repositories first, because Bitbucket refuses to delete a project that still holds any. No test reads another's fixtures or depends on the order tests run in, so the suite runs in parallel, as many at once as `LIVE_PARALLEL` in Taskfile.yml says. A test that asserts on an instance-wide listing looks for its own fixtures by their unique names. A cleanup that cannot finish logs and leaves the project behind rather than fail a test that passed; `task stack:purge-fixtures` removes the `LT` projects that accumulate, and `task stack:reset` starts the instance over.

When a test needs new kinds of state, seed it in the test or extend the harness. Do not seed shared state once for the suite, and do not rely on what an earlier test left behind.

Shared mutable state makes a failure depend on what ran before it, which is the expensive kind to debug, and it stops tests running at the same time.

## Not chosen

- **Shared long-lived mutable test state**: Increases flakiness and debugging cost, and rules out a parallel suite.
