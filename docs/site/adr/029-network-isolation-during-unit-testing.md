---
search:
  boost: 0.3
---

# ADR-029: Network Isolation during Unit Testing

Unit tests reach nothing beyond the machine. `network.SafeTransport`, the round tripper bb's HTTP clients are built on, refuses any host but localhost, 127.0.0.1 and ::1 while BB_BLOCK_EXTERNAL_NETWORK is 1, and the error names the host it refused. The seal a unit test package installs (ADR-082) sets it, so a test that reaches out fails at once.

Build every HTTP client on `network.NewSafeTransport`, so the block covers it. A test that needs a server uses a loopback listener, within what ADR-079 allows. A test that needs a connection to fail, or must show that none was made, points at `testsupport.RefusedURL`, a loopback port no listener is ever given.

An unintended network call makes a suite slow and flaky, fails on an isolated CI runner, and lets a command under test act on the world, such as installing a downloaded release over the test binary.

## Not chosen

- **Depend on manual environment configuration**: Error-prone, and it misses a new test that leaks a network call.
- **Use a general mock library only**: Gives no project-wide safety net for a direct http.Client.
