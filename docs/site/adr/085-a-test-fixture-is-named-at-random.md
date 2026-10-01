---
search:
  boost: 0.3
---

# ADR-085: A live test seeds and owns its fixtures, and names them at random

Every live test seeds the state it needs and owns it. The harness in tests/integration/live gives a test a project of its own (`seedRepo`, `seedIsolatedProject`), with repositories filled by real git pushes, and deletes it in `t.Cleanup`, repositories first, because Bitbucket refuses to delete a project that still holds any. No test reads another's fixtures or depends on the order tests run in, so the suite runs in parallel, as many at once as `LIVE_PARALLEL` in Taskfile.yml says. A test that asserts on an instance-wide listing looks for its own fixtures by their unique names. A cleanup that cannot finish logs and leaves the project behind rather than fail a test that passed; `task stack:purge-fixtures` removes the `LT` projects that accumulate, and `task stack:reset` starts the instance over.

A fixture's name is a readable prefix, such as the `LT` of a project key, and a unique part from testsupport.UniqueSuffix or testsupport.UniqueName, which draw from crypto/rand. Not a timestamp, not a timestamp with a counter beside it, and not a timestamp cut down to fit. The prefix traces a fixture left behind to the test that made it.

When a test needs new kinds of state, seed it in the test or extend the harness. Do not seed shared state once for the suite, and do not rely on what an earlier test left behind. Name anything a test creates that the server requires to be unique with testsupport.UniqueSuffix or testsupport.UniqueName, and upper-case a value Bitbucket stores upper-cased, such as a project key. TestNoFixtureIsNamedFromTheClock fails a test file that builds a string from time.Now() by formatting, joining or concatenation, directly or through a local variable. A clock value that is not a name, such as a query window, carries a `clock-value-not-a-name:` comment giving the reason.

Shared mutable state makes a failure depend on what ran before it, which is the expensive kind to debug, and it stops tests running at the same time. Clock-derived names collide in three ways, and each looks like a product bug. Truncated, they repeat within a run. The clock is coarser than the suite is parallel, so two tests read the same value. And a counter beside the clock restarts with the process, so a run collides with the fixtures a crashed run left behind. Randomness removes all three, and the reasoning about them.

## Not chosen

- **Clean the instance before every run**: It depends on a teardown that a crash is exactly what prevents, and does nothing about two tests colliding inside one run.
