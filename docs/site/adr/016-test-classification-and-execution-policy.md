---
search:
  boost: 0.3
---

# ADR-016: Test classification and execution policy

Tests are sorted by what they need. A unit test needs nothing beyond the machine it runs on: `task test:unit` runs the unit tests, the pre-commit hook runs that task, and CI runs it on Linux, Windows and macOS. A test that needs more sits behind a build tag and a task of its own: `live` for a real Bitbucket (tests/integration/live, `task test:live`), `views` for headless Chrome (`task test:views`), and `e2e` for bb updating itself from a published release (tests/e2e). Without a tag, `go test` needs no Bitbucket and no browser.

Tests run on every operating system. A test decides by the capability it needs, by asking the file system or looking for the tool, and asserts on every system rather than skipping one by name. A test that cannot reach its dependency fails and says what is missing; it does not skip. The live suite exits before running anything when it has no credentials or no git, and the view tests fail without Chrome.

Put a test that needs an outside dependency behind a tag and a task, so the fast suite stays fast and needs no setup. Do not skip on a missing dependency, and do not write a test that runs on one operating system only.

A skipped test reads as a passing one: a run that skipped everything reports success having proven nothing, and a skip on one system is coverage that never ran there. bb ships for all three systems, and what differs between them, such as paths and shells, is exactly what a test confined to one never sees.

## Not chosen

- **Skip when a dependency is missing**: The run stays green on a machine that cannot run the test, so nobody learns that it did not.
- **Live and unit tests in one suite**: Every commit would wait for a Bitbucket instance, and a contributor without one could not run the tests at all.
